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

// Package status implements the butleradm status command.
package status

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/butlerdotdev/butler/internal/common/client"
	"github.com/butlerdotdev/butler/internal/common/log"
	"github.com/butlerdotdev/butler/internal/common/output"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

const (
	butlerSystem    = "butler-system"
	butlerTenants   = "butler-tenants"
	capiSystem      = "capi-system"
	certManager     = "cert-manager"
	longhornSystem  = "longhorn-system"
	metallbSystem   = "metallb-system"
	ciliumNamespace = "kube-system"
	fluxSystem      = "flux-system"
)

// Styles for status output
var (
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("4")).
			MarginBottom(1)

	sectionStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("6"))

	okStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	warnStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("3"))
	errorStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
	pendingStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
)

type statusOptions struct {
	kubeconfig string
	wide       bool
}

// NewStatusCmd creates the status command
func NewStatusCmd(logger *log.Logger) *cobra.Command {
	opts := &statusOptions{}

	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show Butler platform status",
		Long: `Display the health and status of the Butler platform.

Shows the status of:
  • Butler controllers (butler-controller, butler-bootstrap)
  • CAPI providers (capk, capx, capmox)
  • Infrastructure addons (Steward, Cilium, Longhorn, MetalLB, cert-manager)
  • GitOps components (Flux)
  • Provider configurations
  • Tenant cluster summary

The command automatically looks for kubeconfigs in ~/.butler/ if not specified.

Examples:
  # Check status using default kubeconfig discovery
  butleradm status

  # Check status of a specific management cluster
  butleradm status --kubeconfig ~/.butler/butler-ntnx-kubeconfig

  # Show detailed status
  butleradm status --wide`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runStatus(cmd.Context(), logger, opts)
		},
	}

	cmd.Flags().StringVar(&opts.kubeconfig, "kubeconfig", "", "path to management cluster kubeconfig")
	cmd.Flags().BoolVar(&opts.wide, "wide", false, "show detailed status")

	return cmd
}

func runStatus(ctx context.Context, logger *log.Logger, opts *statusOptions) error {
	// Resolve kubeconfig
	kubeconfigPath := opts.kubeconfig
	if kubeconfigPath == "" {
		kubeconfigPath = findButlerKubeconfig()
	}

	if kubeconfigPath == "" {
		return fmt.Errorf("no kubeconfig found - specify with --kubeconfig or ensure ~/.butler/ contains kubeconfig files")
	}

	// Connect to cluster
	c, err := client.NewFromKubeconfig(kubeconfigPath)
	if err != nil {
		return fmt.Errorf("connecting to cluster: %w", err)
	}

	// Get cluster info
	serverVersion, err := c.Clientset.Discovery().ServerVersion()
	if err != nil {
		return fmt.Errorf("getting server version: %w", err)
	}

	// Extract cluster name from kubeconfig path
	clusterName := extractClusterName(kubeconfigPath)

	// Print header
	if output.IsTTY() {
		fmt.Println(titleStyle.Render("Butler Platform Status"))
		fmt.Println(strings.Repeat("═", 50))
	} else {
		fmt.Println("Butler Platform Status")
		fmt.Println(strings.Repeat("=", 50))
	}
	fmt.Println()

	// Basic info
	fmt.Printf("Management Cluster: %s\n", clusterName)
	fmt.Printf("Kubernetes Version: %s\n", serverVersion.GitVersion)
	fmt.Printf("Kubeconfig: %s\n", kubeconfigPath)
	fmt.Println()

	// Check components
	printSection("Butler Components")
	checkDeployment(ctx, c, butlerSystem, "butler-controller", "Butler Controller")
	checkDeployment(ctx, c, capiSystem, "capi-controller-manager", "CAPI Core")

	// CAPI providers - check common naming patterns
	checkCAPIProvider(ctx, c, "nutanix", []providerCheck{
		{"capx-system", "capx-controller-manager"},
		{"capx-system", "controller-manager"},
		{capiSystem, "capx-controller-manager"},
		{"nutanix-system", "controller-manager"},
	})
	checkCAPIProvider(ctx, c, "harvester", []providerCheck{
		{"capi-harvester-system", "capi-harvester-controller-manager"},
		{capiSystem, "capi-harvester-controller-manager"},
	})
	checkCAPIProvider(ctx, c, "kubevirt", []providerCheck{
		{"capk-system", "capk-controller-manager"},
		{capiSystem, "capk-controller-manager"},
	})

	checkDeployment(ctx, c, "steward-system", "steward", "Steward")
	fmt.Println()

	// Check infrastructure
	printSection("Infrastructure Addons")
	checkDeployment(ctx, c, certManager, "cert-manager", "cert-manager")
	checkDeployment(ctx, c, certManager, "cert-manager-webhook", "cert-manager webhook")
	checkDaemonSet(ctx, c, ciliumNamespace, "cilium", "Cilium")
	checkDeployment(ctx, c, ciliumNamespace, "cilium-operator", "Cilium Operator")
	checkDeployment(ctx, c, longhornSystem, "longhorn-driver-deployer", "Longhorn")

	// MetalLB - check various naming patterns
	if hasDeployment(ctx, c, metallbSystem, "controller") || hasDeployment(ctx, c, metallbSystem, "metallb-controller") {
		checkDeploymentPatterns(ctx, c, metallbSystem, []string{"metallb-controller", "controller"}, "MetalLB Controller")
		checkDaemonSetPatterns(ctx, c, metallbSystem, []string{"metallb-speaker", "speaker"}, "MetalLB Speaker")
	}
	fmt.Println()

	// Check GitOps - only show if Flux is installed
	if hasNamespace(ctx, c, fluxSystem) {
		printSection("GitOps")
		checkDeployment(ctx, c, fluxSystem, "source-controller", "Flux Source")
		checkDeployment(ctx, c, fluxSystem, "kustomize-controller", "Flux Kustomize")
		checkDeployment(ctx, c, fluxSystem, "helm-controller", "Flux Helm")
		checkDeployment(ctx, c, fluxSystem, "notification-controller", "Flux Notification")
		fmt.Println()
	}

	// Check ProviderConfigs
	printSection("Provider Configs")
	if err := listProviderConfigs(ctx, c); err != nil {
		fmt.Printf("  %s Error listing ProviderConfigs: %v\n", statusIcon("error"), err)
	}
	fmt.Println()

	// Check TenantClusters
	printSection("Tenant Clusters")
	if err := summarizeTenantClusters(ctx, c); err != nil {
		fmt.Printf("  %s Error listing TenantClusters: %v\n", statusIcon("error"), err)
	}

	return nil
}

func findButlerKubeconfig() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}

	butlerDir := filepath.Join(home, ".butler")

	// Look for files ending in -kubeconfig
	entries, err := os.ReadDir(butlerDir)
	if err != nil {
		return ""
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasSuffix(name, "-kubeconfig") {
			return filepath.Join(butlerDir, name)
		}
	}

	// Try just "kubeconfig" if no suffixed files found
	kubeconfig := filepath.Join(butlerDir, "kubeconfig")
	if _, err := os.Stat(kubeconfig); err == nil {
		return kubeconfig
	}

	return ""
}

func extractClusterName(kubeconfigPath string) string {
	base := filepath.Base(kubeconfigPath)
	// Remove -kubeconfig suffix
	name := strings.TrimSuffix(base, "-kubeconfig")
	// Remove .yaml/.yml suffix
	name = strings.TrimSuffix(name, ".yaml")
	name = strings.TrimSuffix(name, ".yml")
	return name
}

func printSection(name string) {
	if output.IsTTY() {
		fmt.Println(sectionStyle.Render(name + ":"))
	} else {
		fmt.Println(name + ":")
	}
}

// hasDeployment returns true if a deployment exists (doesn't check readiness)
func hasDeployment(ctx context.Context, c *client.Client, namespace, name string) bool {
	_, err := c.Clientset.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
	return err == nil
}

// hasNamespace returns true if a namespace exists
func hasNamespace(ctx context.Context, c *client.Client, name string) bool {
	_, err := c.Clientset.CoreV1().Namespaces().Get(ctx, name, metav1.GetOptions{})
	return err == nil
}

// checkDeploymentPatterns checks multiple possible deployment names
func checkDeploymentPatterns(ctx context.Context, c *client.Client, namespace string, names []string, displayName string) {
	for _, name := range names {
		deploy, err := c.Clientset.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			continue
		}

		ready := deploy.Status.ReadyReplicas
		desired := *deploy.Spec.Replicas

		var status string
		var icon string
		if ready >= desired && desired > 0 {
			status = okStyle.Render(fmt.Sprintf("%d/%d ready", ready, desired))
			icon = statusIcon("ok")
		} else if ready > 0 {
			status = warnStyle.Render(fmt.Sprintf("%d/%d ready", ready, desired))
			icon = statusIcon("warn")
		} else {
			status = errorStyle.Render(fmt.Sprintf("%d/%d ready", ready, desired))
			icon = statusIcon("error")
		}

		fmt.Printf("  %s %-25s %s\n", icon, displayName, status)
		return
	}
	// Not found
	fmt.Printf("  %s %-25s %s\n", statusIcon("missing"), displayName, pendingStyle.Render("not found"))
}

// checkDaemonSetPatterns checks multiple possible daemonset names
func checkDaemonSetPatterns(ctx context.Context, c *client.Client, namespace string, names []string, displayName string) {
	for _, name := range names {
		ds, err := c.Clientset.AppsV1().DaemonSets(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			continue
		}

		ready := ds.Status.NumberReady
		desired := ds.Status.DesiredNumberScheduled

		var status string
		var icon string
		if ready >= desired && desired > 0 {
			status = okStyle.Render(fmt.Sprintf("%d/%d ready", ready, desired))
			icon = statusIcon("ok")
		} else if ready > 0 {
			status = warnStyle.Render(fmt.Sprintf("%d/%d ready", ready, desired))
			icon = statusIcon("warn")
		} else {
			status = errorStyle.Render(fmt.Sprintf("%d/%d ready", ready, desired))
			icon = statusIcon("error")
		}

		fmt.Printf("  %s %-25s %s\n", icon, displayName, status)
		return
	}
	// Not found
	fmt.Printf("  %s %-25s %s\n", statusIcon("missing"), displayName, pendingStyle.Render("not found"))
}

func checkDeployment(ctx context.Context, c *client.Client, namespace, name, displayName string) {
	deploy, err := c.Clientset.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		fmt.Printf("  %s %-25s %s\n", statusIcon("missing"), displayName, pendingStyle.Render("not found"))
		return
	}

	ready := deploy.Status.ReadyReplicas
	desired := *deploy.Spec.Replicas

	var status string
	var icon string
	if ready >= desired && desired > 0 {
		status = okStyle.Render(fmt.Sprintf("%d/%d ready", ready, desired))
		icon = statusIcon("ok")
	} else if ready > 0 {
		status = warnStyle.Render(fmt.Sprintf("%d/%d ready", ready, desired))
		icon = statusIcon("warn")
	} else {
		status = errorStyle.Render(fmt.Sprintf("%d/%d ready", ready, desired))
		icon = statusIcon("error")
	}

	fmt.Printf("  %s %-25s %s\n", icon, displayName, status)
}

func checkDaemonSet(ctx context.Context, c *client.Client, namespace, name, displayName string) {
	ds, err := c.Clientset.AppsV1().DaemonSets(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		fmt.Printf("  %s %-25s %s\n", statusIcon("missing"), displayName, pendingStyle.Render("not found"))
		return
	}

	ready := ds.Status.NumberReady
	desired := ds.Status.DesiredNumberScheduled

	var status string
	var icon string
	if ready >= desired && desired > 0 {
		status = okStyle.Render(fmt.Sprintf("%d/%d ready", ready, desired))
		icon = statusIcon("ok")
	} else if ready > 0 {
		status = warnStyle.Render(fmt.Sprintf("%d/%d ready", ready, desired))
		icon = statusIcon("warn")
	} else {
		status = errorStyle.Render(fmt.Sprintf("%d/%d ready", ready, desired))
		icon = statusIcon("error")
	}

	fmt.Printf("  %s %-25s %s\n", icon, displayName, status)
}

// providerCheck defines a namespace/deployment pair to check
type providerCheck struct {
	namespace  string
	deployment string
}

// checkCAPIProvider checks multiple possible locations for a CAPI provider
func checkCAPIProvider(ctx context.Context, c *client.Client, providerName string, checks []providerCheck) {
	// Map provider names to display names
	displayNames := map[string]string{
		"nutanix":   "CAPI Nutanix",
		"harvester": "CAPI Harvester",
		"kubevirt":  "CAPI KubeVirt",
		"proxmox":   "CAPI Proxmox",
	}
	displayName := displayNames[providerName]
	if displayName == "" {
		displayName = "CAPI " + providerName
	}

	for _, check := range checks {
		deploy, err := c.Clientset.AppsV1().Deployments(check.namespace).Get(ctx, check.deployment, metav1.GetOptions{})
		if err != nil {
			continue // Try next location
		}

		ready := deploy.Status.ReadyReplicas
		desired := *deploy.Spec.Replicas

		var status string
		var icon string
		if ready >= desired && desired > 0 {
			status = okStyle.Render(fmt.Sprintf("%d/%d ready", ready, desired))
			icon = statusIcon("ok")
		} else if ready > 0 {
			status = warnStyle.Render(fmt.Sprintf("%d/%d ready", ready, desired))
			icon = statusIcon("warn")
		} else {
			status = errorStyle.Render(fmt.Sprintf("%d/%d ready", ready, desired))
			icon = statusIcon("error")
		}

		fmt.Printf("  %s %-25s %s\n", icon, displayName, status)
		return
	}

	// Not found in any location - that's OK, provider might not be installed
	// Only print if we expect it based on ProviderConfigs
}

func listProviderConfigs(ctx context.Context, c *client.Client) error {
	list, err := c.Dynamic.Resource(client.ProviderConfigGVR).Namespace(butlerSystem).List(ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}

	if len(list.Items) == 0 {
		fmt.Printf("  %s No ProviderConfigs found\n", statusIcon("warn"))
		return nil
	}

	for _, pc := range list.Items {
		name := pc.GetName()
		provider, _, _ := unstructured.NestedString(pc.Object, "spec", "provider")
		validated, _, _ := unstructured.NestedBool(pc.Object, "status", "validated")

		var status string
		var icon string
		if validated {
			status = okStyle.Render("validated")
			icon = statusIcon("ok")
		} else {
			status = warnStyle.Render("not validated")
			icon = statusIcon("warn")
		}

		// Get endpoint for display
		var endpoint string
		switch provider {
		case "nutanix":
			endpoint, _, _ = unstructured.NestedString(pc.Object, "spec", "nutanix", "endpoint")
		case "harvester":
			endpoint = "(in-cluster)"
		}

		if endpoint != "" {
			fmt.Printf("  %s %-15s %-10s %s  endpoint: %s\n", icon, name, provider, status, endpoint)
		} else {
			fmt.Printf("  %s %-15s %-10s %s\n", icon, name, provider, status)
		}
	}

	return nil
}

func summarizeTenantClusters(ctx context.Context, c *client.Client) error {
	// List across all namespaces
	tcGVR := schema.GroupVersionResource{
		Group:    "butler.butlerlabs.dev",
		Version:  "v1alpha1",
		Resource: "tenantclusters",
	}

	list, err := c.Dynamic.Resource(tcGVR).List(ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}

	if len(list.Items) == 0 {
		fmt.Printf("  No tenant clusters found\n")
		return nil
	}

	// Count by phase
	phases := make(map[string]int)
	for _, tc := range list.Items {
		phase, _, _ := unstructured.NestedString(tc.Object, "status", "phase")
		if phase == "" {
			phase = "Unknown"
		}
		phases[phase]++
	}

	// Print summary
	total := len(list.Items)
	ready := phases["Ready"]
	provisioning := phases["Provisioning"] + phases["Installing"]
	failed := phases["Failed"]

	fmt.Printf("  Total: %d", total)
	if ready > 0 {
		fmt.Printf(" | %s", okStyle.Render(fmt.Sprintf("Ready: %d", ready)))
	}
	if provisioning > 0 {
		fmt.Printf(" | %s", warnStyle.Render(fmt.Sprintf("Provisioning: %d", provisioning)))
	}
	if failed > 0 {
		fmt.Printf(" | %s", errorStyle.Render(fmt.Sprintf("Failed: %d", failed)))
	}
	fmt.Println()

	// List clusters
	for _, tc := range list.Items {
		name := tc.GetName()
		namespace := tc.GetNamespace()
		phase, _, _ := unstructured.NestedString(tc.Object, "status", "phase")

		icon := statusIcon(strings.ToLower(phase))
		phaseStr := formatPhase(phase)

		fmt.Printf("    %s %s/%s: %s\n", icon, namespace, name, phaseStr)
	}

	return nil
}

func statusIcon(status string) string {
	if !output.IsTTY() {
		switch status {
		case "ok", "ready":
			return "[✓]"
		case "warn", "provisioning", "installing":
			return "[!]"
		case "error", "failed":
			return "[✗]"
		default:
			return "[○]"
		}
	}

	switch status {
	case "ok", "ready":
		return okStyle.Render("✓")
	case "warn", "provisioning", "installing":
		return warnStyle.Render("!")
	case "error", "failed":
		return errorStyle.Render("✗")
	case "missing":
		return pendingStyle.Render("-")
	default:
		return pendingStyle.Render("○")
	}
}

func formatPhase(phase string) string {
	if !output.IsTTY() {
		return phase
	}

	switch strings.ToLower(phase) {
	case "ready":
		return okStyle.Render(phase)
	case "provisioning", "installing":
		return warnStyle.Render(phase)
	case "failed":
		return errorStyle.Render(phase)
	default:
		return pendingStyle.Render(phase)
	}
}
