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
)

// conditionInfo holds extracted condition information for display.
type conditionInfo struct {
	Type               string `json:"type"`
	Status             string `json:"status"`
	Reason             string `json:"reason"`
	Message            string `json:"message"`
	LastTransitionTime string `json:"lastTransitionTime"`
}

type conditionsOptions struct {
	namespace      string
	outputFormat   string
	kubeconfigPath string
	kubeContext    string
}

// newConditionsCmd creates the cluster conditions command.
func newConditionsCmd(logger *log.Logger) *cobra.Command {
	opts := &conditionsOptions{}

	cmd := &cobra.Command{
		Use:   "conditions NAME",
		Short: "Show conditions for a tenant cluster",
		Long: `Display status conditions from a TenantCluster resource.

Conditions show the detailed state of a cluster's reconciliation
progress. Each condition has a type, status (True/False/Unknown),
reason, and optional message.

Common condition types:
  Ready                  Overall cluster readiness
  ControlPlaneReady      Steward control plane is running
  WorkersReady           All worker nodes are provisioned
  AddonsReady            Cluster addons are installed

Examples:
  # Show conditions for a cluster
  butlerctl cluster conditions my-cluster

  # Show conditions in a specific namespace
  butlerctl cluster conditions my-cluster -n team-payments

  # Output as JSON
  butlerctl cluster conditions my-cluster -o json`,
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeClusterNames,
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.kubeContext, _ = cmd.Flags().GetString("context")
			return runConditions(cmd.Context(), logger, args[0], opts)
		},
	}

	cmd.Flags().StringVarP(&opts.namespace, "namespace", "n", DefaultTenantNamespace, "namespace of the TenantCluster")
	cmd.Flags().StringVarP(&opts.outputFormat, "output", "o", "", "output format (json, yaml)")
	cmd.Flags().StringVar(&opts.kubeconfigPath, "kubeconfig", "", "path to management cluster kubeconfig")

	return cmd
}

func runConditions(ctx context.Context, logger *log.Logger, clusterName string, opts *conditionsOptions) error {
	// Connect to management cluster
	c, err := client.New(opts.kubeconfigPath, opts.kubeContext)
	if err != nil {
		return fmt.Errorf("connecting to management cluster: %w", err)
	}

	// Get TenantCluster
	tc, err := c.GetTenantCluster(ctx, opts.namespace, clusterName)
	if err != nil {
		return fmt.Errorf("getting TenantCluster %s/%s: %w", opts.namespace, clusterName, err)
	}

	// Extract conditions from status.conditions
	conditions, found, _ := unstructuredNestedSlice(tc.Object, "status", "conditions")
	if !found || len(conditions) == 0 {
		phase := client.GetNestedString(tc.Object, "status", "phase")
		fmt.Printf("No conditions found for cluster %s (phase: %s).\n", clusterName, phase)
		return nil
	}

	// Build condition info list
	var infos []conditionInfo
	for _, c := range conditions {
		cond, ok := c.(map[string]interface{})
		if !ok {
			continue
		}

		infos = append(infos, conditionInfo{
			Type:               client.GetNestedString(cond, "type"),
			Status:             client.GetNestedString(cond, "status"),
			Reason:             client.GetNestedString(cond, "reason"),
			Message:            client.GetNestedString(cond, "message"),
			LastTransitionTime: client.GetNestedString(cond, "lastTransitionTime"),
		})
	}

	// Sort by type alphabetically
	sort.Slice(infos, func(i, j int) bool {
		return infos[i].Type < infos[j].Type
	})

	// Handle structured output
	if opts.outputFormat == "json" {
		return output.PrintJSON(os.Stdout, infos)
	}
	if opts.outputFormat == "yaml" {
		return output.PrintYAML(os.Stdout, infos)
	}

	// Table output
	return printConditionsTable(os.Stdout, infos)
}

// printConditionsTable renders condition info as a table.
func printConditionsTable(w io.Writer, conditions []conditionInfo) error {
	table := output.NewTable(w, "TYPE", "STATUS", "REASON", "MESSAGE", "LAST TRANSITION")

	for _, ci := range conditions {
		status := colorizeConditionStatus(ci.Status)

		// Format the transition time as relative age if possible
		lastTransition := ci.LastTransitionTime
		if lastTransition != "" {
			if t, err := time.Parse(time.RFC3339, lastTransition); err == nil {
				lastTransition = output.FormatAge(t)
			}
		}

		table.AddRow(ci.Type, status, ci.Reason, ci.Message, lastTransition)
	}

	return table.Flush()
}

// colorizeConditionStatus returns a colorized condition status string.
func colorizeConditionStatus(status string) string {
	if !output.ColorEnabled() {
		return status
	}
	switch status {
	case "True":
		return output.Success(status)
	case "False":
		return output.Danger(status)
	case "Unknown":
		return output.Warning(status)
	default:
		return status
	}
}
