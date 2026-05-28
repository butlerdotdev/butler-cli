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

// Package idp implements butleradm idp commands for managing
// IdentityProvider resources.
package idp

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/butlerdotdev/butler/internal/common/auth"
	"github.com/butlerdotdev/butler/internal/common/client"
	"github.com/butlerdotdev/butler/internal/common/log"
	"github.com/butlerdotdev/butler/internal/common/output"
	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// NewIDPCmd creates the idp parent command for butleradm.
func NewIDPCmd(logger *log.Logger) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "idp",
		Args:  cobra.NoArgs,
		RunE:  output.ShowHelp,
		Short: "Manage identity providers",
		Long: `Manage IdentityProvider resources for SSO/OIDC authentication.

IdentityProviders configure external authentication providers such as
Google Workspace, Microsoft Entra ID, Okta, or generic OIDC providers.

Note: idp create is not implemented due to the complexity of OIDC
configuration (secrets, scopes, claim mappings). Use kubectl apply
with a YAML manifest instead.

Examples:
  butleradm idp list
  butleradm idp get google-workspace
  butleradm idp delete old-provider`,
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			auth.WarnIfUnauthenticated()
		},
	}

	cmd.AddCommand(newListCmd(logger))
	cmd.AddCommand(newGetCmd(logger))
	cmd.AddCommand(newDeleteCmd(logger))

	return cmd
}

func newListCmd(logger *log.Logger) *cobra.Command {
	var (
		outputFmt  string
		kubeconfig string
	)

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List identity providers",
		Long: `List all IdentityProvider resources configured on the platform.

Examples:
  butleradm idp list
  butleradm idp list -o json
  butleradm idp list -o yaml`,
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

	// IdentityProvider is cluster-scoped
	list, err := c.Dynamic.Resource(client.IdentityProviderGVR).List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("listing identity providers: %w", err)
	}

	if outputFormat == "json" {
		return output.PrintJSON(os.Stdout, list.Items)
	}
	if outputFormat == "yaml" {
		return output.PrintYAML(os.Stdout, list.Items)
	}

	headers := []string{"NAME", "TYPE", "ISSUER", "STATUS", "AGE"}
	var rows [][]string

	for _, idp := range list.Items {
		name := idp.GetName()
		idpType := client.GetNestedString(idp.Object, "spec", "type")
		issuer := client.GetNestedString(idp.Object, "spec", "issuerURL")
		phase := client.GetNestedString(idp.Object, "status", "phase")
		age := output.FormatAge(idp.GetCreationTimestamp().Time)

		// Truncate long issuer URLs for table display
		if len(issuer) > 50 {
			issuer = issuer[:47] + "..."
		}

		rows = append(rows, []string{
			name,
			idpType,
			issuer,
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

func newGetCmd(logger *log.Logger) *cobra.Command {
	var (
		outputFmt  string
		kubeconfig string
	)

	cmd := &cobra.Command{
		Use:   "get NAME",
		Short: "Get details of an identity provider",
		Long: `Get detailed information about a specific IdentityProvider.

Examples:
  butleradm idp get google-workspace
  butleradm idp get google-workspace -o yaml
  butleradm idp get okta-prod -o json`,
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

	idp, err := c.Dynamic.Resource(client.IdentityProviderGVR).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("getting IdentityProvider %s: %w", name, err)
	}

	if outputFormat == "json" {
		return output.PrintJSON(os.Stdout, idp.Object)
	}
	if outputFormat == "yaml" {
		return output.PrintYAML(os.Stdout, idp.Object)
	}

	// Human-readable detail view
	idpType := client.GetNestedString(idp.Object, "spec", "type")
	issuer := client.GetNestedString(idp.Object, "spec", "issuerURL")
	clientID := client.GetNestedString(idp.Object, "spec", "clientID")
	phase := client.GetNestedString(idp.Object, "status", "phase")
	message := client.GetNestedString(idp.Object, "status", "message")
	age := output.FormatAge(idp.GetCreationTimestamp().Time)

	// Extract scopes
	scopesRaw, _, _ := unstructuredNestedSlice(idp.Object, "spec", "scopes")
	var scopes []string
	for _, s := range scopesRaw {
		if str, ok := s.(string); ok {
			scopes = append(scopes, str)
		}
	}

	// Extract claim mappings
	emailClaim := client.GetNestedString(idp.Object, "spec", "claims", "email")
	groupsClaim := client.GetNestedString(idp.Object, "spec", "claims", "groups")

	fmt.Printf("Name:         %s\n", idp.GetName())
	fmt.Printf("Type:         %s\n", idpType)
	fmt.Printf("Issuer URL:   %s\n", issuer)
	fmt.Printf("Client ID:    %s\n", clientID)
	if len(scopes) > 0 {
		fmt.Printf("Scopes:       %s\n", strings.Join(scopes, ", "))
	}
	fmt.Printf("Phase:        %s\n", output.ColorizePhase(phase))
	fmt.Printf("Age:          %s\n", age)
	if message != "" {
		fmt.Printf("Message:      %s\n", message)
	}

	if emailClaim != "" || groupsClaim != "" {
		fmt.Printf("\nClaim Mappings:\n")
		if emailClaim != "" {
			fmt.Printf("  Email:  %s\n", emailClaim)
		}
		if groupsClaim != "" {
			fmt.Printf("  Groups: %s\n", groupsClaim)
		}
	}

	return nil
}

func newDeleteCmd(logger *log.Logger) *cobra.Command {
	var kubeconfig string

	cmd := &cobra.Command{
		Use:   "delete NAME",
		Short: "Delete an identity provider",
		Long: `Delete an IdentityProvider resource from the platform.

This removes the SSO/OIDC configuration. Users authenticated through this
provider will no longer be able to log in via SSO.

Examples:
  butleradm idp delete old-okta-provider`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			kubeContext, _ := cmd.Flags().GetString("context")
			return runDelete(cmd.Context(), logger, args[0], kubeconfig, kubeContext)
		},
	}

	cmd.Flags().StringVar(&kubeconfig, "kubeconfig", "", "path to management cluster kubeconfig")

	return cmd
}

func runDelete(ctx context.Context, logger *log.Logger, name, kubeconfigPath, kubeContext string) error {
	c, err := client.New(kubeconfigPath, kubeContext)
	if err != nil {
		return fmt.Errorf("connecting to management cluster: %w", err)
	}

	err = c.Dynamic.Resource(client.IdentityProviderGVR).Delete(ctx, name, metav1.DeleteOptions{})
	if err != nil {
		return fmt.Errorf("deleting IdentityProvider %s: %w", name, err)
	}

	logger.Success("identity provider deleted", "name", name)
	return nil
}

// unstructuredNestedSlice extracts a slice from nested map fields.
func unstructuredNestedSlice(obj map[string]interface{}, fields ...string) ([]interface{}, bool, error) {
	var val interface{} = obj
	for _, field := range fields {
		m, ok := val.(map[string]interface{})
		if !ok {
			return nil, false, nil
		}
		val, ok = m[field]
		if !ok {
			return nil, false, nil
		}
	}
	slice, ok := val.([]interface{})
	if !ok {
		return nil, false, fmt.Errorf("value is not a slice")
	}
	return slice, true, nil
}
