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

// Package addon implements butleradm addon commands for managing
// ManagementAddons and the AddonDefinition catalog.
package addon

import (
	"context"
	"fmt"
	"os"

	"github.com/butlerdotdev/butler/internal/common/auth"
	"github.com/butlerdotdev/butler/internal/common/client"
	"github.com/butlerdotdev/butler/internal/common/log"
	"github.com/butlerdotdev/butler/internal/common/output"
	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// NewAddonCmd creates the addon parent command for butleradm.
func NewAddonCmd(logger *log.Logger) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "addon",
		Args:  cobra.NoArgs,
		RunE:  output.ShowHelp,
		Short: "Manage platform addons and the addon catalog",
		Long: `Manage addons installed on the management cluster and administer
the addon definition catalog.

ManagementAddons are Helm charts installed on the management cluster itself
(Steward, cert-manager, MetalLB, etc.). The catalog contains AddonDefinitions
that describe available addons for both management and tenant clusters.

Examples:
  butleradm addon list
  butleradm addon install cilium --version 1.16.0
  butleradm addon get cilium
  butleradm addon uninstall cilium
  butleradm addon catalog list
  butleradm addon catalog get cilium`,
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			auth.WarnIfUnauthenticated()
		},
	}

	cmd.AddCommand(newListCmd(logger))
	cmd.AddCommand(newInstallCmd(logger))
	cmd.AddCommand(newGetCmd(logger))
	cmd.AddCommand(newUninstallCmd(logger))
	cmd.AddCommand(NewCatalogCmd(logger))

	return cmd
}

func newListCmd(logger *log.Logger) *cobra.Command {
	var (
		outputFmt  string
		kubeconfig string
	)

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List management addons",
		Long: `List all ManagementAddons installed on the management cluster.

Examples:
  butleradm addon list
  butleradm addon list -o json
  butleradm addon list -o yaml`,
		Aliases: []string{"ls"},
		RunE: func(cmd *cobra.Command, args []string) error {
			kubeContext, _ := cmd.Flags().GetString("context")
			return runList(cmd.Context(), logger, kubeconfig, kubeContext, outputFmt)
		},
	}

	cmd.Flags().StringVarP(&outputFmt, "output", "o", "", "output format (json, yaml)")
	cmd.Flags().StringVar(&kubeconfig, "kubeconfig", "", "path to management cluster kubeconfig")

	return cmd
}

func runList(ctx context.Context, logger *log.Logger, kubeconfigPath, kubeContext, outputFormat string) error {
	c, err := client.New(kubeconfigPath, kubeContext)
	if err != nil {
		return fmt.Errorf("connecting to management cluster: %w", err)
	}

	// ManagementAddon is cluster-scoped, no namespace needed
	list, err := c.Dynamic.Resource(client.ManagementAddonGVR).List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("listing management addons: %w", err)
	}

	if outputFormat == "json" {
		return output.PrintJSON(os.Stdout, list.Items)
	}
	if outputFormat == "yaml" {
		return output.PrintYAML(os.Stdout, list.Items)
	}

	headers := []string{"NAME", "ADDON", "VERSION", "INSTALLED", "PHASE", "AGE"}
	var rows [][]string

	for _, ma := range list.Items {
		name := ma.GetName()
		addon := client.GetNestedString(ma.Object, "spec", "addon")
		version := client.GetNestedString(ma.Object, "spec", "version")
		installed := client.GetNestedString(ma.Object, "status", "installedVersion")
		phase := client.GetNestedString(ma.Object, "status", "phase")
		age := output.FormatAge(ma.GetCreationTimestamp().Time)

		rows = append(rows, []string{
			name,
			addon,
			version,
			installed,
			output.ColorizePhase(phase),
			age,
		})
	}

	table := output.NewTable(os.Stdout, headers...)
	for _, row := range rows {
		table.AddRow(row...)
	}
	table.Flush()
	return nil
}

func newInstallCmd(logger *log.Logger) *cobra.Command {
	var (
		version    string
		paused     bool
		kubeconfig string
	)

	cmd := &cobra.Command{
		Use:   "install NAME",
		Short: "Install a management addon",
		Long: `Install an addon on the management cluster by creating a ManagementAddon resource.

The addon must exist as an AddonDefinition in the catalog. If no version
is specified, the default version from the AddonDefinition is used.

Examples:
  butleradm addon install cilium
  butleradm addon install cert-manager --version v1.16.2
  butleradm addon install longhorn --paused`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			kubeContext, _ := cmd.Flags().GetString("context")
			return runInstall(cmd.Context(), logger, args[0], version, paused, kubeconfig, kubeContext)
		},
	}

	cmd.Flags().StringVar(&version, "version", "", "addon version (defaults to catalog default)")
	cmd.Flags().BoolVar(&paused, "paused", false, "create the addon in a paused state")
	cmd.Flags().StringVar(&kubeconfig, "kubeconfig", "", "path to management cluster kubeconfig")

	return cmd
}

func runInstall(ctx context.Context, logger *log.Logger, name, version string, paused bool, kubeconfigPath, kubeContext string) error {
	c, err := client.New(kubeconfigPath, kubeContext)
	if err != nil {
		return fmt.Errorf("connecting to management cluster: %w", err)
	}

	// Resolve version from AddonDefinition if not specified
	if version == "" {
		ad, err := c.Dynamic.Resource(client.AddonDefinitionGVR).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("addon %q not found in catalog: %w", name, err)
		}
		version = client.GetNestedString(ad.Object, "spec", "chart", "defaultVersion")
		if version == "" {
			return fmt.Errorf("addon %q has no default version; specify --version", name)
		}
		logger.Info("using default version from catalog", "version", version)
	}

	// Build ManagementAddon CR (cluster-scoped, no namespace)
	spec := map[string]interface{}{
		"addon":   name,
		"version": version,
	}
	if paused {
		spec["paused"] = true
	}

	ma := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "butler.butlerlabs.dev/v1alpha1",
			"kind":       "ManagementAddon",
			"metadata": map[string]interface{}{
				"name": name,
			},
			"spec": spec,
		},
	}

	_, err = c.Dynamic.Resource(client.ManagementAddonGVR).Create(ctx, ma, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("creating ManagementAddon: %w", err)
	}

	logger.Success("management addon install initiated", "addon", name, "version", version)
	return nil
}

func newGetCmd(logger *log.Logger) *cobra.Command {
	var (
		outputFmt  string
		kubeconfig string
	)

	cmd := &cobra.Command{
		Use:   "get NAME",
		Short: "Get details of a management addon",
		Long: `Get detailed information about a specific ManagementAddon.

Examples:
  butleradm addon get cilium
  butleradm addon get cilium -o yaml
  butleradm addon get cert-manager -o json`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			kubeContext, _ := cmd.Flags().GetString("context")
			return runGet(cmd.Context(), logger, args[0], kubeconfig, kubeContext, outputFmt)
		},
	}

	cmd.Flags().StringVarP(&outputFmt, "output", "o", "", "output format (json, yaml)")
	cmd.Flags().StringVar(&kubeconfig, "kubeconfig", "", "path to management cluster kubeconfig")

	return cmd
}

func runGet(ctx context.Context, logger *log.Logger, name, kubeconfigPath, kubeContext, outputFormat string) error {
	c, err := client.New(kubeconfigPath, kubeContext)
	if err != nil {
		return fmt.Errorf("connecting to management cluster: %w", err)
	}

	ma, err := c.Dynamic.Resource(client.ManagementAddonGVR).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("getting ManagementAddon %s: %w", name, err)
	}

	if outputFormat == "json" {
		return output.PrintJSON(os.Stdout, ma.Object)
	}
	if outputFormat == "yaml" {
		return output.PrintYAML(os.Stdout, ma.Object)
	}

	// Human-readable detail view
	addon := client.GetNestedString(ma.Object, "spec", "addon")
	version := client.GetNestedString(ma.Object, "spec", "version")
	paused := client.GetNestedBool(ma.Object, "spec", "paused")
	phase := client.GetNestedString(ma.Object, "status", "phase")
	installed := client.GetNestedString(ma.Object, "status", "installedVersion")
	message := client.GetNestedString(ma.Object, "status", "message")
	helmName := client.GetNestedString(ma.Object, "status", "helmRelease", "name")
	helmNs := client.GetNestedString(ma.Object, "status", "helmRelease", "namespace")
	helmStatus := client.GetNestedString(ma.Object, "status", "helmRelease", "status")
	age := output.FormatAge(ma.GetCreationTimestamp().Time)

	fmt.Printf("Name:              %s\n", ma.GetName())
	fmt.Printf("Addon:             %s\n", addon)
	fmt.Printf("Requested Version: %s\n", version)
	fmt.Printf("Installed Version: %s\n", installed)
	fmt.Printf("Phase:             %s\n", output.ColorizePhase(phase))
	fmt.Printf("Paused:            %v\n", paused)
	fmt.Printf("Age:               %s\n", age)
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

func newUninstallCmd(logger *log.Logger) *cobra.Command {
	var kubeconfig string

	cmd := &cobra.Command{
		Use:   "uninstall NAME",
		Short: "Uninstall a management addon",
		Long: `Remove a management addon by deleting the ManagementAddon resource.

The controller will uninstall the Helm release from the management cluster.

Examples:
  butleradm addon uninstall longhorn
  butleradm addon uninstall cert-manager`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			kubeContext, _ := cmd.Flags().GetString("context")
			return runUninstall(cmd.Context(), logger, args[0], kubeconfig, kubeContext)
		},
	}

	cmd.Flags().StringVar(&kubeconfig, "kubeconfig", "", "path to management cluster kubeconfig")

	return cmd
}

func runUninstall(ctx context.Context, logger *log.Logger, name, kubeconfigPath, kubeContext string) error {
	c, err := client.New(kubeconfigPath, kubeContext)
	if err != nil {
		return fmt.Errorf("connecting to management cluster: %w", err)
	}

	err = c.Dynamic.Resource(client.ManagementAddonGVR).Delete(ctx, name, metav1.DeleteOptions{})
	if err != nil {
		return fmt.Errorf("deleting ManagementAddon %s: %w", name, err)
	}

	logger.Success("management addon uninstall initiated", "addon", name)
	return nil
}
