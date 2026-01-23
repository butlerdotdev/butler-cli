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

// Package cluster implements butlerctl cluster commands.
package cluster

import (
	"context"
	"fmt"
	"time"

	"github.com/butlerdotdev/butler/internal/common/client"
	"github.com/butlerdotdev/butler/internal/common/log"
	"github.com/spf13/cobra"
)

// NewClusterCmd creates the cluster parent command
func NewClusterCmd(logger *log.Logger) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cluster",
		Short: "Manage tenant Kubernetes clusters",
		Long: `Manage tenant Kubernetes clusters on the Butler platform.

Tenant clusters are isolated Kubernetes environments for your workloads.
Butler uses Steward for hosted control planes (resource-efficient) with
worker nodes running on your infrastructure provider.

Commands:
  create      Create a new tenant cluster
  list        List all tenant clusters
  get         Get details of a specific cluster
  scale       Scale worker node count
  export      Export cluster config as clean YAML
  kubeconfig  Download kubeconfig for cluster access
  destroy     Permanently destroy a cluster

Examples:
  # Create a new cluster
  butlerctl cluster create my-cluster --lb-pool 10.127.14.40

  # List all clusters
  butlerctl cluster list

  # Scale workers
  butlerctl cluster scale my-cluster --workers 3

  # Export for GitOps
  butlerctl cluster export my-cluster -o my-cluster.yaml

  # Get kubeconfig
  butlerctl cluster kubeconfig my-cluster --merge

  # Destroy a cluster
  butlerctl cluster destroy my-cluster`,
	}

	// Register subcommands
	cmd.AddCommand(newListCmd(logger))
	cmd.AddCommand(NewCreateCmd(logger))
	cmd.AddCommand(NewScaleCmd(logger))
	cmd.AddCommand(NewExportCmd(logger))
	cmd.AddCommand(newKubeconfigCmd(logger))
	cmd.AddCommand(newGetCmd(logger))
	cmd.AddCommand(NewDestroyCmd(logger))

	return cmd
}

// newGetCmd creates the cluster get command
func newGetCmd(logger *log.Logger) *cobra.Command {
	var (
		namespace    string
		outputFormat string
		kubeconfig   string
	)

	cmd := &cobra.Command{
		Use:   "get NAME",
		Short: "Get details of a tenant cluster",
		Long: `Get detailed information about a specific tenant cluster.

Displays cluster configuration, status, worker nodes, and installed addons.

Examples:
  # Get cluster details
  butlerctl cluster get my-cluster

  # Get cluster in a specific namespace
  butlerctl cluster get my-cluster -n team-payments

  # Output as YAML
  butlerctl cluster get my-cluster -o yaml`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runGet(cmd.Context(), logger, args[0], namespace, outputFormat, kubeconfig)
		},
	}

	cmd.Flags().StringVarP(&namespace, "namespace", "n", DefaultTenantNamespace, "namespace of the TenantCluster")
	cmd.Flags().StringVarP(&outputFormat, "output", "o", "", "output format (yaml, json)")
	cmd.Flags().StringVar(&kubeconfig, "kubeconfig", "", "path to management cluster kubeconfig")

	return cmd
}

func runGet(ctx context.Context, logger *log.Logger, name, namespace, outputFormat, kubeconfigPath string) error {
	// Connect to management cluster
	var c *client.Client
	var err error
	if kubeconfigPath != "" {
		c, err = client.NewFromKubeconfig(kubeconfigPath)
	} else {
		c, err = client.NewFromDefault()
	}
	if err != nil {
		return fmt.Errorf("connecting to management cluster: %w", err)
	}

	// Get TenantCluster
	tc, err := c.GetTenantCluster(ctx, namespace, name)
	if err != nil {
		return fmt.Errorf("getting TenantCluster %s/%s: %w", namespace, name, err)
	}

	// For YAML/JSON output, print the raw resource
	if outputFormat == "yaml" || outputFormat == "json" {
		// TODO: Implement proper yaml/json output
		fmt.Printf("Output format %s not yet implemented\n", outputFormat)
		return nil
	}

	// Extract info
	info := ExtractTenantClusterInfo(tc)

	// Format age
	var age string
	if info.CreationTime != "" {
		t, err := time.Parse(time.RFC3339, info.CreationTime)
		if err == nil {
			duration := time.Since(t)
			if duration < time.Hour {
				age = fmt.Sprintf("%dm", int(duration.Minutes()))
			} else if duration < 24*time.Hour {
				age = fmt.Sprintf("%dh", int(duration.Hours()))
			} else {
				age = fmt.Sprintf("%dd", int(duration.Hours()/24))
			}
		}
	}

	// Print details
	fmt.Printf("Name:             %s\n", info.Name)
	fmt.Printf("Namespace:        %s\n", info.Namespace)
	fmt.Printf("Phase:            %s\n", info.Phase)
	fmt.Printf("K8s Version:      %s\n", info.KubernetesVersion)
	fmt.Printf("Workers:          %d/%d Ready\n", info.WorkersReady, info.WorkersDesired)
	fmt.Printf("Endpoint:         %s\n", orDefault(info.Endpoint, "<pending>"))
	fmt.Printf("Tenant Namespace: %s\n", orDefault(info.TenantNamespace, "<pending>"))
	fmt.Printf("Provider Config:  %s\n", orDefault(info.ProviderConfig, "<default>"))
	fmt.Printf("Age:              %s\n", orDefault(age, "<unknown>"))

	// Print conditions if available
	conditions, found, _ := unstructuredNestedSlice(tc.Object, "status", "conditions")
	if found && len(conditions) > 0 {
		fmt.Println("\nConditions:")
		for _, c := range conditions {
			cond, ok := c.(map[string]interface{})
			if !ok {
				continue
			}
			condType := GetNestedString(cond, "type")
			status := GetNestedString(cond, "status")
			reason := GetNestedString(cond, "reason")
			fmt.Printf("  %s: %s (%s)\n", condType, status, reason)
		}
	}

	// Print addons if available
	addons, found, _ := unstructuredNestedSlice(tc.Object, "status", "observedState", "addons")
	if found && len(addons) > 0 {
		fmt.Println("\nAddons:")
		for _, a := range addons {
			addon, ok := a.(map[string]interface{})
			if !ok {
				continue
			}
			name := GetNestedString(addon, "name")
			version := GetNestedString(addon, "version")
			status := GetNestedString(addon, "status")
			fmt.Printf("  %s: %s (%s)\n", name, version, status)
		}
	}

	return nil
}

// Helper functions

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

func unstructuredNestedSlice(obj map[string]interface{}, fields ...string) ([]interface{}, bool, error) {
	val, found, err := nestedFieldNoCopy(obj, fields...)
	if !found || err != nil {
		return nil, found, err
	}
	slice, ok := val.([]interface{})
	if !ok {
		return nil, false, fmt.Errorf("value is not a slice")
	}
	return slice, true, nil
}

func nestedFieldNoCopy(obj map[string]interface{}, fields ...string) (interface{}, bool, error) {
	var val interface{} = obj
	for _, field := range fields {
		m, ok := val.(map[string]interface{})
		if !ok {
			return nil, false, nil
		}
		val, ok = m[field]
		if !ok {
			return nil, false, nil
		}
	}
	return val, true, nil
}
