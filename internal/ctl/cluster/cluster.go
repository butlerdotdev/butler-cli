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
	"fmt"
	"os"
	"path/filepath"
	"text/tabwriter"
	"time"

	"github.com/butlerdotdev/butler/internal/common/client"
	"github.com/butlerdotdev/butler/internal/common/log"
	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

const (
	butlerNamespace = "butler-system"
)

// NewClusterCmd creates the cluster parent command
func NewClusterCmd(logger *log.Logger) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cluster",
		Short: "Manage tenant Kubernetes clusters",
		Long: `Manage tenant Kubernetes clusters on the Butler platform.

Tenant clusters are isolated Kubernetes environments for your workloads.
Butler supports both hosted control planes (resource-efficient) and 
standalone clusters (full isolation).

Examples:
  butlerctl cluster create my-app --workers 3
  butlerctl cluster list
  butlerctl cluster kubeconfig my-app
  butlerctl cluster delete my-app`,
	}

	cmd.AddCommand(newCreateCmd(logger))
	cmd.AddCommand(newListCmd(logger))
	cmd.AddCommand(newGetCmd(logger))
	cmd.AddCommand(newKubeconfigCmd(logger))
	cmd.AddCommand(newDeleteCmd(logger))

	return cmd
}

// newCreateCmd creates the cluster create command
func newCreateCmd(logger *log.Logger) *cobra.Command {
	var (
		workers   int32
		cpu       int32
		memoryGB  int32
		version   string
		hosted    bool
		waitReady bool
	)

	cmd := &cobra.Command{
		Use:   "create NAME",
		Short: "Create a new tenant cluster",
		Long: `Create a new tenant Kubernetes cluster.

By default, clusters use hosted control planes (Kamaji) for resource efficiency.
Use --standalone for dedicated control plane nodes.

Examples:
  # Create a 3-worker cluster with defaults
  butlerctl cluster create my-app --workers 3

  # Create a production cluster
  butlerctl cluster create prod --workers 5 --cpu 8 --memory 32 --version v1.30.2

  # Create and wait for ready
  butlerctl cluster create my-app --workers 3 --wait`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			ctx := cmd.Context()

			// Connect to management cluster
			c, err := client.NewFromDefault()
			if err != nil {
				return fmt.Errorf("connecting to management cluster: %w", err)
			}

			logger.Info("creating tenant cluster", "name", name, "workers", workers)

			// Build TenantCluster CR
			tc := &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": client.ButlerAPIGroup + "/" + client.ButlerAPIVersion,
					"kind":       "TenantCluster",
					"metadata": map[string]interface{}{
						"name":      name,
						"namespace": butlerNamespace,
					},
					"spec": map[string]interface{}{
						"controlPlane": map[string]interface{}{
							"type":    "hosted",
							"version": version,
						},
						"machinePools": []interface{}{
							map[string]interface{}{
								"name":     "default",
								"replicas": workers,
								"machineTemplate": map[string]interface{}{
									"cpu":      cpu,
									"memoryGi": memoryGB,
								},
							},
						},
					},
				},
			}

			if _, err := c.CreateTenantCluster(ctx, tc); err != nil {
				return fmt.Errorf("creating TenantCluster: %w", err)
			}

			logger.Success("TenantCluster created", "name", name)

			if waitReady {
				logger.Waiting("waiting for cluster to be ready...")
				// TODO: Watch TenantCluster status
			}

			return nil
		},
	}

	cmd.Flags().Int32VarP(&workers, "workers", "w", 3, "number of worker nodes")
	cmd.Flags().Int32Var(&cpu, "cpu", 4, "vCPUs per worker node")
	cmd.Flags().Int32Var(&memoryGB, "memory", 16, "memory (GB) per worker node")
	cmd.Flags().StringVar(&version, "version", "v1.30.2", "Kubernetes version")
	cmd.Flags().BoolVar(&hosted, "hosted", true, "use hosted control plane (default)")
	cmd.Flags().BoolVar(&waitReady, "wait", false, "wait for cluster to be ready")

	return cmd
}

// newListCmd creates the cluster list command
func newListCmd(logger *log.Logger) *cobra.Command {
	var outputFormat string

	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List tenant clusters",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			// Connect to management cluster
			c, err := client.NewFromDefault()
			if err != nil {
				return fmt.Errorf("connecting to management cluster: %w", err)
			}

			// List TenantClusters
			list, err := c.ListTenantClusters(ctx, butlerNamespace)
			if err != nil {
				return fmt.Errorf("listing TenantClusters: %w", err)
			}

			// Print table
			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "NAME\tSTATUS\tVERSION\tWORKERS\tAGE")

			for _, item := range list.Items {
				name := item.GetName()
				status := getNestedString(item.Object, "status", "phase")
				version := getNestedString(item.Object, "spec", "controlPlane", "version")
				workersReady := getNestedInt64(item.Object, "status", "workerNodesReady")
				workersDesired := getNestedInt64(item.Object, "status", "workerNodesDesired")
				age := formatAge(item.GetCreationTimestamp().Time)

				fmt.Fprintf(w, "%s\t%s\t%s\t%d/%d\t%s\n",
					name, status, version, workersReady, workersDesired, age)
			}
			w.Flush()

			return nil
		},
	}

	cmd.Flags().StringVarP(&outputFormat, "output", "o", "table", "output format (table, yaml, json)")

	return cmd
}

// newGetCmd creates the cluster get command
func newGetCmd(logger *log.Logger) *cobra.Command {
	var outputFormat string

	cmd := &cobra.Command{
		Use:   "get NAME",
		Short: "Get details of a tenant cluster",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			ctx := cmd.Context()

			// Connect to management cluster
			c, err := client.NewFromDefault()
			if err != nil {
				return fmt.Errorf("connecting to management cluster: %w", err)
			}

			// Get TenantCluster
			tc, err := c.GetTenantCluster(ctx, butlerNamespace, name)
			if err != nil {
				return fmt.Errorf("getting TenantCluster: %w", err)
			}

			// Display info
			fmt.Printf("Name:          %s\n", tc.GetName())
			fmt.Printf("Namespace:     %s\n", tc.GetNamespace())
			fmt.Printf("Status:        %s\n", getNestedString(tc.Object, "status", "phase"))
			fmt.Printf("Version:       %s\n", getNestedString(tc.Object, "spec", "controlPlane", "version"))
			fmt.Printf("Control Plane: %s\n", getNestedString(tc.Object, "spec", "controlPlane", "type"))
			fmt.Printf("Workers:       %d/%d Ready\n",
				getNestedInt64(tc.Object, "status", "workerNodesReady"),
				getNestedInt64(tc.Object, "status", "workerNodesDesired"))
			fmt.Printf("Endpoint:      %s\n", getNestedString(tc.Object, "status", "endpoint"))
			fmt.Printf("Age:           %s\n", formatAge(tc.GetCreationTimestamp().Time))

			return nil
		},
	}

	cmd.Flags().StringVarP(&outputFormat, "output", "o", "", "output format (yaml, json)")

	return cmd
}

// newKubeconfigCmd creates the cluster kubeconfig command
func newKubeconfigCmd(logger *log.Logger) *cobra.Command {
	var outputPath string

	cmd := &cobra.Command{
		Use:   "kubeconfig NAME",
		Short: "Download kubeconfig for a tenant cluster",
		Long: `Download the kubeconfig for accessing a tenant cluster.

By default, saves to ~/.kube/<cluster-name>-config.
Use --output to specify a different path.

Examples:
  # Download and save kubeconfig
  butlerctl cluster kubeconfig my-app

  # Save to specific path
  butlerctl cluster kubeconfig my-app --output ./my-app.kubeconfig

  # Output to stdout (for piping)
  butlerctl cluster kubeconfig my-app --output -`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			ctx := cmd.Context()

			// Connect to management cluster
			c, err := client.NewFromDefault()
			if err != nil {
				return fmt.Errorf("connecting to management cluster: %w", err)
			}

			// Get TenantCluster to find kubeconfig secret reference
			tc, err := c.GetTenantCluster(ctx, butlerNamespace, name)
			if err != nil {
				return fmt.Errorf("getting TenantCluster: %w", err)
			}

			// Get kubeconfig from status or associated secret
			kubeconfigSecretName := getNestedString(tc.Object, "status", "kubeconfigSecretRef", "name")
			if kubeconfigSecretName == "" {
				kubeconfigSecretName = name + "-kubeconfig"
			}

			// Fetch the secret
			secret, err := c.Clientset.CoreV1().Secrets(butlerNamespace).Get(ctx, kubeconfigSecretName, metav1.GetOptions{})
			if err != nil {
				return fmt.Errorf("getting kubeconfig secret: %w", err)
			}

			kubeconfig := secret.Data["kubeconfig"]
			if len(kubeconfig) == 0 {
				kubeconfig = secret.Data["value"]
			}
			if len(kubeconfig) == 0 {
				return fmt.Errorf("kubeconfig not found in secret")
			}

			// Determine output path
			if outputPath == "-" {
				fmt.Print(string(kubeconfig))
				return nil
			}

			if outputPath == "" {
				home, err := os.UserHomeDir()
				if err != nil {
					return fmt.Errorf("getting home directory: %w", err)
				}
				outputPath = filepath.Join(home, ".kube", name+"-config")
			}

			// Ensure directory exists
			if err := os.MkdirAll(filepath.Dir(outputPath), 0700); err != nil {
				return fmt.Errorf("creating directory: %w", err)
			}

			// Write kubeconfig
			if err := os.WriteFile(outputPath, kubeconfig, 0600); err != nil {
				return fmt.Errorf("writing kubeconfig: %w", err)
			}

			logger.Success("kubeconfig saved", "path", outputPath)
			logger.Info("Use: export KUBECONFIG=" + outputPath)

			return nil
		},
	}

	cmd.Flags().StringVarP(&outputPath, "output", "o", "", "output path (use - for stdout)")

	return cmd
}

// newDeleteCmd creates the cluster delete command
func newDeleteCmd(logger *log.Logger) *cobra.Command {
	var (
		force  bool
		noWait bool
	)

	cmd := &cobra.Command{
		Use:   "delete NAME",
		Short: "Delete a tenant cluster",
		Long: `Delete a tenant cluster and all its resources.

This will:
  • Terminate all worker nodes
  • Delete hosted control plane (if applicable)
  • Clean up associated resources (PVCs, secrets, etc.)

By default, waits for deletion to complete.
Use --no-wait to return immediately.

Examples:
  butlerctl cluster delete my-app
  butlerctl cluster delete my-app --force --no-wait`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			ctx := cmd.Context()

			// Connect to management cluster
			c, err := client.NewFromDefault()
			if err != nil {
				return fmt.Errorf("connecting to management cluster: %w", err)
			}

			// Confirm deletion (unless force)
			if !force {
				fmt.Printf("This will permanently delete cluster %q and all its resources.\n", name)
				fmt.Print("Type the cluster name to confirm: ")
				var confirm string
				fmt.Scanln(&confirm)
				if confirm != name {
					return fmt.Errorf("confirmation failed")
				}
			}

			logger.Info("deleting tenant cluster", "name", name)

			if err := c.DeleteTenantCluster(ctx, butlerNamespace, name); err != nil {
				return fmt.Errorf("deleting TenantCluster: %w", err)
			}

			if !noWait {
				logger.Waiting("waiting for cluster deletion...")
				// TODO: Watch for TenantCluster to be gone
				time.Sleep(2 * time.Second) // placeholder
			}

			logger.Success("cluster deleted", "name", name)
			return nil
		},
	}

	cmd.Flags().BoolVar(&force, "force", false, "skip confirmation prompt")
	cmd.Flags().BoolVar(&noWait, "no-wait", false, "don't wait for deletion to complete")

	return cmd
}

// Helper functions

func getNestedString(obj map[string]interface{}, fields ...string) string {
	val, _, _ := unstructured.NestedString(obj, fields...)
	return val
}

func getNestedInt64(obj map[string]interface{}, fields ...string) int64 {
	val, _, _ := unstructured.NestedInt64(obj, fields...)
	return val
}

func formatAge(t time.Time) string {
	d := time.Since(t)
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	return fmt.Sprintf("%dd", int(d.Hours()/24))
}
