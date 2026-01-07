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
	"os"
	"time"

	"github.com/butlerdotdev/butler/internal/common/client"
	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

const (
	// DefaultTenantNamespace is the default namespace for TenantClusters
	// when multi-tenancy is disabled or no team is specified
	DefaultTenantNamespace = "butler-tenants"

	// ButlerSystemNamespace is where platform components live
	ButlerSystemNamespace = "butler-system"

	// EnvButlerNamespace allows overriding the default namespace via environment
	EnvButlerNamespace = "BUTLER_NAMESPACE"
)

// NamespaceFlags holds namespace-related flag values
type NamespaceFlags struct {
	Namespace     string
	AllNamespaces bool
}

// AddNamespaceFlags adds namespace flags to a command
func AddNamespaceFlags(cmd *cobra.Command, flags *NamespaceFlags) {
	cmd.Flags().StringVarP(&flags.Namespace, "namespace", "n", "", "namespace to operate in (default: butler-tenants)")
	cmd.Flags().BoolVarP(&flags.AllNamespaces, "all-namespaces", "A", false, "list across all namespaces")
}

// ResolveNamespace determines which namespace(s) to use based on flags and environment
// Returns (namespace, allNamespaces)
// If allNamespaces is true, namespace is empty and caller should list across all namespaces
func (f *NamespaceFlags) ResolveNamespace() (string, bool) {
	if f.AllNamespaces {
		return "", true
	}

	if f.Namespace != "" {
		return f.Namespace, false
	}

	// Check environment variable
	if envNS := os.Getenv(EnvButlerNamespace); envNS != "" {
		return envNS, false
	}

	return DefaultTenantNamespace, false
}

// GetNestedString extracts a string from nested map fields
func GetNestedString(obj map[string]interface{}, fields ...string) string {
	val, _, _ := unstructured.NestedString(obj, fields...)
	return val
}

// GetNestedInt64 extracts an int64 from nested map fields
func GetNestedInt64(obj map[string]interface{}, fields ...string) int64 {
	val, _, _ := unstructured.NestedInt64(obj, fields...)
	return val
}

// GetNestedBool extracts a bool from nested map fields
func GetNestedBool(obj map[string]interface{}, fields ...string) bool {
	val, _, _ := unstructured.NestedBool(obj, fields...)
	return val
}

// TenantClusterInfo holds extracted information from a TenantCluster resource
// for display purposes
type TenantClusterInfo struct {
	Name              string
	Namespace         string
	Phase             string
	KubernetesVersion string
	WorkersReady      int64
	WorkersDesired    int64
	Endpoint          string
	TenantNamespace   string
	ProviderConfig    string
	CreationTime      string
}

// ExtractTenantClusterInfo extracts display information from an unstructured TenantCluster
func ExtractTenantClusterInfo(tc *unstructured.Unstructured) TenantClusterInfo {
	obj := tc.Object

	// Try to get workers from status.observedState first
	workersReady := GetNestedInt64(obj, "status", "observedState", "workers", "ready")
	workersDesired := GetNestedInt64(obj, "status", "observedState", "workers", "desired")

	// Fallback to spec.workers.replicas if status not populated
	if workersDesired == 0 {
		workersDesired = GetNestedInt64(obj, "spec", "workers", "replicas")
	}

	return TenantClusterInfo{
		Name:              tc.GetName(),
		Namespace:         tc.GetNamespace(),
		Phase:             GetNestedString(obj, "status", "phase"),
		KubernetesVersion: GetNestedString(obj, "spec", "kubernetesVersion"),
		WorkersReady:      workersReady,
		WorkersDesired:    workersDesired,
		Endpoint:          GetNestedString(obj, "status", "controlPlaneEndpoint"),
		TenantNamespace:   GetNestedString(obj, "status", "tenantNamespace"),
		ProviderConfig:    GetNestedString(obj, "spec", "providerConfigRef", "name"),
		CreationTime:      tc.GetCreationTimestamp().UTC().Format(time.RFC3339),
	}
}

// EnrichWithMachineDeploymentStatus fetches actual worker counts from MachineDeployment
// This provides accurate ready/desired counts when status.observedState isn't populated
func EnrichWithMachineDeploymentStatus(ctx context.Context, c *client.Client, info *TenantClusterInfo) {
	if info.TenantNamespace == "" {
		return
	}

	// MachineDeployment naming patterns to try:
	// 1. <cluster-name>-workers (Butler convention)
	// 2. <cluster-name>-md-0 (CAPI convention)
	mdNames := []string{
		info.Name + "-workers",
		info.Name + "-md-0",
	}

	for _, mdName := range mdNames {
		md, err := c.Dynamic.Resource(client.MachineDeploymentGVR).Namespace(info.TenantNamespace).Get(ctx, mdName, metav1.GetOptions{})
		if err != nil {
			continue // Try next name
		}

		// Get desired from spec.replicas
		specReplicas := GetNestedInt64(md.Object, "spec", "replicas")
		if specReplicas > 0 {
			info.WorkersDesired = specReplicas
		}

		// Try multiple status fields for ready count
		// CAPI MachineDeployment uses different fields than standard Deployment
		readyReplicas := GetNestedInt64(md.Object, "status", "readyReplicas")
		if readyReplicas == 0 {
			// Try availableReplicas
			readyReplicas = GetNestedInt64(md.Object, "status", "availableReplicas")
		}
		if readyReplicas == 0 {
			// Try updatedReplicas
			readyReplicas = GetNestedInt64(md.Object, "status", "updatedReplicas")
		}
		if readyReplicas == 0 {
			// Check phase - if phase is Running/Available, assume replicas are ready
			phase, _, _ := unstructured.NestedString(md.Object, "status", "phase")
			if phase == "Running" || phase == "Available" || phase == "ScaledUp" {
				replicas := GetNestedInt64(md.Object, "status", "replicas")
				if replicas > 0 {
					readyReplicas = replicas
				}
			}
		}

		info.WorkersReady = readyReplicas
		return // Found it
	}
}

// EnrichWithControlPlaneEndpoint fetches the API server endpoint from CAPI Cluster
// if not present in TenantCluster status
func EnrichWithControlPlaneEndpoint(ctx context.Context, c *client.Client, info *TenantClusterInfo) {
	if info.Endpoint != "" || info.TenantNamespace == "" {
		return // Already have endpoint or no namespace to look in
	}

	// Try to get endpoint from CAPI Cluster
	cluster, err := c.Dynamic.Resource(client.ClusterGVR).Namespace(info.TenantNamespace).Get(ctx, info.Name, metav1.GetOptions{})
	if err != nil {
		return
	}

	// Check spec.controlPlaneEndpoint first (set by controller)
	host := GetNestedString(cluster.Object, "spec", "controlPlaneEndpoint", "host")
	port := GetNestedInt64(cluster.Object, "spec", "controlPlaneEndpoint", "port")

	if host == "" {
		// Try status
		host = GetNestedString(cluster.Object, "status", "controlPlaneEndpoint", "host")
		port = GetNestedInt64(cluster.Object, "status", "controlPlaneEndpoint", "port")
	}

	if host != "" {
		if port > 0 {
			info.Endpoint = fmt.Sprintf("%s:%d", host, port)
		} else {
			info.Endpoint = host + ":6443"
		}
	}
}

// ManagementClusterError provides a helpful error when connected to wrong cluster.
type ManagementClusterError struct {
	CurrentContext string
	Reason         string
}

func (e *ManagementClusterError) Error() string {
	msg := fmt.Sprintf(`
Error: This command must be run against a Butler management cluster.

%s

Current context: %s

To fix this:
  1. List available contexts:
     kubectl config get-contexts

  2. Switch to your management cluster:
     kubectl config use-context <management-cluster-context>

  3. Retry your command

If you don't have a management cluster set up yet, run:
  butleradm bootstrap <provider> --config <config.yaml>
`, e.Reason, e.CurrentContext)
	return msg
}

// RequireManagementCluster verifies we're connected to a management cluster.
// This prevents confusing errors when users accidentally run commands against
// a tenant cluster.
//
// Detection heuristics:
//   - butler-system namespace must exist
//   - TenantCluster CRD must be registered
//   - butler-controller deployment should exist (warning if not)
func RequireManagementCluster(ctx context.Context) error {
	c, err := client.NewFromDefault()
	if err != nil {
		return fmt.Errorf("connecting to cluster: %w", err)
	}

	// Get current context for error message
	currentContext := getCurrentContext()

	// Check 1: butler-system namespace exists
	_, err = c.Clientset.CoreV1().Namespaces().Get(ctx, ButlerSystemNamespace, metav1.GetOptions{})
	if err != nil {
		return &ManagementClusterError{
			CurrentContext: currentContext,
			Reason:         fmt.Sprintf("The '%s' namespace does not exist.\nThis namespace is present on Butler management clusters.", ButlerSystemNamespace),
		}
	}

	// Check 2: TenantCluster CRD exists (try to list, if CRD doesn't exist we get an error)
	_, err = c.Dynamic.Resource(client.TenantClusterGVR).Namespace(DefaultTenantNamespace).List(ctx, metav1.ListOptions{Limit: 1})
	if err != nil {
		// Check if it's a "resource not found" type error (CRD doesn't exist)
		errStr := err.Error()
		if contains(errStr, "the server could not find the requested resource") ||
			contains(errStr, "no matches for kind") {
			return &ManagementClusterError{
				CurrentContext: currentContext,
				Reason:         "The TenantCluster CRD is not registered.\nButler CRDs are only installed on management clusters.",
			}
		}
		// Other errors (like network issues) - don't treat as wrong cluster
	}

	// Check 3: butler-controller deployment exists (soft check - just for validation)
	_, err = c.Clientset.AppsV1().Deployments(ButlerSystemNamespace).Get(ctx, "butler-controller", metav1.GetOptions{})
	if err != nil {
		// This is a soft warning - CRDs might exist from a previous install
		// but controller might not be running. Still allow the operation.
	}

	return nil
}

// getCurrentContext returns the current kubectl context name.
func getCurrentContext() string {
	// Try to get from KUBECONFIG or default location
	kubeconfigPath := os.Getenv("KUBECONFIG")
	if kubeconfigPath == "" {
		home, _ := os.UserHomeDir()
		kubeconfigPath = home + "/.kube/config"
	}

	// Read the kubeconfig to get current-context
	// For simplicity, we'll just return a placeholder if we can't determine it
	data, err := os.ReadFile(kubeconfigPath)
	if err != nil {
		return "(unknown)"
	}

	// Simple parsing - look for current-context line
	lines := string(data)
	for _, line := range splitLines(lines) {
		if contains(line, "current-context:") {
			parts := splitOnColon(line)
			if len(parts) >= 2 {
				return trimSpace(parts[1])
			}
		}
	}

	return "(unknown)"
}

// Helper functions for string operations (avoiding regex for simple parsing)
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsAt(s, substr))
}

func containsAt(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}

func splitOnColon(s string) []string {
	for i := 0; i < len(s); i++ {
		if s[i] == ':' {
			return []string{s[:i], s[i+1:]}
		}
	}
	return []string{s}
}

func trimSpace(s string) string {
	start := 0
	end := len(s)
	for start < end && (s[start] == ' ' || s[start] == '\t') {
		start++
	}
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t' || s[end-1] == '\n' || s[end-1] == '\r') {
		end--
	}
	return s[start:end]
}
