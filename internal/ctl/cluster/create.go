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
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/butlerdotdev/butler/internal/common/client"
	"github.com/butlerdotdev/butler/internal/common/log"
	"github.com/butlerdotdev/butler/internal/common/output"
	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"
)

// CreateOptions holds all options for the create command.
// Separated from the command to enable testing and reuse.
type CreateOptions struct {
	// Cluster identification
	Name      string
	Namespace string

	// Provider configuration
	Provider string

	// Machine configuration
	Workers  int32
	CPU      int32
	MemoryMB int32
	DiskGB   int32

	// OS Image (provider-specific: UUID for Nutanix, namespace/name for Harvester)
	ImageRef string

	// Kubernetes version
	KubernetesVersion string

	// Networking (optional overrides)
	PodCIDR     string
	ServiceCIDR string

	// Load balancer pool for MetalLB
	LBPoolStart string
	LBPoolEnd   string

	// Control plane (optional)
	ControlPlaneReplicas int32

	// Behavior flags
	Wait    bool
	Timeout time.Duration
	DryRun  bool

	// File-based creation
	Filename string

	// Output
	Output io.Writer
	Logger *log.Logger
}

// DefaultCreateOptions returns CreateOptions with sensible defaults.
func DefaultCreateOptions(logger *log.Logger) *CreateOptions {
	return &CreateOptions{
		Namespace:            DefaultTenantNamespace,
		Workers:              1,
		CPU:                  4,
		MemoryMB:             8192, // 8Gi
		DiskGB:               50,
		KubernetesVersion:    "v1.30.2",
		ControlPlaneReplicas: 1,
		Timeout:              15 * time.Minute,
		Output:               os.Stdout,
		Logger:               logger,
	}
}

// Validate checks that all required options are set and valid.
func (o *CreateOptions) Validate() error {
	// If using file, skip other validation
	if o.Filename != "" {
		return nil
	}

	if o.Name == "" {
		return fmt.Errorf("cluster name is required")
	}

	// Validate cluster name format (DNS-1123 subdomain)
	if !isValidClusterName(o.Name) {
		return fmt.Errorf("invalid cluster name %q: must be lowercase alphanumeric, may contain '-', max 63 chars", o.Name)
	}

	if o.Workers < 1 || o.Workers > 10 {
		return fmt.Errorf("workers must be between 1 and 10, got %d", o.Workers)
	}

	if o.CPU < 1 || o.CPU > 128 {
		return fmt.Errorf("cpu must be between 1 and 128, got %d", o.CPU)
	}

	if o.MemoryMB < 2048 {
		return fmt.Errorf("memory must be at least 2048MB (2Gi), got %dMB", o.MemoryMB)
	}

	if o.DiskGB < 20 {
		return fmt.Errorf("disk must be at least 20GB, got %dGB", o.DiskGB)
	}

	// Kubernetes version format
	if !strings.HasPrefix(o.KubernetesVersion, "v") {
		return fmt.Errorf("kubernetes version must start with 'v', got %q", o.KubernetesVersion)
	}

	// Load balancer pool is required for MetalLB
	if o.LBPoolStart == "" || o.LBPoolEnd == "" {
		return fmt.Errorf("load balancer IP pool is required; specify --lb-pool-start and --lb-pool-end (or use --lb-pool START-END)")
	}

	// Validate IP formats
	if !isValidIP(o.LBPoolStart) {
		return fmt.Errorf("invalid IP address for --lb-pool-start: %q", o.LBPoolStart)
	}
	if !isValidIP(o.LBPoolEnd) {
		return fmt.Errorf("invalid IP address for --lb-pool-end: %q", o.LBPoolEnd)
	}

	return nil
}

// isValidIP checks if a string is a valid IPv4 address.
func isValidIP(ip string) bool {
	parts := strings.Split(ip, ".")
	if len(parts) != 4 {
		return false
	}
	for _, part := range parts {
		var num int
		if _, err := fmt.Sscanf(part, "%d", &num); err != nil {
			return false
		}
		if num < 0 || num > 255 {
			return false
		}
	}
	return true
}

// isValidClusterName validates cluster name against DNS-1123 subdomain rules.
func isValidClusterName(name string) bool {
	if len(name) == 0 || len(name) > 63 {
		return false
	}
	// Must be lowercase alphanumeric, may contain '-', must start/end with alphanumeric
	pattern := regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)
	return pattern.MatchString(name)
}

// NewCreateCmd creates the cluster create command.
func NewCreateCmd(logger *log.Logger) *cobra.Command {
	opts := DefaultCreateOptions(logger)

	cmd := &cobra.Command{
		Use:   "create NAME",
		Short: "Create a new tenant cluster",
		Long: `Create a new tenant Kubernetes cluster.

Butler will provision the cluster using the specified provider configuration,
including control plane (via Steward) and worker nodes.

The --lb-pool flag (or --lb-pool-start/--lb-pool-end) is required to configure
the IP range for LoadBalancer services (MetalLB).

Examples:
  # Create a cluster with a single LoadBalancer IP
  butlerctl cluster create my-cluster --lb-pool 10.127.14.40

  # Create with an IP range for LoadBalancer services
  butlerctl cluster create my-cluster --lb-pool 10.127.14.40-10.127.14.50

  # Full production configuration
  butlerctl cluster create prod-cluster \
    --provider nutanix \
    --workers 3 \
    --cpu 8 \
    --memory 16Gi \
    --disk 100Gi \
    --lb-pool 10.127.14.100-10.127.14.110 \
    --image 41720566-c4a7-4300-a60a-b2786ebfa8bd \
    --k8s-version v1.30.2

  # Create from a YAML file
  butlerctl cluster create -f cluster.yaml

  # Create and wait for Ready status
  butlerctl cluster create my-cluster --lb-pool 10.127.14.40 --wait

  # Preview what would be created (dry-run)
  butlerctl cluster create my-cluster --lb-pool 10.127.14.40 --dry-run`,
		Args:              cobra.MaximumNArgs(1),
		ValidArgsFunction: cobra.NoFileCompletions,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Get name from args if provided
			if len(args) > 0 {
				opts.Name = args[0]
			}

			// Resolve namespace
			if ns, _ := cmd.Flags().GetString("namespace"); ns != "" {
				opts.Namespace = ns
			}

			return runCreate(cmd.Context(), opts)
		},
	}

	// Provider flags
	cmd.Flags().StringVarP(&opts.Provider, "provider", "p", "", "ProviderConfig name (auto-detected if only one exists)")

	// Machine configuration
	cmd.Flags().Int32VarP(&opts.Workers, "workers", "w", opts.Workers, "Number of worker nodes (1-10)")
	cmd.Flags().Int32Var(&opts.CPU, "cpu", opts.CPU, "CPU cores per worker (1-128)")
	cmd.Flags().StringVar(&memoryFlag, "memory", "8Gi", "Memory per worker (e.g., 8Gi, 16384Mi)")
	cmd.Flags().StringVar(&diskFlag, "disk", "50Gi", "Disk size per worker (e.g., 50Gi, 100Gi)")
	cmd.Flags().StringVar(&opts.ImageRef, "image", "", "OS image reference (UUID for Nutanix, namespace/name for Harvester)")

	// Kubernetes version
	cmd.Flags().StringVar(&opts.KubernetesVersion, "k8s-version", opts.KubernetesVersion, "Kubernetes version")

	// Networking
	cmd.Flags().StringVar(&opts.PodCIDR, "pod-cidr", "", "Pod network CIDR (default: 10.244.0.0/16)")
	cmd.Flags().StringVar(&opts.ServiceCIDR, "service-cidr", "", "Service network CIDR (default: 10.96.0.0/12)")
	cmd.Flags().StringVar(&lbPoolFlag, "lb-pool", "", "LoadBalancer IP pool (SINGLE_IP or START-END range)")
	cmd.Flags().StringVar(&opts.LBPoolStart, "lb-pool-start", "", "LoadBalancer pool start IP")
	cmd.Flags().StringVar(&opts.LBPoolEnd, "lb-pool-end", "", "LoadBalancer pool end IP")

	// Namespace
	cmd.Flags().StringVarP(&opts.Namespace, "namespace", "n", opts.Namespace, "Namespace for the TenantCluster")

	// Behavior
	cmd.Flags().BoolVar(&opts.Wait, "wait", false, "Wait for cluster to reach Ready status")
	cmd.Flags().DurationVar(&opts.Timeout, "timeout", opts.Timeout, "Timeout when using --wait")
	cmd.Flags().BoolVar(&opts.DryRun, "dry-run", false, "Preview the TenantCluster without creating it")

	// File-based
	cmd.Flags().StringVarP(&opts.Filename, "filename", "f", "", "Create from YAML file")

	return cmd
}

// Global vars for string flags that need parsing
var (
	memoryFlag string
	diskFlag   string
	lbPoolFlag string
)

// runCreate executes the create operation.
func runCreate(ctx context.Context, opts *CreateOptions) error {
	// Parse memory and disk flags
	if memoryFlag != "" {
		memMB, err := parseMemoryToMB(memoryFlag)
		if err != nil {
			return fmt.Errorf("invalid memory value %q: %w", memoryFlag, err)
		}
		opts.MemoryMB = memMB
	}
	if diskFlag != "" {
		diskGB, err := parseDiskToGB(diskFlag)
		if err != nil {
			return fmt.Errorf("invalid disk value %q: %w", diskFlag, err)
		}
		opts.DiskGB = diskGB
	}

	// Parse lb-pool flag (supports "IP" or "START-END" format)
	if lbPoolFlag != "" {
		start, end, err := parseLBPool(lbPoolFlag)
		if err != nil {
			return fmt.Errorf("invalid --lb-pool value %q: %w", lbPoolFlag, err)
		}
		opts.LBPoolStart = start
		opts.LBPoolEnd = end
	}

	// Verify we're connected to a management cluster
	if err := RequireManagementCluster(ctx); err != nil {
		return err
	}

	// Create client
	c, err := client.NewFromDefault()
	if err != nil {
		return fmt.Errorf("creating client: %w", err)
	}

	// If filename provided, create from file
	if opts.Filename != "" {
		return createFromFile(ctx, c, opts)
	}

	// Validate options
	if err := opts.Validate(); err != nil {
		return err
	}

	// Auto-detect provider if not specified
	if opts.Provider == "" {
		provider, err := autoDetectProvider(ctx, c, opts.Logger)
		if err != nil {
			return err
		}
		opts.Provider = provider
	} else {
		// Validate provider exists
		if err := validateProviderExists(ctx, c, opts.Provider); err != nil {
			return err
		}
	}

	// Build the TenantCluster resource
	tc := buildTenantCluster(opts)

	// Dry-run: just print and exit
	if opts.DryRun {
		return printDryRun(opts, tc)
	}

	// Check if cluster already exists
	_, err = c.Dynamic.Resource(client.TenantClusterGVR).Namespace(opts.Namespace).Get(ctx, opts.Name, metav1.GetOptions{})
	if err == nil {
		return fmt.Errorf("TenantCluster %q already exists in namespace %q", opts.Name, opts.Namespace)
	}
	if !errors.IsNotFound(err) {
		return fmt.Errorf("checking for existing cluster: %w", err)
	}

	// Print creation summary
	printCreationSummary(opts)

	// Create the TenantCluster
	opts.Logger.Info("creating TenantCluster", "name", opts.Name, "namespace", opts.Namespace)

	_, err = c.Dynamic.Resource(client.TenantClusterGVR).Namespace(opts.Namespace).Create(ctx, tc, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("creating TenantCluster: %w", err)
	}

	opts.Logger.Success("TenantCluster created", "name", opts.Name)

	// Wait for Ready if requested
	if opts.Wait {
		if err := waitForReady(ctx, c, opts); err != nil {
			return err
		}
	} else {
		fmt.Fprintf(opts.Output, "\nNext steps:\n")
		fmt.Fprintf(opts.Output, "  Watch progress: butlerctl cluster get %s\n", opts.Name)
		fmt.Fprintf(opts.Output, "  Get kubeconfig: butlerctl cluster kubeconfig %s --merge\n", opts.Name)
	}

	return nil
}

// autoDetectProvider finds the provider to use.
// Returns an error if no providers exist or multiple exist without --provider flag.
func autoDetectProvider(ctx context.Context, c *client.Client, logger *log.Logger) (string, error) {
	list, err := c.Dynamic.Resource(client.ProviderConfigGVR).Namespace(ButlerSystemNamespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return "", fmt.Errorf("listing ProviderConfigs: %w", err)
	}

	if len(list.Items) == 0 {
		return "", fmt.Errorf("no ProviderConfigs found in %s namespace; create one first with butleradm", ButlerSystemNamespace)
	}

	if len(list.Items) == 1 {
		name := list.Items[0].GetName()
		logger.Info("auto-detected provider", "name", name)
		return name, nil
	}

	// Multiple providers - user must specify
	names := make([]string, len(list.Items))
	for i, pc := range list.Items {
		names[i] = pc.GetName()
	}
	return "", fmt.Errorf("multiple ProviderConfigs found (%s); specify one with --provider", strings.Join(names, ", "))
}

// validateProviderExists checks that a ProviderConfig exists.
func validateProviderExists(ctx context.Context, c *client.Client, name string) error {
	_, err := c.Dynamic.Resource(client.ProviderConfigGVR).Namespace(ButlerSystemNamespace).Get(ctx, name, metav1.GetOptions{})
	if errors.IsNotFound(err) {
		return fmt.Errorf("ProviderConfig %q not found in %s namespace", name, ButlerSystemNamespace)
	}
	return err
}

// buildTenantCluster constructs the TenantCluster unstructured resource.
func buildTenantCluster(opts *CreateOptions) *unstructured.Unstructured {
	tc := &unstructured.Unstructured{}
	tc.SetAPIVersion("butler.butlerlabs.dev/v1alpha1")
	tc.SetKind("TenantCluster")
	tc.SetName(opts.Name)
	tc.SetNamespace(opts.Namespace)

	// Build machineTemplate
	machineTemplate := map[string]interface{}{
		"cpu":      int64(opts.CPU),
		"memory":   fmt.Sprintf("%dMi", opts.MemoryMB),
		"diskSize": fmt.Sprintf("%dGi", opts.DiskGB),
	}

	// Add OS imageRef if specified
	if opts.ImageRef != "" {
		machineTemplate["os"] = map[string]interface{}{
			"imageRef": opts.ImageRef,
		}
	}

	// Build spec
	spec := map[string]interface{}{
		"kubernetesVersion": opts.KubernetesVersion,
		"providerConfigRef": map[string]interface{}{
			"name": opts.Provider,
		},
		"workers": map[string]interface{}{
			"replicas":        int64(opts.Workers),
			"machineTemplate": machineTemplate,
		},
	}

	// Build networking section
	networking := map[string]interface{}{}
	if opts.PodCIDR != "" {
		networking["podCIDR"] = opts.PodCIDR
	}
	if opts.ServiceCIDR != "" {
		networking["serviceCIDR"] = opts.ServiceCIDR
	}
	// Add loadBalancerPool (required for MetalLB)
	if opts.LBPoolStart != "" && opts.LBPoolEnd != "" {
		networking["loadBalancerPool"] = map[string]interface{}{
			"start": opts.LBPoolStart,
			"end":   opts.LBPoolEnd,
		}
	}
	if len(networking) > 0 {
		spec["networking"] = networking
	}

	// Add control plane if non-default
	if opts.ControlPlaneReplicas != 1 {
		spec["controlPlane"] = map[string]interface{}{
			"replicas": int64(opts.ControlPlaneReplicas),
		}
	}

	tc.Object["spec"] = spec
	return tc
}

// printCreationSummary outputs what will be created.
func printCreationSummary(opts *CreateOptions) {
	fmt.Fprintf(opts.Output, "\nCreating TenantCluster %s:\n", output.ColorizePhase(opts.Name))
	fmt.Fprintf(opts.Output, "  Provider:    %s\n", opts.Provider)
	fmt.Fprintf(opts.Output, "  Kubernetes:  %s\n", opts.KubernetesVersion)
	fmt.Fprintf(opts.Output, "  Workers:     %d × (%d CPU, %s RAM, %s disk)\n",
		opts.Workers, opts.CPU, formatMemory(opts.MemoryMB), formatDisk(opts.DiskGB))
	if opts.LBPoolStart == opts.LBPoolEnd {
		fmt.Fprintf(opts.Output, "  LB Pool:     %s\n", opts.LBPoolStart)
	} else {
		fmt.Fprintf(opts.Output, "  LB Pool:     %s - %s\n", opts.LBPoolStart, opts.LBPoolEnd)
	}
	if opts.ImageRef != "" {
		fmt.Fprintf(opts.Output, "  Image:       %s\n", opts.ImageRef)
	}
	fmt.Fprintln(opts.Output)
}

// printDryRun outputs the YAML that would be created.
func printDryRun(opts *CreateOptions, tc *unstructured.Unstructured) error {
	fmt.Fprintf(opts.Output, "# Dry-run: TenantCluster that would be created\n")
	fmt.Fprintf(opts.Output, "# Use 'butlerctl cluster create %s' to create it\n\n", opts.Name)

	data, err := yaml.Marshal(tc.Object)
	if err != nil {
		return fmt.Errorf("marshaling to YAML: %w", err)
	}
	fmt.Fprintln(opts.Output, string(data))
	return nil
}

// waitForReady polls until the cluster reaches Ready status.
func waitForReady(ctx context.Context, c *client.Client, opts *CreateOptions) error {
	opts.Logger.Info("waiting for cluster to be Ready", "timeout", opts.Timeout)

	ctx, cancel := context.WithTimeout(ctx, opts.Timeout)
	defer cancel()

	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	startTime := time.Now()
	lastPhase := ""

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout waiting for cluster to be Ready after %v", opts.Timeout)
		case <-ticker.C:
			tc, err := c.Dynamic.Resource(client.TenantClusterGVR).Namespace(opts.Namespace).Get(ctx, opts.Name, metav1.GetOptions{})
			if err != nil {
				opts.Logger.Warn("error checking cluster status", "error", err)
				continue
			}

			phase := GetNestedString(tc.Object, "status", "phase")
			elapsed := time.Since(startTime).Round(time.Second)

			// Log phase transitions
			if phase != lastPhase {
				opts.Logger.Info("cluster phase changed", "phase", phase, "elapsed", elapsed)
				lastPhase = phase
			}

			switch phase {
			case "Ready":
				opts.Logger.Success("cluster is Ready", "elapsed", elapsed)

				// Get endpoint for display
				info := ExtractTenantClusterInfo(tc)
				EnrichWithControlPlaneEndpoint(ctx, c, &info)

				fmt.Fprintf(opts.Output, "\nCluster %s is ready!\n", opts.Name)
				if info.Endpoint != "" {
					fmt.Fprintf(opts.Output, "  API Server: %s\n", info.Endpoint)
				}
				fmt.Fprintf(opts.Output, "\nGet kubeconfig:\n")
				fmt.Fprintf(opts.Output, "  butlerctl cluster kubeconfig %s --merge\n", opts.Name)
				return nil

			case "Failed":
				// Try to get error message from conditions
				conditions, _, _ := unstructured.NestedSlice(tc.Object, "status", "conditions")
				errMsg := "unknown error"
				for _, c := range conditions {
					cond, ok := c.(map[string]interface{})
					if ok && cond["type"] == "Ready" && cond["status"] == "False" {
						if msg, ok := cond["message"].(string); ok {
							errMsg = msg
						}
						break
					}
				}
				return fmt.Errorf("cluster provisioning failed: %s", errMsg)
			}
		}
	}
}

// createFromFile creates a TenantCluster from a YAML file.
func createFromFile(ctx context.Context, c *client.Client, opts *CreateOptions) error {
	data, err := os.ReadFile(opts.Filename)
	if err != nil {
		return fmt.Errorf("reading file %s: %w", opts.Filename, err)
	}

	tc := &unstructured.Unstructured{}
	if err := yaml.Unmarshal(data, &tc.Object); err != nil {
		return fmt.Errorf("parsing YAML: %w", err)
	}

	// Validate it's a TenantCluster
	if tc.GetKind() != "TenantCluster" {
		return fmt.Errorf("expected Kind 'TenantCluster', got %q", tc.GetKind())
	}

	name := tc.GetName()
	namespace := tc.GetNamespace()
	if namespace == "" {
		namespace = opts.Namespace
		tc.SetNamespace(namespace)
	}

	if opts.DryRun {
		fmt.Fprintf(opts.Output, "# Dry-run: Would create TenantCluster from %s\n\n", opts.Filename)
		data, _ := yaml.Marshal(tc.Object)
		fmt.Fprintln(opts.Output, string(data))
		return nil
	}

	opts.Logger.Info("creating TenantCluster from file", "file", opts.Filename, "name", name, "namespace", namespace)

	_, err = c.Dynamic.Resource(client.TenantClusterGVR).Namespace(namespace).Create(ctx, tc, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("creating TenantCluster: %w", err)
	}

	opts.Logger.Success("TenantCluster created from file", "name", name)

	if opts.Wait {
		opts.Name = name
		opts.Namespace = namespace
		return waitForReady(ctx, c, opts)
	}

	return nil
}

// parseMemoryToMB converts memory strings like "8Gi" or "8192Mi" to MB.
func parseMemoryToMB(s string) (int32, error) {
	s = strings.TrimSpace(s)

	if strings.HasSuffix(s, "Gi") {
		val := strings.TrimSuffix(s, "Gi")
		var gi int32
		if _, err := fmt.Sscanf(val, "%d", &gi); err != nil {
			return 0, fmt.Errorf("invalid Gi value: %s", val)
		}
		return gi * 1024, nil
	}

	if strings.HasSuffix(s, "Mi") {
		val := strings.TrimSuffix(s, "Mi")
		var mi int32
		if _, err := fmt.Sscanf(val, "%d", &mi); err != nil {
			return 0, fmt.Errorf("invalid Mi value: %s", val)
		}
		return mi, nil
	}

	// Try parsing as plain number (assume MB)
	var mb int32
	if _, err := fmt.Sscanf(s, "%d", &mb); err != nil {
		return 0, fmt.Errorf("must specify unit (e.g., 8Gi or 8192Mi)")
	}
	return mb, nil
}

// parseDiskToGB converts disk strings like "50Gi" to GB.
func parseDiskToGB(s string) (int32, error) {
	s = strings.TrimSpace(s)

	if strings.HasSuffix(s, "Gi") {
		val := strings.TrimSuffix(s, "Gi")
		var gi int32
		if _, err := fmt.Sscanf(val, "%d", &gi); err != nil {
			return 0, fmt.Errorf("invalid Gi value: %s", val)
		}
		return gi, nil
	}

	if strings.HasSuffix(s, "Ti") {
		val := strings.TrimSuffix(s, "Ti")
		var ti int32
		if _, err := fmt.Sscanf(val, "%d", &ti); err != nil {
			return 0, fmt.Errorf("invalid Ti value: %s", val)
		}
		return ti * 1024, nil
	}

	// Try parsing as plain number (assume GB)
	var gb int32
	if _, err := fmt.Sscanf(s, "%d", &gb); err != nil {
		return 0, fmt.Errorf("must specify unit (e.g., 50Gi)")
	}
	return gb, nil
}

// formatMemory formats MB to human-readable string.
func formatMemory(mb int32) string {
	if mb >= 1024 && mb%1024 == 0 {
		return fmt.Sprintf("%dGi", mb/1024)
	}
	return fmt.Sprintf("%dMi", mb)
}

// formatDisk formats GB to human-readable string.
func formatDisk(gb int32) string {
	if gb >= 1024 && gb%1024 == 0 {
		return fmt.Sprintf("%dTi", gb/1024)
	}
	return fmt.Sprintf("%dGi", gb)
}

// parseLBPool parses the --lb-pool flag.
// Accepts either a single IP ("10.127.14.40") or a range ("10.127.14.40-10.127.14.50").
func parseLBPool(s string) (start, end string, err error) {
	s = strings.TrimSpace(s)

	// Check for range format (START-END)
	if strings.Contains(s, "-") {
		parts := strings.SplitN(s, "-", 2)
		if len(parts) != 2 {
			return "", "", fmt.Errorf("invalid range format, expected START-END")
		}
		start = strings.TrimSpace(parts[0])
		end = strings.TrimSpace(parts[1])

		if !isValidIP(start) {
			return "", "", fmt.Errorf("invalid start IP: %s", start)
		}
		if !isValidIP(end) {
			return "", "", fmt.Errorf("invalid end IP: %s", end)
		}
		return start, end, nil
	}

	// Single IP - use same for start and end
	if !isValidIP(s) {
		return "", "", fmt.Errorf("invalid IP address: %s", s)
	}
	return s, s, nil
}
