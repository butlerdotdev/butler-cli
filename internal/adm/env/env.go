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
	"strconv"
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

// createOptions holds inputs to the env create subcommand. Mirrors
// updateOptions so both commands evolve together as ADR-009
// EnvironmentSpec fields grow.
type createOptions struct {
	team                 string
	name                 string
	maxClusters          int32
	maxClustersPerMember int32
	description          string
	clusterDefaults      []string
	accessUsers          []string
	accessGroups         []string
	kubeconfig           string
	kubeContext          string
}

func newCreateCmd(logger *log.Logger) *cobra.Command {
	opts := &createOptions{}

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
  butleradm env create prod --team payments --description "production workloads"
  butleradm env create prod --team payments --cluster-default kubernetesVersion=v1.31.0 --cluster-default workerCount=3
  butleradm env create prod --team payments --access-user alice@example.com:admin --access-user bob@example.com:operator
  butleradm env create prod --team payments --access-group sre:admin --access-group developers:viewer:okta`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.name = args[0]
			opts.kubeContext, _ = cmd.Flags().GetString("context")
			return runCreate(cmd.Context(), logger, opts)
		},
	}

	cmd.Flags().StringVar(&opts.team, "team", "", "team name (required)")
	cmd.Flags().Int32Var(&opts.maxClusters, "max-clusters", 0, "max TenantClusters in this environment (0 = no cap)")
	cmd.Flags().Int32Var(&opts.maxClustersPerMember, "max-clusters-per-member", 0, "max TenantClusters per member in this environment (0 = no cap)")
	cmd.Flags().StringVar(&opts.description, "description", "", "human-readable description for this environment")
	cmd.Flags().StringArrayVar(&opts.clusterDefaults, "cluster-default", nil, "env-level cluster default, key=value (repeatable). Keys: kubernetesVersion, workerCount, workerCPU, workerMemoryGi, workerDiskGi")
	cmd.Flags().StringArrayVar(&opts.accessUsers, "access-user", nil, "env access user, email:role (repeatable). Role is admin, operator, or viewer. Additive only; cannot reduce a team-level role.")
	cmd.Flags().StringArrayVar(&opts.accessGroups, "access-group", nil, "env access group, name:role or name:role:identityProvider (repeatable). Role is admin, operator, or viewer.")
	cmd.Flags().StringVar(&opts.kubeconfig, "kubeconfig", "", "path to management cluster kubeconfig")
	_ = cmd.MarkFlagRequired("team")

	return cmd
}

func runCreate(ctx context.Context, logger *log.Logger, opts *createOptions) error {
	if err := validateEnvName(opts.name); err != nil {
		return err
	}

	defaults, err := parseClusterDefaults(opts.clusterDefaults)
	if err != nil {
		return err
	}

	users, err := parseAccessUsers(opts.accessUsers)
	if err != nil {
		return err
	}

	groups, err := parseAccessGroups(opts.accessGroups)
	if err != nil {
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
	for _, e := range envs {
		if envName(e) == opts.name {
			return fmt.Errorf("environment %q already exists on team %s", opts.name, opts.team)
		}
	}

	entry := map[string]interface{}{
		"name": opts.name,
	}
	if opts.description != "" {
		entry["description"] = opts.description
	}
	limits := map[string]interface{}{}
	if opts.maxClusters > 0 {
		limits["maxClusters"] = int64(opts.maxClusters)
	}
	if opts.maxClustersPerMember > 0 {
		limits["maxClustersPerMember"] = int64(opts.maxClustersPerMember)
	}
	if len(limits) > 0 {
		entry["limits"] = limits
	}
	if len(defaults) > 0 {
		entry["clusterDefaults"] = defaults
	}
	if len(users) > 0 || len(groups) > 0 {
		access := map[string]interface{}{}
		if len(users) > 0 {
			access["users"] = users
		}
		if len(groups) > 0 {
			access["groups"] = groups
		}
		entry["access"] = access
	}
	envs = append(envs, entry)

	if err := unstructured.SetNestedSlice(tm.Object, envs, "spec", "environments"); err != nil {
		return fmt.Errorf("setting spec.environments: %w", err)
	}

	if _, err := c.Dynamic.Resource(client.TeamGVR).Update(ctx, tm, metav1.UpdateOptions{}); err != nil {
		return fmt.Errorf("updating Team %s: %w", opts.team, err)
	}

	logger.Success("environment created", "team", opts.team, "environment", opts.name)
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
	clusterDefaults      []string
	clearClusterDefaults bool
	accessUsers          []string
	accessGroups         []string
	clearAccess          bool
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
  butleradm env update prod --team payments --description "production workloads"
  butleradm env update prod --team payments --cluster-default kubernetesVersion=v1.31.0
  butleradm env update prod --team payments --cluster-default workerCount= (clears one key)
  butleradm env update prod --team payments --clear-cluster-defaults
  butleradm env update prod --team payments --access-user alice@example.com:admin
  butleradm env update prod --team payments --access-user alice@example.com: (remove one user)
  butleradm env update prod --team payments --access-group sre:admin
  butleradm env update prod --team payments --access-group developers:viewer:okta
  butleradm env update prod --team payments --access-group sre: (remove one group)
  butleradm env update prod --team payments --clear-access`,
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
	cmd.Flags().StringArrayVar(&opts.clusterDefaults, "cluster-default", nil, "env-level cluster default, key=value (repeatable). Empty value clears that key. Keys: kubernetesVersion, workerCount, workerCPU, workerMemoryGi, workerDiskGi")
	cmd.Flags().BoolVar(&opts.clearClusterDefaults, "clear-cluster-defaults", false, "drop the entire clusterDefaults block on this environment")
	cmd.Flags().StringArrayVar(&opts.accessUsers, "access-user", nil, "env access user, email:role (repeatable). Empty role removes that user. Role is admin, operator, or viewer.")
	cmd.Flags().StringArrayVar(&opts.accessGroups, "access-group", nil, "env access group, name:role or name:role:identityProvider (repeatable). Empty role removes that group.")
	cmd.Flags().BoolVar(&opts.clearAccess, "clear-access", false, "drop the entire access block (users and groups) on this environment")
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

	if opts.clearClusterDefaults {
		if _, had := entry["clusterDefaults"]; had {
			delete(entry, "clusterDefaults")
			changed = true
		}
	} else if len(opts.clusterDefaults) > 0 {
		existing, _ := entry["clusterDefaults"].(map[string]interface{})
		merged, err := mergeClusterDefaultPatches(existing, opts.clusterDefaults)
		if err != nil {
			return err
		}
		if len(merged) == 0 {
			delete(entry, "clusterDefaults")
		} else {
			entry["clusterDefaults"] = merged
		}
		changed = true
	}

	if opts.clearAccess {
		if _, had := entry["access"]; had {
			delete(entry, "access")
			changed = true
		}
	} else if len(opts.accessUsers) > 0 || len(opts.accessGroups) > 0 {
		access, _ := entry["access"].(map[string]interface{})
		if access == nil {
			access = map[string]interface{}{}
		}
		if len(opts.accessUsers) > 0 {
			existingUsers, _ := access["users"].([]interface{})
			merged, err := mergeAccessUserPatches(existingUsers, opts.accessUsers)
			if err != nil {
				return err
			}
			if len(merged) == 0 {
				delete(access, "users")
			} else {
				access["users"] = merged
			}
		}
		if len(opts.accessGroups) > 0 {
			existingGroups, _ := access["groups"].([]interface{})
			merged, err := mergeAccessGroupPatches(existingGroups, opts.accessGroups)
			if err != nil {
				return err
			}
			if len(merged) == 0 {
				delete(access, "groups")
			} else {
				access["groups"] = merged
			}
		}
		if len(access) == 0 {
			delete(entry, "access")
		} else {
			entry["access"] = access
		}
		changed = true
	}

	if !changed {
		return fmt.Errorf("nothing to update: pass --description, --max-clusters, --max-clusters-per-member, --clear-limits, --cluster-default, --clear-cluster-defaults, --access-user, --access-group, or --clear-access")
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

// clusterDefaultNumericKeys are ClusterDefaults fields with int32 types
// in butler-api's Team CRD. parseClusterDefaultValue coerces them to
// int64 so SetNestedSlice accepts the unstructured shape.
var clusterDefaultNumericKeys = map[string]struct{}{
	"workerCount":    {},
	"workerCPU":      {},
	"workerMemoryGi": {},
	"workerDiskGi":   {},
}

// clusterDefaultStringKeys are string-typed ClusterDefaults fields.
var clusterDefaultStringKeys = map[string]struct{}{
	"kubernetesVersion": {},
}

// parseClusterDefaults expands repeatable --cluster-default key=value
// pairs into a map the ClusterDefaults schema accepts. Empty value is
// rejected here because create semantics never want a zero-length
// string. The update path has its own merge that treats empty as delete.
func parseClusterDefaults(pairs []string) (map[string]interface{}, error) {
	if len(pairs) == 0 {
		return nil, nil
	}
	out := map[string]interface{}{}
	for _, raw := range pairs {
		key, val, err := splitClusterDefaultPair(raw)
		if err != nil {
			return nil, err
		}
		if val == "" {
			return nil, fmt.Errorf("--cluster-default %q: value is required (empty values are only allowed in env update to clear a key)", raw)
		}
		typed, err := coerceClusterDefaultValue(key, val)
		if err != nil {
			return nil, err
		}
		out[key] = typed
	}
	return out, nil
}

// mergeClusterDefaultPatches applies a series of key=value pairs over
// an existing clusterDefaults map for env update. An empty value
// deletes the key so operators can narrow the set without a full
// --clear-cluster-defaults reset.
func mergeClusterDefaultPatches(existing map[string]interface{}, pairs []string) (map[string]interface{}, error) {
	merged := map[string]interface{}{}
	for k, v := range existing {
		merged[k] = v
	}
	for _, raw := range pairs {
		key, val, err := splitClusterDefaultPair(raw)
		if err != nil {
			return nil, err
		}
		if val == "" {
			delete(merged, key)
			continue
		}
		typed, err := coerceClusterDefaultValue(key, val)
		if err != nil {
			return nil, err
		}
		merged[key] = typed
	}
	return merged, nil
}

func splitClusterDefaultPair(raw string) (string, string, error) {
	idx := strings.Index(raw, "=")
	if idx < 0 {
		return "", "", fmt.Errorf("--cluster-default %q: expected key=value", raw)
	}
	key := strings.TrimSpace(raw[:idx])
	val := strings.TrimSpace(raw[idx+1:])
	if key == "" {
		return "", "", fmt.Errorf("--cluster-default %q: key is required", raw)
	}
	if _, ok := clusterDefaultNumericKeys[key]; ok {
		return key, val, nil
	}
	if _, ok := clusterDefaultStringKeys[key]; ok {
		return key, val, nil
	}
	return "", "", fmt.Errorf("--cluster-default %q: unknown key %q (supported: kubernetesVersion, workerCount, workerCPU, workerMemoryGi, workerDiskGi)", raw, key)
}

func coerceClusterDefaultValue(key, val string) (interface{}, error) {
	if _, numeric := clusterDefaultNumericKeys[key]; numeric {
		n, err := strconv.ParseInt(val, 10, 32)
		if err != nil {
			return nil, fmt.Errorf("--cluster-default %s=%s: value must be an integer", key, val)
		}
		return n, nil
	}
	return val, nil
}

// validAccessRoles is the TeamRole enum from butler-api team_types.go.
// Kept as a map so lookups are O(1) and the set stays in sync with the
// CRD enum when we update it.
var validAccessRoles = map[string]struct{}{
	"admin":    {},
	"operator": {},
	"viewer":   {},
}

// parseAccessUsers expands repeatable --access-user email:role pairs
// into the TeamAccess.Users shape butler-api expects. Empty role is
// rejected on create; the update path has its own merge that treats
// empty role as "remove this user".
func parseAccessUsers(pairs []string) ([]interface{}, error) {
	if len(pairs) == 0 {
		return nil, nil
	}
	seen := map[string]struct{}{}
	out := make([]interface{}, 0, len(pairs))
	for _, raw := range pairs {
		email, role, err := splitAccessUserPair(raw)
		if err != nil {
			return nil, err
		}
		if role == "" {
			return nil, fmt.Errorf("--access-user %q: role is required (empty role is only allowed in env update to remove a user)", raw)
		}
		if _, dup := seen[email]; dup {
			return nil, fmt.Errorf("--access-user %s: email listed more than once", email)
		}
		seen[email] = struct{}{}
		out = append(out, map[string]interface{}{"name": email, "role": role})
	}
	return out, nil
}

// mergeAccessUserPatches applies a series of email:role pairs over an
// existing users list for env update. An empty role removes the user;
// a matching email with a new role overwrites the entry in place.
func mergeAccessUserPatches(existing []interface{}, pairs []string) ([]interface{}, error) {
	// Build an index of the current list so we can patch in place without
	// forgetting entries the operator did not touch.
	byEmail := map[string]int{}
	merged := make([]interface{}, 0, len(existing))
	for _, e := range existing {
		m, ok := e.(map[string]interface{})
		if !ok {
			merged = append(merged, e)
			continue
		}
		email, _ := m["name"].(string)
		byEmail[email] = len(merged)
		merged = append(merged, m)
	}

	for _, raw := range pairs {
		email, role, err := splitAccessUserPair(raw)
		if err != nil {
			return nil, err
		}
		if role == "" {
			if idx, ok := byEmail[email]; ok {
				merged = append(merged[:idx], merged[idx+1:]...)
				delete(byEmail, email)
				// Shift indices for everything after idx.
				for k, v := range byEmail {
					if v > idx {
						byEmail[k] = v - 1
					}
				}
			}
			continue
		}
		entry := map[string]interface{}{"name": email, "role": role}
		if idx, ok := byEmail[email]; ok {
			merged[idx] = entry
		} else {
			byEmail[email] = len(merged)
			merged = append(merged, entry)
		}
	}
	return merged, nil
}

func splitAccessUserPair(raw string) (string, string, error) {
	idx := strings.Index(raw, ":")
	if idx < 0 {
		return "", "", fmt.Errorf("--access-user %q: expected email:role", raw)
	}
	email := strings.TrimSpace(raw[:idx])
	role := strings.TrimSpace(raw[idx+1:])
	if email == "" {
		return "", "", fmt.Errorf("--access-user %q: email is required", raw)
	}
	if role != "" {
		if _, ok := validAccessRoles[role]; !ok {
			return "", "", fmt.Errorf("--access-user %q: role %q must be admin, operator, or viewer", raw, role)
		}
	}
	return email, role, nil
}

// parseAccessGroups expands repeatable --access-group name:role or
// name:role:idp pairs into the TeamAccess.Groups shape. IdentityProvider
// is optional and written only when the 3rd segment is present so we
// never stamp an empty string where the CRD expects an absent field.
func parseAccessGroups(pairs []string) ([]interface{}, error) {
	if len(pairs) == 0 {
		return nil, nil
	}
	// Track (name, idp) as a composite key: two groups with the same name
	// but different identityProviders are distinct by the CRD, so the
	// duplicate check must be scoped to the pair.
	seen := map[string]struct{}{}
	out := make([]interface{}, 0, len(pairs))
	for _, raw := range pairs {
		name, role, idp, err := splitAccessGroupPair(raw)
		if err != nil {
			return nil, err
		}
		if role == "" {
			return nil, fmt.Errorf("--access-group %q: role is required (empty role is only allowed in env update to remove a group)", raw)
		}
		key := name + "\x00" + idp
		if _, dup := seen[key]; dup {
			return nil, fmt.Errorf("--access-group %s: same name+identityProvider listed more than once", raw)
		}
		seen[key] = struct{}{}
		entry := map[string]interface{}{"name": name, "role": role}
		if idp != "" {
			entry["identityProvider"] = idp
		}
		out = append(out, entry)
	}
	return out, nil
}

// mergeAccessGroupPatches applies a series of group patches over an
// existing groups list. Match key is (name, identityProvider) so two
// groups with the same name from different IdPs remain separate
// entries. Empty role removes the matched group; missing match appends.
func mergeAccessGroupPatches(existing []interface{}, pairs []string) ([]interface{}, error) {
	type key struct{ name, idp string }
	byKey := map[key]int{}
	merged := make([]interface{}, 0, len(existing))
	for _, g := range existing {
		m, ok := g.(map[string]interface{})
		if !ok {
			merged = append(merged, g)
			continue
		}
		name, _ := m["name"].(string)
		idp, _ := m["identityProvider"].(string)
		byKey[key{name, idp}] = len(merged)
		merged = append(merged, m)
	}

	for _, raw := range pairs {
		name, role, idp, err := splitAccessGroupPair(raw)
		if err != nil {
			return nil, err
		}
		k := key{name, idp}
		if role == "" {
			if idx, ok := byKey[k]; ok {
				merged = append(merged[:idx], merged[idx+1:]...)
				delete(byKey, k)
				for kk, v := range byKey {
					if v > idx {
						byKey[kk] = v - 1
					}
				}
			}
			continue
		}
		entry := map[string]interface{}{"name": name, "role": role}
		if idp != "" {
			entry["identityProvider"] = idp
		}
		if idx, ok := byKey[k]; ok {
			merged[idx] = entry
		} else {
			byKey[k] = len(merged)
			merged = append(merged, entry)
		}
	}
	return merged, nil
}

// splitAccessGroupPair parses name:role or name:role:idp. Using SplitN
// with n=3 keeps any further colons inside the identityProvider string,
// which matters for LDAP DN group names that embed colons in attribute
// values (rare but legal).
func splitAccessGroupPair(raw string) (string, string, string, error) {
	parts := strings.SplitN(raw, ":", 3)
	if len(parts) < 2 {
		return "", "", "", fmt.Errorf("--access-group %q: expected name:role or name:role:identityProvider", raw)
	}
	name := strings.TrimSpace(parts[0])
	role := strings.TrimSpace(parts[1])
	idp := ""
	if len(parts) == 3 {
		idp = strings.TrimSpace(parts[2])
	}
	if name == "" {
		return "", "", "", fmt.Errorf("--access-group %q: name is required", raw)
	}
	if role != "" {
		if _, ok := validAccessRoles[role]; !ok {
			return "", "", "", fmt.Errorf("--access-group %q: role %q must be admin, operator, or viewer", raw, role)
		}
	}
	return name, role, idp, nil
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
