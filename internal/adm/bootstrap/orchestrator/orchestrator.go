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
	"fmt"
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
	logger  *log.Logger
	options Options
}

// New creates a new orchestrator
func New(logger *log.Logger, options Options) *Orchestrator {
	return &Orchestrator{
		logger:  logger,
		options: options,
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

	// Create context with timeout
	ctx, cancel := context.WithTimeout(ctx, o.options.Timeout)
	defer cancel()

	// Phase 1: Create KIND cluster
	o.logger.Phase("Creating temporary KIND cluster")
	kindProvider := cluster.NewProvider()

	kubeconfigPath, err := o.createKINDCluster(ctx, kindProvider)
	if err != nil {
		return fmt.Errorf("creating KIND cluster: %w", err)
	}
	defer func() {
		if !o.options.SkipCleanup {
			o.logger.Phase("Cleaning up KIND cluster")
			if err := kindProvider.Delete(kindClusterName, ""); err != nil {
				o.logger.Error("failed to delete KIND cluster", "error", err)
			}
		}
	}()

	// Inject host aliases for corporate DNS resolution (must be after KIND cluster creation)
	hostAliases := o.getHostAliases(cfg)
	if len(hostAliases) > 0 {
		if err := o.injectHostAliases(ctx, hostAliases); err != nil {
			o.logger.Warn("Failed to inject host aliases", "error", err)
		}
	}

	// Build and load images in local dev mode
	if o.options.LocalDev {
		o.logger.Phase("Building and loading controller images (local dev mode)")
		if err := o.buildAndLoadImages(ctx, cfg.Provider); err != nil {
			return fmt.Errorf("building/loading images: %w", err)
		}
	}

	// Create Kubernetes clients
	o.logger.Phase("Connecting to KIND cluster")
	clientset, dynamicClient, err := o.createClients(kubeconfigPath)
	if err != nil {
		return fmt.Errorf("creating clients: %w", err)
	}

	// Deploy Butler CRDs
	o.logger.Phase("Deploying Butler CRDs")
	if err := o.deployCRDs(ctx, clientset, dynamicClient); err != nil {
		return fmt.Errorf("deploying CRDs: %w", err)
	}

	// Create namespace and provider secret
	o.logger.Phase("Creating namespace and secrets")
	if err := o.createNamespaceAndSecrets(ctx, clientset, cfg); err != nil {
		return fmt.Errorf("creating namespace/secrets: %w", err)
	}

	// Deploy controllers
	o.logger.Phase("Deploying Butler controllers")
	if err := o.deployControllers(ctx, clientset, dynamicClient, cfg); err != nil {
		return fmt.Errorf("deploying controllers: %w", err)
	}

	// Create ProviderConfig CR
	o.logger.Phase("Creating ProviderConfig")
	if err := o.createProviderConfig(ctx, dynamicClient, cfg); err != nil {
		return fmt.Errorf("creating ProviderConfig: %w", err)
	}

	// Create ClusterBootstrap CR
	o.logger.Phase("Creating ClusterBootstrap")
	if err := o.createClusterBootstrap(ctx, dynamicClient, cfg); err != nil {
		return fmt.Errorf("creating ClusterBootstrap: %w", err)
	}

	// Watch for completion
	o.logger.Phase("Waiting for cluster bootstrap")
	creds, err := o.watchBootstrap(ctx, dynamicClient, cfg)
	if err != nil {
		return fmt.Errorf("watching bootstrap: %w", err)
	}

	// Save cluster credentials
	o.logger.Phase("Saving cluster credentials")
	if err := o.saveClusterCredentials(cfg.Cluster.Name, creds); err != nil {
		return fmt.Errorf("saving cluster credentials: %w", err)
	}

	o.logger.Success("Bootstrap complete!")
	o.logger.Info("")
	o.logger.Info("Cluster credentials saved to:")
	o.logger.Info("  Kubeconfig:   ~/.butler/" + cfg.Cluster.Name + "-kubeconfig")
	o.logger.Info("  Talosconfig:  ~/.butler/" + cfg.Cluster.Name + "-talosconfig")
	o.logger.Info("")

	if creds.consoleURL != "" {
		o.logger.Info("Butler Console:")
		if strings.HasPrefix(creds.consoleURL, "kubectl") {
			// It's a port-forward instruction
			o.logger.Info("  Access via: " + creds.consoleURL)
		} else {
			o.logger.Info("  URL: " + creds.consoleURL)
		}
		o.logger.Info("  Credentials: admin / admin (change after first login)")
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

// buildKINDConfig generates a KIND cluster configuration with CA certificate mounts
func (o *Orchestrator) buildKINDConfig(caCerts []string) string {
	if len(caCerts) == 0 {
		// No custom certs, use minimal config
		return `kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
nodes:
  - role: control-plane
`
	}

	// Build extraMounts for each certificate
	var mounts strings.Builder
	for i, certPath := range caCerts {
		containerPath := fmt.Sprintf("/usr/local/share/ca-certificates/butler-custom-%d.crt", i)
		mounts.WriteString(fmt.Sprintf(`      - hostPath: %s
        containerPath: %s
        readOnly: true
`, certPath, containerPath))
	}

	return fmt.Sprintf(`kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
nodes:
  - role: control-plane
    extraMounts:
%s`, mounts.String())
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

// patchCoreDNS fixes CoreDNS to use Google DNS instead of /etc/resolv.conf
// This is needed because KIND's resolv.conf may not work properly on Mac
func (o *Orchestrator) patchCoreDNS(kubeconfigPath string) error {
	corefile := `.:53 {
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
    forward . 8.8.8.8 8.8.4.4 {
       max_concurrent 1000
    }
    cache 30
    loop
    reload
    loadbalance
}
`
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

	o.logger.Debug("CoreDNS patched to use Google DNS")
	return nil
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
		"machinerequests.butler.butlerlabs.dev",
		"providerconfigs.butler.butlerlabs.dev",
		"clusterbootstraps.butler.butlerlabs.dev",
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
				"network": map[string]interface{}{
					"podCIDR":     cfg.Network.PodCIDR,
					"serviceCIDR": cfg.Network.ServiceCIDR,
					"vip":         cfg.Network.VIP,
				},
				"talos": map[string]interface{}{
					"version":   cfg.Talos.Version,
					"schematic": cfg.Talos.Schematic,
				},
				"addons": map[string]interface{}{
					"cni": map[string]interface{}{
						"type": cfg.Addons.CNI.Type,
					},
					"storage": map[string]interface{}{
						"type": cfg.Addons.Storage.Type,
					},
					"loadBalancer": map[string]interface{}{
						"type":        cfg.Addons.LoadBalancer.Type,
						"addressPool": cfg.Addons.LoadBalancer.AddressPool,
					},
					"gitOps": map[string]interface{}{
						"type": cfg.Addons.GitOps.Type,
					},
					"capi": map[string]interface{}{
						"enabled": cfg.Addons.CAPI.Enabled,
						"version": cfg.Addons.CAPI.Version,
					},
					"butlerController": map[string]interface{}{
						"enabled": cfg.Addons.ButlerController.Enabled,
						"version": cfg.Addons.ButlerController.Version,
						"image":   cfg.Addons.ButlerController.Image,
					},
					"console": buildConsoleConfig(cfg.Addons.Console),
				},
			},
		},
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

			// Collect control plane IPs from machine status
			var controlPlaneIPs []string
			if machines, ok := status["machines"].([]interface{}); ok {
				for _, m := range machines {
					if machine, ok := m.(map[string]interface{}); ok {
						o.logger.Debug("machine status",
							"name", machine["name"],
							"phase", machine["phase"],
							"ip", machine["ipAddress"],
							"ready", machine["ready"],
						)
						// Collect control plane IPs for talosconfig endpoints
						if role, _ := machine["role"].(string); role == "control-plane" {
							if ip, _ := machine["ipAddress"].(string); ip != "" {
								controlPlaneIPs = append(controlPlaneIPs, ip)
							}
						}
					}
				}
			}

			switch phase {
			case "Ready":
				o.logger.Success("Cluster is ready!")

				// Decode kubeconfig
				kubeconfig, _ := status["kubeconfig"].(string)
				kubeconfigBytes, err := base64.StdEncoding.DecodeString(kubeconfig)
				if err != nil {
					return nil, fmt.Errorf("decoding kubeconfig: %w", err)
				}

				// Decode talosconfig - NOTE: JSON field is lowercase "talosconfig"
				talosconfig, _ := status["talosconfig"].(string)
				talosconfigBytes, err := base64.StdEncoding.DecodeString(talosconfig)
				if err != nil {
					return nil, fmt.Errorf("decoding talosconfig: %w", err)
				}

				consoleURL, _ := status["consoleURL"].(string)

				return &clusterCredentials{
					kubeconfig:      kubeconfigBytes,
					talosconfig:     talosconfigBytes,
					controlPlaneIPs: controlPlaneIPs,
					consoleURL:      consoleURL,
				}, nil
			case "Failed":
				reason, _ := status["failureReason"].(string)
				message, _ := status["failureMessage"].(string)
				return nil, fmt.Errorf("bootstrap failed: %s - %s", reason, message)
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

	// Define images to build
	images := []struct {
		name    string
		repoDir string
		image   string
	}{
		{
			name:    "butler-bootstrap",
			repoDir: filepath.Join(o.options.RepoRoot, "butler-bootstrap"),
			image:   "ghcr.io/butlerdotdev/butler-bootstrap:latest",
		},
		{
			name:    fmt.Sprintf("butler-provider-%s", provider),
			repoDir: filepath.Join(o.options.RepoRoot, fmt.Sprintf("butler-provider-%s", provider)),
			image:   fmt.Sprintf("ghcr.io/butlerdotdev/butler-provider-%s:latest", provider),
		},
	}

	for _, img := range images {
		// Check if repo directory exists
		if _, err := os.Stat(img.repoDir); os.IsNotExist(err) {
			return fmt.Errorf("repo directory not found: %s", img.repoDir)
		}

		// Build Docker image
		o.logger.Info("building image", "name", img.name, "dir", img.repoDir)
		buildCmd := exec.CommandContext(ctx, "docker", "build", "-t", img.image, ".")
		buildCmd.Dir = img.repoDir
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

// buildConsoleConfig builds the console addon config for the ClusterBootstrap CR
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
