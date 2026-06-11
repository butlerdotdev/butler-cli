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

// Package team implements butleradm team commands for managing
// Team resources.
package team

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

// NewTeamCmd creates the team parent command for butleradm.
func NewTeamCmd(logger *log.Logger) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "team",
		Args:  cobra.NoArgs,
		RunE:  output.ShowHelp,
		Short: "Manage teams",
		Long: `Manage Team resources for multi-tenancy.

Teams are the multi-tenancy unit in Butler. Each Team gets its own namespace
where TenantClusters are created. Teams define user/group access with roles
(admin, operator, viewer) and optional resource quotas.

Examples:
  butleradm team list
  butleradm team get engineering
  butleradm team create engineering --display-name "Engineering"
  butleradm team delete old-team
  butleradm team add-member engineering --email user@example.com --role admin
  butleradm team remove-member engineering --email user@example.com`,
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			auth.WarnIfUnauthenticated()
		},
	}

	cmd.AddCommand(newListCmd(logger))
	cmd.AddCommand(newGetCmd(logger))
	cmd.AddCommand(newCreateCmd(logger))
	cmd.AddCommand(newDeleteCmd(logger))
	cmd.AddCommand(newAddMemberCmd(logger))
	cmd.AddCommand(newRemoveMemberCmd(logger))

	cmd.AddCommand(newUpdateMemberRoleCmd(logger))
	cmd.AddCommand(newAddGroupCmd(logger))
	cmd.AddCommand(newUpdateGroupRoleCmd(logger))
	cmd.AddCommand(newRemoveGroupCmd(logger))

	return cmd
}

func newListCmd(logger *log.Logger) *cobra.Command {
	var (
		outputFmt  string
		kubeconfig string
	)

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List teams",
		Long: `List all Team resources on the platform.

Examples:
  butleradm team list
  butleradm team list -o json
  butleradm team list -o yaml`,
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

	// Team is cluster-scoped
	list, err := c.Dynamic.Resource(client.TeamGVR).List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("listing teams: %w", err)
	}

	if format == output.FormatJSON {
		return output.PrintJSON(os.Stdout, list.Items)
	}
	if format == output.FormatYAML {
		return output.PrintYAML(os.Stdout, list.Items)
	}

	wide := format == output.FormatWide

	headers := []string{"NAME", "DISPLAY NAME", "PHASE", "CLUSTERS", "MEMBERS", "NAMESPACE", "AGE"}
	if wide {
		headers = append(headers, "QUOTA STATUS", "PROVIDER")
	}

	table := output.NewTable(os.Stdout, headers...)

	for _, tm := range list.Items {
		name := tm.GetName()
		displayName := client.GetNestedString(tm.Object, "spec", "displayName")
		phase := client.GetNestedString(tm.Object, "status", "phase")
		clusterCount := client.GetNestedInt64(tm.Object, "status", "clusterCount")
		memberCount := client.GetNestedInt64(tm.Object, "status", "memberCount")
		namespace := client.GetNestedString(tm.Object, "status", "namespace")
		age := output.FormatAge(tm.GetCreationTimestamp().Time)

		row := []string{
			name,
			displayName,
			output.ColorizePhase(phase),
			fmt.Sprintf("%d", clusterCount),
			fmt.Sprintf("%d", memberCount),
			namespace,
			age,
		}

		if wide {
			quotaStatus := client.GetNestedString(tm.Object, "status", "quotaStatus")
			if quotaStatus == "" {
				quotaStatus = "-"
			}
			providerRef := client.GetNestedString(tm.Object, "spec", "providerConfigRef", "name")
			if providerRef == "" {
				providerRef = "(default)"
			}
			row = append(row, quotaStatus, providerRef)
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
		Short: "Get details of a team",
		Long: `Get detailed information about a specific Team.

Shows team configuration, members, groups, resource limits, and usage.

Examples:
  butleradm team get engineering
  butleradm team get engineering -o yaml
  butleradm team get engineering -o json`,
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

	tm, err := c.Dynamic.Resource(client.TeamGVR).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("getting Team %s: %w", name, err)
	}

	if outputFormat == "json" {
		return output.PrintJSON(os.Stdout, tm.Object)
	}
	if outputFormat == "yaml" {
		return output.PrintYAML(os.Stdout, tm.Object)
	}

	// Human-readable detail view
	displayName := client.GetNestedString(tm.Object, "spec", "displayName")
	description := client.GetNestedString(tm.Object, "spec", "description")
	phase := client.GetNestedString(tm.Object, "status", "phase")
	namespace := client.GetNestedString(tm.Object, "status", "namespace")
	clusterCount := client.GetNestedInt64(tm.Object, "status", "clusterCount")
	memberCount := client.GetNestedInt64(tm.Object, "status", "memberCount")
	quotaStatus := client.GetNestedString(tm.Object, "status", "quotaStatus")
	quotaMessage := client.GetNestedString(tm.Object, "status", "quotaMessage")
	age := output.FormatAge(tm.GetCreationTimestamp().Time)

	fmt.Printf("Name:          %s\n", tm.GetName())
	if displayName != "" {
		fmt.Printf("Display Name:  %s\n", displayName)
	}
	if description != "" {
		fmt.Printf("Description:   %s\n", description)
	}
	fmt.Printf("Phase:         %s\n", output.ColorizePhase(phase))
	fmt.Printf("Namespace:     %s\n", namespace)
	fmt.Printf("Clusters:      %d\n", clusterCount)
	fmt.Printf("Members:       %d\n", memberCount)
	fmt.Printf("Age:           %s\n", age)

	// Resource limits
	maxClusters := client.GetNestedInt64(tm.Object, "spec", "resourceLimits", "maxClusters")
	maxNodes := client.GetNestedInt64(tm.Object, "spec", "resourceLimits", "maxTotalNodes")
	maxNodesPerCluster := client.GetNestedInt64(tm.Object, "spec", "resourceLimits", "maxNodesPerCluster")
	if maxClusters > 0 || maxNodes > 0 || maxNodesPerCluster > 0 {
		fmt.Printf("\nResource Limits:\n")
		if maxClusters > 0 {
			fmt.Printf("  Max Clusters:           %d\n", maxClusters)
		}
		if maxNodesPerCluster > 0 {
			fmt.Printf("  Max Nodes Per Cluster:  %d\n", maxNodesPerCluster)
		}
		if maxNodes > 0 {
			fmt.Printf("  Max Total Nodes:        %d\n", maxNodes)
		}
	}

	// Resource usage
	usageClusters := client.GetNestedInt64(tm.Object, "status", "resourceUsage", "clusters")
	usageNodes := client.GetNestedInt64(tm.Object, "status", "resourceUsage", "totalNodes")
	if usageClusters > 0 || usageNodes > 0 {
		fmt.Printf("\nResource Usage:\n")
		fmt.Printf("  Clusters:     %d\n", usageClusters)
		fmt.Printf("  Total Nodes:  %d\n", usageNodes)
	}

	// Quota status
	if quotaStatus != "" {
		fmt.Printf("\nQuota Status:  %s\n", quotaStatus)
		if quotaMessage != "" {
			fmt.Printf("Quota Message: %s\n", quotaMessage)
		}
	}

	// Members sub-table
	users, usersFound := unstructuredNestedSlice(tm.Object, "spec", "access", "users")
	if usersFound && len(users) > 0 {
		fmt.Printf("\nMembers:\n")
		memberHeaders := []string{"EMAIL", "ROLE"}
		memberTable := output.NewTable(os.Stdout, memberHeaders...)
		for _, u := range users {
			uMap, ok := u.(map[string]interface{})
			if !ok {
				continue
			}
			email := client.GetNestedString(uMap, "name")
			role := client.GetNestedString(uMap, "role")
			memberTable.AddRow(email, role)
		}
		memberTable.Flush()
	}

	// Groups sub-table
	groups, groupsFound := unstructuredNestedSlice(tm.Object, "spec", "access", "groups")
	if groupsFound && len(groups) > 0 {
		fmt.Printf("\nGroups:\n")
		groupHeaders := []string{"GROUP", "ROLE", "IDENTITY PROVIDER"}
		groupTable := output.NewTable(os.Stdout, groupHeaders...)
		for _, g := range groups {
			gMap, ok := g.(map[string]interface{})
			if !ok {
				continue
			}
			groupName := client.GetNestedString(gMap, "name")
			role := client.GetNestedString(gMap, "role")
			idp := client.GetNestedString(gMap, "identityProvider")
			groupTable.AddRow(groupName, role, idp)
		}
		groupTable.Flush()
	}

	return nil
}

func newCreateCmd(logger *log.Logger) *cobra.Command {
	var (
		displayName string
		description string
		kubeconfig  string
	)

	cmd := &cobra.Command{
		Use:   "create NAME",
		Short: "Create a team",
		Long: `Create a new Team resource.

The controller will create the team namespace and configure RBAC.

Examples:
  butleradm team create engineering
  butleradm team create engineering --display-name "Engineering Team"
  butleradm team create platform --display-name "Platform" --description "Platform engineering team"`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			kubeContext, _ := cmd.Flags().GetString("context")
			return runCreate(cmd.Context(), logger, args[0], displayName, description, kubeconfig, kubeContext)
		},
	}

	cmd.Flags().StringVar(&displayName, "display-name", "", "human-readable display name")
	cmd.Flags().StringVar(&description, "description", "", "team description")
	cmd.Flags().StringVar(&kubeconfig, "kubeconfig", "", "path to management cluster kubeconfig")

	return cmd
}

func runCreate(ctx context.Context, logger *log.Logger, name, displayName, description, kubeconfigPath, kubeContext string) error {
	c, err := client.New(kubeconfigPath, kubeContext)
	if err != nil {
		return fmt.Errorf("connecting to management cluster: %w", err)
	}

	spec := map[string]interface{}{}
	if displayName != "" {
		spec["displayName"] = displayName
	}
	if description != "" {
		spec["description"] = description
	}

	tm := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "butler.butlerlabs.dev/v1alpha1",
			"kind":       "Team",
			"metadata": map[string]interface{}{
				"name": name,
			},
			"spec": spec,
		},
	}

	_, err = c.Dynamic.Resource(client.TeamGVR).Create(ctx, tm, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("creating Team: %w", err)
	}

	logger.Success("team created", "name", name)
	return nil
}

func newDeleteCmd(logger *log.Logger) *cobra.Command {
	var kubeconfig string

	cmd := &cobra.Command{
		Use:   "delete NAME",
		Short: "Delete a team",
		Long: `Delete a Team resource.

This will trigger cleanup of the team namespace and all resources within it,
including any TenantClusters owned by the team.

Examples:
  butleradm team delete old-team`,
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

	err = c.Dynamic.Resource(client.TeamGVR).Delete(ctx, name, metav1.DeleteOptions{})
	if err != nil {
		return fmt.Errorf("deleting Team %s: %w", name, err)
	}

	logger.Success("team deleted", "name", name)
	return nil
}

func newAddMemberCmd(logger *log.Logger) *cobra.Command {
	var (
		email      string
		role       string
		kubeconfig string
	)

	cmd := &cobra.Command{
		Use:   "add-member NAME",
		Short: "Add a member to a team",
		Long: `Add a user to a team by patching the Team's spec.access.users list.

The role must be one of: admin, operator, viewer.

Examples:
  butleradm team add-member engineering --email user@example.com --role admin
  butleradm team add-member engineering --email viewer@example.com --role viewer`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			kubeContext, _ := cmd.Flags().GetString("context")
			return runAddMember(cmd.Context(), logger, args[0], email, role, kubeconfig, kubeContext)
		},
	}

	cmd.Flags().StringVar(&email, "email", "", "email address of the user to add")
	cmd.Flags().StringVar(&role, "role", "viewer", "role for the user (admin, operator, viewer)")
	cmd.Flags().StringVar(&kubeconfig, "kubeconfig", "", "path to management cluster kubeconfig")
	cmd.MarkFlagRequired("email")

	return cmd
}

func runAddMember(ctx context.Context, logger *log.Logger, name, email, role, kubeconfigPath, kubeContext string) error {
	// Validate role
	if role != "admin" && role != "operator" && role != "viewer" {
		return fmt.Errorf("invalid role %q: must be admin, operator, or viewer", role)
	}

	c, err := client.New(kubeconfigPath, kubeContext)
	if err != nil {
		return fmt.Errorf("connecting to management cluster: %w", err)
	}

	// Get current Team
	tm, err := c.Dynamic.Resource(client.TeamGVR).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("getting Team %s: %w", name, err)
	}

	// Extract current users list
	users, _ := unstructuredNestedSlice(tm.Object, "spec", "access", "users")

	// Check if user already exists
	for _, u := range users {
		uMap, ok := u.(map[string]interface{})
		if !ok {
			continue
		}
		existingEmail := client.GetNestedString(uMap, "name")
		if existingEmail == email {
			return fmt.Errorf("user %s is already a member of team %s", email, name)
		}
	}

	// Append new user
	newUser := map[string]interface{}{
		"name": email,
		"role": role,
	}
	users = append(users, newUser)

	// Set the updated users list back on the object
	if err := unstructured.SetNestedSlice(tm.Object, users, "spec", "access", "users"); err != nil {
		return fmt.Errorf("setting spec.access.users: %w", err)
	}

	// Update the Team
	_, err = c.Dynamic.Resource(client.TeamGVR).Update(ctx, tm, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("updating Team %s: %w", name, err)
	}

	logger.Success("member added to team", "team", name, "email", email, "role", role)
	return nil
}

func newRemoveMemberCmd(logger *log.Logger) *cobra.Command {
	var (
		email      string
		kubeconfig string
	)

	cmd := &cobra.Command{
		Use:   "remove-member NAME",
		Short: "Remove a member from a team",
		Long: `Remove a user from a team by patching the Team's spec.access.users list.

Examples:
  butleradm team remove-member engineering --email user@example.com`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			kubeContext, _ := cmd.Flags().GetString("context")
			return runRemoveMember(cmd.Context(), logger, args[0], email, kubeconfig, kubeContext)
		},
	}

	cmd.Flags().StringVar(&email, "email", "", "email address of the user to remove")
	cmd.Flags().StringVar(&kubeconfig, "kubeconfig", "", "path to management cluster kubeconfig")
	cmd.MarkFlagRequired("email")

	return cmd
}

func runRemoveMember(ctx context.Context, logger *log.Logger, name, email, kubeconfigPath, kubeContext string) error {
	c, err := client.New(kubeconfigPath, kubeContext)
	if err != nil {
		return fmt.Errorf("connecting to management cluster: %w", err)
	}

	// Get current Team
	tm, err := c.Dynamic.Resource(client.TeamGVR).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("getting Team %s: %w", name, err)
	}

	// Extract current users list
	users, found := unstructuredNestedSlice(tm.Object, "spec", "access", "users")
	if !found || len(users) == 0 {
		return fmt.Errorf("team %s has no members", name)
	}

	// Filter out the user
	var updated []interface{}
	removed := false
	for _, u := range users {
		uMap, ok := u.(map[string]interface{})
		if !ok {
			updated = append(updated, u)
			continue
		}
		existingEmail := client.GetNestedString(uMap, "name")
		if existingEmail == email {
			removed = true
			continue
		}
		updated = append(updated, u)
	}

	if !removed {
		return fmt.Errorf("user %s is not a member of team %s", email, name)
	}

	// Set the updated users list back on the object
	if err := unstructured.SetNestedSlice(tm.Object, updated, "spec", "access", "users"); err != nil {
		return fmt.Errorf("setting spec.access.users: %w", err)
	}

	// Update the Team
	_, err = c.Dynamic.Resource(client.TeamGVR).Update(ctx, tm, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("updating Team %s: %w", name, err)
	}

	logger.Success("member removed from team", "team", name, "email", email)
	return nil
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
