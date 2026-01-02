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

	// Phase 1.5: Build and load images in local dev mode
	if o.options.LocalDev {
		o.logger.Phase("Building and loading controller images (local dev mode)")
		if err := o.buildAndLoadImages(ctx, cfg.Provider); err != nil {
			return fmt.Errorf("building/loading images: %w", err)
		}
	}

	// Phase 2: Create Kubernetes clients
	o.logger.Phase("Connecting to KIND cluster")
	clientset, dynamicClient, err := o.createClients(kubeconfigPath)
	if err != nil {
		return fmt.Errorf("creating clients: %w", err)
	}

	// Phase 3: Deploy Butler CRDs
	o.logger.Phase("Deploying Butler CRDs")
	if err := o.deployCRDs(ctx, clientset, dynamicClient); err != nil {
		return fmt.Errorf("deploying CRDs: %w", err)
	}

	// Phase 4: Create namespace and provider secret
	o.logger.Phase("Creating namespace and secrets")
	if err := o.createNamespaceAndSecrets(ctx, clientset, cfg); err != nil {
		return fmt.Errorf("creating namespace/secrets: %w", err)
	}

	// Phase 5: Deploy controllers
	o.logger.Phase("Deploying Butler controllers")
	if err := o.deployControllers(ctx, clientset, dynamicClient, cfg); err != nil {
		return fmt.Errorf("deploying controllers: %w", err)
	}

	// Phase 6: Create ProviderConfig CR
	o.logger.Phase("Creating ProviderConfig")
	if err := o.createProviderConfig(ctx, dynamicClient, cfg); err != nil {
		return fmt.Errorf("creating ProviderConfig: %w", err)
	}

	// Phase 7: Create ClusterBootstrap CR
	o.logger.Phase("Creating ClusterBootstrap")
	if err := o.createClusterBootstrap(ctx, dynamicClient, cfg); err != nil {
		return fmt.Errorf("creating ClusterBootstrap: %w", err)
	}

	// Phase 8: Watch for completion
	o.logger.Phase("Waiting for cluster bootstrap")
	kubeconfBytes, err := o.watchBootstrap(ctx, dynamicClient, cfg)
	if err != nil {
		return fmt.Errorf("watching bootstrap: %w", err)
	}

	// Phase 9: Save kubeconfig
	o.logger.Phase("Saving kubeconfig")
	if err := o.saveKubeconfig(cfg.Cluster.Name, kubeconfBytes); err != nil {
		return fmt.Errorf("saving kubeconfig: %w", err)
	}

	o.logger.Success("Bootstrap complete!")
	o.logger.Info("Kubeconfig saved to ~/.butler/" + cfg.Cluster.Name + "-kubeconfig")
	o.logger.Info("Use: export KUBECONFIG=~/.butler/" + cfg.Cluster.Name + "-kubeconfig")

	return nil
}

// dryRun shows what would be created
func (o *Orchestrator) dryRun(cfg *Config) error {
	o.logger.Info("DRY RUN - showing what would be created")

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

	// Show MachineRequests that would be created
	fmt.Println("\n--- MachineRequests (created by controller) ---")
	for i := int32(0); i < cfg.Cluster.ControlPlane.Replicas; i++ {
		fmt.Printf("- %s-cp-%d (control-plane, %d CPU, %d MB RAM)\n",
			cfg.Cluster.Name, i, cfg.Cluster.ControlPlane.CPU, cfg.Cluster.ControlPlane.MemoryMB)
	}
	for i := int32(0); i < cfg.Cluster.Workers.Replicas; i++ {
		fmt.Printf("- %s-worker-%d (worker, %d CPU, %d MB RAM)\n",
			cfg.Cluster.Name, i, cfg.Cluster.Workers.CPU, cfg.Cluster.Workers.MemoryMB)
	}

	return nil
}

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
	// Create cluster
	if err := provider.Create(kindClusterName); err != nil {
		return "", fmt.Errorf("creating cluster: %w", err)
	}
	o.logger.Success("KIND cluster created")

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
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("patching CoreDNS: %w, output: %s", err, string(output))
	}

	// Restart CoreDNS to pick up changes
	cmd = exec.Command("kubectl", "--kubeconfig", kubeconfigPath,
		"rollout", "restart", "deployment/coredns", "-n", "kube-system")
	output, err = cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("restarting CoreDNS: %w, output: %s", err, string(output))
	}

	// Wait for rollout
	cmd = exec.Command("kubectl", "--kubeconfig", kubeconfigPath,
		"rollout", "status", "deployment/coredns", "-n", "kube-system", "--timeout=60s")
	cmd.Run() // Ignore error, just best effort

	return nil
}

// getKINDKubeconfig writes KIND kubeconfig to temp file and returns path
func (o *Orchestrator) getKINDKubeconfig(provider *cluster.Provider) (string, error) {
	kubeconfig, err := provider.KubeConfig(kindClusterName, false)
	if err != nil {
		return "", fmt.Errorf("getting kubeconfig: %w", err)
	}

	// Write to temp file
	tmpFile, err := os.CreateTemp("", "butler-bootstrap-kubeconfig-*")
	if err != nil {
		return "", fmt.Errorf("creating temp file: %w", err)
	}

	if _, err := tmpFile.WriteString(kubeconfig); err != nil {
		os.Remove(tmpFile.Name())
		return "", fmt.Errorf("writing kubeconfig: %w", err)
	}
	tmpFile.Close()

	return tmpFile.Name(), nil
}

// createClients creates Kubernetes clients from kubeconfig path
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

// createNamespaceAndSecrets creates the butler-system namespace and provider secrets
func (o *Orchestrator) createNamespaceAndSecrets(ctx context.Context, clientset *kubernetes.Clientset, cfg *Config) error {
	// Create namespace
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: butlerNamespace,
		},
	}
	if _, err := clientset.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{}); err != nil {
		o.logger.Debug("namespace may already exist", "error", err)
	}

	// Create provider secret based on provider type
	switch cfg.Provider {
	case "harvester":
		kubeconfBytes, err := os.ReadFile(cfg.ProviderConfig.Harvester.KubeconfigPath)
		if err != nil {
			return fmt.Errorf("reading harvester kubeconfig: %w", err)
		}

		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      cfg.Cluster.Name + "-harvester-credentials",
				Namespace: butlerNamespace,
			},
			Type: corev1.SecretTypeOpaque,
			Data: map[string][]byte{
				"kubeconfig": kubeconfBytes,
			},
		}
		if _, err := clientset.CoreV1().Secrets(butlerNamespace).Create(ctx, secret, metav1.CreateOptions{}); err != nil {
			return fmt.Errorf("creating secret: %w", err)
		}

	case "nutanix":
		// TODO: Create Nutanix credentials secret
		o.logger.Debug("Nutanix credentials not yet implemented")

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
	waitCtx, cancel := context.WithTimeout(ctx, 120*time.Second)
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
	pc := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": butlerAPIGroup + "/" + butlerAPIVersion,
			"kind":       "ProviderConfig",
			"metadata": map[string]interface{}{
				"name":      cfg.Cluster.Name + "-provider",
				"namespace": butlerNamespace,
			},
			"spec": map[string]interface{}{
				"provider": cfg.Provider,
				"credentialsRef": map[string]interface{}{
					"name":      cfg.Cluster.Name + "-harvester-credentials",
					"namespace": butlerNamespace,
					"key":       "kubeconfig",
				},
			},
		},
	}

	// Add provider-specific config
	spec := pc.Object["spec"].(map[string]interface{})

	switch cfg.Provider {
	case "harvester":
		spec["harvester"] = map[string]interface{}{
			"namespace":   cfg.ProviderConfig.Harvester.Namespace,
			"networkName": cfg.ProviderConfig.Harvester.NetworkName,
			"imageName":   cfg.ProviderConfig.Harvester.ImageName,
		}
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
				"cluster": map[string]interface{}{
					"name": cfg.Cluster.Name,
					"controlPlane": map[string]interface{}{
						"replicas": cfg.Cluster.ControlPlane.Replicas,
						"cpu":      cfg.Cluster.ControlPlane.CPU,
						"memoryMB": cfg.Cluster.ControlPlane.MemoryMB,
						"diskGB":   cfg.Cluster.ControlPlane.DiskGB,
					},
					"workers": map[string]interface{}{
						"replicas":   cfg.Cluster.Workers.Replicas,
						"cpu":        cfg.Cluster.Workers.CPU,
						"memoryMB":   cfg.Cluster.Workers.MemoryMB,
						"diskGB":     cfg.Cluster.Workers.DiskGB,
						"extraDisks": extraDisks,
					},
				},
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
				},
			},
		},
	}

	return cb
}

// watchBootstrap watches the ClusterBootstrap CR for completion
func (o *Orchestrator) watchBootstrap(ctx context.Context, client dynamic.Interface, cfg *Config) ([]byte, error) {
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

			// Log machine status if available
			if machines, ok := status["machines"].([]interface{}); ok {
				for _, m := range machines {
					if machine, ok := m.(map[string]interface{}); ok {
						o.logger.Debug("machine status",
							"name", machine["name"],
							"phase", machine["phase"],
							"ip", machine["ipAddress"],
							"ready", machine["ready"],
						)
					}
				}
			}

			switch phase {
			case "Ready":
				o.logger.Success("Cluster is ready!")
				kubeconfig, _ := status["kubeconfig"].(string)
				decoded, err := base64.StdEncoding.DecodeString(kubeconfig)
				if err != nil {
					return nil, fmt.Errorf("decoding kubeconfig: %w", err)
				}
				return decoded, nil
			case "Failed":
				reason, _ := status["failureReason"].(string)
				message, _ := status["failureMessage"].(string)
				return nil, fmt.Errorf("bootstrap failed: %s - %s", reason, message)
			}
		}
	}
}

// saveKubeconfig saves the kubeconfig to ~/.butler/
func (o *Orchestrator) saveKubeconfig(clusterName string, kubeconfig []byte) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("getting home directory: %w", err)
	}

	butlerDir := filepath.Join(home, ".butler")
	if err := os.MkdirAll(butlerDir, 0700); err != nil {
		return fmt.Errorf("creating .butler directory: %w", err)
	}

	kubeconfigPath := filepath.Join(butlerDir, clusterName+"-kubeconfig")
	if err := os.WriteFile(kubeconfigPath, kubeconfig, 0600); err != nil {
		return fmt.Errorf("writing kubeconfig: %w", err)
	}

	return nil
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
			image:   "ghcr.io/butlerdotdev/butler-bootstrap:v0.1.0",
		},
		{
			name:    fmt.Sprintf("butler-provider-%s", provider),
			repoDir: filepath.Join(o.options.RepoRoot, fmt.Sprintf("butler-provider-%s", provider)),
			image:   fmt.Sprintf("ghcr.io/butlerdotdev/butler-provider-%s:v0.1.0", provider),
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
