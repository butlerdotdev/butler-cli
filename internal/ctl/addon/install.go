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

package addon

import (
	"context"
	"fmt"

	"github.com/butlerdotdev/butler/internal/common/client"
	"github.com/butlerdotdev/butler/internal/common/log"
	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func newInstallCmd(logger *log.Logger) *cobra.Command {
	var (
		namespace  string
		version    string
		kubeconfig string
	)

	cmd := &cobra.Command{
		Use:   "install CLUSTER ADDON",
		Short: "Install an addon on a tenant cluster",
		Long: `Install an addon from the catalog onto a tenant cluster.

The addon must exist as an AddonDefinition in the catalog. If no version
is specified, the default version from the AddonDefinition is used.

Examples:
  butlerctl addon install my-cluster cilium
  butlerctl addon install my-cluster cert-manager --version v1.16.2
  butlerctl addon install my-cluster longhorn -n team-payments`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			kubeContext, _ := cmd.Flags().GetString("context")
			return runInstall(cmd.Context(), logger, args[0], args[1], namespace, version, kubeconfig, kubeContext)
		},
	}

	cmd.Flags().StringVarP(&namespace, "namespace", "n", defaultTenantNamespace, "namespace of the TenantCluster")
	cmd.Flags().StringVar(&version, "version", "", "addon version (defaults to catalog default)")
	cmd.Flags().StringVar(&kubeconfig, "kubeconfig", "", "path to management cluster kubeconfig")

	return cmd
}

func runInstall(ctx context.Context, logger *log.Logger, clusterName, addonName, namespace, version, kubeconfigPath, kubeContext string) error {
	c, err := client.New(kubeconfigPath, kubeContext)
	if err != nil {
		return fmt.Errorf("connecting to management cluster: %w", err)
	}

	// Resolve version from AddonDefinition if not specified
	if version == "" {
		ad, err := c.Dynamic.Resource(client.AddonDefinitionGVR).Get(ctx, addonName, metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("addon %q not found in catalog: %w", addonName, err)
		}
		version = client.GetNestedString(ad.Object, "spec", "chart", "defaultVersion")
		if version == "" {
			return fmt.Errorf("addon %q has no default version; specify --version", addonName)
		}
		logger.Info("using default version from catalog", "version", version)
	}

	// Build TenantAddon CR
	ta := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "butler.butlerlabs.dev/v1alpha1",
			"kind":       "TenantAddon",
			"metadata": map[string]interface{}{
				"name":      addonName,
				"namespace": namespace,
			},
			"spec": map[string]interface{}{
				"clusterRef": map[string]interface{}{
					"name": clusterName,
				},
				"addon":   addonName,
				"version": version,
			},
		},
	}

	_, err = c.Dynamic.Resource(client.TenantAddonGVR).Namespace(namespace).Create(ctx, ta, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("creating TenantAddon: %w", err)
	}

	logger.Success("addon install initiated", "addon", addonName, "version", version, "cluster", clusterName)
	return nil
}

func newUninstallCmd(logger *log.Logger) *cobra.Command {
	var (
		namespace  string
		kubeconfig string
	)

	cmd := &cobra.Command{
		Use:   "uninstall CLUSTER ADDON",
		Short: "Uninstall an addon from a tenant cluster",
		Long: `Remove an addon from a tenant cluster by deleting the TenantAddon resource.

The controller will uninstall the Helm release from the tenant cluster.

Examples:
  butlerctl addon uninstall my-cluster longhorn
  butlerctl addon uninstall my-cluster cert-manager -n team-payments`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			kubeContext, _ := cmd.Flags().GetString("context")
			return runUninstall(cmd.Context(), logger, args[0], args[1], namespace, kubeconfig, kubeContext)
		},
	}

	cmd.Flags().StringVarP(&namespace, "namespace", "n", defaultTenantNamespace, "namespace of the TenantCluster")
	cmd.Flags().StringVar(&kubeconfig, "kubeconfig", "", "path to management cluster kubeconfig")

	return cmd
}

func runUninstall(ctx context.Context, logger *log.Logger, clusterName, addonName, namespace, kubeconfigPath, kubeContext string) error {
	c, err := client.New(kubeconfigPath, kubeContext)
	if err != nil {
		return fmt.Errorf("connecting to management cluster: %w", err)
	}

	// Try addon name directly, then cluster-prefixed
	err = c.Dynamic.Resource(client.TenantAddonGVR).Namespace(namespace).Delete(ctx, addonName, metav1.DeleteOptions{})
	if err != nil {
		prefixed := clusterName + "-" + addonName
		err = c.Dynamic.Resource(client.TenantAddonGVR).Namespace(namespace).Delete(ctx, prefixed, metav1.DeleteOptions{})
		if err != nil {
			return fmt.Errorf("deleting TenantAddon %s: %w", addonName, err)
		}
	}

	logger.Success("addon uninstall initiated", "addon", addonName, "cluster", clusterName)
	return nil
}
