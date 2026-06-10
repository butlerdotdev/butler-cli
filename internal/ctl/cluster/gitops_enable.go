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

// gitopsEnableTimeout is generous: enabling GitOps runs flux bootstrap on the
// tenant cluster server-side, which installs controllers and waits for them.
const gitopsEnableTimeout = 5 * time.Minute

// enableRequest mirrors butler-server's gitops.EnableGitOpsRequest
// (POST /api/clusters/{ns}/{name}/gitops/enable).
type enableRequest struct {
	Provider   string `json:"provider,omitempty"`
	Repository string `json:"repository"`
	Branch     string `json:"branch,omitempty"`
	Path       string `json:"path,omitempty"`
	Private    bool   `json:"private,omitempty"`
}

// enableResult mirrors butler-server's gitops.EnableGitOpsResponse.
type enableResult struct {
	Success       bool   `json:"success"`
	Message       string `json:"message"`
	RepositoryURL string `json:"repositoryUrl"`
	Provider      string `json:"provider"`
	Version       string `json:"version,omitempty"`
	Path          string `json:"path"`
}

type enableOptions struct {
	repository string
	branch     string
	path       string
	provider   string
	private    bool
}

// newGitopsEnableCmd creates `cluster gitops enable`.
func newGitopsEnableCmd(_ *log.Logger) *cobra.Command {
	opts := &enableOptions{}

	cmd := &cobra.Command{
		Use:   "enable NAME",
		Short: "Enable GitOps on a tenant cluster",
		Long: `Enable GitOps on a tenant cluster.

butler-server bootstraps the GitOps engine (Flux) on the cluster against the
given repository. This is a long-running operation; the command waits for the
server to finish.

Exit codes:
  0  GitOps enabled
  1  client-side error or server error

The --repo value is the repository in owner/repo form (for example acme/clusters),
not a URL.

Examples:
  butlerctl cluster gitops enable my-cluster --repo acme/clusters
  butlerctl cluster gitops enable my-cluster --repo acme/clusters --branch main --path clusters/my-cluster`,
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeClusterNames,
		RunE: func(cmd *cobra.Command, args []string) error {
			ns, _ := cmd.Flags().GetString("namespace")
			outputFormat, _ := cmd.Flags().GetString("output")
			return runGitopsEnable(cmd.Context(), os.Stdout, args[0], ns, outputFormat, opts)
		},
	}

	cmd.Flags().StringVar(&opts.repository, "repo", "", "GitOps repository in owner/repo form, e.g. acme/clusters (required)")
	cmd.Flags().StringVar(&opts.branch, "branch", "main", "repository branch")
	cmd.Flags().StringVar(&opts.path, "path", "", "path within the repository (defaults server-side to clusters/<name>)")
	cmd.Flags().StringVar(&opts.provider, "provider", "github", "git provider (github, gitlab)")
	cmd.Flags().BoolVar(&opts.private, "private", false, "treat the repository as private")
	_ = cmd.MarkFlagRequired("repo")

	return cmd
}

func buildEnableRequest(opts *enableOptions) enableRequest {
	return enableRequest{
		Provider:   opts.provider,
		Repository: opts.repository,
		Branch:     opts.branch,
		Path:       opts.path,
		Private:    opts.private,
	}
}

func runGitopsEnable(ctx context.Context, out io.Writer, name, namespace, outputFormat string, opts *enableOptions) error {
	if err := validateRepoFullName(opts.repository); err != nil {
		return err
	}

	sh, err := serverhttp.NewWithTimeout(gitopsEnableTimeout)
	if err != nil {
		return err
	}

	var res enableResult
	path := fmt.Sprintf("/api/clusters/%s/%s/gitops/enable", namespace, name)
	if err := sh.Post(ctx, path, buildEnableRequest(opts), &res); err != nil {
		return translateGitopsError(err)
	}

	switch outputFormat {
	case "json":
		return output.PrintJSON(out, res)
	case "yaml":
		return output.PrintYAML(out, res)
	case "", "table":
		return printEnableResult(out, res)
	default:
		return fmt.Errorf("unsupported output format %q (use json or yaml)", outputFormat)
	}
}

func printEnableResult(out io.Writer, res enableResult) error {
	fmt.Fprintln(out, "GitOps enabled.")
	t := output.NewTable(out, "FIELD", "VALUE")
	t.AddRow("Provider", res.Provider)
	t.AddRow("Repository", res.RepositoryURL)
	if res.Path != "" {
		t.AddRow("Path", res.Path)
	}
	if res.Version != "" {
		t.AddRow("Version", res.Version)
	}
	return t.Flush()
}
