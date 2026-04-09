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

// Package user implements butleradm user commands for managing
// User resources.
package user

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
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// NewUserCmd creates the user parent command for butleradm.
func NewUserCmd(logger *log.Logger) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "user",
		Short: "Manage users",
		Long: `Manage User resources on the platform.

Users can authenticate via SSO (OIDC) or with email/password (internal).
SSO users are created automatically on first login. Internal users are
created by admins and use an invite flow to set their password.

Note: Creating a user via the CLI only creates the User CRD with authType
"internal". The invite flow (generating an invite URL) is handled by
butler-server.

Examples:
  butleradm user list
  butleradm user get john-doe
  butleradm user create --email user@example.com --display-name "John Doe"
  butleradm user delete john-doe
  butleradm user disable john-doe
  butleradm user enable john-doe`,
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			auth.WarnIfUnauthenticated()
		},
	}

	cmd.AddCommand(newListCmd(logger))
	cmd.AddCommand(newGetCmd(logger))
	cmd.AddCommand(newCreateCmd(logger))
	cmd.AddCommand(newDeleteCmd(logger))
	cmd.AddCommand(newDisableCmd(logger))
	cmd.AddCommand(newEnableCmd(logger))

	return cmd
}

func newListCmd(logger *log.Logger) *cobra.Command {
	var (
		outputFmt  string
		kubeconfig string
	)

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List users",
		Long: `List all User resources on the platform.

Examples:
  butleradm user list
  butleradm user list -o json
  butleradm user list -o yaml`,
		Aliases: []string{"ls"},
		RunE: func(cmd *cobra.Command, args []string) error {
			kubeContext, _ := cmd.Flags().GetString("context")
			return runList(cmd.Context(), logger, kubeconfig, kubeContext, outputFmt)
		},
	}

	cmd.Flags().StringVarP(&outputFmt, "output", "o", "", "output format (table, wide, json, yaml)")
	cmd.Flags().StringVar(&kubeconfig, "kubeconfig", "", "path to management cluster kubeconfig")

	return cmd
}

func runList(ctx context.Context, logger *log.Logger, kubeconfigPath, kubeContext, outputFormat string) error {
	format, err := output.ParseFormat(outputFormat)
	if err != nil {
		return err
	}

	c, err := client.New(kubeconfigPath, kubeContext)
	if err != nil {
		return fmt.Errorf("connecting to management cluster: %w", err)
	}

	// User is cluster-scoped
	list, err := c.Dynamic.Resource(client.UserGVR).List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("listing users: %w", err)
	}

	if format == output.FormatJSON {
		return output.PrintJSON(os.Stdout, list.Items)
	}
	if format == output.FormatYAML {
		return output.PrintYAML(os.Stdout, list.Items)
	}

	wide := format == output.FormatWide

	headers := []string{"NAME", "EMAIL", "DISPLAY NAME", "PHASE", "AUTH TYPE", "ADMIN", "AGE"}
	if wide {
		headers = append(headers, "LAST LOGIN", "LOGINS", "TEAMS")
	}

	table := output.NewTable(os.Stdout, headers...)

	for _, usr := range list.Items {
		name := usr.GetName()
		email := client.GetNestedString(usr.Object, "spec", "email")
		displayName := client.GetNestedString(usr.Object, "spec", "displayName")
		phase := client.GetNestedString(usr.Object, "status", "phase")
		authType := client.GetNestedString(usr.Object, "spec", "authType")
		isAdmin := client.GetNestedBool(usr.Object, "spec", "isPlatformAdmin")
		age := output.FormatAge(usr.GetCreationTimestamp().Time)

		adminStr := ""
		if isAdmin {
			adminStr = "Yes"
		}

		row := []string{
			name,
			email,
			displayName,
			output.ColorizePhase(phase),
			authType,
			adminStr,
			age,
		}

		if wide {
			lastLogin := client.GetNestedString(usr.Object, "status", "lastLoginTime")
			if lastLogin == "" {
				lastLogin = "-"
			}
			loginCount := client.GetNestedInt64(usr.Object, "status", "loginCount")

			// Collect team names from status.teams[]
			teams, teamsFound := unstructuredNestedSlice(usr.Object, "status", "teams")
			teamNames := "-"
			if teamsFound && len(teams) > 0 {
				var names []string
				for _, t := range teams {
					tMap, ok := t.(map[string]interface{})
					if !ok {
						continue
					}
					teamName := client.GetNestedString(tMap, "name")
					if teamName != "" {
						names = append(names, teamName)
					}
				}
				if len(names) > 0 {
					teamNames = strings.Join(names, ",")
				}
			}

			row = append(row, lastLogin, fmt.Sprintf("%d", loginCount), teamNames)
		}

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
		Short: "Get details of a user",
		Long: `Get detailed information about a specific User.

Shows user profile, authentication details, team memberships, and login history.

Examples:
  butleradm user get john-doe
  butleradm user get john-doe -o yaml
  butleradm user get john-doe -o json`,
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

	usr, err := c.Dynamic.Resource(client.UserGVR).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("getting User %s: %w", name, err)
	}

	if outputFormat == "json" {
		return output.PrintJSON(os.Stdout, usr.Object)
	}
	if outputFormat == "yaml" {
		return output.PrintYAML(os.Stdout, usr.Object)
	}

	// Human-readable detail view
	email := client.GetNestedString(usr.Object, "spec", "email")
	displayName := client.GetNestedString(usr.Object, "spec", "displayName")
	authType := client.GetNestedString(usr.Object, "spec", "authType")
	ssoProvider := client.GetNestedString(usr.Object, "spec", "ssoProvider")
	isAdmin := client.GetNestedBool(usr.Object, "spec", "isPlatformAdmin")
	disabled := client.GetNestedBool(usr.Object, "spec", "disabled")
	phase := client.GetNestedString(usr.Object, "status", "phase")
	lastLogin := client.GetNestedString(usr.Object, "status", "lastLoginTime")
	loginCount := client.GetNestedInt64(usr.Object, "status", "loginCount")
	age := output.FormatAge(usr.GetCreationTimestamp().Time)

	fmt.Printf("Name:            %s\n", usr.GetName())
	fmt.Printf("Email:           %s\n", email)
	if displayName != "" {
		fmt.Printf("Display Name:    %s\n", displayName)
	}
	fmt.Printf("Phase:           %s\n", output.ColorizePhase(phase))
	fmt.Printf("Auth Type:       %s\n", authType)
	if ssoProvider != "" {
		fmt.Printf("SSO Provider:    %s\n", ssoProvider)
	}
	fmt.Printf("Platform Admin:  %v\n", isAdmin)
	fmt.Printf("Disabled:        %v\n", disabled)
	fmt.Printf("Age:             %s\n", age)
	if lastLogin != "" {
		fmt.Printf("Last Login:      %s\n", lastLogin)
	}
	fmt.Printf("Login Count:     %d\n", loginCount)

	// Teams sub-table
	teams, teamsFound := unstructuredNestedSlice(usr.Object, "status", "teams")
	if teamsFound && len(teams) > 0 {
		fmt.Printf("\nTeam Memberships:\n")
		teamHeaders := []string{"TEAM", "ROLE"}
		teamTable := output.NewTable(os.Stdout, teamHeaders...)
		for _, t := range teams {
			tMap, ok := t.(map[string]interface{})
			if !ok {
				continue
			}
			teamName := client.GetNestedString(tMap, "name")
			role := client.GetNestedString(tMap, "role")
			teamTable.AddRow(teamName, role)
		}
		teamTable.Flush()
	}

	return nil
}

func newCreateCmd(logger *log.Logger) *cobra.Command {
	var (
		email       string
		displayName string
		admin       bool
		kubeconfig  string
	)

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a user",
		Long: `Create a new internal User resource.

This creates the User CRD with authType "internal". The user will need
an invite URL from butler-server to set their password and activate
their account.

Examples:
  butleradm user create --email user@example.com
  butleradm user create --email admin@example.com --display-name "Admin User" --admin`,
		RunE: func(cmd *cobra.Command, args []string) error {
			kubeContext, _ := cmd.Flags().GetString("context")
			return runCreate(cmd.Context(), logger, email, displayName, admin, kubeconfig, kubeContext)
		},
	}

	cmd.Flags().StringVar(&email, "email", "", "email address for the user (required)")
	cmd.Flags().StringVar(&displayName, "display-name", "", "display name for the user")
	cmd.Flags().BoolVar(&admin, "admin", false, "grant platform admin privileges")
	cmd.Flags().StringVar(&kubeconfig, "kubeconfig", "", "path to management cluster kubeconfig")
	cmd.MarkFlagRequired("email")

	return cmd
}

func runCreate(ctx context.Context, logger *log.Logger, email, displayName string, admin bool, kubeconfigPath, kubeContext string) error {
	c, err := client.New(kubeconfigPath, kubeContext)
	if err != nil {
		return fmt.Errorf("connecting to management cluster: %w", err)
	}

	// Derive a resource name from the email address
	resourceName := emailToResourceName(email)

	spec := map[string]interface{}{
		"email":    email,
		"authType": "internal",
	}
	if displayName != "" {
		spec["displayName"] = displayName
	}
	if admin {
		spec["isPlatformAdmin"] = true
	}

	usr := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "butler.butlerlabs.dev/v1alpha1",
			"kind":       "User",
			"metadata": map[string]interface{}{
				"name": resourceName,
			},
			"spec": spec,
		},
	}

	_, err = c.Dynamic.Resource(client.UserGVR).Create(ctx, usr, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("creating User: %w", err)
	}

	logger.Success("user created", "name", resourceName, "email", email)
	logger.Info("the user needs an invite URL from butler-server to set their password")
	return nil
}

func newDeleteCmd(logger *log.Logger) *cobra.Command {
	var kubeconfig string

	cmd := &cobra.Command{
		Use:   "delete NAME",
		Short: "Delete a user",
		Long: `Delete a User resource from the platform.

This removes the user account. The user will no longer be able to log in.

Examples:
  butleradm user delete john-doe`,
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

	err = c.Dynamic.Resource(client.UserGVR).Delete(ctx, name, metav1.DeleteOptions{})
	if err != nil {
		return fmt.Errorf("deleting User %s: %w", name, err)
	}

	logger.Success("user deleted", "name", name)
	return nil
}

func newDisableCmd(logger *log.Logger) *cobra.Command {
	var kubeconfig string

	cmd := &cobra.Command{
		Use:   "disable NAME",
		Short: "Disable a user",
		Long: `Disable a user account by setting spec.disabled to true.

Disabled users cannot log in to the platform.

Examples:
  butleradm user disable john-doe`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			kubeContext, _ := cmd.Flags().GetString("context")
			return runSetDisabled(cmd.Context(), logger, args[0], true, kubeconfig, kubeContext)
		},
	}

	cmd.Flags().StringVar(&kubeconfig, "kubeconfig", "", "path to management cluster kubeconfig")

	return cmd
}

func newEnableCmd(logger *log.Logger) *cobra.Command {
	var kubeconfig string

	cmd := &cobra.Command{
		Use:   "enable NAME",
		Short: "Enable a user",
		Long: `Enable a previously disabled user account by setting spec.disabled to false.

Examples:
  butleradm user enable john-doe`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			kubeContext, _ := cmd.Flags().GetString("context")
			return runSetDisabled(cmd.Context(), logger, args[0], false, kubeconfig, kubeContext)
		},
	}

	cmd.Flags().StringVar(&kubeconfig, "kubeconfig", "", "path to management cluster kubeconfig")

	return cmd
}

func runSetDisabled(ctx context.Context, logger *log.Logger, name string, disabled bool, kubeconfigPath, kubeContext string) error {
	c, err := client.New(kubeconfigPath, kubeContext)
	if err != nil {
		return fmt.Errorf("connecting to management cluster: %w", err)
	}

	// Get current User
	usr, err := c.Dynamic.Resource(client.UserGVR).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("getting User %s: %w", name, err)
	}

	// Set spec.disabled
	if err := unstructured.SetNestedField(usr.Object, disabled, "spec", "disabled"); err != nil {
		return fmt.Errorf("setting spec.disabled: %w", err)
	}

	// Update the User
	_, err = c.Dynamic.Resource(client.UserGVR).Update(ctx, usr, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("updating User %s: %w", name, err)
	}

	if disabled {
		logger.Success("user disabled", "name", name)
	} else {
		logger.Success("user enabled", "name", name)
	}
	return nil
}

// emailToResourceName converts an email address to a valid Kubernetes resource name.
// For example, "john.doe@example.com" becomes "john-doe".
func emailToResourceName(email string) string {
	// Take the local part before @
	name := email
	for i, c := range email {
		if c == '@' {
			name = email[:i]
			break
		}
	}

	// Replace dots, underscores, and plus signs with hyphens
	var result []byte
	for i := 0; i < len(name); i++ {
		c := name[i]
		switch {
		case c >= 'a' && c <= 'z':
			result = append(result, c)
		case c >= 'A' && c <= 'Z':
			result = append(result, c+32) // lowercase
		case c >= '0' && c <= '9':
			result = append(result, c)
		case c == '-':
			result = append(result, c)
		default:
			result = append(result, '-')
		}
	}

	return string(result)
}

// unstructuredNestedSlice extracts a slice from nested map fields.
func unstructuredNestedSlice(obj map[string]interface{}, fields ...string) ([]interface{}, bool) {
	var val interface{} = obj
	for _, field := range fields {
		m, ok := val.(map[string]interface{})
		if !ok {
			return nil, false
		}
		val, ok = m[field]
		if !ok {
			return nil, false
		}
	}
	slice, ok := val.([]interface{})
	if !ok {
		return nil, false
	}
	return slice, true
}
