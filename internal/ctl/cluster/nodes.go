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
)

// nodeInfo holds extracted CAPI Machine information for display.
type nodeInfo struct {
	Name       string `json:"name"`
	Phase      string `json:"phase"`
	ProviderID string `json:"providerID"`
	Version    string `json:"version"`
	Age        string `json:"age"`
	CreatedAt  time.Time `json:"createdAt"`
}

type nodesOptions struct {
	namespace      string
	outputFormat   string
	kubeconfigPath string
	kubeContext    string
}

// newNodesCmd creates the cluster nodes command.
func newNodesCmd(logger *log.Logger) *cobra.Command {
	opts := &nodesOptions{}

	cmd := &cobra.Command{
		Use:   "nodes NAME",
		Short: "List nodes of a tenant cluster",
		Long: `List CAPI Machine resources for a tenant cluster.

Reads Machine CRDs from the cluster's namespace, filtered by the
cluster-name label. Shows machine phase, provider ID, Kubernetes
version, and age.

Examples:
  # List nodes for a cluster
  butlerctl cluster nodes my-cluster

  # List nodes in a specific namespace
  butlerctl cluster nodes my-cluster -n team-payments

  # Output as JSON
  butlerctl cluster nodes my-cluster -o json`,
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeClusterNames,
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.kubeContext, _ = cmd.Flags().GetString("context")
			return runNodes(cmd.Context(), logger, args[0], opts)
		},
	}

	cmd.Flags().StringVarP(&opts.namespace, "namespace", "n", DefaultTenantNamespace, "namespace of the TenantCluster")
	cmd.Flags().StringVarP(&opts.outputFormat, "output", "o", "", "output format (json, yaml)")
	cmd.Flags().StringVar(&opts.kubeconfigPath, "kubeconfig", "", "path to management cluster kubeconfig")

	return cmd
}

func runNodes(ctx context.Context, logger *log.Logger, clusterName string, opts *nodesOptions) error {
	// Connect to management cluster
	c, err := client.New(opts.kubeconfigPath, opts.kubeContext)
	if err != nil {
		return fmt.Errorf("connecting to management cluster: %w", err)
	}

	// Get the TenantCluster to find the tenant namespace where CAPI resources live
	tc, err := c.GetTenantCluster(ctx, opts.namespace, clusterName)
	if err != nil {
		return fmt.Errorf("getting TenantCluster %s/%s: %w", opts.namespace, clusterName, err)
	}

	tenantNS := client.GetNestedString(tc.Object, "status", "tenantNamespace")
	if tenantNS == "" {
		return fmt.Errorf("TenantCluster %s does not have a tenant namespace yet (phase: %s)",
			clusterName, client.GetNestedString(tc.Object, "status", "phase"))
	}

	// List CAPI Machines with the cluster-name label
	labelSelector := fmt.Sprintf("cluster.x-k8s.io/cluster-name=%s", clusterName)
	machineList, err := c.Dynamic.Resource(client.MachineGVR).Namespace(tenantNS).List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err != nil {
		return fmt.Errorf("listing Machines in namespace %s: %w", tenantNS, err)
	}

	if len(machineList.Items) == 0 {
		fmt.Printf("No machines found for cluster %s in namespace %s.\n", clusterName, tenantNS)
		return nil
	}

	// Sort by name
	sort.Slice(machineList.Items, func(i, j int) bool {
		return machineList.Items[i].GetName() < machineList.Items[j].GetName()
	})

	// Build node info list
	infos := make([]nodeInfo, len(machineList.Items))
	for i, m := range machineList.Items {
		createdAt := m.GetCreationTimestamp().Time
		var age string
		if !createdAt.IsZero() {
			age = output.FormatAge(createdAt)
		} else {
			age = "<unknown>"
		}

		infos[i] = nodeInfo{
			Name:       m.GetName(),
			Phase:      client.GetNestedString(m.Object, "status", "phase"),
			ProviderID: client.GetNestedString(m.Object, "spec", "providerID"),
			Version:    client.GetNestedString(m.Object, "spec", "version"),
			Age:        age,
			CreatedAt:  createdAt,
		}
	}

	// Handle structured output
	if opts.outputFormat == "json" {
		return output.PrintJSON(os.Stdout, infos)
	}
	if opts.outputFormat == "yaml" {
		return output.PrintYAML(os.Stdout, infos)
	}

	// Table output
	return printNodesTable(os.Stdout, infos)
}

// printNodesTable renders machine info as a table.
func printNodesTable(w io.Writer, nodes []nodeInfo) error {
	table := output.NewTable(w, "NAME", "PHASE", "PROVIDER-ID", "VERSION", "AGE")

	for _, n := range nodes {
		phase := output.ColorizePhase(n.Phase)
		providerID := n.ProviderID
		if providerID == "" {
			providerID = "<pending>"
		}
		table.AddRow(n.Name, phase, providerID, n.Version, n.Age)
	}

	return table.Flush()
}
