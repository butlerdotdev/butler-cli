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

package cluster

import (
	"context"
	"fmt"
	"os"

	"github.com/butlerdotdev/butler/internal/common/client"
	"github.com/butlerdotdev/butler/internal/common/log"
	"github.com/butlerdotdev/butler/internal/common/output"
	"github.com/spf13/cobra"
)

// gitopsConfig holds GitOps configuration extracted from a TenantCluster.
type gitopsConfig struct {
	Provider   string `json:"provider,omitempty"`
	Version    string `json:"version,omitempty"`
	Repository string `json:"repository,omitempty"`
	Branch     string `json:"branch,omitempty"`
	Path       string `json:"path,omitempty"`
}

type gitopsOptions struct {
	namespace      string
	outputFormat   string
	kubeconfigPath string
	kubeContext    string
}

// newGitopsCmd creates the cluster gitops command.
func newGitopsCmd(logger *log.Logger) *cobra.Command {
	opts := &gitopsOptions{}

	cmd := &cobra.Command{
		Use:   "gitops NAME",
		Short: "Show GitOps configuration for a tenant cluster",
		Long: `Display the GitOps configuration for a tenant cluster.

Reads the gitops section from the TenantCluster spec.addons and
shows the configured provider, version, repository, branch, and path.

If no GitOps provider is configured on the cluster, a message is
printed indicating that GitOps is not set up.

Examples:
  # Show GitOps config
  butlerctl cluster gitops my-cluster

  # Show GitOps config for a cluster in a specific namespace
  butlerctl cluster gitops my-cluster -n team-payments

  # Output as JSON
  butlerctl cluster gitops my-cluster -o json`,
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeClusterNames,
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.kubeContext, _ = cmd.Flags().GetString("context")
			return runGitops(cmd.Context(), logger, args[0], opts)
		},
	}

	cmd.Flags().StringVarP(&opts.namespace, "namespace", "n", DefaultTenantNamespace, "namespace of the TenantCluster")
	cmd.Flags().StringVarP(&opts.outputFormat, "output", "o", "", "output format (json, yaml)")
	cmd.Flags().StringVar(&opts.kubeconfigPath, "kubeconfig", "", "path to management cluster kubeconfig")

	return cmd
}

func runGitops(ctx context.Context, logger *log.Logger, clusterName string, opts *gitopsOptions) error {
	// Connect to management cluster
	c, err := client.New(opts.kubeconfigPath, opts.kubeContext)
	if err != nil {
		return fmt.Errorf("connecting to management cluster: %w", err)
	}

	// Get the TenantCluster
	tc, err := c.GetTenantCluster(ctx, opts.namespace, clusterName)
	if err != nil {
		return fmt.Errorf("getting TenantCluster %s/%s: %w", opts.namespace, clusterName, err)
	}

	// Extract GitOps configuration from spec.addons.gitops
	obj := tc.Object
	cfg := gitopsConfig{
		Provider:   client.GetNestedString(obj, "spec", "addons", "gitops", "provider"),
		Version:    client.GetNestedString(obj, "spec", "addons", "gitops", "version"),
		Repository: client.GetNestedString(obj, "spec", "addons", "gitops", "repository", "url"),
		Branch:     client.GetNestedString(obj, "spec", "addons", "gitops", "repository", "branch"),
		Path:       client.GetNestedString(obj, "spec", "addons", "gitops", "repository", "path"),
	}

	// Check whether any GitOps config is present
	notConfigured := cfg.Provider == "" && cfg.Version == "" && cfg.Repository == ""

	// Handle structured output
	if opts.outputFormat == "json" {
		if notConfigured {
			return output.PrintJSON(os.Stdout, nil)
		}
		return output.PrintJSON(os.Stdout, cfg)
	}
	if opts.outputFormat == "yaml" {
		if notConfigured {
			return output.PrintYAML(os.Stdout, nil)
		}
		return output.PrintYAML(os.Stdout, cfg)
	}

	if notConfigured {
		fmt.Println("GitOps is not configured on this cluster.")
		return nil
	}

	// Human-readable output
	fmt.Println("GitOps Configuration:")
	fmt.Printf("  Provider:   %s\n", orDefault(cfg.Provider, "<not set>"))
	fmt.Printf("  Version:    %s\n", orDefault(cfg.Version, "<not set>"))
	fmt.Printf("  Repository: %s\n", orDefault(cfg.Repository, "<not set>"))
	fmt.Printf("  Branch:     %s\n", orDefault(cfg.Branch, "<not set>"))
	fmt.Printf("  Path:       %s\n", orDefault(cfg.Path, "<not set>"))

	return nil
}
