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
	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// NewCatalogCmd creates the catalog subcommand for addon catalog administration.
func NewCatalogCmd(logger *log.Logger) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "catalog",
		Short: "Administer the addon definition catalog",
		Long: `Administer AddonDefinitions that describe available addons.

The catalog contains addon metadata including Helm chart coordinates,
default versions, and categories. Tenant users browse this catalog
via butlerctl addon catalog.

Note: catalog create and update are not yet implemented (requires
complex YAML input). Use kubectl apply for now.

Examples:
  butleradm addon catalog list
  butleradm addon catalog get cilium
  butleradm addon catalog delete cilium`,
	}

	cmd.AddCommand(newCatalogListCmd(logger))
	cmd.AddCommand(newCatalogGetCmd(logger))
	cmd.AddCommand(newCatalogDeleteCmd(logger))

	return cmd
}

func newCatalogListCmd(logger *log.Logger) *cobra.Command {
	var (
		outputFmt  string
		kubeconfig string
	)

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List addon definitions in the catalog",
		Long: `List all AddonDefinitions registered in the platform.

Examples:
  butleradm addon catalog list
  butleradm addon catalog list -o yaml
  butleradm addon catalog list -o json`,
		Aliases: []string{"ls"},
		RunE: func(cmd *cobra.Command, args []string) error {
			kubeContext, _ := cmd.Flags().GetString("context")
			return runCatalogList(cmd.Context(), logger, kubeconfig, kubeContext, outputFmt)
		},
	}

	cmd.Flags().StringVarP(&outputFmt, "output", "o", "", "output format (json, yaml)")
	cmd.Flags().StringVar(&kubeconfig, "kubeconfig", "", "path to management cluster kubeconfig")

	return cmd
}

func runCatalogList(ctx context.Context, logger *log.Logger, kubeconfigPath, kubeContext, outputFormat string) error {
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

func newCatalogGetCmd(logger *log.Logger) *cobra.Command {
	var (
		outputFmt  string
		kubeconfig string
	)

	cmd := &cobra.Command{
		Use:   "get NAME",
		Short: "Get details of an addon definition",
		Long: `Get detailed information about a specific AddonDefinition.

Examples:
  butleradm addon catalog get cilium
  butleradm addon catalog get cilium -o yaml
  butleradm addon catalog get cert-manager -o json`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			kubeContext, _ := cmd.Flags().GetString("context")
			return runCatalogGet(cmd.Context(), logger, args[0], kubeconfig, kubeContext, outputFmt)
		},
	}

	cmd.Flags().StringVarP(&outputFmt, "output", "o", "", "output format (json, yaml)")
	cmd.Flags().StringVar(&kubeconfig, "kubeconfig", "", "path to management cluster kubeconfig")

	return cmd
}

func runCatalogGet(ctx context.Context, logger *log.Logger, name, kubeconfigPath, kubeContext, outputFormat string) error {
	c, err := client.New(kubeconfigPath, kubeContext)
	if err != nil {
		return fmt.Errorf("connecting to management cluster: %w", err)
	}

	ad, err := c.Dynamic.Resource(client.AddonDefinitionGVR).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("getting AddonDefinition %s: %w", name, err)
	}

	if outputFormat == "json" {
		return output.PrintJSON(os.Stdout, ad.Object)
	}
	if outputFormat == "yaml" {
		return output.PrintYAML(os.Stdout, ad.Object)
	}

	// Human-readable detail view
	displayName := client.GetNestedString(ad.Object, "spec", "displayName")
	description := client.GetNestedString(ad.Object, "spec", "description")
	category := client.GetNestedString(ad.Object, "spec", "category")
	defaultVersion := client.GetNestedString(ad.Object, "spec", "chart", "defaultVersion")
	repoURL := client.GetNestedString(ad.Object, "spec", "chart", "repoURL")
	chartName := client.GetNestedString(ad.Object, "spec", "chart", "name")
	platform := client.GetNestedBool(ad.Object, "spec", "platform")
	age := output.FormatAge(ad.GetCreationTimestamp().Time)

	fmt.Printf("Name:            %s\n", ad.GetName())
	if displayName != "" {
		fmt.Printf("Display Name:    %s\n", displayName)
	}
	if description != "" {
		fmt.Printf("Description:     %s\n", description)
	}
	fmt.Printf("Category:        %s\n", category)
	fmt.Printf("Default Version: %s\n", defaultVersion)
	fmt.Printf("Platform Addon:  %v\n", platform)
	fmt.Printf("Age:             %s\n", age)
	if repoURL != "" || chartName != "" {
		fmt.Printf("\nChart:\n")
		if repoURL != "" {
			fmt.Printf("  Repository: %s\n", repoURL)
		}
		if chartName != "" {
			fmt.Printf("  Name:       %s\n", chartName)
		}
	}

	return nil
}

func newCatalogDeleteCmd(logger *log.Logger) *cobra.Command {
	var kubeconfig string

	cmd := &cobra.Command{
		Use:   "delete NAME",
		Short: "Delete an addon definition from the catalog",
		Long: `Delete an AddonDefinition from the platform catalog.

This removes the addon from the catalog so it can no longer be installed
on new clusters. Existing installations are not affected.

Examples:
  butleradm addon catalog delete my-custom-addon`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			kubeContext, _ := cmd.Flags().GetString("context")
			return runCatalogDelete(cmd.Context(), logger, args[0], kubeconfig, kubeContext)
		},
	}

	cmd.Flags().StringVar(&kubeconfig, "kubeconfig", "", "path to management cluster kubeconfig")

	return cmd
}

func runCatalogDelete(ctx context.Context, logger *log.Logger, name, kubeconfigPath, kubeContext string) error {
	c, err := client.New(kubeconfigPath, kubeContext)
	if err != nil {
		return fmt.Errorf("connecting to management cluster: %w", err)
	}

	err = c.Dynamic.Resource(client.AddonDefinitionGVR).Delete(ctx, name, metav1.DeleteOptions{})
	if err != nil {
		return fmt.Errorf("deleting AddonDefinition %s: %w", name, err)
	}

	logger.Success("addon definition deleted", "name", name)
	return nil
}
