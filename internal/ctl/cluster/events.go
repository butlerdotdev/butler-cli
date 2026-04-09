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

// eventInfo holds extracted event information for display.
type eventInfo struct {
	Type    string `json:"type"`
	Reason  string `json:"reason"`
	Message string `json:"message"`
	Age     string `json:"age"`
	LastSeen time.Time `json:"lastSeen"`
}

type eventsOptions struct {
	namespace      string
	outputFormat   string
	kubeconfigPath string
	kubeContext    string
	limit          int
}

// newEventsCmd creates the cluster events command.
func newEventsCmd(logger *log.Logger) *cobra.Command {
	opts := &eventsOptions{
		limit: 50,
	}

	cmd := &cobra.Command{
		Use:   "events NAME",
		Short: "List events for a tenant cluster",
		Long: `List Kubernetes Events related to a TenantCluster.

Events are retrieved from the same namespace as the cluster, filtered
by the cluster name. Results are sorted by time with newest first.

Examples:
  # List events for a cluster
  butlerctl cluster events my-cluster

  # List events in a specific namespace
  butlerctl cluster events my-cluster -n team-payments

  # Limit to 10 most recent events
  butlerctl cluster events my-cluster --limit 10

  # Output as JSON
  butlerctl cluster events my-cluster -o json`,
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeClusterNames,
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.kubeContext, _ = cmd.Flags().GetString("context")
			return runEvents(cmd.Context(), logger, args[0], opts)
		},
	}

	cmd.Flags().StringVarP(&opts.namespace, "namespace", "n", DefaultTenantNamespace, "namespace of the TenantCluster")
	cmd.Flags().StringVarP(&opts.outputFormat, "output", "o", "", "output format (json, yaml)")
	cmd.Flags().StringVar(&opts.kubeconfigPath, "kubeconfig", "", "path to management cluster kubeconfig")
	cmd.Flags().IntVar(&opts.limit, "limit", 50, "maximum number of events to display")

	return cmd
}

func runEvents(ctx context.Context, logger *log.Logger, clusterName string, opts *eventsOptions) error {
	// Connect to management cluster
	c, err := client.New(opts.kubeconfigPath, opts.kubeContext)
	if err != nil {
		return fmt.Errorf("connecting to management cluster: %w", err)
	}

	// List events in the cluster's namespace filtered by involved object name
	fieldSelector := fmt.Sprintf("involvedObject.name=%s", clusterName)
	events, err := c.Clientset.CoreV1().Events(opts.namespace).List(ctx, metav1.ListOptions{
		FieldSelector: fieldSelector,
	})
	if err != nil {
		return fmt.Errorf("listing events for %s/%s: %w", opts.namespace, clusterName, err)
	}

	if len(events.Items) == 0 {
		fmt.Printf("No events found for cluster %s in namespace %s.\n", clusterName, opts.namespace)
		return nil
	}

	// Sort by lastTimestamp descending (newest first)
	sort.Slice(events.Items, func(i, j int) bool {
		ti := events.Items[i].LastTimestamp.Time
		tj := events.Items[j].LastTimestamp.Time
		// Fall back to event time if lastTimestamp is zero
		if ti.IsZero() {
			ti = events.Items[i].EventTime.Time
		}
		if tj.IsZero() {
			tj = events.Items[j].EventTime.Time
		}
		return ti.After(tj)
	})

	// Apply limit
	items := events.Items
	if opts.limit > 0 && len(items) > opts.limit {
		items = items[:opts.limit]
	}

	// Build event info list
	infos := make([]eventInfo, len(items))
	for i, ev := range items {
		lastSeen := ev.LastTimestamp.Time
		if lastSeen.IsZero() {
			lastSeen = ev.EventTime.Time
		}

		var age string
		if !lastSeen.IsZero() {
			age = output.FormatAge(lastSeen)
		} else {
			age = "<unknown>"
		}

		infos[i] = eventInfo{
			Type:     ev.Type,
			Reason:   ev.Reason,
			Message:  ev.Message,
			Age:      age,
			LastSeen: lastSeen,
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
	return printEventsTable(os.Stdout, infos)
}

// printEventsTable renders event info as a table.
func printEventsTable(w io.Writer, events []eventInfo) error {
	table := output.NewTable(w, "TYPE", "REASON", "MESSAGE", "AGE")

	for _, ev := range events {
		eventType := colorizeEventType(ev.Type)
		table.AddRow(eventType, ev.Reason, ev.Message, ev.Age)
	}

	return table.Flush()
}

// colorizeEventType returns a colorized event type string.
func colorizeEventType(eventType string) string {
	if !output.ColorEnabled() {
		return eventType
	}
	switch eventType {
	case "Normal":
		return output.Success(eventType)
	case "Warning":
		return output.Warning(eventType)
	default:
		return eventType
	}
}
