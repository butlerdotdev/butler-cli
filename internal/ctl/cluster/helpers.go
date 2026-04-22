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
	"strings"
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

	// EnvironmentLabel is the label key for ADR-009 Team Environments membership.
	EnvironmentLabel = "butler.butlerlabs.dev/environment"

	// CreatorEmailAnnotation records the authenticated user who created the
	// TenantCluster. Required by the admission webhook when the target
	// environment sets maxClustersPerMember; always audit-useful otherwise.
	// Mirrors butler-server's API-path behavior for CLI-direct (ADR-002) creates.
	CreatorEmailAnnotation = "butler.butlerlabs.dev/creator-email"
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
	Environment       string
}

// ExtractTenantClusterInfo extracts display information from an unstructured TenantCluster
func ExtractTenantClusterInfo(tc *unstructured.Unstructured) TenantClusterInfo {
	obj := tc.Object

	// Read worker counts from status
	workersReady := client.GetNestedInt64(obj, "status", "workerNodesReady")
	workersDesired := client.GetNestedInt64(obj, "status", "workerNodesDesired")

	// Fallback to spec.workers.replicas if status not populated
	if workersDesired == 0 {
		workersDesired = client.GetNestedInt64(obj, "spec", "workers", "replicas")
	}

	return TenantClusterInfo{
		Name:              tc.GetName(),
		Namespace:         tc.GetNamespace(),
		Phase:             client.GetNestedString(obj, "status", "phase"),
		KubernetesVersion: client.GetNestedString(obj, "spec", "kubernetesVersion"),
		WorkersReady:      workersReady,
		WorkersDesired:    workersDesired,
		Endpoint:          client.GetNestedString(obj, "status", "controlPlaneEndpoint"),
		TenantNamespace:   client.GetNestedString(obj, "status", "tenantNamespace"),
		ProviderConfig:    client.GetNestedString(obj, "spec", "providerConfigRef", "name"),
		CreationTime:      tc.GetCreationTimestamp().UTC().Format(time.RFC3339),
		Environment:       tc.GetLabels()[EnvironmentLabel],
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
		specReplicas := client.GetNestedInt64(md.Object, "spec", "replicas")
		if specReplicas > 0 {
			info.WorkersDesired = specReplicas
		}

		// Try multiple status fields for ready count
		// CAPI MachineDeployment uses different fields than standard Deployment
		readyReplicas := client.GetNestedInt64(md.Object, "status", "readyReplicas")
		if readyReplicas == 0 {
			// Try availableReplicas
			readyReplicas = client.GetNestedInt64(md.Object, "status", "availableReplicas")
		}
		if readyReplicas == 0 {
			// Try updatedReplicas
			readyReplicas = client.GetNestedInt64(md.Object, "status", "updatedReplicas")
		}
		if readyReplicas == 0 {
			// Check phase - if phase is Running/Available, assume replicas are ready
			phase, _, _ := unstructured.NestedString(md.Object, "status", "phase")
			if phase == "Running" || phase == "Available" || phase == "ScaledUp" {
				replicas := client.GetNestedInt64(md.Object, "status", "replicas")
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
	host := client.GetNestedString(cluster.Object, "spec", "controlPlaneEndpoint", "host")
	port := client.GetNestedInt64(cluster.Object, "spec", "controlPlaneEndpoint", "port")

	if host == "" {
		// Try status
		host = client.GetNestedString(cluster.Object, "status", "controlPlaneEndpoint", "host")
		port = client.GetNestedInt64(cluster.Object, "status", "controlPlaneEndpoint", "port")
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
		if strings.Contains(errStr, "the server could not find the requested resource") ||
			strings.Contains(errStr, "no matches for kind") {
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
	for _, line := range strings.Split(lines, "\n") {
		if strings.Contains(line, "current-context:") {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) >= 2 {
				return strings.TrimSpace(parts[1])
			}
		}
	}

	return "(unknown)"
}
