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
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"

	"github.com/butlerdotdev/butler/internal/common/log"
	"github.com/butlerdotdev/butler/internal/common/output"
	"github.com/butlerdotdev/butler/internal/common/serverhttp"
	"github.com/spf13/cobra"
)

// previewRequest mirrors butler-server's previewClusterRequest
// (POST /api/clusters/{ns}/{name}/gitops/preview-cluster). Both fields optional.
type previewRequest struct {
	Env         string `json:"env,omitempty"`
	ClusterName string `json:"clusterName,omitempty"`
}

// previewSummary mirrors the surfaced fields of PreviewClusterSummary.
type previewSummary struct {
	FileCount  int `json:"fileCount"`
	Collisions int `json:"collisions"`
	Failures   int `json:"failures"`
}

// previewResult mirrors butler-server's PreviewClusterResponse. coverage is
// kept as raw JSON so -o json round-trips the full report.
type previewResult struct {
	ClusterName string            `json:"clusterName"`
	Files       map[string]string `json:"files"`
	Summary     previewSummary    `json:"summary"`
	Coverage    json.RawMessage   `json:"coverage,omitempty"`
}

type previewOptions struct {
	env         string
	clusterName string
}

// newGitopsPreviewCmd creates `cluster gitops preview`.
func newGitopsPreviewCmd(_ *log.Logger) *cobra.Command {
	opts := &previewOptions{}

	cmd := &cobra.Command{
		Use:   "preview NAME",
		Short: "Preview a cluster-wide GitOps export (dry-run, no git writes)",
		Long: `Preview what a cluster-wide GitOps export would produce.

butler-server discovers the cluster inventory and renders the GitOps layout
without writing to git. Use this before 'cluster gitops export' to see the
file set, coverage, and any collisions.

Exit codes:
  0  preview generated
  1  client-side error or server error

Examples:
  butlerctl cluster gitops preview my-cluster
  butlerctl cluster gitops preview my-cluster --env prd -o json`,
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeClusterNames,
		RunE: func(cmd *cobra.Command, args []string) error {
			ns, _ := cmd.Flags().GetString("namespace")
			outputFormat, _ := cmd.Flags().GetString("output")
			return runGitopsPreview(cmd.Context(), os.Stdout, args[0], ns, outputFormat, opts)
		},
	}

	cmd.Flags().StringVar(&opts.env, "env", "", "apps overlay name (defaults server-side to prd)")
	cmd.Flags().StringVar(&opts.clusterName, "cluster-name", "", "directory name emitted under clusters/ (defaults server-side)")

	return cmd
}

func runGitopsPreview(ctx context.Context, out io.Writer, name, namespace, outputFormat string, opts *previewOptions) error {
	sh, err := serverhttp.NewWithTimeout(gitopsInventoryTimeout)
	if err != nil {
		return err
	}

	req := previewRequest{Env: opts.env, ClusterName: opts.clusterName}
	var res previewResult
	path := fmt.Sprintf("/api/clusters/%s/%s/gitops/preview-cluster", namespace, name)
	if err := sh.Post(ctx, path, req, &res); err != nil {
		return translateGitopsError(err)
	}

	switch outputFormat {
	case "json":
		return output.PrintJSON(out, res)
	case "yaml":
		return output.PrintYAML(out, res)
	case "", "table":
		return printPreview(out, res)
	default:
		return fmt.Errorf("unsupported output format %q (use json or yaml)", outputFormat)
	}
}

func printPreview(out io.Writer, res previewResult) error {
	fmt.Fprintf(out, "Cluster directory: %s\n", res.ClusterName)
	fmt.Fprintf(out, "Files: %d   Collisions: %d   Failures: %d\n", res.Summary.FileCount, res.Summary.Collisions, res.Summary.Failures)

	if len(res.Files) == 0 {
		return nil
	}
	paths := make([]string, 0, len(res.Files))
	for p := range res.Files {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	fmt.Fprintln(out, "\nFiles that would be written:")
	for _, p := range paths {
		fmt.Fprintf(out, "  %s\n", p)
	}
	return nil
}
