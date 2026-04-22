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

// Package env implements butleradm env commands for managing Team
// environments (ADR-009). Environments are entries in the Team's
// spec.environments[] list. TenantClusters opt in to an environment
// via the butler.butlerlabs.dev/environment label.
package env

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/butlerdotdev/butler/internal/common/auth"
	"github.com/butlerdotdev/butler/internal/common/client"
	"github.com/butlerdotdev/butler/internal/common/log"
	"github.com/butlerdotdev/butler/internal/common/output"
	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// EnvironmentLabel is the TenantCluster label key that opts a cluster
// into a named Team environment. Must stay in sync with the controller
// webhook.
const EnvironmentLabel = "butler.butlerlabs.dev/environment"

// NewEnvCmd creates the env parent command for butleradm.
func NewEnvCmd(logger *log.Logger) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "env",
		Short: "Manage Team environments",
		Long: `Manage Team environments (ADR-009).

An environment is a named slot within a Team that optionally caps cluster
count (MaxClusters) and per-member cluster count (MaxClustersPerMember).
TenantClusters opt into an environment by carrying the
butler.butlerlabs.dev/environment label.

Examples:
  butleradm env create staging --team payments --max-clusters 5
  butleradm env update staging --team payments --max-clusters 10
  butleradm env list --team payments
  butleradm env delete staging --team payments
  butleradm env migrate --team payments --environment staging --all`,
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			auth.WarnIfUnauthenticated()
		},
	}

	cmd.AddCommand(newCreateCmd(logger))
	cmd.AddCommand(newUpdateCmd(logger))
	cmd.AddCommand(newListCmd(logger))
	cmd.AddCommand(newDeleteCmd(logger))
	cmd.AddCommand(newMigrateCmd(logger))

	return cmd
}

// teamNamespace returns the namespace Butler creates for a given Team name.
func teamNamespace(team string) string {
	return "team-" + team
}

// fetchTeam returns the Team unstructured object for the given name.
func fetchTeam(ctx context.Context, c *client.Client, name string) (*unstructured.Unstructured, error) {
	tm, err := c.Dynamic.Resource(client.TeamGVR).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("getting Team %s: %w", name, err)
	}
	return tm, nil
}

// envEntries returns the Team's spec.environments[] slice.
func envEntries(tm *unstructured.Unstructured) []interface{} {
	envs, _, _ := unstructured.NestedSlice(tm.Object, "spec", "environments")
	return envs
}

// envName returns the name field of an environment map entry.
func envName(entry interface{}) string {
	m, ok := entry.(map[string]interface{})
	if !ok {
		return ""
	}
	name, _, _ := unstructured.NestedString(m, "name")
	return name
}

func newCreateCmd(logger *log.Logger) *cobra.Command {
	var (
		team                 string
		maxClusters          int32
		maxClustersPerMember int32
		description          string
		kubeconfig           string
	)

	cmd := &cobra.Command{
		Use:   "create NAME",
		Short: "Create a new environment on a team",
		Long: `Create a new environment slot on a Team.

The environment is appended to the Team's spec.environments list. Quota
fields default to zero (no cap). Operators can set MaxClusters and
MaxClustersPerMember to bound usage inside the environment.

Examples:
  butleradm env create staging --team payments
  butleradm env create sandbox --team payments --max-clusters-per-member 1
  butleradm env create prod --team payments --max-clusters 10
  butleradm env create prod --team payments --description "production workloads"`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			kubeContext, _ := cmd.Flags().GetString("context")
			return runCreate(cmd.Context(), logger, args[0], team, description, maxClusters, maxClustersPerMember, kubeconfig, kubeContext)
		},
	}

	cmd.Flags().StringVar(&team, "team", "", "team name (required)")
	cmd.Flags().Int32Var(&maxClusters, "max-clusters", 0, "max TenantClusters in this environment (0 = no cap)")
	cmd.Flags().Int32Var(&maxClustersPerMember, "max-clusters-per-member", 0, "max TenantClusters per member in this environment (0 = no cap)")
	cmd.Flags().StringVar(&description, "description", "", "human-readable description for this environment")
	cmd.Flags().StringVar(&kubeconfig, "kubeconfig", "", "path to management cluster kubeconfig")
	_ = cmd.MarkFlagRequired("team")

	return cmd
}

func runCreate(ctx context.Context, logger *log.Logger, name, team, description string, maxClusters, maxClustersPerMember int32, kubeconfigPath, kubeContext string) error {
	if err := validateEnvName(name); err != nil {
		return err
	}

	c, err := client.New(kubeconfigPath, kubeContext)
	if err != nil {
		return fmt.Errorf("connecting to management cluster: %w", err)
	}

	tm, err := fetchTeam(ctx, c, team)
	if err != nil {
		return err
	}

	envs := envEntries(tm)
	for _, e := range envs {
		if envName(e) == name {
			return fmt.Errorf("environment %q already exists on team %s", name, team)
		}
	}

	entry := map[string]interface{}{
		"name": name,
	}
	if description != "" {
		entry["description"] = description
	}
	limits := map[string]interface{}{}
	if maxClusters > 0 {
		limits["maxClusters"] = int64(maxClusters)
	}
	if maxClustersPerMember > 0 {
		limits["maxClustersPerMember"] = int64(maxClustersPerMember)
	}
	if len(limits) > 0 {
		entry["limits"] = limits
	}
	envs = append(envs, entry)

	if err := unstructured.SetNestedSlice(tm.Object, envs, "spec", "environments"); err != nil {
		return fmt.Errorf("setting spec.environments: %w", err)
	}

	if _, err := c.Dynamic.Resource(client.TeamGVR).Update(ctx, tm, metav1.UpdateOptions{}); err != nil {
		return fmt.Errorf("updating Team %s: %w", team, err)
	}

	logger.Success("environment created", "team", team, "environment", name)
	return nil
}

// updateOptions holds inputs to the env update subcommand. Parity with
// migrateOptions and the server's PUT /api/teams/{name}/environments
// body: name is immutable, everything else is a partial patch.
type updateOptions struct {
	team                 string
	name                 string
	maxClusters          int32
	maxClustersPerMember int32
	clearLimits          bool
	description          string
	kubeconfig           string
	kubeContext          string
}

func newUpdateCmd(logger *log.Logger) *cobra.Command {
	opts := &updateOptions{}

	cmd := &cobra.Command{
		Use:   "update NAME",
		Short: "Update an existing environment on a team",
		Long: `Update fields on an existing environment in the Team's spec.environments list.

The environment name is immutable and identifies which entry to patch.
Flags that are not set leave the corresponding field unchanged. Pass
--clear-limits to drop both MaxClusters and MaxClustersPerMember in a
single call.

Examples:
  butleradm env update staging --team payments --max-clusters 10
  butleradm env update sandbox --team payments --max-clusters-per-member 2
  butleradm env update prod --team payments --clear-limits
  butleradm env update prod --team payments --description "production workloads"`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.name = args[0]
			opts.kubeContext, _ = cmd.Flags().GetString("context")
			return runUpdate(cmd.Context(), logger, cmd, opts)
		},
	}

	cmd.Flags().StringVar(&opts.team, "team", "", "team name (required)")
	cmd.Flags().Int32Var(&opts.maxClusters, "max-clusters", 0, "max TenantClusters in this environment (0 = no cap)")
	cmd.Flags().Int32Var(&opts.maxClustersPerMember, "max-clusters-per-member", 0, "max TenantClusters per member in this environment (0 = no cap)")
	cmd.Flags().BoolVar(&opts.clearLimits, "clear-limits", false, "drop MaxClusters and MaxClustersPerMember on this environment")
	cmd.Flags().StringVar(&opts.description, "description", "", "human-readable description for this environment (pass empty string to clear)")
	cmd.Flags().StringVar(&opts.kubeconfig, "kubeconfig", "", "path to management cluster kubeconfig")
	_ = cmd.MarkFlagRequired("team")

	return cmd
}

func runUpdate(ctx context.Context, logger *log.Logger, cmd *cobra.Command, opts *updateOptions) error {
	if err := validateEnvName(opts.name); err != nil {
		return err
	}

	c, err := client.New(opts.kubeconfig, opts.kubeContext)
	if err != nil {
		return fmt.Errorf("connecting to management cluster: %w", err)
	}

	tm, err := fetchTeam(ctx, c, opts.team)
	if err != nil {
		return err
	}

	envs := envEntries(tm)
	idx := -1
	for i, e := range envs {
		if envName(e) == opts.name {
			idx = i
			break
		}
	}
	if idx < 0 {
		return fmt.Errorf("environment %q not found on team %s", opts.name, opts.team)
	}

	entry, ok := envs[idx].(map[string]interface{})
	if !ok {
		return fmt.Errorf("environment %q has unexpected shape in team %s", opts.name, opts.team)
	}

	// Track whether anything changed so we can short-circuit the Update
	// call and give operators a clear error when they pass no patch flags.
	changed := false

	if cmd.Flags().Changed("description") {
		if opts.description == "" {
			if _, hadDesc := entry["description"]; hadDesc {
				delete(entry, "description")
				changed = true
			}
		} else {
			entry["description"] = opts.description
			changed = true
		}
	}

	if opts.clearLimits {
		if _, hasLimits := entry["limits"]; hasLimits {
			delete(entry, "limits")
			changed = true
		}
	} else if cmd.Flags().Changed("max-clusters") || cmd.Flags().Changed("max-clusters-per-member") {
		limits, _ := entry["limits"].(map[string]interface{})
		if limits == nil {
			limits = map[string]interface{}{}
		}
		if cmd.Flags().Changed("max-clusters") {
			if opts.maxClusters > 0 {
				limits["maxClusters"] = int64(opts.maxClusters)
			} else {
				delete(limits, "maxClusters")
			}
			changed = true
		}
		if cmd.Flags().Changed("max-clusters-per-member") {
			if opts.maxClustersPerMember > 0 {
				limits["maxClustersPerMember"] = int64(opts.maxClustersPerMember)
			} else {
				delete(limits, "maxClustersPerMember")
			}
			changed = true
		}
		if len(limits) == 0 {
			delete(entry, "limits")
		} else {
			entry["limits"] = limits
		}
	}

	if !changed {
		return fmt.Errorf("nothing to update: pass --description, --max-clusters, --max-clusters-per-member, or --clear-limits")
	}

	envs[idx] = entry
	if err := unstructured.SetNestedSlice(tm.Object, envs, "spec", "environments"); err != nil {
		return fmt.Errorf("setting spec.environments: %w", err)
	}

	if _, err := c.Dynamic.Resource(client.TeamGVR).Update(ctx, tm, metav1.UpdateOptions{}); err != nil {
		return fmt.Errorf("updating Team %s: %w", opts.team, err)
	}

	logger.Success("environment updated", "team", opts.team, "environment", opts.name)
	return nil
}

func newListCmd(logger *log.Logger) *cobra.Command {
	var (
		team       string
		outputFmt  string
		kubeconfig string
	)

	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List environments on a team",
		Long: `List environments defined on a Team, with per-environment quota
and usage counts computed from labeled TenantClusters.

Examples:
  butleradm env list --team payments
  butleradm env list --team payments -o json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			kubeContext, _ := cmd.Flags().GetString("context")
			return runList(cmd.Context(), logger, team, outputFmt, kubeconfig, kubeContext)
		},
	}

	cmd.Flags().StringVar(&team, "team", "", "team name (required)")
	cmd.Flags().StringVarP(&outputFmt, "output", "o", "", "output format (table, json, yaml)")
	cmd.Flags().StringVar(&kubeconfig, "kubeconfig", "", "path to management cluster kubeconfig")
	_ = cmd.MarkFlagRequired("team")

	return cmd
}

func runList(ctx context.Context, _ *log.Logger, team, outputFormat, kubeconfigPath, kubeContext string) error {
	format, err := output.ParseFormat(outputFormat)
	if err != nil {
		return err
	}

	c, err := client.New(kubeconfigPath, kubeContext)
	if err != nil {
		return fmt.Errorf("connecting to management cluster: %w", err)
	}

	tm, err := fetchTeam(ctx, c, team)
	if err != nil {
		return err
	}

	envs := envEntries(tm)

	// Count live clusters per env via label selector against the team ns.
	usage := map[string]int{}
	ns := teamNamespace(team)
	tcList, tcErr := c.Dynamic.Resource(client.TenantClusterGVR).Namespace(ns).List(ctx, metav1.ListOptions{})
	if tcErr == nil {
		for i := range tcList.Items {
			labels := tcList.Items[i].GetLabels()
			if env := labels[EnvironmentLabel]; env != "" {
				usage[env]++
			}
		}
	}

	type row struct {
		Name                 string `json:"name"`
		MaxClusters          int64  `json:"maxClusters,omitempty"`
		MaxClustersPerMember int64  `json:"maxClustersPerMember,omitempty"`
		ClusterCount         int    `json:"clusterCount"`
	}

	rows := make([]row, 0, len(envs))
	for _, e := range envs {
		m, ok := e.(map[string]interface{})
		if !ok {
			continue
		}
		r := row{Name: client.GetNestedString(m, "name")}
		r.MaxClusters = client.GetNestedInt64(m, "limits", "maxClusters")
		r.MaxClustersPerMember = client.GetNestedInt64(m, "limits", "maxClustersPerMember")
		r.ClusterCount = usage[r.Name]
		rows = append(rows, r)
	}

	sort.Slice(rows, func(i, j int) bool { return rows[i].Name < rows[j].Name })

	if format == output.FormatJSON {
		return output.PrintJSON(os.Stdout, rows)
	}
	if format == output.FormatYAML {
		return output.PrintYAML(os.Stdout, rows)
	}

	headers := []string{"NAME", "CLUSTERS", "MAX CLUSTERS", "MAX PER MEMBER"}
	table := output.NewTable(os.Stdout, headers...)
	for _, r := range rows {
		table.AddRow(
			r.Name,
			fmt.Sprintf("%d", r.ClusterCount),
			formatLimit(r.MaxClusters),
			formatLimit(r.MaxClustersPerMember),
		)
	}
	return table.Flush()
}

// formatLimit renders a zero-value (no cap) as "-" and non-zero as the
// decimal count.
func formatLimit(v int64) string {
	if v <= 0 {
		return "-"
	}
	return fmt.Sprintf("%d", v)
}

func newDeleteCmd(logger *log.Logger) *cobra.Command {
	var (
		team       string
		force      bool
		kubeconfig string
	)

	cmd := &cobra.Command{
		Use:   "delete NAME",
		Short: "Delete an environment from a team",
		Long: `Remove an environment entry from the Team's spec.environments list.

Existing TenantClusters keep the butler.butlerlabs.dev/environment label
but the environment is no longer recognized by the webhook. Operators
should relabel those clusters first.

Examples:
  butleradm env delete staging --team payments
  butleradm env delete staging --team payments --force`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			kubeContext, _ := cmd.Flags().GetString("context")
			return runDelete(cmd.Context(), logger, args[0], team, force, kubeconfig, kubeContext)
		},
	}

	cmd.Flags().StringVar(&team, "team", "", "team name (required)")
	cmd.Flags().BoolVar(&force, "force", false, "skip confirmation prompt (dangerous)")
	cmd.Flags().BoolVarP(&force, "yes", "y", false, "skip confirmation prompt (alias for --force)")
	cmd.Flags().StringVar(&kubeconfig, "kubeconfig", "", "path to management cluster kubeconfig")
	_ = cmd.MarkFlagRequired("team")

	return cmd
}

func runDelete(ctx context.Context, logger *log.Logger, name, team string, force bool, kubeconfigPath, kubeContext string) error {
	c, err := client.New(kubeconfigPath, kubeContext)
	if err != nil {
		return fmt.Errorf("connecting to management cluster: %w", err)
	}

	tm, err := fetchTeam(ctx, c, team)
	if err != nil {
		return err
	}

	envs := envEntries(tm)
	found := -1
	for i, e := range envs {
		if envName(e) == name {
			found = i
			break
		}
	}
	if found < 0 {
		return fmt.Errorf("environment %q not found on team %s", name, team)
	}

	if !force {
		if err := confirmDelete(name, team); err != nil {
			return err
		}
	}

	envs = append(envs[:found], envs[found+1:]...)
	if len(envs) == 0 {
		unstructured.RemoveNestedField(tm.Object, "spec", "environments")
	} else if err := unstructured.SetNestedSlice(tm.Object, envs, "spec", "environments"); err != nil {
		return fmt.Errorf("setting spec.environments: %w", err)
	}

	if _, err := c.Dynamic.Resource(client.TeamGVR).Update(ctx, tm, metav1.UpdateOptions{}); err != nil {
		return fmt.Errorf("updating Team %s: %w", team, err)
	}

	logger.Success("environment deleted", "team", team, "environment", name)
	return nil
}

func confirmDelete(name, team string) error {
	fmt.Printf("Remove environment %q from team %s? Type the environment name to confirm: ", name, team)
	reader := bufio.NewReader(os.Stdin)
	input, err := reader.ReadString('\n')
	if err != nil {
		return fmt.Errorf("reading confirmation: %w", err)
	}
	input = strings.TrimSpace(input)
	if input != name {
		return fmt.Errorf("deletion cancelled: you typed %q, expected %q", input, name)
	}
	return nil
}

// validateEnvName enforces the label-value syntax ADR-009 requires.
func validateEnvName(name string) error {
	if name == "" {
		return fmt.Errorf("environment name is required")
	}
	if len(name) > 63 {
		return fmt.Errorf("environment name %q exceeds 63 characters", name)
	}
	// Label values allow [a-z0-9A-Z._-] with alphanumeric anchors.
	// Keep permissive here (lowercase is a convention, not a hard rule).
	allowed := func(r byte) bool {
		return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.'
	}
	first := name[0]
	last := name[len(name)-1]
	anchor := func(r byte) bool {
		return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
	}
	if !anchor(first) || !anchor(last) {
		return fmt.Errorf("environment name %q must start and end with an alphanumeric character", name)
	}
	for i := 0; i < len(name); i++ {
		if !allowed(name[i]) {
			return fmt.Errorf("environment name %q contains invalid character %q", name, string(name[i]))
		}
	}
	return nil
}
