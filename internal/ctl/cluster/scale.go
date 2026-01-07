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
	"encoding/json"
	"fmt"
	"time"

	"github.com/butlerdotdev/butler/internal/common/client"
	"github.com/butlerdotdev/butler/internal/common/log"
	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// ScaleOptions holds options for the scale command.
type ScaleOptions struct {
	Name      string
	Namespace string
	Workers   int32
	Wait      bool
	Timeout   time.Duration
	Logger    *log.Logger
}

// DefaultScaleOptions returns ScaleOptions with sensible defaults.
func DefaultScaleOptions(logger *log.Logger) *ScaleOptions {
	return &ScaleOptions{
		Namespace: DefaultTenantNamespace,
		Timeout:   10 * time.Minute,
		Logger:    logger,
	}
}

// Validate checks that all required options are set and valid.
func (o *ScaleOptions) Validate() error {
	if o.Name == "" {
		return fmt.Errorf("cluster name is required")
	}

	if o.Workers < 1 || o.Workers > 10 {
		return fmt.Errorf("workers must be between 1 and 10, got %d", o.Workers)
	}

	return nil
}

// NewScaleCmd creates the cluster scale command.
func NewScaleCmd(logger *log.Logger) *cobra.Command {
	opts := DefaultScaleOptions(logger)

	cmd := &cobra.Command{
		Use:   "scale NAME --workers COUNT",
		Short: "Scale the number of worker nodes in a cluster",
		Long: `Scale the number of worker nodes in a tenant cluster.

This command adjusts the worker node count by patching spec.workers.replicas.
Scaling up provisions new nodes; scaling down terminates excess nodes gracefully.

Examples:
  # Scale to 3 workers
  butlerctl cluster scale my-cluster --workers 3

  # Scale up and wait for all nodes to be ready
  butlerctl cluster scale my-cluster --workers 5 --wait

  # Scale down with timeout
  butlerctl cluster scale my-cluster --workers 1 --wait --timeout 5m`,
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeClusterNames,
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.Name = args[0]

			// Resolve namespace from flag
			if ns, _ := cmd.Flags().GetString("namespace"); ns != "" {
				opts.Namespace = ns
			}

			return runScale(cmd.Context(), opts)
		},
	}

	cmd.Flags().Int32VarP(&opts.Workers, "workers", "w", 0, "Target number of worker nodes (required)")
	cmd.Flags().StringVarP(&opts.Namespace, "namespace", "n", opts.Namespace, "Namespace of the TenantCluster")
	cmd.Flags().BoolVar(&opts.Wait, "wait", false, "Wait for scaling to complete")
	cmd.Flags().DurationVar(&opts.Timeout, "timeout", opts.Timeout, "Timeout when using --wait")

	// Mark workers as required
	_ = cmd.MarkFlagRequired("workers")

	return cmd
}

// completeClusterNames provides shell completion for cluster names.
func completeClusterNames(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) != 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	c, err := client.NewFromDefault()
	if err != nil {
		return nil, cobra.ShellCompDirectiveError
	}

	namespace := DefaultTenantNamespace
	if ns, _ := cmd.Flags().GetString("namespace"); ns != "" {
		namespace = ns
	}

	list, err := c.Dynamic.Resource(client.TenantClusterGVR).Namespace(namespace).List(context.Background(), metav1.ListOptions{})
	if err != nil {
		return nil, cobra.ShellCompDirectiveError
	}

	names := make([]string, 0, len(list.Items))
	for _, item := range list.Items {
		names = append(names, item.GetName())
	}

	return names, cobra.ShellCompDirectiveNoFileComp
}

// runScale executes the scale operation.
func runScale(ctx context.Context, opts *ScaleOptions) error {
	if err := opts.Validate(); err != nil {
		return err
	}

	// Verify we're connected to a management cluster
	if err := RequireManagementCluster(ctx); err != nil {
		return err
	}

	c, err := client.NewFromDefault()
	if err != nil {
		return fmt.Errorf("creating client: %w", err)
	}

	// Get current cluster state
	tc, err := c.Dynamic.Resource(client.TenantClusterGVR).Namespace(opts.Namespace).Get(ctx, opts.Name, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			return fmt.Errorf("TenantCluster %q not found in namespace %q", opts.Name, opts.Namespace)
		}
		return fmt.Errorf("getting TenantCluster: %w", err)
	}

	// Get current replica count
	currentReplicas := GetNestedInt64(tc.Object, "spec", "workers", "replicas")
	if currentReplicas == 0 {
		currentReplicas = 1 // Default if not set
	}

	targetReplicas := int64(opts.Workers)

	// Check if already at target
	if currentReplicas == targetReplicas {
		opts.Logger.Info("cluster already at target scale", "workers", targetReplicas)
		return nil
	}

	// Determine operation type for messaging
	operation := "Scaling"
	if targetReplicas > currentReplicas {
		operation = "Scaling up"
	} else {
		operation = "Scaling down"
	}

	opts.Logger.Info(fmt.Sprintf("%s cluster", operation),
		"name", opts.Name,
		"from", currentReplicas,
		"to", targetReplicas,
	)

	// Build the patch
	patch := map[string]interface{}{
		"spec": map[string]interface{}{
			"workers": map[string]interface{}{
				"replicas": targetReplicas,
			},
		},
	}

	patchBytes, err := json.Marshal(patch)
	if err != nil {
		return fmt.Errorf("marshaling patch: %w", err)
	}

	// Apply the patch
	_, err = c.Dynamic.Resource(client.TenantClusterGVR).Namespace(opts.Namespace).Patch(
		ctx,
		opts.Name,
		types.MergePatchType,
		patchBytes,
		metav1.PatchOptions{},
	)
	if err != nil {
		return fmt.Errorf("patching TenantCluster: %w", err)
	}

	opts.Logger.Success("scale operation initiated",
		"from", currentReplicas,
		"to", targetReplicas,
	)

	// Wait for scaling to complete if requested
	if opts.Wait {
		return waitForScale(ctx, c, opts, targetReplicas)
	}

	return nil
}

// waitForScale polls until the desired number of workers are ready.
func waitForScale(ctx context.Context, c *client.Client, opts *ScaleOptions, targetReplicas int64) error {
	opts.Logger.Info("waiting for workers to be ready", "target", targetReplicas, "timeout", opts.Timeout)

	ctx, cancel := context.WithTimeout(ctx, opts.Timeout)
	defer cancel()

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	startTime := time.Now()
	lastReady := int64(-1)

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout waiting for scale to complete after %v", opts.Timeout)

		case <-ticker.C:
			// Get current cluster info
			tc, err := c.Dynamic.Resource(client.TenantClusterGVR).Namespace(opts.Namespace).Get(ctx, opts.Name, metav1.GetOptions{})
			if err != nil {
				opts.Logger.Warn("error checking cluster status", "error", err)
				continue
			}

			info := ExtractTenantClusterInfo(tc)
			EnrichWithMachineDeploymentStatus(ctx, c, &info)

			ready := info.WorkersReady
			desired := info.WorkersDesired
			if desired == 0 {
				desired = targetReplicas
			}

			// Log progress on changes
			if ready != lastReady {
				elapsed := time.Since(startTime).Round(time.Second)
				opts.Logger.Info("scaling progress", "ready", ready, "desired", targetReplicas, "elapsed", elapsed)
				lastReady = ready
			}

			// Check if complete
			if ready == targetReplicas {
				elapsed := time.Since(startTime).Round(time.Second)
				opts.Logger.Success("scaling complete", "workers", ready, "elapsed", elapsed)
				return nil
			}
		}
	}
}
