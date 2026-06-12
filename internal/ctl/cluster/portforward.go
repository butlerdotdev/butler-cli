/*
Copyright 2026 The Butler Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package cluster

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/butlerdotdev/butler/internal/common/client"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

// localAccessNeeded reports whether local-provider tenant clusters need a
// host-routable kubeconfig on this OS. On macOS and Windows the kind/CAPD docker
// bridge IPs are not routable from the host, so we tunnel via a port-forward. On
// Linux the bridge IP is reachable directly, so the stored kubeconfig works as-is.
func localAccessNeeded() bool {
	return runtime.GOOS == "darwin" || runtime.GOOS == "windows"
}

// isLocalTenant reports whether the tenant cluster is backed by the local
// provider. Detection failures degrade to false (emit the kubeconfig verbatim).
func isLocalTenant(ctx context.Context, c *client.Client, tc *unstructured.Unstructured) bool {
	name := client.GetNestedString(tc.Object, "spec", "providerConfigRef", "name")
	if name == "" {
		return false
	}
	ns := client.GetNestedString(tc.Object, "spec", "providerConfigRef", "namespace")
	if ns == "" {
		// Platform-scoped ProviderConfigs (including the local one created by
		// `butleradm bootstrap local`) live in butler-system.
		ns = "butler-system"
	}
	pc, err := c.GetProviderConfig(ctx, ns, name)
	if err != nil {
		return false
	}
	return client.GetNestedString(pc.Object, "spec", "provider") == "local"
}

// providerIsLocal reports whether the named ProviderConfig (in butler-system) is
// the local provider. Detection failures degrade to false.
func providerIsLocal(ctx context.Context, c *client.Client, name string) bool {
	pc, err := c.GetProviderConfig(ctx, ButlerSystemNamespace, name)
	if err != nil {
		return false
	}
	return client.GetNestedString(pc.Object, "spec", "provider") == "local"
}

// deriveLocalLBPool returns a LoadBalancer IP range high in the kind docker
// network's IPv4 subnet, matching what `butleradm bootstrap local` configures for
// MetalLB. This lets `cluster create` work for local clusters without a
// machine-specific --lb-pool flag.
func deriveLocalLBPool(ctx context.Context) (start, end string, err error) {
	out, err := exec.CommandContext(ctx, "docker", "network", "inspect", "kind",
		"-f", "{{range .IPAM.Config}}{{.Subnet}} {{end}}").Output()
	if err != nil {
		return "", "", fmt.Errorf("inspecting kind docker network: %w", err)
	}
	for _, field := range strings.Fields(string(out)) {
		ip, _, perr := net.ParseCIDR(field)
		if perr != nil {
			continue
		}
		ip4 := ip.To4()
		if ip4 == nil {
			continue // skip IPv6 subnets
		}
		return fmt.Sprintf("%d.%d.255.200", ip4[0], ip4[1]),
			fmt.Sprintf("%d.%d.255.250", ip4[0], ip4[1]), nil
	}
	return "", "", fmt.Errorf("no IPv4 subnet found on the kind docker network")
}

// rewriteKubeconfigForLocalAccess rewrites the tenant kubeconfig so its API server
// endpoint is reachable from the host. It ensures a background port-forward to the
// tenant API server service is running and points the kubeconfig at 127.0.0.1, while
// preserving the original host as tls-server-name so the server certificate (whose
// SANs include the docker-bridge IP) still verifies.
func rewriteKubeconfigForLocalAccess(clusterName, tenantNS, mgmtKubeconfigPath, mgmtContext string, kubeconfigData []byte) ([]byte, error) {
	cfg, err := clientcmd.Load(kubeconfigData)
	if err != nil {
		return nil, fmt.Errorf("parsing tenant kubeconfig: %w", err)
	}

	// The Steward Service fronting the tenant API server is named after the cluster.
	port, err := ensureLocalForward(clusterName, tenantNS, clusterName, mgmtKubeconfigPath, mgmtContext)
	if err != nil {
		return nil, err
	}

	pointClustersAtLocalPort(cfg, port)

	out, err := clientcmd.Write(*cfg)
	if err != nil {
		return nil, fmt.Errorf("serializing kubeconfig: %w", err)
	}
	return out, nil
}

// pointClustersAtLocalPort rewrites every cluster entry's server to
// 127.0.0.1:<port>, preserving the original host as tls-server-name so the server
// certificate (whose SANs include the original docker-bridge IP) still verifies.
func pointClustersAtLocalPort(cfg *clientcmdapi.Config, port int) {
	for _, cl := range cfg.Clusters {
		if cl == nil {
			continue
		}
		if u, perr := url.Parse(cl.Server); perr == nil && u.Hostname() != "" {
			cl.TLSServerName = u.Hostname()
		}
		cl.Server = fmt.Sprintf("https://127.0.0.1:%d", port)
	}
}

type forwardState struct {
	PID       int    `json:"pid"`
	Port      int    `json:"port"`
	Namespace string `json:"namespace"`
	Service   string `json:"service"`
}

// ensureLocalForward returns a local port that forwards to the tenant API server
// service on the management cluster, reusing a healthy background forward if one is
// already recorded. The forward is a detached `kubectl port-forward` (kubectl is a
// prerequisite for using any tenant cluster) that survives this process exiting.
func ensureLocalForward(clusterName, tenantNS, service, mgmtKubeconfigPath, mgmtContext string) (int, error) {
	statePath, err := forwardStatePath(clusterName)
	if err != nil {
		return 0, err
	}

	if st, ok := readForwardState(statePath); ok && st.Namespace == tenantNS &&
		pidAlive(st.PID) && portOpen(st.Port, time.Second) {
		return st.Port, nil
	}

	port, err := freePort()
	if err != nil {
		return 0, fmt.Errorf("finding a free local port: %w", err)
	}

	args := []string{"port-forward"}
	if mgmtKubeconfigPath != "" {
		args = append(args, "--kubeconfig", mgmtKubeconfigPath)
	}
	if mgmtContext != "" {
		args = append(args, "--context", mgmtContext)
	}
	args = append(args, "-n", tenantNS, "svc/"+service, fmt.Sprintf("%d:6443", port), "--address", "127.0.0.1")

	dir, _ := forwardDir()
	logPath := filepath.Join(dir, clusterName+".log")
	logFile, _ := os.OpenFile(logPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)

	cmd := exec.Command("kubectl", args...)
	if logFile != nil {
		cmd.Stdout = logFile
		cmd.Stderr = logFile
	}
	// Detach into a new session so the forward survives the CLI exiting; the
	// follow-up `kubectl get nodes` runs as a separate process.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return 0, fmt.Errorf("starting tunnel (kubectl is required to use local clusters): %w", err)
	}
	pid := cmd.Process.Pid
	_ = cmd.Process.Release()

	_ = writeForwardState(statePath, forwardState{PID: pid, Port: port, Namespace: tenantNS, Service: service})

	if err := waitForPort(port, 15*time.Second); err != nil {
		return 0, fmt.Errorf("local cluster tunnel did not become ready: %w%s", err, logTail(logPath))
	}

	// Note to stderr only (stdout may be the piped kubeconfig).
	fmt.Fprintf(os.Stderr, "Local cluster reachable via a background tunnel on 127.0.0.1:%d (pid %d).\n", port, pid)
	return port, nil
}

func forwardDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, ".butler", "forwards")
	return dir, os.MkdirAll(dir, 0o700)
}

func forwardStatePath(cluster string) (string, error) {
	dir, err := forwardDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, cluster+".json"), nil
}

func readForwardState(path string) (forwardState, bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return forwardState{}, false
	}
	var st forwardState
	if json.Unmarshal(b, &st) != nil {
		return forwardState{}, false
	}
	return st, true
}

func writeForwardState(path string, st forwardState) error {
	b, err := json.Marshal(st)
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o600)
}

func pidAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return p.Signal(syscall.Signal(0)) == nil
}

func freePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

func portOpen(port int, timeout time.Duration) bool {
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), timeout)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

func waitForPort(port int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if portOpen(port, time.Second) {
			return nil
		}
		time.Sleep(300 * time.Millisecond)
	}
	return fmt.Errorf("timed out waiting for 127.0.0.1:%d to accept connections", port)
}

func logTail(path string) string {
	b, err := os.ReadFile(path)
	if err != nil || len(b) == 0 {
		return ""
	}
	s := string(b)
	if len(s) > 400 {
		s = s[len(s)-400:]
	}
	return "\nport-forward log:\n" + s
}
