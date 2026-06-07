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

// gitopsInventoryTimeout covers discover/preview, which walk the cluster's
// Helm and native inventory server-side and can exceed the default timeout.
const gitopsInventoryTimeout = 2 * time.Minute

// discoveredRelease mirrors the relevant fields of butler-server's
// gitops.DiscoveredRelease (the full struct carries more; the CLI surfaces
// the identity and status).
type discoveredRelease struct {
	Name         string `json:"name"`
	Namespace    string `json:"namespace"`
	Chart        string `json:"chart"`
	ChartVersion string `json:"chartVersion"`
	Status       string `json:"status"`
}

// gitopsEngineStatus mirrors butler-server's gitops.GitOpsEngineStatus.
type gitopsEngineStatus struct {
	Provider  string `json:"provider,omitempty"`
	Installed bool   `json:"installed"`
	Ready     bool   `json:"ready"`
	Version   string `json:"version,omitempty"`
}

// discoveryResult mirrors butler-server's gitops.DiscoveryResult.
type discoveryResult struct {
	Matched      []discoveredRelease `json:"matched"`
	Unmatched    []discoveredRelease `json:"unmatched"`
	GitOpsEngine *gitopsEngineStatus `json:"gitopsEngine,omitempty"`
}

// newGitopsDiscoverCmd creates `cluster gitops discover`.
func newGitopsDiscoverCmd(_ *log.Logger) *cobra.Command {
	return &cobra.Command{
		Use:   "discover NAME",
		Short: "Discover Helm releases and GitOps engine state on a cluster",
		Long: `Discover the Helm releases running on a tenant cluster and the state of its
GitOps engine.

Releases are reported as matched (recognized as Butler addons) or unmatched
(everything else). Useful before an export to see what would be captured.

Exit codes:
  0  discovery completed
  1  client-side error or server error

Examples:
  butlerctl cluster gitops discover my-cluster
  butlerctl cluster gitops discover my-cluster -o json`,
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeClusterNames,
		RunE: func(cmd *cobra.Command, args []string) error {
			ns, _ := cmd.Flags().GetString("namespace")
			outputFormat, _ := cmd.Flags().GetString("output")
			return runGitopsDiscover(cmd.Context(), os.Stdout, args[0], ns, outputFormat)
		},
	}
}

func runGitopsDiscover(ctx context.Context, out io.Writer, name, namespace, outputFormat string) error {
	sh, err := serverhttp.NewWithTimeout(gitopsInventoryTimeout)
	if err != nil {
		return err
	}

	var res discoveryResult
	path := fmt.Sprintf("/api/clusters/%s/%s/gitops/discover", namespace, name)
	if err := sh.Get(ctx, path, &res); err != nil {
		return translateGitopsError(err)
	}

	switch outputFormat {
	case "json":
		return output.PrintJSON(out, res)
	case "yaml":
		return output.PrintYAML(out, res)
	case "", "table":
		return printDiscovery(out, res)
	default:
		return fmt.Errorf("unsupported output format %q (use json or yaml)", outputFormat)
	}
}

func printDiscovery(out io.Writer, res discoveryResult) error {
	if res.GitOpsEngine != nil {
		e := res.GitOpsEngine
		fmt.Fprintf(out, "GitOps engine: provider=%s installed=%t ready=%t", orDefault(e.Provider, "none"), e.Installed, e.Ready)
		if e.Version != "" {
			fmt.Fprintf(out, " version=%s", e.Version)
		}
		fmt.Fprintln(out)
	}

	printReleaseSection(out, "Matched (Butler addons)", res.Matched)
	printReleaseSection(out, "Unmatched", res.Unmatched)
	return nil
}

func printReleaseSection(out io.Writer, title string, releases []discoveredRelease) {
	fmt.Fprintf(out, "\n%s: %d\n", title, len(releases))
	if len(releases) == 0 {
		return
	}
	t := output.NewTable(out, "NAME", "NAMESPACE", "CHART", "VERSION", "STATUS")
	for _, r := range releases {
		t.AddRow(r.Name, r.Namespace, r.Chart, r.ChartVersion, r.Status)
	}
	_ = t.Flush()
}
