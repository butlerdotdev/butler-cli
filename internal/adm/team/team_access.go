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

package team

import (
	"context"
	"fmt"

	"github.com/butlerdotdev/butler/internal/common/client"
	"github.com/butlerdotdev/butler/internal/common/log"
	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// validateRole rejects any role outside the Team RBAC set.
func validateRole(role string) error {
	if role != "admin" && role != "operator" && role != "viewer" {
		return fmt.Errorf("invalid role %q: must be admin, operator, or viewer", role)
	}
	return nil
}

// setMemberRole finds the user whose name matches email (the CRD stores the
// email under users[].name) and sets its role. It mutates users in place and
// returns the resulting slice. Returns an error if no member matches. The
// other entries are left untouched.
func setMemberRole(users []interface{}, email, role string) ([]interface{}, error) {
	for _, u := range users {
		uMap, ok := u.(map[string]interface{})
		if !ok {
			continue
		}
		if client.GetNestedString(uMap, "name") == email {
			uMap["role"] = role
			return users, nil
		}
	}
	return nil, fmt.Errorf("user %s is not a member of this team", email)
}

// matchGroup reports whether a group entry matches name, and the identity
// provider when idp is non-empty. When idp is empty, it matches on name alone.
func matchGroup(gMap map[string]interface{}, name, idp string) bool {
	if client.GetNestedString(gMap, "name") != name {
		return false
	}
	if idp == "" {
		return true
	}
	return client.GetNestedString(gMap, "identityProvider") == idp
}

// findGroupIndex locates the single group matching name (and idp when given).
// It returns the index, or an error. If multiple groups share the name and no
// idp was given, it fails safe (D2): edit-the-wrong-rule is the failure mode to
// prevent, so it asks the operator to disambiguate with --identity-provider
// rather than picking one.
func findGroupIndex(groups []interface{}, name, idp string) (int, error) {
	matches := []int{}
	for i, g := range groups {
		gMap, ok := g.(map[string]interface{})
		if !ok {
			continue
		}
		if matchGroup(gMap, name, idp) {
			matches = append(matches, i)
		}
	}
	switch len(matches) {
	case 0:
		return -1, fmt.Errorf("group %q is not configured on this team", name)
	case 1:
		return matches[0], nil
	default:
		return -1, fmt.Errorf("group %q matches multiple entries; specify --identity-provider to choose one", name)
	}
}

// The edit* functions below mutate a fetched Team object in place: they read
// the relevant access slice, change only the targeted entry, and write the
// slice back with SetNestedSlice. status, metadata (resourceVersion), and the
// other entries are left untouched, so a run* that does Get -> edit -> Update
// preserves status and resourceVersion for free. They are split out so the
// load-bearing preservation is unit-testable without a cluster.

func editMemberRole(obj *unstructured.Unstructured, email, role string) error {
	users, found := unstructuredNestedSlice(obj.Object, "spec", "access", "users")
	if !found || len(users) == 0 {
		return fmt.Errorf("this team has no members")
	}
	updated, err := setMemberRole(users, email, role)
	if err != nil {
		return err
	}
	return unstructured.SetNestedSlice(obj.Object, updated, "spec", "access", "users")
}

func editAddGroup(obj *unstructured.Unstructured, group, role, idp string) error {
	groups, _ := unstructuredNestedSlice(obj.Object, "spec", "access", "groups")
	for _, g := range groups {
		gMap, ok := g.(map[string]interface{})
		if !ok {
			continue
		}
		if client.GetNestedString(gMap, "name") == group &&
			client.GetNestedString(gMap, "identityProvider") == idp {
			return fmt.Errorf("group %q is already configured on this team", group)
		}
	}
	newGroup := map[string]interface{}{"name": group, "role": role}
	if idp != "" {
		newGroup["identityProvider"] = idp
	}
	groups = append(groups, newGroup)
	return unstructured.SetNestedSlice(obj.Object, groups, "spec", "access", "groups")
}

func editGroupRole(obj *unstructured.Unstructured, group, idp, role string) error {
	groups, found := unstructuredNestedSlice(obj.Object, "spec", "access", "groups")
	if !found || len(groups) == 0 {
		return fmt.Errorf("this team has no group access rules")
	}
	idx, err := findGroupIndex(groups, group, idp)
	if err != nil {
		return err
	}
	groups[idx].(map[string]interface{})["role"] = role
	return unstructured.SetNestedSlice(obj.Object, groups, "spec", "access", "groups")
}

func editRemoveGroup(obj *unstructured.Unstructured, group, idp string) error {
	groups, found := unstructuredNestedSlice(obj.Object, "spec", "access", "groups")
	if !found || len(groups) == 0 {
		return fmt.Errorf("this team has no group access rules")
	}
	idx, err := findGroupIndex(groups, group, idp)
	if err != nil {
		return err
	}
	updated := append(groups[:idx:idx], groups[idx+1:]...)
	return unstructured.SetNestedSlice(obj.Object, updated, "spec", "access", "groups")
}

// --- update-member-role ---

func newUpdateMemberRoleCmd(logger *log.Logger) *cobra.Command {
	var (
		email      string
		role       string
		kubeconfig string
	)

	cmd := &cobra.Command{
		Use:   "update-member-role NAME",
		Short: "Change a team member's role",
		Long: `Change an existing member's role by patching the Team's spec.access.users
list. The member must already exist (use add-member to add one).

The role must be one of: admin, operator, viewer.

Examples:
  butleradm team update-member-role engineering --email user@example.com --role operator`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			kubeContext, _ := cmd.Flags().GetString("context")
			return runUpdateMemberRole(cmd.Context(), logger, args[0], email, role, kubeconfig, kubeContext)
		},
	}

	cmd.Flags().StringVar(&email, "email", "", "email address of the member")
	cmd.Flags().StringVar(&role, "role", "", "new role for the member (admin, operator, viewer)")
	cmd.Flags().StringVar(&kubeconfig, "kubeconfig", "", "path to management cluster kubeconfig")
	_ = cmd.MarkFlagRequired("email")
	_ = cmd.MarkFlagRequired("role")

	return cmd
}

func runUpdateMemberRole(ctx context.Context, logger *log.Logger, name, email, role, kubeconfigPath, kubeContext string) error {
	if err := validateRole(role); err != nil {
		return err
	}

	c, err := client.New(kubeconfigPath, kubeContext)
	if err != nil {
		return fmt.Errorf("connecting to management cluster: %w", err)
	}

	tm, err := c.Dynamic.Resource(client.TeamGVR).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("getting Team %s: %w", name, err)
	}

	if err := editMemberRole(tm, email, role); err != nil {
		return err
	}

	if _, err := c.Dynamic.Resource(client.TeamGVR).Update(ctx, tm, metav1.UpdateOptions{}); err != nil {
		return fmt.Errorf("updating Team %s: %w", name, err)
	}

	logger.Success("member role updated", "team", name, "email", email, "role", role)
	return nil
}

// --- add-group ---

func newAddGroupCmd(logger *log.Logger) *cobra.Command {
	var (
		group      string
		role       string
		idp        string
		kubeconfig string
	)

	cmd := &cobra.Command{
		Use:   "add-group NAME",
		Short: "Add a group access rule to a team",
		Long: `Add a group-to-role mapping to the Team's spec.access.groups list. Members
of the group inherit the role when they authenticate. Matching happens at
login; this verb only declares the rule.

The role must be one of: admin, operator, viewer. Use --identity-provider to
scope the rule to a single IdP; without it, the group name matches groups from
any IdP.

Examples:
  butleradm team add-group engineering --group platform-eng --role operator
  butleradm team add-group engineering --group eng --role admin --identity-provider okta`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			kubeContext, _ := cmd.Flags().GetString("context")
			return runAddGroup(cmd.Context(), logger, args[0], group, role, idp, kubeconfig, kubeContext)
		},
	}

	cmd.Flags().StringVar(&group, "group", "", "group identifier (OIDC group name or AD group DN)")
	cmd.Flags().StringVar(&role, "role", "viewer", "role for the group (admin, operator, viewer)")
	cmd.Flags().StringVar(&idp, "identity-provider", "", "scope the rule to a specific IdentityProvider")
	cmd.Flags().StringVar(&kubeconfig, "kubeconfig", "", "path to management cluster kubeconfig")
	_ = cmd.MarkFlagRequired("group")

	return cmd
}

func runAddGroup(ctx context.Context, logger *log.Logger, name, group, role, idp, kubeconfigPath, kubeContext string) error {
	if err := validateRole(role); err != nil {
		return err
	}

	c, err := client.New(kubeconfigPath, kubeContext)
	if err != nil {
		return fmt.Errorf("connecting to management cluster: %w", err)
	}

	tm, err := c.Dynamic.Resource(client.TeamGVR).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("getting Team %s: %w", name, err)
	}

	if err := editAddGroup(tm, group, role, idp); err != nil {
		return err
	}

	if _, err := c.Dynamic.Resource(client.TeamGVR).Update(ctx, tm, metav1.UpdateOptions{}); err != nil {
		return fmt.Errorf("updating Team %s: %w", name, err)
	}

	logger.Success("group access rule added", "team", name, "group", group, "role", role)
	return nil
}

// --- update-group-role ---

func newUpdateGroupRoleCmd(logger *log.Logger) *cobra.Command {
	var (
		group      string
		role       string
		idp        string
		kubeconfig string
	)

	cmd := &cobra.Command{
		Use:   "update-group-role NAME",
		Short: "Change a team group's role",
		Long: `Change an existing group rule's role by patching the Team's
spec.access.groups list.

When more than one rule shares the group name, pass --identity-provider to
pick the one to change.

Examples:
  butleradm team update-group-role engineering --group platform-eng --role admin
  butleradm team update-group-role engineering --group eng --role viewer --identity-provider okta`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			kubeContext, _ := cmd.Flags().GetString("context")
			return runUpdateGroupRole(cmd.Context(), logger, args[0], group, role, idp, kubeconfig, kubeContext)
		},
	}

	cmd.Flags().StringVar(&group, "group", "", "group identifier")
	cmd.Flags().StringVar(&role, "role", "", "new role for the group (admin, operator, viewer)")
	cmd.Flags().StringVar(&idp, "identity-provider", "", "disambiguate when multiple rules share the group name")
	cmd.Flags().StringVar(&kubeconfig, "kubeconfig", "", "path to management cluster kubeconfig")
	_ = cmd.MarkFlagRequired("group")
	_ = cmd.MarkFlagRequired("role")

	return cmd
}

func runUpdateGroupRole(ctx context.Context, logger *log.Logger, name, group, role, idp, kubeconfigPath, kubeContext string) error {
	if err := validateRole(role); err != nil {
		return err
	}

	c, err := client.New(kubeconfigPath, kubeContext)
	if err != nil {
		return fmt.Errorf("connecting to management cluster: %w", err)
	}

	tm, err := c.Dynamic.Resource(client.TeamGVR).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("getting Team %s: %w", name, err)
	}

	if err := editGroupRole(tm, group, idp, role); err != nil {
		return err
	}

	if _, err := c.Dynamic.Resource(client.TeamGVR).Update(ctx, tm, metav1.UpdateOptions{}); err != nil {
		return fmt.Errorf("updating Team %s: %w", name, err)
	}

	logger.Success("group role updated", "team", name, "group", group, "role", role)
	return nil
}

// --- remove-group ---

func newRemoveGroupCmd(logger *log.Logger) *cobra.Command {
	var (
		group      string
		idp        string
		kubeconfig string
	)

	cmd := &cobra.Command{
		Use:   "remove-group NAME",
		Short: "Remove a group access rule from a team",
		Long: `Remove a group-to-role mapping from the Team's spec.access.groups list.

When more than one rule shares the group name, pass --identity-provider to
pick the one to remove.

Examples:
  butleradm team remove-group engineering --group platform-eng
  butleradm team remove-group engineering --group eng --identity-provider okta`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			kubeContext, _ := cmd.Flags().GetString("context")
			return runRemoveGroup(cmd.Context(), logger, args[0], group, idp, kubeconfig, kubeContext)
		},
	}

	cmd.Flags().StringVar(&group, "group", "", "group identifier")
	cmd.Flags().StringVar(&idp, "identity-provider", "", "disambiguate when multiple rules share the group name")
	cmd.Flags().StringVar(&kubeconfig, "kubeconfig", "", "path to management cluster kubeconfig")
	_ = cmd.MarkFlagRequired("group")

	return cmd
}

func runRemoveGroup(ctx context.Context, logger *log.Logger, name, group, idp, kubeconfigPath, kubeContext string) error {
	c, err := client.New(kubeconfigPath, kubeContext)
	if err != nil {
		return fmt.Errorf("connecting to management cluster: %w", err)
	}

	tm, err := c.Dynamic.Resource(client.TeamGVR).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("getting Team %s: %w", name, err)
	}

	if err := editRemoveGroup(tm, group, idp); err != nil {
		return err
	}

	if _, err := c.Dynamic.Resource(client.TeamGVR).Update(ctx, tm, metav1.UpdateOptions{}); err != nil {
		return fmt.Errorf("updating Team %s: %w", name, err)
	}

	logger.Success("group access rule removed", "team", name, "group", group)
	return nil
}
