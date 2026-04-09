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
	"time"

	"github.com/butlerdotdev/butler/internal/common/client"
	"github.com/butlerdotdev/butler/internal/common/log"
	"github.com/butlerdotdev/butler/internal/common/output"
	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/watch"
)

type watchOptions struct {
	namespace      string
	kubeconfigPath string
	kubeContext    string
}

// newWatchCmd creates the cluster watch command.
func newWatchCmd(logger *log.Logger) *cobra.Command {
	opts := &watchOptions{}

	cmd := &cobra.Command{
		Use:   "watch NAME",
		Short: "Watch a cluster's status in real-time",
		Long: `Watch a TenantCluster's status in real-time, printing updates
when the phase or conditions change.

A timestamp is printed with each update. Press Ctrl+C to stop watching.

Examples:
  # Watch a cluster
  butlerctl cluster watch my-cluster

  # Watch a cluster in a specific namespace
  butlerctl cluster watch my-cluster -n team-payments`,
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeClusterNames,
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.kubeContext, _ = cmd.Flags().GetString("context")
			return runWatch(cmd.Context(), logger, args[0], opts)
		},
	}

	cmd.Flags().StringVarP(&opts.namespace, "namespace", "n", DefaultTenantNamespace, "namespace of the TenantCluster")
	cmd.Flags().StringVar(&opts.kubeconfigPath, "kubeconfig", "", "path to management cluster kubeconfig")

	return cmd
}

func runWatch(ctx context.Context, logger *log.Logger, clusterName string, opts *watchOptions) error {
	// Connect to management cluster
	c, err := client.New(opts.kubeconfigPath, opts.kubeContext)
	if err != nil {
		return fmt.Errorf("connecting to management cluster: %w", err)
	}

	// Verify the cluster exists before starting the watch
	tc, err := c.GetTenantCluster(ctx, opts.namespace, clusterName)
	if err != nil {
		return fmt.Errorf("getting TenantCluster %s/%s: %w", opts.namespace, clusterName, err)
	}

	// Print initial state
	phase := client.GetNestedString(tc.Object, "status", "phase")
	fmt.Printf("[%s] Watching cluster %s/%s\n", time.Now().Format("15:04:05"), opts.namespace, clusterName)
	fmt.Printf("[%s] Current phase: %s\n", time.Now().Format("15:04:05"), output.ColorizePhase(phase))
	printWatchConditions(tc.Object)

	// Track last known state
	lastPhase := phase
	lastConditions := extractConditionSummary(tc.Object)

	// Start watching
	fieldSelector := fmt.Sprintf("metadata.name=%s", clusterName)
	watcher, err := c.Dynamic.Resource(client.TenantClusterGVR).Namespace(opts.namespace).Watch(ctx, metav1.ListOptions{
		FieldSelector: fieldSelector,
	})
	if err != nil {
		return fmt.Errorf("starting watch: %w", err)
	}
	defer watcher.Stop()

	for {
		select {
		case <-ctx.Done():
			fmt.Printf("\n[%s] Watch stopped.\n", time.Now().Format("15:04:05"))
			return nil

		case event, ok := <-watcher.ResultChan():
			if !ok {
				// Watch channel closed, restart
				fmt.Printf("[%s] Watch connection lost, reconnecting...\n", time.Now().Format("15:04:05"))
				watcher, err = c.Dynamic.Resource(client.TenantClusterGVR).Namespace(opts.namespace).Watch(ctx, metav1.ListOptions{
					FieldSelector: fieldSelector,
				})
				if err != nil {
					return fmt.Errorf("reconnecting watch: %w", err)
				}
				continue
			}

			if event.Type == watch.Deleted {
				fmt.Printf("[%s] Cluster %s has been deleted.\n", time.Now().Format("15:04:05"), clusterName)
				return nil
			}

			if event.Type != watch.Modified {
				continue
			}

			u, ok := event.Object.(*unstructured.Unstructured)
			if !ok {
				continue
			}
			unstructuredMap := u.Object

			// Check for phase changes
			currentPhase := client.GetNestedString(unstructuredMap, "status", "phase")
			if currentPhase != lastPhase && currentPhase != "" {
				fmt.Printf("[%s] Phase: %s -> %s\n",
					time.Now().Format("15:04:05"),
					output.ColorizePhase(lastPhase),
					output.ColorizePhase(currentPhase),
				)
				lastPhase = currentPhase
			}

			// Check for condition changes
			currentConditions := extractConditionSummaryFromMap(unstructuredMap)
			for condType, condStatus := range currentConditions {
				if old, exists := lastConditions[condType]; !exists || old != condStatus {
					fmt.Printf("[%s] Condition %s: %s\n",
						time.Now().Format("15:04:05"),
						condType,
						colorizeConditionStatus(condStatus),
					)
				}
			}
			lastConditions = currentConditions
		}
	}
}

// printWatchConditions prints current conditions in a compact format.
func printWatchConditions(obj map[string]interface{}) {
	conditions, found, _ := unstructuredNestedSlice(obj, "status", "conditions")
	if !found || len(conditions) == 0 {
		return
	}

	for _, c := range conditions {
		cond, ok := c.(map[string]interface{})
		if !ok {
			continue
		}
		condType := client.GetNestedString(cond, "type")
		status := client.GetNestedString(cond, "status")
		fmt.Printf("[%s] Condition %s: %s\n",
			time.Now().Format("15:04:05"),
			condType,
			colorizeConditionStatus(status),
		)
	}
}

// extractConditionSummary returns a map of condition type -> status from an unstructured object.
func extractConditionSummary(obj map[string]interface{}) map[string]string {
	return extractConditionSummaryFromMap(obj)
}

// extractConditionSummaryFromMap returns a map of condition type -> status.
func extractConditionSummaryFromMap(obj map[string]interface{}) map[string]string {
	result := make(map[string]string)
	conditions, found, _ := unstructuredNestedSlice(obj, "status", "conditions")
	if !found {
		return result
	}

	for _, c := range conditions {
		cond, ok := c.(map[string]interface{})
		if !ok {
			continue
		}
		condType := client.GetNestedString(cond, "type")
		status := client.GetNestedString(cond, "status")
		if condType != "" {
			result[condType] = status
		}
	}
	return result
}
