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

// Package orchestrator implements the bootstrap orchestration logic.
package orchestrator

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/butlerdotdev/butler/internal/adm/bootstrap/manifests"
	"github.com/butlerdotdev/butler/internal/common/log"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/kind/pkg/cluster"
	"sigs.k8s.io/yaml"
)

const (
	// Namespace for Butler resources in KIND cluster
	butlerNamespace = "butler-system"

	// KIND cluster name
	kindClusterName = "butler-bootstrap"

	// API Group for Butler CRDs
	butlerAPIGroup   = "butler.butlerlabs.dev"
	butlerAPIVersion = "v1alpha1"

	// Environment variable for custom CA certificate path
	envCACertPath = "BUTLER_CA_CERT_PATH"

	// Default directory for CA certificates
	defaultCACertDir = ".butler/certificates"
)

// GVR definitions for Butler CRDs
var (
	clusterBootstrapGVR = schema.GroupVersionResource{
		Group:    butlerAPIGroup,
		Version:  butlerAPIVersion,
		Resource: "clusterbootstraps",
	}
	providerConfigGVR = schema.GroupVersionResource{
		Group:    butlerAPIGroup,
		Version:  butlerAPIVersion,
		Resource: "providerconfigs",
	}
	networkPoolGVR = schema.GroupVersionResource{
		Group:    butlerAPIGroup,
		Version:  butlerAPIVersion,
		Resource: "networkpools",
	}
	butlerConfigGVR = schema.GroupVersionResource{
		Group:    butlerAPIGroup,
		Version:  butlerAPIVersion,
		Resource: "butlerconfigs",
	}
)

// Options configures the orchestrator
type Options struct {
	// DryRun shows what would be created without executing
	DryRun bool

	// SkipCleanup prevents KIND cluster deletion on failure
	SkipCleanup bool

	// Timeout is the maximum time to wait for bootstrap
	Timeout time.Duration

	// LocalDev enables local development mode - builds images from source
	LocalDev bool

	// RepoRoot is the path to butlerdotdev repos (for LocalDev mode)
	RepoRoot string
}

// Orchestrator manages the bootstrap process
type Orchestrator struct {
	logger    *log.Logger
	options   Options
	eventSink EventSink

	// isLocal is true for the local provider, which installs Butler onto the KIND
	// cluster itself (no provisioned cluster, no pivot, no teardown).
	isLocal bool
	// kindKubeconfigPath is the path to the KIND cluster's kubeconfig. For the local
	// provider this kubeconfig is also the management cluster kubeconfig.
	kindKubeconfigPath string
}

// New creates a new orchestrator
func New(logger *log.Logger, options Options) *Orchestrator {
	return &Orchestrator{
		logger:  logger,
		options: options,
	}
}

// SetEventSink sets an optional event sink for TUI integration. Events are
// emitted at phase boundaries and during watchBootstrap status polling. The
// sink implementation must be safe for concurrent use.
func (o *Orchestrator) SetEventSink(sink EventSink) {
	o.eventSink = sink
}

// emit sends an event to the configured sink, if any.
func (o *Orchestrator) emit(e Event) {
	if o.eventSink != nil {
		o.eventSink.Send(e)
	}
}

// clusterCredentials holds the kubeconfig and talosconfig for a cluster
type clusterCredentials struct {
	kubeconfig      []byte
	talosconfig     []byte
	controlPlaneIPs []string
	consoleURL      string
}

// Run executes the bootstrap process
func (o *Orchestrator) Run(ctx context.Context, cfg *Config) error {
	if o.options.DryRun {
		return o.dryRun(cfg)
	}

	o.logger.Phase("Initializing bootstrap")
	o.emit(Event{Type: EventPhaseChange, Phase: "Initializing", Message: "Initializing bootstrap"})

	o.isLocal = cfg.Provider == "local"

	// Create context with timeout
	ctx, cancel := context.WithTimeout(ctx, o.options.Timeout)
	defer cancel()

	// Phase 1: Create KIND cluster
	o.logger.Phase("Creating temporary KIND cluster")
	o.emit(Event{Type: EventPhaseChange, Phase: "CreatingKIND", Message: "Creating temporary KIND cluster"})
	kindProvider := cluster.NewProvider()

	kubeconfigPath, err := o.createKINDCluster(ctx, kindProvider)
	if err != nil {
		return fmt.Errorf("creating KIND cluster: %w", err)
	}
	o.kindKubeconfigPath = kubeconfigPath

	// The local provider's MetalLB pool must live in the kind docker network so CAPD
	// worker containers (on the same network) can reach tenant LoadBalancer IPs. Derive
	// it now that the network exists.
	if o.isLocal && cfg.Network.LoadBalancerPool == nil {
		if pool, derr := deriveKindLBPool(ctx); derr != nil {
			o.logger.Warn("Failed to derive MetalLB pool from kind network, using default", "error", derr)
			cfg.Network.LoadBalancerPool = &LBPoolConfig{Start: "172.18.255.200", End: "172.18.255.250"}
		} else {
			o.logger.Info("Derived MetalLB pool from kind docker network", "start", pool.Start, "end", pool.End)
			cfg.Network.LoadBalancerPool = pool
		}
	}
	// Tell the TUI where the KIND kubeconfig lives so it can start streaming
	// butler-bootstrap-controller and butler-provider-* pod logs into the
	// debug panel.
	o.emit(Event{
		Type:           EventKINDReady,
		Phase:          "KINDReady",
		Message:        "KIND cluster ready",
		KINDKubeconfig: kubeconfigPath,
	})
	defer func() {
		// The local provider keeps the KIND cluster: it is the management cluster,
		// not throwaway scaffolding, so the demo needs it to persist.
		if !o.options.SkipCleanup && !o.isLocal {
			o.logger.Phase("Cleaning up KIND cluster")
			o.emit(Event{Type: EventPhaseChange, Phase: "CleaningUp", Message: "Cleaning up KIND cluster"})
			if err := kindProvider.Delete(kindClusterName, ""); err != nil {
				o.logger.Error("failed to delete KIND cluster", "error", err)
			}
		}
	}()

	// Inject host aliases for corporate DNS resolution (must be after KIND cluster creation).
	// Start with any explicitly configured aliases, then auto-resolve the
	// provider endpoint from the host. Docker Desktop's DNS proxy often
	// can't resolve hostnames that require VPN/ZTNA split-DNS, so we
	// resolve on the Mac and inject the result into the KIND node.
	hostAliases := o.getHostAliases(cfg)
	if auto := o.autoResolveProviderEndpoint(cfg); auto != "" {
		hostAliases = append(hostAliases, auto)
	}
	if len(hostAliases) > 0 {
		if err := o.injectHostAliases(ctx, hostAliases); err != nil {
			o.logger.Warn("Failed to inject host aliases", "error", err)
		}
		// Re-patch CoreDNS with host overrides so cluster DNS (used by
		// the provider controller with ClusterFirstWithHostNet) returns
		// the correct IPs for VPN/ZTNA-only hostnames.
		if err := o.patchCoreDNS(kubeconfigPath, hostAliases...); err != nil {
			o.logger.Warn("Failed to re-patch CoreDNS with host overrides", "error", err)
		}
	}

	// Build and load images in local dev mode
	if o.options.LocalDev {
		o.logger.Phase("Building and loading controller images (local dev mode)")
		o.emit(Event{Type: EventPhaseChange, Phase: "BuildingImages", Message: "Building and loading controller images"})
		if err := o.buildAndLoadImages(ctx, cfg.Provider); err != nil {
			return fmt.Errorf("building/loading images: %w", err)
		}
	}

	// Create Kubernetes clients
	o.logger.Phase("Connecting to KIND cluster")
	o.emit(Event{Type: EventPhaseChange, Phase: "ConnectingKIND", Message: "Connecting to KIND cluster"})
	clientset, dynamicClient, err := o.createClients(kubeconfigPath)
	if err != nil {
		return fmt.Errorf("creating clients: %w", err)
	}

	// Deploy Butler CRDs
	o.logger.Phase("Deploying Butler CRDs")
	o.emit(Event{Type: EventPhaseChange, Phase: "DeployingCRDs", Message: "Deploying Butler CRDs"})
	if err := o.deployCRDs(ctx, clientset, dynamicClient); err != nil {
		return fmt.Errorf("deploying CRDs: %w", err)
	}

	// Create namespace and provider secret
	o.logger.Phase("Creating namespace and secrets")
	o.emit(Event{Type: EventPhaseChange, Phase: "CreatingSecrets", Message: "Creating namespace and secrets"})
	if err := o.createNamespaceAndSecrets(ctx, clientset, cfg); err != nil {
		return fmt.Errorf("creating namespace/secrets: %w", err)
	}

	// Deploy controllers
	o.logger.Phase("Deploying Butler controllers")
	o.emit(Event{Type: EventPhaseChange, Phase: "DeployingControllers", Message: "Deploying Butler controllers"})
	if err := o.deployControllers(ctx, clientset, dynamicClient, cfg); err != nil {
		return fmt.Errorf("deploying controllers: %w", err)
	}

	// Create NetworkPool CR for management cluster IPAM (optional — only if config
	// requested it). Runs before ProviderConfig so the ProviderConfig's
	// poolRefs resolve immediately.
	if cfg.NetworkPool != nil {
		o.logger.Phase("Creating NetworkPool for management cluster IPAM")
		o.emit(Event{Type: EventPhaseChange, Phase: "CreatingNetworkPool", Message: "Creating NetworkPool"})
		if err := o.createNetworkPool(ctx, dynamicClient, cfg); err != nil {
			return fmt.Errorf("creating NetworkPool: %w", err)
		}
	}

	// Create ProviderConfig CR
	o.logger.Phase("Creating ProviderConfig")
	o.emit(Event{Type: EventPhaseChange, Phase: "CreatingProviderConfig", Message: "Creating ProviderConfig"})
	if err := o.createProviderConfig(ctx, dynamicClient, cfg); err != nil {
		return fmt.Errorf("creating ProviderConfig: %w", err)
	}

	// For cloud HA: the bootstrap controller creates a LoadBalancerRequest CRD
	// during the ProvisioningMachines phase. The provider controller reconciles
	// it to provision a cloud-native LB. The LB endpoint is used as the control
	// plane API server address. No CLI-side LB creation is needed.
	if isCloudProvider(cfg.Provider) && cfg.Cluster.Topology == "ha" {
		o.logger.Info("Cloud HA: load balancer will be provisioned via LoadBalancerRequest CRD")
	}

	// Create ClusterBootstrap CR
	o.logger.Phase("Creating ClusterBootstrap")
	o.emit(Event{Type: EventPhaseChange, Phase: "CreatingBootstrap", Message: "Creating ClusterBootstrap"})
	if err := o.createClusterBootstrap(ctx, dynamicClient, cfg); err != nil {
		return fmt.Errorf("creating ClusterBootstrap: %w", err)
	}

	// Watch for completion
	o.logger.Phase("Waiting for cluster bootstrap")
	o.emit(Event{Type: EventPhaseChange, Phase: "WatchingBootstrap", Message: "Waiting for cluster bootstrap"})
	creds, err := o.watchBootstrap(ctx, dynamicClient, cfg)
	if err != nil {
		return fmt.Errorf("watching bootstrap: %w", err)
	}

	// Save cluster credentials
	o.logger.Phase("Saving cluster credentials")
	o.emit(Event{Type: EventPhaseChange, Phase: "SavingCredentials", Message: "Saving cluster credentials"})
	if err := o.saveClusterCredentials(cfg.Cluster.Name, creds); err != nil {
		return fmt.Errorf("saving cluster credentials: %w", err)
	}

	// Post-bootstrap management cluster configuration: apply settings that
	// the bootstrap controller doesn't handle from ClusterBootstrap.spec
	// (IPAM, multi-tenancy mode). Uses the freshly-saved kubeconfig.
	// Retries up to 3 times with 10s backoff — the management cluster's
	// API server may not be reachable immediately after bootstrap if
	// kube-vip hasn't converged or the node is still starting.
	if cfg.NetworkPool != nil || cfg.ProviderNetwork != nil || cfg.MultiTenancyMode != "" {
		o.logger.Phase("Configuring management cluster")
		o.emit(Event{Type: EventPhaseChange, Phase: "ConfiguringManagement", Message: "Configuring management cluster"})

		home, _ := os.UserHomeDir()
		mgmtKubeconfig := filepath.Join(home, ".butler", cfg.Cluster.Name+"-kubeconfig")

		var lastErr error
		for attempt := 1; attempt <= 3; attempt++ {
			lastErr = o.configureManagementCluster(ctx, cfg, mgmtKubeconfig)
			if lastErr == nil {
				break
			}
			if attempt < 3 {
				o.logger.Warn("Management cluster configuration failed, retrying...", "attempt", attempt, "error", lastErr)
				select {
				case <-ctx.Done():
					lastErr = ctx.Err()
					break
				case <-time.After(10 * time.Second):
				}
			}
		}
		if lastErr != nil {
			o.logger.Warn("Failed to configure management cluster after 3 attempts", "error", lastErr)
			o.logger.Info("You can configure settings manually: kubectl edit butlerconfig butler / kubectl edit providerconfig")
		}
	}

	o.logger.Success("Bootstrap complete!")
	o.emit(Event{Type: EventSuccess, Phase: "Complete", Message: "Bootstrap complete"})
	o.logger.Info("")
	o.logger.Info("Cluster credentials saved to:")
	o.logger.Info("  Kubeconfig:   ~/.butler/" + cfg.Cluster.Name + "-kubeconfig")
	o.logger.Info("  Talosconfig:  ~/.butler/" + cfg.Cluster.Name + "-talosconfig")
	o.logger.Info("")

	if creds.consoleURL != "" {
		o.logger.Info("Butler Console:")
		if strings.HasPrefix(creds.consoleURL, "kubectl") {
			o.logger.Info("  Access via: " + creds.consoleURL)
		} else {
			o.logger.Info("  URL: " + creds.consoleURL)
		}
		o.logger.Info("  Username: admin")
		o.logger.Info("  Password: Run the following command to retrieve:")
		o.logger.Info("    kubectl get secret butler-console-admin -n butler-system -o jsonpath='{.data.admin-password}' | base64 -d && echo")
		o.logger.Info("")
	}

	o.logger.Info("Usage:")
	o.logger.Info("  export KUBECONFIG=~/.butler/" + cfg.Cluster.Name + "-kubeconfig")
	o.logger.Info("  export TALOSCONFIG=~/.butler/" + cfg.Cluster.Name + "-talosconfig")
	o.logger.Info("")
	o.logger.Info("  kubectl get nodes")
	o.logger.Info("  talosctl health --nodes <CONTROL_PLANE_IP>")

	return nil
}

// dryRun shows what would be created
func (o *Orchestrator) dryRun(cfg *Config) error {
	o.logger.Info("DRY RUN - showing what would be created")

	// Show topology information
	fmt.Println("\n--- Cluster Topology ---")
	fmt.Printf("Topology: %s\n", cfg.Cluster.Topology)
	if cfg.IsSingleNode() {
		fmt.Printf("Mode: Single control plane node running workloads (no workers)\n")
		fmt.Printf("Note: Control plane replicas forced to 1, workers ignored\n")
	} else {
		fmt.Printf("Mode: HA with separate control plane and workers\n")
	}

	// Show ProviderConfig
	pc := o.buildProviderConfigUnstructured(cfg)
	pcYAML, _ := yaml.Marshal(pc.Object)
	fmt.Println("\n--- ProviderConfig ---")
	fmt.Println(string(pcYAML))

	// Show ClusterBootstrap
	cb := o.buildClusterBootstrapUnstructured(cfg)
	cbYAML, _ := yaml.Marshal(cb.Object)
	fmt.Println("\n--- ClusterBootstrap ---")
	fmt.Println(string(cbYAML))

	// Show MachineRequests that would be created (topology-aware)
	fmt.Println("\n--- MachineRequests (created by controller) ---")
	for i := int32(0); i < cfg.Cluster.ControlPlane.Replicas; i++ {
		fmt.Printf("- %s-cp-%d (control-plane, %d CPU, %d MB RAM)\n",
			cfg.Cluster.Name, i, cfg.Cluster.ControlPlane.CPU, cfg.Cluster.ControlPlane.MemoryMB)
	}
	// Only show workers for non-single-node topologies
	if !cfg.IsSingleNode() {
		for i := int32(0); i < cfg.Cluster.Workers.Replicas; i++ {
			fmt.Printf("- %s-worker-%d (worker, %d CPU, %d MB RAM)\n",
				cfg.Cluster.Name, i, cfg.Cluster.Workers.CPU, cfg.Cluster.Workers.MemoryMB)
		}
	} else {
		fmt.Println("(no workers - single-node topology)")
	}

	// Show CA certificates that would be injected
	caCerts := o.findCACertificates()
	if len(caCerts) > 0 {
		fmt.Println("\n--- CA Certificates (will be injected into KIND) ---")
		for _, cert := range caCerts {
			fmt.Printf("- %s\n", cert)
		}
	}

	// Show host aliases that would be injected
	hostAliases := o.getHostAliases(cfg)
	if len(hostAliases) > 0 {
		fmt.Println("\n--- Host Aliases (will be injected into KIND /etc/hosts) ---")
		for _, alias := range hostAliases {
			fmt.Printf("- %s\n", alias)
		}
	}

	// Show console configuration
	if cfg.Addons.Console.Enabled {
		fmt.Println("\n--- Butler Console ---")
		fmt.Printf("Version: %s\n", cfg.Addons.Console.Version)
		if cfg.Addons.Console.Ingress.Enabled {
			scheme := "http"
			if cfg.Addons.Console.Ingress.TLS {
				scheme = "https"
			}
			fmt.Printf("URL: %s://%s\n", scheme, cfg.Addons.Console.Ingress.Host)
			if cfg.Addons.Console.Ingress.ClassName != "" {
				fmt.Printf("Ingress Class: %s\n", cfg.Addons.Console.Ingress.ClassName)
			}
		} else {
			fmt.Println("Access: via port-forward (no ingress configured)")
		}
	}

	return nil
}

// findCACertificates discovers CA certificates from standard locations.
// Priority order:
// 1. BUTLER_CA_CERT_PATH environment variable (single file or directory)
// 2. ~/.butler/certificates/ directory (all .crt and .pem files)
func (o *Orchestrator) findCACertificates() []string {
	var certs []string

	// Check environment variable first
	if envPath := os.Getenv(envCACertPath); envPath != "" {
		info, err := os.Stat(envPath)
		if err == nil {
			if info.IsDir() {
				// It's a directory, scan for cert files
				dirCerts := o.scanCertDirectory(envPath)
				certs = append(certs, dirCerts...)
			} else {
				// It's a file
				certs = append(certs, envPath)
			}
		}
	}

	// Check default directory ~/.butler/certificates/
	home, err := os.UserHomeDir()
	if err == nil {
		certDir := filepath.Join(home, defaultCACertDir)
		if info, err := os.Stat(certDir); err == nil && info.IsDir() {
			dirCerts := o.scanCertDirectory(certDir)
			certs = append(certs, dirCerts...)
		}
	}

	return certs
}

// scanCertDirectory scans a directory for certificate files (.crt, .pem)
func (o *Orchestrator) scanCertDirectory(dir string) []string {
	var certs []string

	entries, err := os.ReadDir(dir)
	if err != nil {
		return certs
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasSuffix(name, ".crt") || strings.HasSuffix(name, ".pem") {
			certs = append(certs, filepath.Join(dir, name))
		}
	}

	return certs
}

// deriveKindLBPool inspects the kind docker network and returns a MetalLB pool high in
// its IPv4 subnet, so LoadBalancer IPs are reachable by CAPD worker containers on the
// same network.
func deriveKindLBPool(ctx context.Context) (*LBPoolConfig, error) {
	out, err := exec.CommandContext(ctx, "docker", "network", "inspect", "kind",
		"-f", "{{range .IPAM.Config}}{{.Subnet}} {{end}}").Output()
	if err != nil {
		return nil, fmt.Errorf("docker network inspect kind: %w", err)
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
		return &LBPoolConfig{
			Start: fmt.Sprintf("%d.%d.255.200", ip4[0], ip4[1]),
			End:   fmt.Sprintf("%d.%d.255.250", ip4[0], ip4[1]),
		}, nil
	}
	return nil, fmt.Errorf("no IPv4 subnet found on kind network")
}

// buildKINDConfig generates a KIND cluster configuration with CA certificate mounts.
// For the local provider it also mounts the host Docker socket so the in-cluster CAPD
// controller can create sibling node containers, and maps the console NodePort to the host.
func (o *Orchestrator) buildKINDConfig(caCerts []string) string {
	// Build extraMounts for each certificate.
	var mounts strings.Builder
	for i, certPath := range caCerts {
		containerPath := fmt.Sprintf("/usr/local/share/ca-certificates/butler-custom-%d.crt", i)
		mounts.WriteString(fmt.Sprintf(`      - hostPath: %s
        containerPath: %s
        readOnly: true
`, certPath, containerPath))
	}

	if o.isLocal {
		// Mount the host Docker socket so the CAPD (Cluster API Docker) controller
		// running inside this cluster can create sibling node containers. Without
		// this mount, DockerMachines never provision and the tenant hangs.
		mounts.WriteString(`      - hostPath: /var/run/docker.sock
        containerPath: /var/run/docker.sock
`)
	}

	var b strings.Builder
	b.WriteString(`kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
nodes:
  - role: control-plane
`)
	if mounts.Len() > 0 {
		b.WriteString("    extraMounts:\n")
		b.WriteString(mounts.String())
	}
	if o.isLocal {
		// Expose the Butler console NodePort on the host for the laptop demo.
		b.WriteString(`    extraPortMappings:
      - containerPort: 30080
        hostPort: 8080
        protocol: TCP
`)
	}
	return b.String()
}

// installCACertificates runs update-ca-certificates in the KIND node
func (o *Orchestrator) installCACertificates(ctx context.Context) error {
	o.logger.Info("Installing CA certificates in KIND node")

	// Run update-ca-certificates inside the KIND container
	cmd := exec.CommandContext(ctx, "docker", "exec",
		kindClusterName+"-control-plane",
		"update-ca-certificates")

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to update CA certificates: %w, output: %s", err, string(output))
	}

	o.logger.Success("CA certificates installed in KIND node")
	return nil
}

// getHostAliases returns host aliases from the provider config
func (o *Orchestrator) getHostAliases(cfg *Config) []string {
	switch cfg.Provider {
	case "nutanix":
		if cfg.ProviderConfig.Nutanix != nil {
			return cfg.ProviderConfig.Nutanix.HostAliases
		}
	case "proxmox":
		if cfg.ProviderConfig.Proxmox != nil {
			return cfg.ProviderConfig.Proxmox.HostAliases
		}
	}
	return nil
}

// autoResolveProviderEndpoint resolves the provider endpoint hostname using
// the host's DNS and returns a "/etc/hosts"-style entry (e.g. "1.2.3.4 host").
// Docker Desktop's DNS proxy often cannot resolve hostnames that require
// VPN/ZTNA split-DNS (e.g. Zscaler Private Access). Resolving on the host
// and injecting the result into KIND ensures the provider controller can
// reach the endpoint regardless of Docker's DNS limitations.
// Returns "" if the endpoint is already an IP or resolution fails.
func (o *Orchestrator) autoResolveProviderEndpoint(cfg *Config) string {
	var endpoint string
	switch cfg.Provider {
	case "nutanix":
		if cfg.ProviderConfig.Nutanix != nil {
			endpoint = cfg.ProviderConfig.Nutanix.Endpoint
		}
	case "proxmox":
		if cfg.ProviderConfig.Proxmox != nil {
			endpoint = cfg.ProviderConfig.Proxmox.Endpoint
		}
	default:
		return ""
	}
	if endpoint == "" {
		return ""
	}

	// Extract hostname from endpoint (may be a URL like https://host)
	host := endpoint
	if strings.Contains(host, "://") {
		if u, err := url.Parse(host); err == nil {
			host = u.Hostname()
		}
	}
	// Strip port if present
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}

	// If it's already an IP address, nothing to do
	if net.ParseIP(host) != nil {
		return ""
	}

	// Resolve using the host's DNS (which goes through VPN/ZTNA)
	ips, err := net.LookupHost(host)
	if err != nil || len(ips) == 0 {
		o.logger.Debug("Could not auto-resolve provider endpoint", "host", host, "error", err)
		return ""
	}

	alias := ips[0] + " " + host
	o.logger.Info("Auto-resolved provider endpoint for KIND /etc/hosts", "alias", alias)
	return alias
}

// injectHostAliases adds /etc/hosts entries to the KIND container
func (o *Orchestrator) injectHostAliases(ctx context.Context, hostAliases []string) error {
	if len(hostAliases) == 0 {
		return nil
	}

	o.logger.Info("Injecting host aliases into KIND node", "count", len(hostAliases))

	for _, alias := range hostAliases {
		cmd := exec.CommandContext(ctx, "docker", "exec",
			kindClusterName+"-control-plane",
			"sh", "-c", fmt.Sprintf("echo '%s' >> /etc/hosts", alias))

		if output, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("failed to inject host alias %q: %w, output: %s", alias, err, string(output))
		}
		o.logger.Debug("Injected host alias", "alias", alias)
	}

	o.logger.Success("Host aliases injected")
	return nil
}

// createKINDCluster creates a KIND cluster with the specified configuration
func (o *Orchestrator) createKINDCluster(ctx context.Context, provider *cluster.Provider) (string, error) {
	// Check if cluster already exists
	clusters, err := provider.List()
	if err != nil {
		return "", fmt.Errorf("listing clusters: %w", err)
	}
	for _, c := range clusters {
		if c == kindClusterName {
			o.logger.Warn("KIND cluster already exists, reusing")
			kubeconfigPath, err := o.getKINDKubeconfig(provider)
			if err != nil {
				return "", err
			}
			// Ensure CoreDNS is patched even for existing cluster
			o.patchCoreDNS(kubeconfigPath)
			return kubeconfigPath, nil
		}
	}

	// Discover CA certificates
	caCerts := o.findCACertificates()
	if len(caCerts) > 0 {
		o.logger.Info("Found CA certificates to inject", "count", len(caCerts))
		for _, cert := range caCerts {
			o.logger.Debug("CA certificate", "path", cert)
		}
	}

	// Build KIND config
	kindConfig := o.buildKINDConfig(caCerts)

	// Write KIND config to temp file
	configFile, err := os.CreateTemp("", "kind-config-*.yaml")
	if err != nil {
		return "", fmt.Errorf("creating temp config file: %w", err)
	}
	defer os.Remove(configFile.Name())

	if _, err := configFile.WriteString(kindConfig); err != nil {
		return "", fmt.Errorf("writing KIND config: %w", err)
	}
	configFile.Close()

	// Create cluster with config
	if err := provider.Create(kindClusterName, cluster.CreateWithConfigFile(configFile.Name())); err != nil {
		return "", fmt.Errorf("creating cluster: %w", err)
	}
	o.logger.Success("KIND cluster created")

	// Tune kernel parameters for controller-heavy workloads
	if err := o.tuneKINDNode(ctx); err != nil {
		o.logger.Warn("Failed to tune KIND node", "error", err)
	}

	// Install CA certificates if we mounted any
	if len(caCerts) > 0 {
		if err := o.installCACertificates(ctx); err != nil {
			o.logger.Warn("Failed to install CA certificates", "error", err)
			// Don't fail the bootstrap, just warn - user might not need them
		}
	}

	kubeconfigPath, err := o.getKINDKubeconfig(provider)
	if err != nil {
		return "", err
	}

	// Fix CoreDNS to use external DNS servers (required for helm repo access)
	if err := o.patchCoreDNS(kubeconfigPath); err != nil {
		o.logger.Warn("Failed to patch CoreDNS", "error", err)
	}

	return kubeconfigPath, nil
}

// tuneKINDNode adjusts kernel parameters inside the KIND node
// to handle controller-runtime's heavy use of inotify watches
func (o *Orchestrator) tuneKINDNode(ctx context.Context) error {
	nodeName := kindClusterName + "-control-plane"

	// Increase inotify instances (default 128 is too low for multiple controllers)
	cmd := exec.CommandContext(ctx, "docker", "exec", nodeName,
		"sysctl", "-w", "fs.inotify.max_user_instances=1024")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("setting inotify instances: %w, output: %s", err, string(output))
	}

	// Increase max watches
	cmd = exec.CommandContext(ctx, "docker", "exec", nodeName,
		"sysctl", "-w", "fs.inotify.max_user_watches=524288")
	if output, err := cmd.CombinedOutput(); err != nil {
		o.logger.Debug("failed to set inotify watches", "error", err, "output", string(output))
	}

	o.logger.Debug("Tuned KIND node kernel parameters")
	return nil
}

// patchCoreDNS configures CoreDNS to forward external queries to the KIND
// node's upstream DNS servers (read from /etc/resolv.conf inside the Docker
// container) with Google DNS as a fallback. This preserves resolution of
// corporate/internal hostnames (e.g., Nutanix Prism Central) while also
// ensuring public DNS works on platforms where Docker DNS is unreliable.
//
// hostOverrides are optional "/etc/hosts"-style entries ("IP hostname") that
// are added as a CoreDNS `hosts` block. This handles VPN/ZTNA split-DNS
// scenarios where Docker Desktop's DNS proxy cannot resolve internal names.
func (o *Orchestrator) patchCoreDNS(kubeconfigPath string, hostOverrides ...string) error {
	// Read the KIND node's /etc/resolv.conf to discover Docker-provided
	// upstream DNS servers. These chain to the host's resolver and can
	// resolve internal hostnames that Google DNS cannot.
	upstreams := o.getKINDUpstreamDNS()
	// Always include Google DNS as fallback for reliability on Mac.
	upstreams = append(upstreams, "8.8.8.8", "8.8.4.4")
	forwardList := strings.Join(upstreams, " ")

	// Build optional hosts block for VPN/ZTNA overrides
	var hostsBlock string
	if len(hostOverrides) > 0 {
		var entries []string
		for _, h := range hostOverrides {
			h = strings.TrimSpace(h)
			if h != "" {
				entries = append(entries, "       "+h)
			}
		}
		if len(entries) > 0 {
			hostsBlock = fmt.Sprintf(`    hosts {
%s
       fallthrough
    }
`, strings.Join(entries, "\n"))
		}
	}

	corefile := fmt.Sprintf(`.:53 {
    errors
    health {
       lameduck 5s
    }
    ready
    kubernetes cluster.local in-addr.arpa ip6.arpa {
       pods insecure
       fallthrough in-addr.arpa ip6.arpa
       ttl 30
    }
    prometheus :9153
%s    forward . %s {
       max_concurrent 1000
    }
    cache 30
    loop
    reload
    loadbalance
}
`, hostsBlock, forwardList)

	// Create the patch JSON
	patch := fmt.Sprintf(`{"data":{"Corefile":%q}}`, corefile)

	cmd := exec.Command("kubectl", "--kubeconfig", kubeconfigPath,
		"patch", "configmap", "coredns", "-n", "kube-system",
		"--type=merge", "-p", patch)

	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("patching CoreDNS: %w, output: %s", err, string(output))
	}

	// Restart CoreDNS to pick up new config
	cmd = exec.Command("kubectl", "--kubeconfig", kubeconfigPath,
		"rollout", "restart", "deployment/coredns", "-n", "kube-system")

	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("restarting CoreDNS: %w, output: %s", err, string(output))
	}

	// Wait for CoreDNS rollout to complete so DNS is fully available
	// before the provider controller tries to resolve external hostnames.
	cmd = exec.Command("kubectl", "--kubeconfig", kubeconfigPath,
		"rollout", "status", "deployment/coredns", "-n", "kube-system",
		"--timeout=60s")

	if output, err := cmd.CombinedOutput(); err != nil {
		o.logger.Warn("CoreDNS rollout status wait failed", "error", err, "output", string(output))
	}

	o.logger.Debug("CoreDNS patched and ready", "upstreams", forwardList)
	return nil
}

// getKINDUpstreamDNS reads nameservers from the KIND node's /etc/resolv.conf.
// These are the Docker-provided DNS servers that chain to the host resolver
// and can resolve corporate/internal hostnames.
func (o *Orchestrator) getKINDUpstreamDNS() []string {
	cmd := exec.Command("docker", "exec", kindClusterName+"-control-plane",
		"cat", "/etc/resolv.conf")
	output, err := cmd.Output()
	if err != nil {
		o.logger.Debug("Could not read KIND resolv.conf, using Google DNS only", "error", err)
		return nil
	}

	var servers []string
	for _, line := range strings.Split(string(output), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "nameserver ") {
			ns := strings.TrimSpace(strings.TrimPrefix(line, "nameserver"))
			// Skip loopback and Kubernetes cluster DNS (would cause a loop).
			if ns != "" && ns != "127.0.0.1" && ns != "::1" && !strings.HasPrefix(ns, "10.96.") {
				servers = append(servers, ns)
			}
		}
	}

	if len(servers) > 0 {
		o.logger.Debug("Discovered KIND upstream DNS servers", "servers", servers)
	}
	return servers
}

// getKINDKubeconfig retrieves the kubeconfig for the KIND cluster
func (o *Orchestrator) getKINDKubeconfig(provider *cluster.Provider) (string, error) {
	kubeconfig, err := provider.KubeConfig(kindClusterName, false)
	if err != nil {
		return "", fmt.Errorf("getting kubeconfig: %w", err)
	}

	// Write to temp file
	kubeconfigPath := "/tmp/kind-kubeconfig"
	if err := os.WriteFile(kubeconfigPath, []byte(kubeconfig), 0600); err != nil {
		return "", fmt.Errorf("writing kubeconfig: %w", err)
	}

	return kubeconfigPath, nil
}

// createClients creates Kubernetes clients for the KIND cluster
func (o *Orchestrator) createClients(kubeconfigPath string) (*kubernetes.Clientset, dynamic.Interface, error) {
	config, err := clientcmd.BuildConfigFromFlags("", kubeconfigPath)
	if err != nil {
		return nil, nil, fmt.Errorf("building config: %w", err)
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, nil, fmt.Errorf("creating clientset: %w", err)
	}

	dynamicClient, err := dynamic.NewForConfig(config)
	if err != nil {
		return nil, nil, fmt.Errorf("creating dynamic client: %w", err)
	}

	return clientset, dynamicClient, nil
}

// deployCRDs deploys Butler CRDs to the KIND cluster
func (o *Orchestrator) deployCRDs(ctx context.Context, clientset *kubernetes.Clientset, dynamicClient dynamic.Interface) error {
	deployer := manifests.NewDeployer(clientset, dynamicClient)

	o.logger.Debug("deploying Butler CRDs from embedded manifests")
	if err := deployer.DeployCRDs(ctx); err != nil {
		return fmt.Errorf("deploying CRDs: %w", err)
	}

	// Wait for CRDs to be established
	o.logger.Debug("waiting for CRDs to be established")
	crdNames := []string{
		"butlerconfigs.butler.butlerlabs.dev",
		"machinerequests.butler.butlerlabs.dev",
		"providerconfigs.butler.butlerlabs.dev",
		"clusterbootstraps.butler.butlerlabs.dev",
		"loadbalancerrequests.butler.butlerlabs.dev",
	}

	// Create a timeout context for waiting
	waitCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	if err := deployer.WaitForCRDs(waitCtx, crdNames); err != nil {
		return fmt.Errorf("waiting for CRDs: %w", err)
	}

	o.logger.Success("CRDs deployed and established")
	return nil
}

// createNamespaceAndSecrets creates the Butler namespace and provider credentials secrets
func (o *Orchestrator) createNamespaceAndSecrets(ctx context.Context, clientset *kubernetes.Clientset, cfg *Config) error {
	// Create namespace
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: butlerNamespace,
		},
	}
	_, err := clientset.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{})
	if err != nil && !strings.Contains(err.Error(), "already exists") {
		return fmt.Errorf("creating namespace: %w", err)
	}

	// Create provider credentials secret based on provider type
	switch cfg.Provider {
	case "harvester":
		// Read kubeconfig file for Harvester
		kubeconfigData, err := os.ReadFile(cfg.ProviderConfig.Harvester.KubeconfigPath)
		if err != nil {
			return fmt.Errorf("reading Harvester kubeconfig: %w", err)
		}

		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      cfg.Cluster.Name + "-harvester-credentials",
				Namespace: butlerNamespace,
			},
			Type: corev1.SecretTypeOpaque,
			Data: map[string][]byte{
				"kubeconfig": kubeconfigData,
			},
		}
		_, err = clientset.CoreV1().Secrets(butlerNamespace).Create(ctx, secret, metav1.CreateOptions{})
		if err != nil && !strings.Contains(err.Error(), "already exists") {
			return fmt.Errorf("creating Harvester secret: %w", err)
		}

	case "nutanix":
		// Create Nutanix credentials secret
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      cfg.Cluster.Name + "-nutanix-credentials",
				Namespace: butlerNamespace,
			},
			Type: corev1.SecretTypeOpaque,
			StringData: map[string]string{
				"username": cfg.ProviderConfig.Nutanix.Username,
				"password": cfg.ProviderConfig.Nutanix.Password,
			},
		}
		_, err = clientset.CoreV1().Secrets(butlerNamespace).Create(ctx, secret, metav1.CreateOptions{})
		if err != nil && !strings.Contains(err.Error(), "already exists") {
			return fmt.Errorf("creating Nutanix secret: %w", err)
		}

	case "proxmox":
		// TODO: Create Proxmox credentials secret
		o.logger.Debug("Proxmox credentials not yet implemented")

	case "gcp":
		// Read service account key file
		saKeyData, err := os.ReadFile(cfg.ProviderConfig.GCP.ServiceAccountKeyPath)
		if err != nil {
			return fmt.Errorf("reading GCP service account key: %w", err)
		}

		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      cfg.Cluster.Name + "-gcp-credentials",
				Namespace: butlerNamespace,
			},
			Type: corev1.SecretTypeOpaque,
			Data: map[string][]byte{
				"serviceAccountKey": saKeyData,
			},
		}
		_, err = clientset.CoreV1().Secrets(butlerNamespace).Create(ctx, secret, metav1.CreateOptions{})
		if err != nil && !strings.Contains(err.Error(), "already exists") {
			return fmt.Errorf("creating GCP secret: %w", err)
		}

	case "aws":
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      cfg.Cluster.Name + "-aws-credentials",
				Namespace: butlerNamespace,
			},
			Type: corev1.SecretTypeOpaque,
			StringData: map[string]string{
				"accessKeyID":     cfg.ProviderConfig.AWS.AccessKeyID,
				"secretAccessKey": cfg.ProviderConfig.AWS.SecretAccessKey,
			},
		}
		_, err = clientset.CoreV1().Secrets(butlerNamespace).Create(ctx, secret, metav1.CreateOptions{})
		if err != nil && !strings.Contains(err.Error(), "already exists") {
			return fmt.Errorf("creating AWS secret: %w", err)
		}

	case "azure":
		azureSecretData := map[string]string{
			"clientID":       cfg.ProviderConfig.Azure.ClientID,
			"clientSecret":   cfg.ProviderConfig.Azure.ClientSecret,
			"tenantID":       cfg.ProviderConfig.Azure.TenantID,
			"subscriptionID": cfg.ProviderConfig.Azure.SubscriptionID,
		}
		if cfg.ProviderConfig.Azure.SecurityGroupName != "" {
			azureSecretData["securityGroupName"] = cfg.ProviderConfig.Azure.SecurityGroupName
		}
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      cfg.Cluster.Name + "-azure-credentials",
				Namespace: butlerNamespace,
			},
			Type:       corev1.SecretTypeOpaque,
			StringData: azureSecretData,
		}
		_, err = clientset.CoreV1().Secrets(butlerNamespace).Create(ctx, secret, metav1.CreateOptions{})
		if err != nil && !strings.Contains(err.Error(), "already exists") {
			return fmt.Errorf("creating Azure secret: %w", err)
		}
	}

	o.logger.Success("Namespace and secrets created")
	return nil
}

// deployControllers deploys Butler controllers
func (o *Orchestrator) deployControllers(ctx context.Context, clientset *kubernetes.Clientset, dynamicClient dynamic.Interface, cfg *Config) error {
	deployer := manifests.NewDeployer(clientset, dynamicClient)

	o.logger.Debug("deploying Butler controllers from embedded manifests", "provider", cfg.Provider)
	if err := deployer.DeployControllers(ctx, cfg.Provider); err != nil {
		return fmt.Errorf("deploying controllers: %w", err)
	}

	// Wait for controllers to be ready
	o.logger.Debug("waiting for controllers to be ready")

	// Create a timeout context for waiting
	waitCtx, cancel := context.WithTimeout(ctx, 300*time.Second)
	defer cancel()

	// Wait for bootstrap controller
	if err := deployer.WaitForDeployment(waitCtx, butlerNamespace, "butler-bootstrap-controller"); err != nil {
		return fmt.Errorf("waiting for butler-bootstrap-controller: %w", err)
	}
	o.logger.Success("butler-bootstrap-controller is ready")

	// Wait for provider controller
	providerDeployment := fmt.Sprintf("butler-provider-%s", cfg.Provider)
	if err := deployer.WaitForDeployment(waitCtx, butlerNamespace, providerDeployment); err != nil {
		return fmt.Errorf("waiting for %s: %w", providerDeployment, err)
	}
	o.logger.Success(providerDeployment + " is ready")

	return nil
}

// createProviderConfig creates the ProviderConfig CR using unstructured
func (o *Orchestrator) createProviderConfig(ctx context.Context, client dynamic.Interface, cfg *Config) error {
	pc := o.buildProviderConfigUnstructured(cfg)

	_, err := client.Resource(providerConfigGVR).Namespace(butlerNamespace).Create(
		ctx, pc, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("creating ProviderConfig: %w", err)
	}

	o.logger.Success("ProviderConfig created", "name", pc.GetName())
	return nil
}

// createNetworkPool creates the NetworkPool CR from cfg.NetworkPool so the
// platform has an IPAM pool ready for management cluster allocations. The pool
// name defaults to "<cluster>-underlay" if unset so every bootstrap gets a
// uniquely-scoped pool without clobbering existing ones on upgrade reruns.
func (o *Orchestrator) createNetworkPool(ctx context.Context, client dynamic.Interface, cfg *Config) error {
	np := cfg.NetworkPool
	name := np.Name
	if name == "" {
		name = cfg.Cluster.Name + "-underlay"
	}

	spec := map[string]interface{}{
		"cidr": np.CIDR,
	}

	if len(np.Reserved) > 0 {
		reserved := make([]interface{}, 0, len(np.Reserved))
		for _, r := range np.Reserved {
			entry := map[string]interface{}{"cidr": r.CIDR}
			if r.Description != "" {
				entry["description"] = r.Description
			}
			reserved = append(reserved, entry)
		}
		spec["reserved"] = reserved
	}

	if np.TenantAllocation.Start != "" && np.TenantAllocation.End != "" {
		ta := map[string]interface{}{
			"start": np.TenantAllocation.Start,
			"end":   np.TenantAllocation.End,
		}
		defaults := map[string]interface{}{}
		if np.TenantAllocation.Defaults.LBPoolPerTenant > 0 {
			defaults["lbPoolPerTenant"] = int64(np.TenantAllocation.Defaults.LBPoolPerTenant)
		}
		if np.TenantAllocation.Defaults.NodesPerTenant > 0 {
			defaults["nodesPerTenant"] = int64(np.TenantAllocation.Defaults.NodesPerTenant)
		}
		if len(defaults) > 0 {
			ta["defaults"] = defaults
		}
		spec["tenantAllocation"] = ta
	}

	pool := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": butlerAPIGroup + "/" + butlerAPIVersion,
			"kind":       "NetworkPool",
			"metadata": map[string]interface{}{
				"name":      name,
				"namespace": butlerNamespace,
			},
			"spec": spec,
		},
	}

	_, err := client.Resource(networkPoolGVR).Namespace(butlerNamespace).Create(
		ctx, pool, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("creating NetworkPool: %w", err)
	}

	o.logger.Success("NetworkPool created", "name", name, "cidr", np.CIDR)
	return nil
}

// configureManagementCluster connects to the freshly-bootstrapped management
// cluster and applies settings the bootstrap controller doesn't handle:
// NetworkPool for IPAM, ProviderConfig.spec.network, ButlerConfig multi-tenancy
// mode. Each step is independent — a ProviderConfig patch failure doesn't
// skip the ButlerConfig patch. All errors are collected and returned as one.
func (o *Orchestrator) configureManagementCluster(ctx context.Context, cfg *Config, mgmtKubeconfig string) error {
	_, dynamicClient, err := o.createClients(mgmtKubeconfig)
	if err != nil {
		return fmt.Errorf("connecting to management cluster: %w", err)
	}

	var errs []error

	// 1. Create NetworkPool in butler-system.
	if cfg.NetworkPool != nil {
		if err := o.createNetworkPool(ctx, dynamicClient, cfg); err != nil {
			o.logger.Warn("NetworkPool creation failed", "error", err)
			errs = append(errs, fmt.Errorf("NetworkPool: %w", err))
		}
	}

	// 2. Patch ProviderConfig with spec.network (independent of NetworkPool
	//    creation succeeding — the pool reference is by name, not by UID).
	if cfg.ProviderNetwork != nil {
		if network := o.buildProviderNetworkSection(cfg); network != nil {
			patch := map[string]interface{}{
				"spec": map[string]interface{}{
					"network": network,
				},
			}
			patchBytes, err := json.Marshal(patch)
			if err != nil {
				errs = append(errs, fmt.Errorf("ProviderConfig marshal: %w", err))
			} else {
				_, err = dynamicClient.Resource(providerConfigGVR).Namespace(butlerNamespace).Patch(
					ctx, cfg.Provider, types.MergePatchType, patchBytes, metav1.PatchOptions{})
				if err != nil {
					o.logger.Warn("ProviderConfig patch failed", "error", err)
					errs = append(errs, fmt.Errorf("ProviderConfig: %w", err))
				} else {
					o.logger.Success("ProviderConfig patched with IPAM network config")
				}
			}
		}
	}

	// 3. Patch ButlerConfig with multiTenancy.mode.
	if cfg.MultiTenancyMode != "" {
		patch := map[string]interface{}{
			"spec": map[string]interface{}{
				"multiTenancy": map[string]interface{}{
					"mode": cfg.MultiTenancyMode,
				},
			},
		}
		patchBytes, err := json.Marshal(patch)
		if err != nil {
			errs = append(errs, fmt.Errorf("ButlerConfig marshal: %w", err))
		} else {
			_, err = dynamicClient.Resource(butlerConfigGVR).Patch(
				ctx, "butler", types.MergePatchType, patchBytes, metav1.PatchOptions{})
			if err != nil {
				o.logger.Warn("ButlerConfig patch failed", "error", err)
				errs = append(errs, fmt.Errorf("ButlerConfig: %w", err))
			} else {
				o.logger.Success("ButlerConfig updated", "multiTenancy.mode", cfg.MultiTenancyMode)
			}
		}
	}

	return errors.Join(errs...)
}

// buildProviderNetworkSection returns the map that goes into
// ProviderConfig.spec.network, or nil if no provider network config is set.
// Keeps the builder's switch-per-provider concise by extracting the shared
// network block.
func (o *Orchestrator) buildProviderNetworkSection(cfg *Config) map[string]interface{} {
	if cfg.ProviderNetwork == nil {
		return nil
	}
	pn := cfg.ProviderNetwork

	network := map[string]interface{}{
		"mode": pn.Mode,
	}

	if len(pn.PoolRefs) > 0 {
		refs := make([]interface{}, 0, len(pn.PoolRefs))
		for _, r := range pn.PoolRefs {
			refs = append(refs, map[string]interface{}{
				"name":     r.Name,
				"priority": int64(r.Priority),
			})
		}
		network["poolRefs"] = refs
	}
	if pn.Gateway != "" {
		network["gateway"] = pn.Gateway
	}
	if len(pn.DNSServers) > 0 {
		dns := make([]interface{}, 0, len(pn.DNSServers))
		for _, d := range pn.DNSServers {
			dns = append(dns, d)
		}
		network["dnsServers"] = dns
	}
	if pn.Subnet != "" {
		network["subnet"] = pn.Subnet
	}
	if pn.LoadBalancer.AllocationMode != "" {
		lb := map[string]interface{}{
			"allocationMode": pn.LoadBalancer.AllocationMode,
		}
		if pn.LoadBalancer.InitialPoolSize > 0 {
			lb["initialPoolSize"] = int64(pn.LoadBalancer.InitialPoolSize)
		}
		if pn.LoadBalancer.DefaultPoolSize > 0 {
			lb["defaultPoolSize"] = int64(pn.LoadBalancer.DefaultPoolSize)
		}
		if pn.LoadBalancer.GrowthIncrement > 0 {
			lb["growthIncrement"] = int64(pn.LoadBalancer.GrowthIncrement)
		}
		network["loadBalancer"] = lb
	}
	if pn.QuotaPerTenant.MaxLoadBalancerIPs > 0 || pn.QuotaPerTenant.MaxNodeIPs > 0 {
		quota := map[string]interface{}{}
		if pn.QuotaPerTenant.MaxLoadBalancerIPs > 0 {
			quota["maxLoadBalancerIPs"] = int64(pn.QuotaPerTenant.MaxLoadBalancerIPs)
		}
		if pn.QuotaPerTenant.MaxNodeIPs > 0 {
			quota["maxNodeIPs"] = int64(pn.QuotaPerTenant.MaxNodeIPs)
		}
		network["quotaPerTenant"] = quota
	}

	return network
}

// buildProviderConfigUnstructured builds a ProviderConfig as unstructured
func (o *Orchestrator) buildProviderConfigUnstructured(cfg *Config) *unstructured.Unstructured {
	spec := map[string]interface{}{
		"provider": cfg.Provider,
	}

	// Add provider-specific config and credentialsRef based on provider type
	switch cfg.Provider {
	case "harvester":
		spec["credentialsRef"] = map[string]interface{}{
			"name":      cfg.Cluster.Name + "-harvester-credentials",
			"namespace": butlerNamespace,
			"key":       "kubeconfig",
		}
		spec["harvester"] = map[string]interface{}{
			"namespace":   cfg.ProviderConfig.Harvester.Namespace,
			"networkName": cfg.ProviderConfig.Harvester.NetworkName,
			"imageName":   cfg.ProviderConfig.Harvester.ImageName,
		}
	case "nutanix":
		spec["credentialsRef"] = map[string]interface{}{
			"name":      cfg.Cluster.Name + "-nutanix-credentials",
			"namespace": butlerNamespace,
		}
		spec["nutanix"] = map[string]interface{}{
			"endpoint":    cfg.ProviderConfig.Nutanix.Endpoint,
			"port":        cfg.ProviderConfig.Nutanix.Port,
			"insecure":    cfg.ProviderConfig.Nutanix.Insecure,
			"clusterUUID": cfg.ProviderConfig.Nutanix.ClusterUUID,
			"subnetUUID":  cfg.ProviderConfig.Nutanix.SubnetUUID,
			"imageUUID":   cfg.ProviderConfig.Nutanix.ImageUUID,
		}
	case "proxmox":
		// TODO: Proxmox ProviderConfig not yet implemented

	case "local":
		// The local provider needs no credentials. It is platform-scoped so tenant
		// clusters can reference it, and uses cloud network mode to skip IPAM
		// (CAPD manages container networking).
		spec["scope"] = map[string]interface{}{"type": "platform"}
		spec["network"] = map[string]interface{}{"mode": "cloud"}
		spec["local"] = map[string]interface{}{}

	case "gcp":
		spec["credentialsRef"] = map[string]interface{}{
			"name":      cfg.Cluster.Name + "-gcp-credentials",
			"namespace": butlerNamespace,
		}
		gcpSpec := map[string]interface{}{
			"projectID": cfg.ProviderConfig.GCP.ProjectID,
			"region":    cfg.ProviderConfig.GCP.Region,
			"network":   cfg.ProviderConfig.GCP.Network,
		}
		if cfg.ProviderConfig.GCP.Subnetwork != "" {
			gcpSpec["subnetwork"] = cfg.ProviderConfig.GCP.Subnetwork
		}
		if cfg.ProviderConfig.GCP.Zone != "" {
			gcpSpec["zone"] = cfg.ProviderConfig.GCP.Zone
		}
		if cfg.ProviderConfig.GCP.MachineType != "" {
			gcpSpec["machineType"] = cfg.ProviderConfig.GCP.MachineType
		}
		if cfg.ProviderConfig.GCP.ImageProject != "" {
			gcpSpec["imageProject"] = cfg.ProviderConfig.GCP.ImageProject
		}
		if cfg.ProviderConfig.GCP.ImageFamily != "" {
			gcpSpec["imageFamily"] = cfg.ProviderConfig.GCP.ImageFamily
		}
		if cfg.ProviderConfig.GCP.Image != "" {
			gcpSpec["image"] = cfg.ProviderConfig.GCP.Image
		}
		spec["gcp"] = gcpSpec

	case "aws":
		spec["credentialsRef"] = map[string]interface{}{
			"name":      cfg.Cluster.Name + "-aws-credentials",
			"namespace": butlerNamespace,
		}
		awsSpec := map[string]interface{}{
			"region": cfg.ProviderConfig.AWS.Region,
		}
		if cfg.ProviderConfig.AWS.VPCID != "" {
			awsSpec["vpcID"] = cfg.ProviderConfig.AWS.VPCID
		}
		if cfg.ProviderConfig.AWS.SubnetID != "" {
			awsSpec["subnetIDs"] = []interface{}{cfg.ProviderConfig.AWS.SubnetID}
		}
		if cfg.ProviderConfig.AWS.SecurityGroupID != "" {
			awsSpec["securityGroupIDs"] = []interface{}{cfg.ProviderConfig.AWS.SecurityGroupID}
		}
		spec["aws"] = awsSpec

	case "azure":
		spec["credentialsRef"] = map[string]interface{}{
			"name":      cfg.Cluster.Name + "-azure-credentials",
			"namespace": butlerNamespace,
		}
		azureSpec := map[string]interface{}{
			"subscriptionID": cfg.ProviderConfig.Azure.SubscriptionID,
			"resourceGroup":  cfg.ProviderConfig.Azure.ResourceGroup,
		}
		if cfg.ProviderConfig.Azure.Location != "" {
			azureSpec["location"] = cfg.ProviderConfig.Azure.Location
		}
		if cfg.ProviderConfig.Azure.VNetName != "" {
			azureSpec["vnetName"] = cfg.ProviderConfig.Azure.VNetName
		}
		if cfg.ProviderConfig.Azure.SubnetName != "" {
			azureSpec["subnetName"] = cfg.ProviderConfig.Azure.SubnetName
		}
		if cfg.ProviderConfig.Azure.VMSize != "" {
			azureSpec["vmSize"] = cfg.ProviderConfig.Azure.VMSize
		}
		if cfg.ProviderConfig.Azure.ImageURN != "" {
			azureSpec["imageURN"] = cfg.ProviderConfig.Azure.ImageURN
		}
		spec["azure"] = azureSpec
	}

	// Bind the provider to the IPAM network pool (if one was configured).
	if network := o.buildProviderNetworkSection(cfg); network != nil {
		spec["network"] = network
	}

	pc := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": butlerAPIGroup + "/" + butlerAPIVersion,
			"kind":       "ProviderConfig",
			"metadata": map[string]interface{}{
				"name":      cfg.Cluster.Name + "-provider",
				"namespace": butlerNamespace,
			},
			"spec": spec,
		},
	}

	return pc
}

// createClusterBootstrap creates the ClusterBootstrap CR using unstructured
func (o *Orchestrator) createClusterBootstrap(ctx context.Context, client dynamic.Interface, cfg *Config) error {
	cb := o.buildClusterBootstrapUnstructured(cfg)

	_, err := client.Resource(clusterBootstrapGVR).Namespace(butlerNamespace).Create(
		ctx, cb, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("creating ClusterBootstrap: %w", err)
	}

	o.logger.Success("ClusterBootstrap created", "name", cb.GetName())
	return nil
}

// buildClusterBootstrapUnstructured builds a ClusterBootstrap as unstructured
func (o *Orchestrator) buildClusterBootstrapUnstructured(cfg *Config) *unstructured.Unstructured {
	// Build cluster spec based on topology
	clusterSpec := map[string]interface{}{
		"name":     cfg.Cluster.Name,
		"topology": cfg.Cluster.Topology, // Include topology field
		"controlPlane": map[string]interface{}{
			"replicas": cfg.Cluster.ControlPlane.Replicas,
			"cpu":      cfg.Cluster.ControlPlane.CPU,
			"memoryMB": cfg.Cluster.ControlPlane.MemoryMB,
			"diskGB":   cfg.Cluster.ControlPlane.DiskGB,
		},
	}

	// Only include workers for non-single-node topologies
	if !cfg.IsSingleNode() && cfg.Cluster.Workers.Replicas > 0 {
		// Build extra disks for workers
		var extraDisks []interface{}
		for _, disk := range cfg.Cluster.Workers.ExtraDisks {
			d := map[string]interface{}{
				"sizeGB": disk.SizeGB,
			}
			if disk.StorageClass != "" {
				d["storageClass"] = disk.StorageClass
			}
			extraDisks = append(extraDisks, d)
		}

		workersSpec := map[string]interface{}{
			"replicas": cfg.Cluster.Workers.Replicas,
			"cpu":      cfg.Cluster.Workers.CPU,
			"memoryMB": cfg.Cluster.Workers.MemoryMB,
			"diskGB":   cfg.Cluster.Workers.DiskGB,
		}
		if len(extraDisks) > 0 {
			workersSpec["extraDisks"] = extraDisks
		}
		clusterSpec["workers"] = workersSpec
	}

	cb := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": butlerAPIGroup + "/" + butlerAPIVersion,
			"kind":       "ClusterBootstrap",
			"metadata": map[string]interface{}{
				"name":      cfg.Cluster.Name,
				"namespace": butlerNamespace,
			},
			"spec": map[string]interface{}{
				"provider": cfg.Provider,
				"providerRef": map[string]interface{}{
					"name":      cfg.Cluster.Name + "-provider",
					"namespace": butlerNamespace,
				},
				"cluster": clusterSpec,
				"network": func() map[string]interface{} {
					n := map[string]interface{}{
						"podCIDR":     cfg.Network.PodCIDR,
						"serviceCIDR": cfg.Network.ServiceCIDR,
					}
					if cfg.Network.VIP != "" {
						n["vip"] = cfg.Network.VIP
					}
					if cfg.Network.LoadBalancerPool != nil {
						n["loadBalancerPool"] = map[string]interface{}{
							"start": cfg.Network.LoadBalancerPool.Start,
							"end":   cfg.Network.LoadBalancerPool.End,
						}
					}
					return n
				}(),
				"talos":  buildTalosSpec(cfg),
				"addons": buildAddonsConfig(cfg),
			},
		},
	}

	// Add controlPlaneExposure if configured
	if cfg.ControlPlaneExposure != nil && cfg.ControlPlaneExposure.Mode != "" {
		spec := cb.Object["spec"].(map[string]interface{})
		exposure := map[string]interface{}{
			"mode": cfg.ControlPlaneExposure.Mode,
		}
		if cfg.ControlPlaneExposure.Hostname != "" {
			exposure["hostname"] = cfg.ControlPlaneExposure.Hostname
		}
		if cfg.ControlPlaneExposure.IngressClassName != "" {
			exposure["ingressClassName"] = cfg.ControlPlaneExposure.IngressClassName
		}
		if cfg.ControlPlaneExposure.ControllerType != "" {
			exposure["controllerType"] = cfg.ControlPlaneExposure.ControllerType
		}
		if cfg.ControlPlaneExposure.GatewayRef != "" {
			exposure["gatewayRef"] = cfg.ControlPlaneExposure.GatewayRef
		}
		spec["controlPlaneExposure"] = exposure
	}

	return cb
}

// watchBootstrap watches the ClusterBootstrap CR for completion
func (o *Orchestrator) watchBootstrap(ctx context.Context, client dynamic.Interface, cfg *Config) (*clusterCredentials, error) {
	// Poll for status updates
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	lastPhase := ""
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
			cb, err := client.Resource(clusterBootstrapGVR).Namespace(butlerNamespace).Get(
				ctx, cfg.Cluster.Name, metav1.GetOptions{})
			if err != nil {
				o.logger.Warn("failed to get ClusterBootstrap", "error", err)
				continue
			}

			// Extract status
			status, ok := cb.Object["status"].(map[string]interface{})
			if !ok {
				o.logger.Debug("no status yet")
				continue
			}

			phase, _ := status["phase"].(string)
			if phase != lastPhase {
				o.logger.Info("phase changed", "phase", phase)
				lastPhase = phase
			}

			// Build machine status list and collect control plane IPs
			var controlPlaneIPs []string
			var machineStatuses []MachineStatus
			if machines, ok := status["machines"].([]interface{}); ok {
				for _, m := range machines {
					if machine, ok := m.(map[string]interface{}); ok {
						name, _ := machine["name"].(string)
						role, _ := machine["role"].(string)
						mPhase, _ := machine["phase"].(string)
						ip, _ := machine["ipAddress"].(string)
						talosConfigured, _ := machine["talosConfigured"].(bool)
						ready, _ := machine["ready"].(bool)

						o.logger.Debug("machine status",
							"name", name,
							"phase", mPhase,
							"ip", ip,
							"ready", ready,
						)

						machineStatuses = append(machineStatuses, MachineStatus{
							Name:            name,
							Role:            role,
							Phase:           mPhase,
							IPAddress:       ip,
							TalosConfigured: talosConfigured,
							Ready:           ready,
						})

						if role == "control-plane" && ip != "" {
							controlPlaneIPs = append(controlPlaneIPs, ip)
						}
					}
				}
			}

			// Build addons installed map
			addonsInstalled := make(map[string]bool)
			if addons, ok := status["addonsInstalled"].(map[string]interface{}); ok {
				for k, v := range addons {
					if b, ok := v.(bool); ok {
						addonsInstalled[k] = b
					}
				}
			}

			endpoint, _ := status["controlPlaneEndpoint"].(string)
			consoleURL, _ := status["consoleURL"].(string)
			failureReason, _ := status["failureReason"].(string)
			failureMessage, _ := status["failureMessage"].(string)

			// Emit full status snapshot for TUI
			o.emit(Event{
				Type:    EventBootstrapStatus,
				Phase:   phase,
				Message: "bootstrap status update",
				Status: &BootstrapSnapshot{
					Phase:           phase,
					Machines:        machineStatuses,
					AddonsInstalled: addonsInstalled,
					FailureReason:   failureReason,
					FailureMessage:  failureMessage,
					Endpoint:        endpoint,
					ConsoleURL:      consoleURL,
				},
			})

			switch phase {
			case "Ready":
				o.logger.Success("Cluster is ready!")

				var kubeconfigBytes, talosconfigBytes []byte
				if o.isLocal {
					// The KIND cluster IS the management cluster, so its kubeconfig is the
					// management kubeconfig. There is no Talos config for a local cluster.
					kc, readErr := os.ReadFile(o.kindKubeconfigPath)
					if readErr != nil {
						return nil, fmt.Errorf("reading KIND kubeconfig: %w", readErr)
					}
					kubeconfigBytes = kc
				} else {
					// Decode kubeconfig
					kubeconfig, _ := status["kubeconfig"].(string)
					kc, decErr := base64.StdEncoding.DecodeString(kubeconfig)
					if decErr != nil {
						return nil, fmt.Errorf("decoding kubeconfig: %w", decErr)
					}
					kubeconfigBytes = kc

					// Decode talosconfig - NOTE: JSON field is lowercase "talosconfig"
					talosconfig, _ := status["talosconfig"].(string)
					tc, decErr := base64.StdEncoding.DecodeString(talosconfig)
					if decErr != nil {
						return nil, fmt.Errorf("decoding talosconfig: %w", decErr)
					}
					talosconfigBytes = tc
				}

				creds := &clusterCredentials{
					kubeconfig:      kubeconfigBytes,
					talosconfig:     talosconfigBytes,
					controlPlaneIPs: controlPlaneIPs,
					consoleURL:      consoleURL,
				}
				o.emit(Event{
					Type:    EventComplete,
					Phase:   phase,
					Message: "Bootstrap complete",
					Creds: &ClusterCredentials{
						Kubeconfig:      kubeconfigBytes,
						Talosconfig:     talosconfigBytes,
						ControlPlaneIPs: controlPlaneIPs,
						ConsoleURL:      consoleURL,
					},
				})
				return creds, nil
			case "Failed":
				err := fmt.Errorf("bootstrap failed: %s - %s", failureReason, failureMessage)
				o.emit(Event{
					Type:    EventFailed,
					Phase:   phase,
					Message: failureMessage,
					Error:   err,
				})
				return nil, err
			}
		}
	}
}

// saveClusterCredentials saves the kubeconfig and talosconfig to ~/.butler/
func (o *Orchestrator) saveClusterCredentials(clusterName string, creds *clusterCredentials) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("getting home directory: %w", err)
	}

	butlerDir := filepath.Join(home, ".butler")
	if err := os.MkdirAll(butlerDir, 0700); err != nil {
		return fmt.Errorf("creating .butler directory: %w", err)
	}

	// Save kubeconfig
	kubeconfigPath := filepath.Join(butlerDir, clusterName+"-kubeconfig")
	if err := os.WriteFile(kubeconfigPath, creds.kubeconfig, 0600); err != nil {
		return fmt.Errorf("writing kubeconfig: %w", err)
	}

	// Fix talosconfig endpoints and save
	talosconfig := o.fixTalosconfigEndpoints(creds.talosconfig, clusterName, creds.controlPlaneIPs)
	talosconfigPath := filepath.Join(butlerDir, clusterName+"-talosconfig")
	if err := os.WriteFile(talosconfigPath, talosconfig, 0600); err != nil {
		return fmt.Errorf("writing talosconfig: %w", err)
	}

	return nil
}

// fixTalosconfigEndpoints adds endpoints to the talosconfig if they're empty
func (o *Orchestrator) fixTalosconfigEndpoints(talosconfig []byte, clusterName string, controlPlaneIPs []string) []byte {
	if len(controlPlaneIPs) == 0 {
		return talosconfig
	}

	// Parse the talosconfig as a map
	var config map[string]interface{}
	if err := yaml.Unmarshal(talosconfig, &config); err != nil {
		o.logger.Warn("failed to parse talosconfig, returning as-is", "error", err)
		return talosconfig
	}

	// Navigate to contexts.<clusterName>.endpoints
	contexts, ok := config["contexts"].(map[string]interface{})
	if !ok {
		return talosconfig
	}

	contextConfig, ok := contexts[clusterName].(map[string]interface{})
	if !ok {
		return talosconfig
	}

	// Check if endpoints is empty or missing
	endpoints, _ := contextConfig["endpoints"].([]interface{})
	if len(endpoints) == 0 {
		// Add control plane IPs as endpoints
		contextConfig["endpoints"] = controlPlaneIPs
		// Also add all IPs as nodes for convenience
		var allNodes []string
		if existingNodes, ok := contextConfig["nodes"].([]interface{}); ok {
			for _, n := range existingNodes {
				if s, ok := n.(string); ok {
					allNodes = append(allNodes, s)
				}
			}
		}
		if len(allNodes) == 0 {
			contextConfig["nodes"] = controlPlaneIPs
		}
	}

	// Marshal back to YAML
	fixed, err := yaml.Marshal(config)
	if err != nil {
		o.logger.Warn("failed to marshal fixed talosconfig", "error", err)
		return talosconfig
	}

	return fixed
}

// buildAndLoadImages builds controller images and loads them into KIND (local dev mode)
func (o *Orchestrator) buildAndLoadImages(ctx context.Context, provider string) error {
	if o.options.RepoRoot == "" {
		return fmt.Errorf("repo root not set - use --repo-root flag")
	}

	// Define images to build.
	// useParentContext signals that the repo's go.mod has a replace directive
	// pointing to a sibling repo (e.g., replace ../butler-api). In that case
	// the Docker build context must be the parent directory so the sibling
	// is accessible, and -f points to the repo's Dockerfile.
	type buildImage struct {
		name             string
		repoDir          string
		image            string
		useParentContext bool
	}
	images := []buildImage{
		{
			name:             "butler-bootstrap",
			repoDir:          filepath.Join(o.options.RepoRoot, "butler-bootstrap"),
			image:            "ghcr.io/butlerdotdev/butler-bootstrap:latest",
			useParentContext: true,
		},
	}
	if o.isLocal {
		// The local provider uses CAPD for tenant workers (there is no
		// butler-provider-local), and it needs butler-controller built from source
		// so the CAPD resource builder is present.
		images = append(images, buildImage{
			name:    "butler-controller",
			repoDir: filepath.Join(o.options.RepoRoot, "butler-controller"),
			image:   "ghcr.io/butlerdotdev/butler-controller:latest",
			// butler-controller fetches butler-api as a normal module (no sibling
			// replace), so it builds with its own repo as the context.
			useParentContext: false,
		})
	} else {
		images = append(images, buildImage{
			name:             fmt.Sprintf("butler-provider-%s", provider),
			repoDir:          filepath.Join(o.options.RepoRoot, fmt.Sprintf("butler-provider-%s", provider)),
			image:            fmt.Sprintf("ghcr.io/butlerdotdev/butler-provider-%s:latest", provider),
			useParentContext: true,
		})
	}

	for _, img := range images {
		// Check if repo directory exists
		if _, err := os.Stat(img.repoDir); os.IsNotExist(err) {
			return fmt.Errorf("repo directory not found: %s", img.repoDir)
		}

		// Build Docker image. When useParentContext is true, set the build
		// context to the repo root (parent of all sibling repos) so that
		// go.mod replace directives like ../butler-api resolve correctly.
		// A temporary .dockerignore limits the context to only the needed repos.
		var buildCmd *exec.Cmd
		if img.useParentContext {
			repoName := filepath.Base(img.repoDir)
			dockerfile := filepath.Join(img.repoDir, "Dockerfile")

			// Create a temporary .dockerignore to limit context size.
			// Only include the target repo and butler-api (for replace directive).
			dockerignorePath := filepath.Join(o.options.RepoRoot, ".dockerignore")
			dockerignore := fmt.Sprintf("*\n!%s\n!butler-api\n**/.git\n**/bin\n", repoName)
			if err := os.WriteFile(dockerignorePath, []byte(dockerignore), 0644); err != nil {
				return fmt.Errorf("creating .dockerignore: %w", err)
			}
			defer os.Remove(dockerignorePath)

			o.logger.Info("building image (parent context)", "name", img.name, "context", o.options.RepoRoot)
			buildCmd = exec.CommandContext(ctx, "docker", "build",
				"-t", img.image,
				"-f", dockerfile,
				"--build-arg", fmt.Sprintf("REPO_DIR=%s", repoName),
				".")
			buildCmd.Dir = o.options.RepoRoot
		} else {
			o.logger.Info("building image", "name", img.name, "dir", img.repoDir)
			buildCmd = exec.CommandContext(ctx, "docker", "build", "-t", img.image, ".")
			buildCmd.Dir = img.repoDir
		}
		buildCmd.Stdout = os.Stdout
		buildCmd.Stderr = os.Stderr

		if err := buildCmd.Run(); err != nil {
			return fmt.Errorf("building %s: %w", img.name, err)
		}
		o.logger.Success("built image", "image", img.image)

		// Load into KIND
		o.logger.Info("loading image into KIND", "image", img.image)
		loadCmd := exec.CommandContext(ctx, "kind", "load", "docker-image", img.image, "--name", kindClusterName)
		loadCmd.Stdout = os.Stdout
		loadCmd.Stderr = os.Stderr

		if err := loadCmd.Run(); err != nil {
			return fmt.Errorf("loading %s into KIND: %w", img.name, err)
		}
		o.logger.Success("loaded image into KIND", "image", img.image)
	}

	return nil
}

// buildTalosSpec builds the talos section of the ClusterBootstrap spec.
// Emits configPatches for optional overrides: NTP servers (when TimeServers
// is set) and primary NIC MTU (when Network.MTU is non-zero).
//
// The CRD value field is a string that the bootstrap controller splices
// directly into a JSON patch via fmt.Sprintf with %s (no additional
// quoting). Each value must therefore be raw JSON that parses verbatim
// when pasted — json.Marshal produces the correct form for both objects
// (NTP) and bare integers (MTU).
func buildTalosSpec(cfg *Config) map[string]interface{} {
	spec := map[string]interface{}{
		"version":   cfg.Talos.Version,
		"schematic": cfg.Talos.Schematic,
	}

	var patches []interface{}

	if len(cfg.Talos.TimeServers) > 0 {
		valueJSON, _ := json.Marshal(map[string]interface{}{
			"servers": cfg.Talos.TimeServers,
		})
		patches = append(patches, map[string]interface{}{
			"op":    "add",
			"path":  "/machine/time",
			"value": string(valueJSON),
		})
	}

	if cfg.Network.MTU > 0 {
		// Replace the entire interfaces array with a single entry that
		// matches any physical NIC by deviceSelector. This is required
		// because Talos's default generated config has no interfaces
		// array — RFC6902 `add` at a deep path like .../interfaces/0/mtu
		// fails with "doc is missing path" when the parent array is
		// absent. Patching at /machine/network/interfaces creates the
		// key on the existing /machine/network object (which is always
		// present in generated configs).
		//
		// deviceSelector.physical=true matches any non-virtual NIC and
		// works across all supported providers (harvester, nutanix,
		// proxmox, aws, azure, gcp) without hard-coding ens3 vs eth0 vs
		// ens5. Cilium re-derives its tunnel MTU from device MTU at
		// startup, so no separate Cilium knob is required.
		//
		// Single-NIC shape only. On nodes with more than one physical
		// NIC, this selector matches all of them — every physical NIC
		// gets the MTU override and dhcp:true. That is acceptable
		// today because every supported provider provisions nodes with
		// a single data NIC. If we grow to node shapes with separate
		// mgmt/data NICs, this patch must be scoped by busPath, driver,
		// or hardwareAddr before shipping.
		//
		// dhcp:true is set explicitly. Talos only auto-enables DHCP on
		// physical NICs when the interfaces array is absent; once an
		// interface entry exists, any addressing mode it omits is
		// treated as disabled. Without this the node boots with MTU
		// 1380 but no IP lease, and talosctl bootstrap fails with a
		// gRPC auth-handshake timeout because the API is unreachable.
		valueJSON, _ := json.Marshal([]interface{}{
			map[string]interface{}{
				"deviceSelector": map[string]interface{}{
					"physical": true,
				},
				"dhcp": true,
				"mtu":  cfg.Network.MTU,
			},
		})
		patches = append(patches, map[string]interface{}{
			"op":    "add",
			"path":  "/machine/network/interfaces",
			"value": string(valueJSON),
		})
	}

	if len(patches) > 0 {
		spec["configPatches"] = patches
	}
	return spec
}

// Fields with CRD-level defaults (butlerController, capi) are only included
// when explicitly configured. Omitting them lets the CRD *bool defaults
// (nil = enabled) take effect instead of writing Go's bool zero value (false).
func buildAddonsConfig(cfg *Config) map[string]interface{} {
	addons := map[string]interface{}{
		"cni": map[string]interface{}{
			"type": cfg.Addons.CNI.Type,
		},
		"storage": map[string]interface{}{
			"type": cfg.Addons.Storage.Type,
		},
		"loadBalancer": func() map[string]interface{} {
			lb := map[string]interface{}{
				"type": cfg.Addons.LoadBalancer.Type,
			}
			// Only write deprecated addressPool if the new loadBalancerPool isn't set
			if cfg.Network.LoadBalancerPool == nil && cfg.Addons.LoadBalancer.AddressPool != "" {
				lb["addressPool"] = cfg.Addons.LoadBalancer.AddressPool
			}
			return lb
		}(),
	}

	// Only include console when the user explicitly configured it.
	// The CRD defaults Enabled to true via *bool, so omitting this section
	// enables butler-console by default.
	if cfg.Addons.Console.Enabled || cfg.Addons.Console.Version != "" {
		addons["console"] = buildConsoleConfig(cfg.Addons.Console)
	}

	// Only include butlerController when the user explicitly configured it.
	// The CRD defaults Enabled to true via *bool, so omitting this section
	// enables butler-controller by default.
	if cfg.Addons.ButlerController.Version != "" || cfg.Addons.ButlerController.Image != "" {
		bc := map[string]interface{}{
			"enabled": true,
		}
		if cfg.Addons.ButlerController.Version != "" {
			bc["version"] = cfg.Addons.ButlerController.Version
		}
		if cfg.Addons.ButlerController.Image != "" {
			bc["image"] = cfg.Addons.ButlerController.Image
		}
		addons["butlerController"] = bc
	}

	// Only include capi when the user explicitly configured it.
	// The CRD defaults Enabled to true via *bool.
	if cfg.Addons.CAPI.Version != "" {
		addons["capi"] = map[string]interface{}{
			"enabled": true,
			"version": cfg.Addons.CAPI.Version,
		}
	}

	// Only include gitOps if explicitly configured
	if cfg.Addons.GitOps.Type != "" {
		addons["gitOps"] = map[string]interface{}{
			"type":    cfg.Addons.GitOps.Type,
			"enabled": true,
		}
	}

	return addons
}

func buildConsoleConfig(cfg ConsoleConfig) map[string]interface{} {
	if !cfg.Enabled {
		return map[string]interface{}{
			"enabled": false,
		}
	}

	result := map[string]interface{}{
		"enabled": true,
		"version": cfg.Version,
	}

	if cfg.Ingress.Enabled {
		result["ingress"] = map[string]interface{}{
			"enabled":       true,
			"host":          cfg.Ingress.Host,
			"className":     cfg.Ingress.ClassName,
			"tls":           cfg.Ingress.TLS,
			"tlsSecretName": cfg.Ingress.TLSSecretName,
		}
	}

	return result
}

// isCloudProvider returns true for cloud providers that need a load balancer for HA.
func isCloudProvider(provider string) bool {
	switch provider {
	case "gcp", "aws", "azure":
		return true
	}
	return false
}
