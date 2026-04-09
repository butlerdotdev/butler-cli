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

// Package network implements butleradm network commands for managing
// NetworkPool resources.
package network

import (
	"context"
	"fmt"
	"os"

	"github.com/butlerdotdev/butler/internal/common/client"
	"github.com/butlerdotdev/butler/internal/common/log"
	"github.com/butlerdotdev/butler/internal/common/output"
	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	butlerSystem = "butler-system"
)

// NewNetworkCmd creates the network parent command for butleradm.
func NewNetworkCmd(logger *log.Logger) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "network",
		Short: "Manage network pools",
		Long: `Manage NetworkPool resources for IPAM (IP Address Management).

NetworkPools define CIDR ranges that Butler allocates to tenant clusters.
Each pool tracks total, allocated, and available IP addresses.

Note: network create is not implemented due to the complexity of CIDR
and range configuration. Use kubectl apply with a YAML manifest instead.

Examples:
  butleradm network list
  butleradm network get node-pool
  butleradm network delete old-pool`,
	}

	cmd.AddCommand(newListCmd(logger))
	cmd.AddCommand(newGetCmd(logger))
	cmd.AddCommand(newDeleteCmd(logger))

	return cmd
}

func newListCmd(logger *log.Logger) *cobra.Command {
	var (
		namespace  string
		outputFmt  string
		kubeconfig string
	)

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List network pools",
		Long: `List all NetworkPool resources.

Examples:
  butleradm network list
  butleradm network list -o json
  butleradm network list -o yaml`,
		Aliases: []string{"ls"},
		RunE: func(cmd *cobra.Command, args []string) error {
			kubeContext, _ := cmd.Flags().GetString("context")
			return runList(cmd.Context(), logger, namespace, kubeconfig, kubeContext, outputFmt)
		},
	}

	cmd.Flags().StringVarP(&namespace, "namespace", "n", butlerSystem, "namespace of the NetworkPools")
	cmd.Flags().StringVarP(&outputFmt, "output", "o", "", "output format (json, yaml)")
	cmd.Flags().StringVar(&kubeconfig, "kubeconfig", "", "path to management cluster kubeconfig")

	return cmd
}

func runList(ctx context.Context, logger *log.Logger, namespace, kubeconfigPath, kubeContext, outputFormat string) error {
	c, err := client.New(kubeconfigPath, kubeContext)
	if err != nil {
		return fmt.Errorf("connecting to management cluster: %w", err)
	}

	list, err := c.Dynamic.Resource(client.NetworkPoolGVR).Namespace(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("listing network pools: %w", err)
	}

	if outputFormat == "json" {
		return output.PrintJSON(os.Stdout, list.Items)
	}
	if outputFormat == "yaml" {
		return output.PrintYAML(os.Stdout, list.Items)
	}

	headers := []string{"NAME", "CIDR", "TOTAL IPS", "ALLOCATED", "AVAILABLE", "AGE"}
	var rows [][]string

	for _, np := range list.Items {
		name := np.GetName()
		cidr := client.GetNestedString(np.Object, "spec", "cidr")
		totalIPs := client.GetNestedInt64(np.Object, "status", "totalIPs")
		allocatedIPs := client.GetNestedInt64(np.Object, "status", "allocatedIPs")
		availableIPs := totalIPs - allocatedIPs
		age := output.FormatAge(np.GetCreationTimestamp().Time)

		rows = append(rows, []string{
			name,
			cidr,
			fmt.Sprintf("%d", totalIPs),
			fmt.Sprintf("%d", allocatedIPs),
			fmt.Sprintf("%d", availableIPs),
			age,
		})
	}

	table := output.NewTable(os.Stdout, headers...)
	for _, row := range rows {
		table.AddRow(row...)
	}
	table.Flush()
	return nil
}

func newGetCmd(logger *log.Logger) *cobra.Command {
	var (
		namespace  string
		outputFmt  string
		kubeconfig string
	)

	cmd := &cobra.Command{
		Use:   "get NAME",
		Short: "Get details of a network pool",
		Long: `Get detailed information about a specific NetworkPool.

Shows CIDR, reserved ranges, tenant allocation configuration, and
utilization statistics.

Examples:
  butleradm network get node-pool
  butleradm network get node-pool -o yaml
  butleradm network get lb-pool -o json`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			kubeContext, _ := cmd.Flags().GetString("context")
			return runGet(cmd.Context(), logger, args[0], namespace, kubeconfig, kubeContext, outputFmt)
		},
	}

	cmd.Flags().StringVarP(&namespace, "namespace", "n", butlerSystem, "namespace of the NetworkPool")
	cmd.Flags().StringVarP(&outputFmt, "output", "o", "", "output format (json, yaml)")
	cmd.Flags().StringVar(&kubeconfig, "kubeconfig", "", "path to management cluster kubeconfig")

	return cmd
}

func runGet(ctx context.Context, logger *log.Logger, name, namespace, kubeconfigPath, kubeContext, outputFormat string) error {
	c, err := client.New(kubeconfigPath, kubeContext)
	if err != nil {
		return fmt.Errorf("connecting to management cluster: %w", err)
	}

	np, err := c.Dynamic.Resource(client.NetworkPoolGVR).Namespace(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("getting NetworkPool %s: %w", name, err)
	}

	if outputFormat == "json" {
		return output.PrintJSON(os.Stdout, np.Object)
	}
	if outputFormat == "yaml" {
		return output.PrintYAML(os.Stdout, np.Object)
	}

	// Human-readable detail view
	cidr := client.GetNestedString(np.Object, "spec", "cidr")
	totalIPs := client.GetNestedInt64(np.Object, "status", "totalIPs")
	allocatedIPs := client.GetNestedInt64(np.Object, "status", "allocatedIPs")
	fragmentation := client.GetNestedString(np.Object, "status", "fragmentation")
	age := output.FormatAge(np.GetCreationTimestamp().Time)

	availableIPs := totalIPs - allocatedIPs
	var utilization string
	if totalIPs > 0 {
		pct := float64(allocatedIPs) / float64(totalIPs) * 100
		utilization = fmt.Sprintf("%.1f%%", pct)
	} else {
		utilization = "0.0%"
	}

	fmt.Printf("Name:          %s\n", np.GetName())
	fmt.Printf("Namespace:     %s\n", np.GetNamespace())
	fmt.Printf("CIDR:          %s\n", cidr)
	fmt.Printf("Total IPs:     %d\n", totalIPs)
	fmt.Printf("Allocated:     %d\n", allocatedIPs)
	fmt.Printf("Available:     %d\n", availableIPs)
	fmt.Printf("Utilization:   %s\n", utilization)
	if fragmentation != "" {
		fmt.Printf("Fragmentation: %s\n", fragmentation)
	}
	fmt.Printf("Age:           %s\n", age)

	// Show reserved ranges
	reserved, found := unstructuredNestedSlice(np.Object, "spec", "reserved")
	if found && len(reserved) > 0 {
		fmt.Printf("\nReserved Ranges:\n")
		for _, r := range reserved {
			rMap, ok := r.(map[string]interface{})
			if !ok {
				continue
			}
			start := client.GetNestedString(rMap, "start")
			end := client.GetNestedString(rMap, "end")
			description := client.GetNestedString(rMap, "description")
			if description != "" {
				fmt.Printf("  %s - %s  (%s)\n", start, end, description)
			} else {
				fmt.Printf("  %s - %s\n", start, end)
			}
		}
	}

	// Show tenant allocation config
	allocStart := client.GetNestedString(np.Object, "spec", "tenantAllocation", "start")
	allocEnd := client.GetNestedString(np.Object, "spec", "tenantAllocation", "end")
	if allocStart != "" || allocEnd != "" {
		fmt.Printf("\nTenant Allocation:\n")
		if allocStart != "" {
			fmt.Printf("  Start: %s\n", allocStart)
		}
		if allocEnd != "" {
			fmt.Printf("  End:   %s\n", allocEnd)
		}
	}

	return nil
}

func newDeleteCmd(logger *log.Logger) *cobra.Command {
	var (
		namespace  string
		kubeconfig string
	)

	cmd := &cobra.Command{
		Use:   "delete NAME",
		Short: "Delete a network pool",
		Long: `Delete a NetworkPool resource.

Existing IP allocations from this pool are not affected, but no new
allocations can be made from it.

Examples:
  butleradm network delete old-pool
  butleradm network delete old-pool -n custom-namespace`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			kubeContext, _ := cmd.Flags().GetString("context")
			return runDelete(cmd.Context(), logger, args[0], namespace, kubeconfig, kubeContext)
		},
	}

	cmd.Flags().StringVarP(&namespace, "namespace", "n", butlerSystem, "namespace of the NetworkPool")
	cmd.Flags().StringVar(&kubeconfig, "kubeconfig", "", "path to management cluster kubeconfig")

	return cmd
}

func runDelete(ctx context.Context, logger *log.Logger, name, namespace, kubeconfigPath, kubeContext string) error {
	c, err := client.New(kubeconfigPath, kubeContext)
	if err != nil {
		return fmt.Errorf("connecting to management cluster: %w", err)
	}

	err = c.Dynamic.Resource(client.NetworkPoolGVR).Namespace(namespace).Delete(ctx, name, metav1.DeleteOptions{})
	if err != nil {
		return fmt.Errorf("deleting NetworkPool %s: %w", name, err)
	}

	logger.Success("network pool deleted", "name", name, "namespace", namespace)
	return nil
}

// unstructuredNestedSlice extracts a slice from nested map fields.
func unstructuredNestedSlice(obj map[string]interface{}, fields ...string) ([]interface{}, bool) {
	var val interface{} = obj
	for _, field := range fields {
		m, ok := val.(map[string]interface{})
		if !ok {
			return nil, false
		}
		val, ok = m[field]
		if !ok {
			return nil, false
		}
	}
	slice, ok := val.([]interface{})
	if !ok {
		return nil, false
	}
	return slice, true
}
