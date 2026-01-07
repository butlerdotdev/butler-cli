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
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/butlerdotdev/butler/internal/common/client"
	"github.com/butlerdotdev/butler/internal/common/log"
	"github.com/butlerdotdev/butler/internal/common/output"
	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// DestroyOptions holds options for the destroy command.
// RBAC Note: In the future, this will check Team membership and permissions
// before allowing destruction. For now, any authenticated user can destroy
// clusters they can access.
type DestroyOptions struct {
	Name      string
	Namespace string

	// Confirmation behavior
	Force   bool // Skip confirmation prompt
	NoWait  bool // Don't wait for deletion to complete
	Timeout time.Duration

	// Future RBAC fields (not implemented yet)
	// Team        string // Team owning this cluster
	// RequireRole string // Minimum role required (owner, admin, member)

	Logger *log.Logger
}

// DefaultDestroyOptions returns DestroyOptions with sensible defaults.
func DefaultDestroyOptions(logger *log.Logger) *DestroyOptions {
	return &DestroyOptions{
		Namespace: DefaultTenantNamespace,
		Timeout:   10 * time.Minute,
		Logger:    logger,
	}
}

// NewDestroyCmd creates the cluster destroy command.
// This is functionally equivalent to 'delete' but with more explicit messaging
// about the destructive nature of the operation.
func NewDestroyCmd(logger *log.Logger) *cobra.Command {
	opts := DefaultDestroyOptions(logger)

	cmd := &cobra.Command{
		Use:   "destroy NAME",
		Short: "Permanently destroy a tenant cluster",
		Long: `Permanently destroy a tenant cluster and all associated resources.

⚠️  WARNING: This is a destructive operation that cannot be undone.

This will permanently delete:
  • All worker node VMs (data on local disks will be lost)
  • The hosted control plane (Kamaji TenantControlPlane)
  • All Kubernetes resources in the tenant cluster
  • The tenant namespace and its contents
  • Associated secrets, PVCs, and configmaps
  • CAPI Machine and MachineDeployment resources

The cluster's workloads, persistent volumes, and any data stored within
the cluster will be permanently lost unless externally backed up.

Examples:
  # Destroy with confirmation prompt (recommended)
  butlerctl cluster destroy my-cluster

  # Destroy without confirmation (use with caution)
  butlerctl cluster destroy my-cluster --force

  # Destroy without waiting for completion
  butlerctl cluster destroy my-cluster --force --no-wait

  # Destroy with custom timeout
  butlerctl cluster destroy my-cluster --force --timeout 20m`,
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeClusterNames,
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.Name = args[0]
			return runDestroy(cmd.Context(), opts)
		},
	}

	cmd.Flags().StringVarP(&opts.Namespace, "namespace", "n", opts.Namespace, "Namespace of the TenantCluster")
	cmd.Flags().BoolVar(&opts.Force, "force", false, "Skip confirmation prompt (dangerous)")
	cmd.Flags().BoolVar(&opts.NoWait, "no-wait", false, "Don't wait for deletion to complete")
	cmd.Flags().DurationVar(&opts.Timeout, "timeout", opts.Timeout, "Timeout when waiting for deletion")

	// Aliases: --yes is common in other tools
	cmd.Flags().BoolVarP(&opts.Force, "yes", "y", false, "Skip confirmation prompt (alias for --force)")

	return cmd
}

// runDestroy executes the destroy operation.
func runDestroy(ctx context.Context, opts *DestroyOptions) error {
	// First, verify we're connected to a management cluster
	if err := RequireManagementCluster(ctx); err != nil {
		return err
	}

	c, err := client.NewFromDefault()
	if err != nil {
		return fmt.Errorf("creating client: %w", err)
	}

	// Get the cluster to show what we're destroying
	tc, err := c.Dynamic.Resource(client.TenantClusterGVR).Namespace(opts.Namespace).Get(ctx, opts.Name, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			return fmt.Errorf("TenantCluster %q not found in namespace %q", opts.Name, opts.Namespace)
		}
		return fmt.Errorf("getting TenantCluster: %w", err)
	}

	info := ExtractTenantClusterInfo(tc)
	EnrichWithMachineDeploymentStatus(ctx, c, &info)
	EnrichWithControlPlaneEndpoint(ctx, c, &info)

	// FUTURE RBAC CHECK:
	// team := info.Labels["butler.butlerlabs.dev/team"]
	// if team != "" {
	//     if err := checkTeamPermission(ctx, c, team, "delete"); err != nil {
	//         return fmt.Errorf("permission denied: %w", err)
	//     }
	// }

	// Show detailed destruction summary
	printDestructionSummary(opts, &info)

	// Confirm destruction unless forced
	if !opts.Force {
		if err := confirmDestruction(opts.Name); err != nil {
			return err
		}
	}

	opts.Logger.Info("destroying tenant cluster", "name", opts.Name, "namespace", opts.Namespace)

	// Delete the TenantCluster CR - controller handles cleanup
	err = c.Dynamic.Resource(client.TenantClusterGVR).Namespace(opts.Namespace).Delete(ctx, opts.Name, metav1.DeleteOptions{})
	if err != nil {
		return fmt.Errorf("deleting TenantCluster: %w", err)
	}

	opts.Logger.Success("destruction initiated", "name", opts.Name)

	if opts.NoWait {
		fmt.Println("\nCluster destruction has been initiated.")
		fmt.Println("The controller will clean up all resources in the background.")
		fmt.Println("\nUse 'butlerctl cluster list' to monitor progress.")
		return nil
	}

	return waitForDestruction(ctx, c, opts)
}

// printDestructionSummary shows what will be destroyed.
func printDestructionSummary(opts *DestroyOptions, info *TenantClusterInfo) {
	fmt.Println()
	fmt.Println(output.ColorizePhase("⚠️  CLUSTER DESTRUCTION WARNING"))
	fmt.Println(strings.Repeat("═", 50))
	fmt.Println()
	fmt.Printf("Cluster:    %s\n", output.ColorizePhase(info.Name))
	fmt.Printf("Namespace:  %s\n", info.Namespace)
	fmt.Printf("Phase:      %s\n", output.ColorizePhase(info.Phase))
	fmt.Printf("K8s:        %s\n", info.KubernetesVersion)
	fmt.Printf("Workers:    %d node(s)\n", info.WorkersReady)
	if info.Endpoint != "" {
		fmt.Printf("Endpoint:   %s\n", info.Endpoint)
	}
	fmt.Println()
	fmt.Println("The following will be permanently deleted:")
	fmt.Println("  • All worker node VMs and their local storage")
	fmt.Println("  • Hosted control plane pods")
	fmt.Println("  • All Kubernetes workloads in the cluster")
	fmt.Println("  • All PersistentVolumes and PersistentVolumeClaims")
	fmt.Println("  • Tenant namespace:", info.TenantNamespace)
	fmt.Println()
}

// confirmDestruction requires the user to type the cluster name.
func confirmDestruction(name string) error {
	fmt.Printf("To confirm destruction, type the cluster name: ")

	reader := bufio.NewReader(os.Stdin)
	input, err := reader.ReadString('\n')
	if err != nil {
		return fmt.Errorf("reading confirmation: %w", err)
	}

	input = strings.TrimSpace(input)
	if input != name {
		fmt.Println()
		return fmt.Errorf("destruction cancelled: you typed %q, expected %q", input, name)
	}

	return nil
}

// waitForDestruction polls until the cluster is gone.
func waitForDestruction(ctx context.Context, c *client.Client, opts *DestroyOptions) error {
	opts.Logger.Info("waiting for destruction to complete", "timeout", opts.Timeout)

	ctx, cancel := context.WithTimeout(ctx, opts.Timeout)
	defer cancel()

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	startTime := time.Now()
	lastPhase := ""

	for {
		select {
		case <-ctx.Done():
			if ctx.Err() == context.DeadlineExceeded {
				return fmt.Errorf("timeout waiting for cluster destruction after %v", opts.Timeout)
			}
			return ctx.Err()

		case <-ticker.C:
			tc, err := c.Dynamic.Resource(client.TenantClusterGVR).Namespace(opts.Namespace).Get(ctx, opts.Name, metav1.GetOptions{})
			if err != nil {
				if errors.IsNotFound(err) {
					elapsed := time.Since(startTime).Round(time.Second)
					opts.Logger.Success("cluster destroyed", "elapsed", elapsed)
					fmt.Println("\n✓ Cluster has been completely destroyed.")
					return nil
				}
				opts.Logger.Warn("error checking cluster status", "error", err)
				continue
			}

			// Check phase for progress updates
			phase := GetNestedString(tc.Object, "status", "phase")
			if phase != lastPhase {
				elapsed := time.Since(startTime).Round(time.Second)
				opts.Logger.Info("destruction progress", "phase", phase, "elapsed", elapsed)
				lastPhase = phase
			}
		}
	}
}
