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
	"os"

	"github.com/butlerdotdev/butler/internal/common/client"
	"github.com/butlerdotdev/butler/internal/common/log"
	"github.com/butlerdotdev/butler/internal/common/output"
	"github.com/butlerdotdev/butler/internal/ctl/cluster"
	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// NewAddonCmd creates the addon parent command.
func NewAddonCmd(logger *log.Logger) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "addon",
		Short: "Manage tenant cluster addons",
		Long: `Manage addons installed on tenant Kubernetes clusters.

Addons are Helm charts managed by Butler. Use the catalog subcommand
to browse available addons, then install them on a cluster.

Examples:
  butlerctl addon catalog
  butlerctl addon list my-cluster -n team-payments
  butlerctl addon install my-cluster cilium
  butlerctl addon get my-cluster cilium
  butlerctl addon uninstall my-cluster cilium`,
	}

	cmd.AddCommand(newCatalogCmd(logger))
	cmd.AddCommand(newListCmd(logger))
	cmd.AddCommand(newInstallCmd(logger))
	cmd.AddCommand(newGetCmd(logger))
	cmd.AddCommand(newUninstallCmd(logger))

	return cmd
}

func newCatalogCmd(logger *log.Logger) *cobra.Command {
	var (
		outputFormat string
		kubeconfig   string
	)

	cmd := &cobra.Command{
		Use:   "catalog",
		Short: "List available addons from the catalog",
		Long: `List all AddonDefinitions registered in the platform.

Examples:
  butlerctl addon catalog
  butlerctl addon catalog -o yaml`,
		RunE: func(cmd *cobra.Command, args []string) error {
			kubeContext, _ := cmd.Flags().GetString("context")
			return runCatalog(cmd.Context(), logger, kubeconfig, kubeContext, outputFormat)
		},
	}

	cmd.Flags().StringVarP(&outputFormat, "output", "o", "", "output format (json, yaml)")
	cmd.Flags().StringVar(&kubeconfig, "kubeconfig", "", "path to management cluster kubeconfig")

	return cmd
}

func runCatalog(ctx context.Context, logger *log.Logger, kubeconfigPath, kubeContext, outputFormat string) error {
	c, err := client.New(kubeconfigPath, kubeContext)
	if err != nil {
		return fmt.Errorf("connecting to management cluster: %w", err)
	}

	list, err := c.Dynamic.Resource(client.AddonDefinitionGVR).List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("listing addon definitions: %w", err)
	}

	if outputFormat == "json" {
		return output.PrintJSON(os.Stdout, list.Items)
	}
	if outputFormat == "yaml" {
		return output.PrintYAML(os.Stdout, list.Items)
	}

	headers := []string{"NAME", "DISPLAY NAME", "CATEGORY", "VERSION", "PLATFORM"}
	var rows [][]string

	for _, ad := range list.Items {
		name := ad.GetName()
		displayName := client.GetNestedString(ad.Object, "spec", "displayName")
		category := client.GetNestedString(ad.Object, "spec", "category")
		version := client.GetNestedString(ad.Object, "spec", "chart", "defaultVersion")
		platform := client.GetNestedBool(ad.Object, "spec", "platform")

		platformStr := ""
		if platform {
			platformStr = "Yes"
		}

		rows = append(rows, []string{name, displayName, category, version, platformStr})
	}

	table := output.NewTable(os.Stdout, headers...)
	for _, row := range rows {
		table.AddRow(row...)
	}
	table.Flush()
	return nil
}

func newListCmd(logger *log.Logger) *cobra.Command {
	var (
		namespace  string
		outputFmt  string
		kubeconfig string
	)

	cmd := &cobra.Command{
		Use:   "list CLUSTER",
		Short: "List addons installed on a cluster",
		Long: `List all TenantAddons installed on a specific tenant cluster.

Examples:
  butlerctl addon list my-cluster
  butlerctl addon list my-cluster -n team-payments
  butlerctl addon list my-cluster -o json`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			kubeContext, _ := cmd.Flags().GetString("context")
			return runList(cmd.Context(), logger, args[0], namespace, kubeconfig, kubeContext, outputFmt)
		},
	}

	cmd.Flags().StringVarP(&namespace, "namespace", "n", cluster.DefaultTenantNamespace, "namespace of the TenantCluster")
	cmd.Flags().StringVarP(&outputFmt, "output", "o", "", "output format (table, wide, json, yaml)")
	cmd.Flags().StringVar(&kubeconfig, "kubeconfig", "", "path to management cluster kubeconfig")

	return cmd
}

func runList(ctx context.Context, logger *log.Logger, clusterName, namespace, kubeconfigPath, kubeContext, outputFormat string) error {
	format, err := output.ParseFormat(outputFormat)
	if err != nil {
		return err
	}

	c, err := client.New(kubeconfigPath, kubeContext)
	if err != nil {
		return fmt.Errorf("connecting to management cluster: %w", err)
	}

	list, err := c.Dynamic.Resource(client.TenantAddonGVR).Namespace(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("listing tenant addons: %w", err)
	}

	// Filter by clusterRef
	var filtered []map[string]interface{}
	for _, ta := range list.Items {
		ref := client.GetNestedString(ta.Object, "spec", "clusterRef", "name")
		if ref == clusterName {
			filtered = append(filtered, ta.Object)
		}
	}

	if format == output.FormatJSON {
		return output.PrintJSON(os.Stdout, filtered)
	}
	if format == output.FormatYAML {
		return output.PrintYAML(os.Stdout, filtered)
	}

	wide := format == output.FormatWide

	headers := []string{"NAME", "ADDON", "VERSION", "PHASE", "INSTALLED"}
	if wide {
		headers = append(headers, "NAMESPACE", "HELM RELEASE", "HELM NAMESPACE")
	}

	table := output.NewTable(os.Stdout, headers...)

	for _, obj := range filtered {
		name := client.GetNestedString(obj, "metadata", "name")
		addon := client.GetNestedString(obj, "spec", "addon")
		version := client.GetNestedString(obj, "spec", "version")
		phase := client.GetNestedString(obj, "status", "phase")
		installed := client.GetNestedString(obj, "status", "installedVersion")

		if addon == "" {
			addon = client.GetNestedString(obj, "spec", "helm", "chart")
		}

		row := []string{name, addon, version, output.ColorizePhase(phase), installed}
		if wide {
			ns := client.GetNestedString(obj, "metadata", "namespace")
			helmRelease := client.GetNestedString(obj, "status", "helmRelease", "name")
			helmNs := client.GetNestedString(obj, "status", "helmRelease", "namespace")
			row = append(row, ns, helmRelease, helmNs)
		}

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
		Use:   "get CLUSTER ADDON",
		Short: "Get details of an installed addon",
		Long: `Get detailed information about a specific addon on a tenant cluster.

Examples:
  butlerctl addon get my-cluster cilium
  butlerctl addon get my-cluster cilium -o yaml`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			kubeContext, _ := cmd.Flags().GetString("context")
			return runGet(cmd.Context(), logger, args[0], args[1], namespace, kubeconfig, kubeContext, outputFmt)
		},
	}

	cmd.Flags().StringVarP(&namespace, "namespace", "n", cluster.DefaultTenantNamespace, "namespace of the TenantCluster")
	cmd.Flags().StringVarP(&outputFmt, "output", "o", "", "output format (json, yaml)")
	cmd.Flags().StringVar(&kubeconfig, "kubeconfig", "", "path to management cluster kubeconfig")

	return cmd
}

func runGet(ctx context.Context, logger *log.Logger, clusterName, addonName, namespace, kubeconfigPath, kubeContext, outputFormat string) error {
	c, err := client.New(kubeconfigPath, kubeContext)
	if err != nil {
		return fmt.Errorf("connecting to management cluster: %w", err)
	}

	// TenantAddon names follow the pattern: {addon} or {cluster}-{addon}
	// Try the addon name directly first, then the cluster-prefixed name.
	ta, err := c.Dynamic.Resource(client.TenantAddonGVR).Namespace(namespace).Get(ctx, addonName, metav1.GetOptions{})
	if err != nil {
		prefixed := clusterName + "-" + addonName
		ta, err = c.Dynamic.Resource(client.TenantAddonGVR).Namespace(namespace).Get(ctx, prefixed, metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("getting TenantAddon %s (also tried %s): %w", addonName, prefixed, err)
		}
	}

	if outputFormat == "json" {
		return output.PrintJSON(os.Stdout, ta.Object)
	}
	if outputFormat == "yaml" {
		return output.PrintYAML(os.Stdout, ta.Object)
	}

	// Table display
	addon := client.GetNestedString(ta.Object, "spec", "addon")
	version := client.GetNestedString(ta.Object, "spec", "version")
	phase := client.GetNestedString(ta.Object, "status", "phase")
	installed := client.GetNestedString(ta.Object, "status", "installedVersion")
	message := client.GetNestedString(ta.Object, "status", "message")
	helmName := client.GetNestedString(ta.Object, "status", "helmRelease", "name")
	helmNs := client.GetNestedString(ta.Object, "status", "helmRelease", "namespace")
	helmStatus := client.GetNestedString(ta.Object, "status", "helmRelease", "status")

	fmt.Printf("Name:              %s\n", ta.GetName())
	fmt.Printf("Cluster:           %s\n", clusterName)
	fmt.Printf("Addon:             %s\n", addon)
	fmt.Printf("Requested Version: %s\n", version)
	fmt.Printf("Installed Version: %s\n", installed)
	fmt.Printf("Phase:             %s\n", phase)
	if message != "" {
		fmt.Printf("Message:           %s\n", message)
	}
	if helmName != "" {
		fmt.Printf("\nHelm Release:\n")
		fmt.Printf("  Name:      %s\n", helmName)
		fmt.Printf("  Namespace: %s\n", helmNs)
		fmt.Printf("  Status:    %s\n", helmStatus)
	}

	return nil
}
