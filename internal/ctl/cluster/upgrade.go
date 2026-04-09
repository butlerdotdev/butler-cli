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

	"github.com/butlerdotdev/butler/internal/common/client"
	"github.com/butlerdotdev/butler/internal/common/log"
	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func newUpgradeCmd(logger *log.Logger) *cobra.Command {
	var (
		namespace  string
		version    string
		kubeconfig string
	)

	cmd := &cobra.Command{
		Use:   "upgrade NAME --version VERSION",
		Short: "Upgrade the Kubernetes version of a cluster",
		Long: `Upgrade the Kubernetes version of a tenant cluster.

This patches spec.kubernetesVersion on the TenantCluster resource. The
controller will then orchestrate the rolling upgrade of the control plane
and worker nodes.

The version must be a valid Kubernetes version (e.g., v1.31.0, v1.32.0).

Examples:
  # Upgrade to a specific version
  butlerctl cluster upgrade my-cluster --version v1.32.0

  # Upgrade a cluster in a specific namespace
  butlerctl cluster upgrade my-cluster --version v1.32.0 -n team-payments`,
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeClusterNames,
		RunE: func(cmd *cobra.Command, args []string) error {
			kubeContext, _ := cmd.Flags().GetString("context")
			return runUpgrade(cmd.Context(), logger, args[0], version, namespace, kubeconfig, kubeContext)
		},
	}

	cmd.Flags().StringVarP(&namespace, "namespace", "n", DefaultTenantNamespace, "namespace of the TenantCluster")
	cmd.Flags().StringVar(&version, "version", "", "target Kubernetes version (required)")
	cmd.Flags().StringVar(&kubeconfig, "kubeconfig", "", "path to management cluster kubeconfig")

	_ = cmd.MarkFlagRequired("version")

	return cmd
}

func runUpgrade(ctx context.Context, logger *log.Logger, name, version, namespace, kubeconfigPath, kubeContext string) error {
	c, err := client.New(kubeconfigPath, kubeContext)
	if err != nil {
		return fmt.Errorf("connecting to management cluster: %w", err)
	}

	// Get current cluster to show old version
	tc, err := c.Dynamic.Resource(client.TenantClusterGVR).Namespace(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("getting TenantCluster %s/%s: %w", namespace, name, err)
	}

	currentVersion := client.GetNestedString(tc.Object, "spec", "kubernetesVersion")
	if currentVersion == version {
		logger.Info("cluster already at requested version", "version", version)
		return nil
	}

	// Build the patch
	patch := map[string]interface{}{
		"spec": map[string]interface{}{
			"kubernetesVersion": version,
		},
	}

	patchBytes, err := json.Marshal(patch)
	if err != nil {
		return fmt.Errorf("marshaling patch: %w", err)
	}

	_, err = c.Dynamic.Resource(client.TenantClusterGVR).Namespace(namespace).Patch(
		ctx,
		name,
		types.MergePatchType,
		patchBytes,
		metav1.PatchOptions{},
	)
	if err != nil {
		return fmt.Errorf("patching TenantCluster: %w", err)
	}

	logger.Success("kubernetes version upgrade initiated",
		"cluster", name,
		"from", currentVersion,
		"to", version,
	)
	return nil
}
