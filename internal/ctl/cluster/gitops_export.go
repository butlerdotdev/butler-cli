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
	"io"
	"os"
	"time"

	"github.com/butlerdotdev/butler/internal/common/log"
	"github.com/butlerdotdev/butler/internal/common/output"
	"github.com/butlerdotdev/butler/internal/common/serverhttp"
	"github.com/spf13/cobra"
)

// gitopsExportTimeout is generous: export runs cluster discovery plus git
// operations (clone/commit/push or PR creation) server-side.
const gitopsExportTimeout = 5 * time.Minute

// exportRequest mirrors butler-server's exportClusterRequest
// (POST /api/clusters/{ns}/{name}/gitops/export-cluster). The selection set is
// deferred (CLI v1 exports everything).
type exportRequest struct {
	Env           string `json:"env,omitempty"`
	ClusterName   string `json:"clusterName,omitempty"`
	Repository    string `json:"repository"`
	Branch        string `json:"branch"`
	CreatePR      bool   `json:"createPR,omitempty"`
	PRTitle       string `json:"prTitle,omitempty"`
	PRBody        string `json:"prBody,omitempty"`
	CommitMessage string `json:"commitMessage,omitempty"`
}

// exportResult mirrors butler-server's exportClusterResponse.
type exportResult struct {
	Success    bool     `json:"success"`
	Message    string   `json:"message"`
	Mode       string   `json:"mode,omitempty"`
	Branch     string   `json:"branch,omitempty"`
	CommitSHA  string   `json:"commitSha,omitempty"`
	PRURL      string   `json:"prUrl,omitempty"`
	PRNumber   int      `json:"prNumber,omitempty"`
	FilesCount int      `json:"filesCount,omitempty"`
	Files      []string `json:"files,omitempty"`
}

type exportOptions struct {
	env           string
	clusterName   string
	repository    string
	branch        string
	createPR      bool
	prTitle       string
	prBody        string
	commitMessage string
}

// newGitopsExportCmd creates `cluster gitops export`.
func newGitopsExportCmd(_ *log.Logger) *cobra.Command {
	opts := &exportOptions{}

	cmd := &cobra.Command{
		Use:   "export NAME",
		Short: "Export a tenant cluster's inventory to a GitOps repository",
		Long: `Export a tenant cluster's full inventory to a GitOps repository.

butler-server discovers the cluster's Helm and native inventory, renders the
GitOps layout, and writes it to the target repository (direct push, or a pull
request with --create-pr). Run 'cluster gitops preview' first to see what would
be written.

Exit codes:
  0  export completed
  1  client-side error or server error

Examples:
  butlerctl cluster gitops export my-cluster --repo https://github.com/acme/clusters
  butlerctl cluster gitops export my-cluster --repo https://github.com/acme/clusters --create-pr --pr-title "GitOps export"`,
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeClusterNames,
		RunE: func(cmd *cobra.Command, args []string) error {
			ns, _ := cmd.Flags().GetString("namespace")
			outputFormat, _ := cmd.Flags().GetString("output")
			return runGitopsExport(cmd.Context(), os.Stdout, args[0], ns, outputFormat, opts)
		},
	}

	cmd.Flags().StringVar(&opts.repository, "repo", "", "target GitOps repository URL (required)")
	cmd.Flags().StringVar(&opts.branch, "branch", "main", "repository branch")
	cmd.Flags().BoolVar(&opts.createPR, "create-pr", false, "open a pull request instead of pushing directly")
	cmd.Flags().StringVar(&opts.prTitle, "pr-title", "", "pull request title (with --create-pr)")
	cmd.Flags().StringVar(&opts.prBody, "pr-body", "", "pull request body (with --create-pr)")
	cmd.Flags().StringVar(&opts.commitMessage, "commit-message", "", "commit message for the export")
	cmd.Flags().StringVar(&opts.env, "env", "", "apps overlay name (defaults server-side to prd)")
	cmd.Flags().StringVar(&opts.clusterName, "cluster-name", "", "directory name emitted under clusters/ (defaults server-side)")
	_ = cmd.MarkFlagRequired("repo")

	return cmd
}

func buildExportRequest(opts *exportOptions) exportRequest {
	return exportRequest{
		Env:           opts.env,
		ClusterName:   opts.clusterName,
		Repository:    opts.repository,
		Branch:        opts.branch,
		CreatePR:      opts.createPR,
		PRTitle:       opts.prTitle,
		PRBody:        opts.prBody,
		CommitMessage: opts.commitMessage,
	}
}

func runGitopsExport(ctx context.Context, out io.Writer, name, namespace, outputFormat string, opts *exportOptions) error {
	sh, err := serverhttp.NewWithTimeout(gitopsExportTimeout)
	if err != nil {
		return err
	}

	var res exportResult
	path := fmt.Sprintf("/api/clusters/%s/%s/gitops/export-cluster", namespace, name)
	if err := sh.Post(ctx, path, buildExportRequest(opts), &res); err != nil {
		return translateGitopsError(err)
	}

	switch outputFormat {
	case "json":
		return output.PrintJSON(out, res)
	case "yaml":
		return output.PrintYAML(out, res)
	case "", "table":
		return printExportResult(out, res)
	default:
		return fmt.Errorf("unsupported output format %q (use json or yaml)", outputFormat)
	}
}

func printExportResult(out io.Writer, res exportResult) error {
	fmt.Fprintf(out, "Export complete (%d files, mode=%s).\n", res.FilesCount, orDefault(res.Mode, "unknown"))
	if res.PRURL != "" {
		fmt.Fprintf(out, "Pull request: %s\n", res.PRURL)
		return nil
	}
	if res.Branch != "" {
		fmt.Fprintf(out, "Branch: %s\n", res.Branch)
	}
	if res.CommitSHA != "" {
		fmt.Fprintf(out, "Commit: %s\n", res.CommitSHA)
	}
	return nil
}
