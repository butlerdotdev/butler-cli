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

package env

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/butlerdotdev/butler/internal/common/client"
	"github.com/butlerdotdev/butler/internal/common/log"
	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// migrateOptions holds inputs to the env migrate subcommand.
type migrateOptions struct {
	team        string
	environment string
	all         bool
	relabel     bool
	force       bool
	kubeconfig  string
	kubeContext string
	names       []string
}

func newMigrateCmd(logger *log.Logger) *cobra.Command {
	opts := &migrateOptions{}

	cmd := &cobra.Command{
		Use:   "migrate [NAME...]",
		Short: "Label TenantClusters into a Team environment",
		Long: `Backfill the butler.butlerlabs.dev/environment label on TenantClusters
in a Team's namespace.

By default migrate only labels TenantClusters that have no environment
label. Pass --all to target every unlabeled TenantCluster in the team
namespace, or name specific clusters as positional arguments.

Pass --relabel to rewrite clusters that already have a different
environment label. Relabeling requires a second confirmation even under
--force because it changes live state operators may depend on.

Examples:
  # Label specific clusters into the staging environment
  butleradm env migrate --team payments --environment staging cluster-a cluster-b

  # Label every unlabeled cluster in the team namespace
  butleradm env migrate --team payments --environment staging --all

  # Rewrite every cluster's environment label (destructive)
  butleradm env migrate --team payments --environment staging --all --relabel`,
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.names = args
			opts.kubeContext, _ = cmd.Flags().GetString("context")
			return runMigrate(cmd.Context(), logger, opts)
		},
	}

	cmd.Flags().StringVar(&opts.team, "team", "", "team name (required)")
	cmd.Flags().StringVar(&opts.environment, "environment", "", "target environment name (required)")
	cmd.Flags().BoolVar(&opts.all, "all", false, "target every matching cluster in the team namespace")
	cmd.Flags().BoolVar(&opts.relabel, "relabel", false, "rewrite existing environment labels (requires extra confirmation)")
	cmd.Flags().BoolVar(&opts.force, "force", false, "skip the first confirmation prompt")
	cmd.Flags().BoolVarP(&opts.force, "yes", "y", false, "skip the first confirmation prompt (alias for --force)")
	cmd.Flags().StringVar(&opts.kubeconfig, "kubeconfig", "", "path to management cluster kubeconfig")
	_ = cmd.MarkFlagRequired("team")
	_ = cmd.MarkFlagRequired("environment")

	return cmd
}

func runMigrate(ctx context.Context, logger *log.Logger, opts *migrateOptions) error {
	if !opts.all && len(opts.names) == 0 {
		return fmt.Errorf("specify TenantCluster names or pass --all")
	}
	if opts.all && len(opts.names) > 0 {
		return fmt.Errorf("cannot mix --all with positional cluster names")
	}

	c, err := client.New(opts.kubeconfig, opts.kubeContext)
	if err != nil {
		return fmt.Errorf("connecting to management cluster: %w", err)
	}

	tm, err := fetchTeam(ctx, c, opts.team)
	if err != nil {
		return err
	}
	if !envDefined(tm, opts.environment) {
		return fmt.Errorf("environment %q is not defined on team %s; create it first", opts.environment, opts.team)
	}

	ns := teamNamespace(opts.team)
	tcList, err := c.Dynamic.Resource(client.TenantClusterGVR).Namespace(ns).List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("listing TenantClusters in %s: %w", ns, err)
	}

	targets := SelectMigrationTargets(tcList.Items, opts.names, opts.all, opts.relabel, opts.environment)
	if len(targets) == 0 {
		logger.Info("no clusters require relabeling", "team", opts.team, "environment", opts.environment)
		return nil
	}

	hasRewrites := false
	for _, t := range targets {
		if t.PreviousEnv != "" && t.PreviousEnv != opts.environment {
			hasRewrites = true
			break
		}
	}

	printMigrationPlan(opts.environment, targets)

	if !opts.force {
		if err := confirmPrompt(fmt.Sprintf("Apply environment %q label to %d cluster(s)? [y/N]: ", opts.environment, len(targets))); err != nil {
			return err
		}
	}
	if opts.relabel && hasRewrites {
		if err := confirmPrompt("Relabel is destructive and rewrites existing environment assignments. Type RELABEL to proceed: "); err != nil {
			return err
		}
	}

	for _, t := range targets {
		item := tcList.Items[t.Index]
		applyMigrationMutation(&item, opts.environment)

		if _, err := c.Dynamic.Resource(client.TenantClusterGVR).Namespace(ns).Update(ctx, &item, metav1.UpdateOptions{}); err != nil {
			return fmt.Errorf("updating TenantCluster %s: %w", item.GetName(), err)
		}
		logger.Success("labeled", "cluster", item.GetName(), "environment", opts.environment)
	}

	return nil
}

// applyMigrationMutation stamps the env label and the migration-operation
// annotation on a TenantCluster prior to Update. The annotation is what the
// controller admission webhook requires for env-label changes (ADR-009
// phased migration); without it the update is rejected. Existing labels and
// annotations are preserved.
func applyMigrationMutation(item *unstructured.Unstructured, env string) {
	labels := item.GetLabels()
	if labels == nil {
		labels = map[string]string{}
	}
	labels[EnvironmentLabel] = env
	item.SetLabels(labels)

	annotations := item.GetAnnotations()
	if annotations == nil {
		annotations = map[string]string{}
	}
	annotations[MigrationOperationAnnotation] = "true"
	item.SetAnnotations(annotations)
}

// MigrationTarget records a single rename that SelectMigrationTargets chose.
type MigrationTarget struct {
	Index       int    // position in the original list
	Name        string // cluster name
	PreviousEnv string // prior label value, empty when unlabeled
}

// SelectMigrationTargets decides which TenantClusters to relabel given the
// operator's flags. Separated from runMigrate so unit tests can exercise the
// selection rules without a live apiserver.
//
// Rules:
//   - When names is non-empty, every named cluster must be present in items
//     (returns the subset in original-list order).
//   - When all is true and relabel is false, include every cluster whose env
//     label is empty or already equals env.
//   - When all is true and relabel is true, include every cluster whose env
//     label differs from env (includes labeled-to-something-else and unlabeled).
//   - Clusters that already carry the target env are filtered out (no-op).
func SelectMigrationTargets(items []unstructured.Unstructured, names []string, all, relabel bool, env string) []MigrationTarget {
	var out []MigrationTarget

	if len(names) > 0 {
		wanted := map[string]struct{}{}
		for _, n := range names {
			wanted[n] = struct{}{}
		}
		for i := range items {
			name := items[i].GetName()
			if _, ok := wanted[name]; !ok {
				continue
			}
			prev := items[i].GetLabels()[EnvironmentLabel]
			if prev == env {
				continue
			}
			// Without --relabel, refuse to overwrite an existing label.
			if prev != "" && !relabel {
				continue
			}
			out = append(out, MigrationTarget{Index: i, Name: name, PreviousEnv: prev})
		}
		return out
	}

	if !all {
		return out
	}

	for i := range items {
		name := items[i].GetName()
		prev := items[i].GetLabels()[EnvironmentLabel]
		if prev == env {
			continue
		}
		if prev != "" && !relabel {
			continue
		}
		out = append(out, MigrationTarget{Index: i, Name: name, PreviousEnv: prev})
	}
	return out
}

// envDefined reports whether the given env name exists in Team.spec.environments.
func envDefined(tm *unstructured.Unstructured, name string) bool {
	for _, e := range envEntries(tm) {
		if envName(e) == name {
			return true
		}
	}
	return false
}

func printMigrationPlan(env string, targets []MigrationTarget) {
	fmt.Printf("Migration plan: target environment %q (%d cluster(s))\n", env, len(targets))
	for _, t := range targets {
		prev := t.PreviousEnv
		if prev == "" {
			prev = "(unlabeled)"
		}
		fmt.Printf("  %s  %s -> %s\n", t.Name, prev, env)
	}
}

// confirmPrompt prints message, reads one line from stdin, and accepts
// a positive response ("y", "yes", or "RELABEL"). Any other input cancels.
func confirmPrompt(message string) error {
	fmt.Print(message)
	reader := bufio.NewReader(os.Stdin)
	input, err := reader.ReadString('\n')
	if err != nil {
		return fmt.Errorf("reading confirmation: %w", err)
	}
	input = strings.TrimSpace(input)
	lower := strings.ToLower(input)
	if lower == "y" || lower == "yes" || input == "RELABEL" {
		return nil
	}
	return fmt.Errorf("migration cancelled: input %q", input)
}
