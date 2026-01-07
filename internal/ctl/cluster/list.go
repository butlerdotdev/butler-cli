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
	"sort"
	"time"

	"github.com/butlerdotdev/butler/internal/common/client"
	"github.com/butlerdotdev/butler/internal/common/log"
	"github.com/butlerdotdev/butler/internal/common/output"
	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

type listOptions struct {
	nsFlags      NamespaceFlags
	outputFormat string
	kubeconfig   string
}

// newListCmd creates the cluster list command
func newListCmd(logger *log.Logger) *cobra.Command {
	opts := &listOptions{}

	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List tenant clusters",
		Long: `List tenant clusters in the Butler platform.

By default, lists clusters in the butler-tenants namespace.
Use -n to specify a different namespace, or -A to list across all namespaces.

Examples:
  # List clusters in default namespace
  butlerctl cluster list

  # List clusters in a specific namespace  
  butlerctl cluster list -n team-payments

  # List clusters across all namespaces
  butlerctl cluster list -A

  # Output in wide format (includes endpoint, provider)
  butlerctl cluster list -o wide

  # Output as JSON
  butlerctl cluster list -o json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runList(cmd.Context(), logger, opts)
		},
	}

	AddNamespaceFlags(cmd, &opts.nsFlags)
	cmd.Flags().StringVarP(&opts.outputFormat, "output", "o", "table", "output format (table, wide, json, yaml)")
	cmd.Flags().StringVar(&opts.kubeconfig, "kubeconfig", "", "path to kubeconfig file")

	return cmd
}

func runList(ctx context.Context, logger *log.Logger, opts *listOptions) error {
	// Parse output format
	format, err := output.ParseFormat(opts.outputFormat)
	if err != nil {
		return err
	}

	// Connect to management cluster
	var c *client.Client
	if opts.kubeconfig != "" {
		c, err = client.NewFromKubeconfig(opts.kubeconfig)
	} else {
		c, err = client.NewFromDefault()
	}
	if err != nil {
		return fmt.Errorf("connecting to management cluster: %w", err)
	}

	// Resolve namespace
	namespace, allNamespaces := opts.nsFlags.ResolveNamespace()

	// List TenantClusters
	var clusters []unstructured.Unstructured

	if allNamespaces {
		// List across all namespaces
		list, err := c.Dynamic.Resource(client.TenantClusterGVR).List(ctx, metav1.ListOptions{})
		if err != nil {
			return fmt.Errorf("listing TenantClusters: %w", err)
		}
		clusters = list.Items
	} else {
		// List in specific namespace
		list, err := c.Dynamic.Resource(client.TenantClusterGVR).Namespace(namespace).List(ctx, metav1.ListOptions{})
		if err != nil {
			return fmt.Errorf("listing TenantClusters in namespace %s: %w", namespace, err)
		}
		clusters = list.Items
	}

	// Sort by namespace, then name
	sort.Slice(clusters, func(i, j int) bool {
		if clusters[i].GetNamespace() != clusters[j].GetNamespace() {
			return clusters[i].GetNamespace() < clusters[j].GetNamespace()
		}
		return clusters[i].GetName() < clusters[j].GetName()
	})

	// Extract info for all clusters and enrich with MachineDeployment data
	infos := make([]TenantClusterInfo, len(clusters))
	for i := range clusters {
		infos[i] = ExtractTenantClusterInfo(&clusters[i])
		// Enrich with actual worker status from MachineDeployment
		EnrichWithMachineDeploymentStatus(ctx, c, &infos[i])
		// Enrich with control plane endpoint from CAPI Cluster
		EnrichWithControlPlaneEndpoint(ctx, c, &infos[i])
	}

	// Create printer and output
	printer := output.NewPrinter(format, os.Stdout)

	// For JSON/YAML, output the raw list
	if format == output.FormatJSON || format == output.FormatYAML {
		// Create a cleaned up structure for output
		outputData := make([]map[string]interface{}, len(infos))
		for i, info := range infos {
			outputData[i] = map[string]interface{}{
				"name":              info.Name,
				"namespace":         info.Namespace,
				"phase":             info.Phase,
				"kubernetesVersion": info.KubernetesVersion,
				"workers": map[string]int64{
					"ready":   info.WorkersReady,
					"desired": info.WorkersDesired,
				},
				"endpoint":        info.Endpoint,
				"tenantNamespace": info.TenantNamespace,
				"providerConfig":  info.ProviderConfig,
				"creationTime":    info.CreationTime,
			}
		}
		return printer.Print(outputData, nil)
	}

	// Table output
	return printer.Print(nil, func(w io.Writer) error {
		return printClusterTable(w, infos, format == output.FormatWide, allNamespaces)
	})
}

func printClusterTable(w io.Writer, clusters []TenantClusterInfo, wide, showNamespace bool) error {
	// Build headers based on options
	headers := []string{"NAME"}
	if showNamespace {
		headers = append(headers, "NAMESPACE")
	}
	headers = append(headers, "PHASE", "K8S VERSION", "WORKERS", "AGE")
	if wide {
		headers = append(headers, "ENDPOINT", "PROVIDER")
	}

	table := output.NewTable(w, headers...)

	for _, tc := range clusters {
		// Format phase with color
		phase := output.ColorizePhase(tc.Phase)

		// Format workers
		workers := output.FormatWorkers(tc.WorkersReady, tc.WorkersDesired)
		if tc.WorkersDesired == 0 {
			// Try to get from spec if status not populated
			workers = "-"
		}

		// Parse and format age
		var age string
		if tc.CreationTime != "" {
			t, err := parseTime(tc.CreationTime)
			if err == nil {
				age = output.FormatAge(t)
			} else {
				age = "<unknown>"
			}
		} else {
			age = "<unknown>"
		}

		// Build row
		row := []string{tc.Name}
		if showNamespace {
			row = append(row, tc.Namespace)
		}
		row = append(row, phase, tc.KubernetesVersion, workers, age)
		if wide {
			endpoint := tc.Endpoint
			if endpoint == "" {
				endpoint = "-"
			}
			provider := tc.ProviderConfig
			if provider == "" {
				provider = "-"
			}
			row = append(row, endpoint, provider)
		}

		table.AddRow(row...)
	}

	return table.Flush()
}

func parseTime(s string) (time.Time, error) {
	// Try RFC3339 first
	t, err := time.Parse(time.RFC3339, s)
	if err == nil {
		return t, nil
	}
	// Try without timezone
	return time.Parse("2006-01-02T15:04:05Z", s)
}
