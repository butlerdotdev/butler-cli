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
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/butlerdotdev/butler/internal/common/log"
	"github.com/butlerdotdev/butler/internal/common/output"
	"github.com/butlerdotdev/butler/internal/common/serverhttp"
	"github.com/spf13/cobra"
)

// gitOpsStatus mirrors butler-server's gitops.GitOpsStatusResponse JSON shape
// (GET /api/clusters/{ns}/{name}/gitops/status). We do not import butler-server
// types; the HTTP contract is the boundary. providerStatus is kept as raw JSON
// so -o json round-trips the nested object without modeling its internals.
type gitOpsStatus struct {
	Enabled        bool            `json:"enabled"`
	Provider       string          `json:"provider,omitempty"`
	Repository     string          `json:"repository,omitempty"`
	Branch         string          `json:"branch,omitempty"`
	Path           string          `json:"path,omitempty"`
	Status         string          `json:"status,omitempty"`
	Version        string          `json:"version,omitempty"`
	ProviderStatus json.RawMessage `json:"providerStatus,omitempty"`
}

// newGitopsCmd creates the `cluster gitops` command group. The bare-noun form
// (`cluster gitops NAME`) defaults to the status subcommand so the historical
// invocation keeps working, now showing live status from butler-server rather
// than the declared spec.
func newGitopsCmd(logger *log.Logger) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "gitops [NAME]",
		Short: "Manage GitOps lifecycle for a tenant cluster",
		Long: `Manage the GitOps lifecycle for a tenant cluster.

Subcommands cover the full lifecycle: status, enable, disable, discover,
preview, and export. These are server-orchestrated: the CLI calls butler-server
over HTTP using the active login credential, and butler-server drives the
tenant cluster.

Running ` + "`cluster gitops NAME`" + ` with no subcommand is equivalent to
` + "`cluster gitops status NAME`" + `.

Examples:
  butlerctl cluster gitops status my-cluster
  butlerctl cluster gitops my-cluster
  butlerctl cluster gitops enable my-cluster --repo https://github.com/acme/clusters`,
		Args:              cobra.MaximumNArgs(1),
		ValidArgsFunction: completeClusterNames,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return cmd.Help()
			}
			ns, _ := cmd.Flags().GetString("namespace")
			outputFormat, _ := cmd.Flags().GetString("output")
			return runGitopsStatus(cmd.Context(), os.Stdout, args[0], ns, outputFormat)
		},
	}

	// Shared across the bare-noun form and every subcommand.
	cmd.PersistentFlags().StringP("namespace", "n", DefaultTenantNamespace, "namespace of the TenantCluster")
	cmd.PersistentFlags().StringP("output", "o", "", "output format (json, yaml)")

	cmd.AddCommand(newGitopsStatusCmd(logger))
	cmd.AddCommand(newGitopsEnableCmd(logger))
	cmd.AddCommand(newGitopsDisableCmd(logger))
	cmd.AddCommand(newGitopsDiscoverCmd(logger))
	cmd.AddCommand(newGitopsPreviewCmd(logger))
	cmd.AddCommand(newGitopsExportCmd(logger))

	return cmd
}

// newGitopsStatusCmd creates `cluster gitops status`.
func newGitopsStatusCmd(_ *log.Logger) *cobra.Command {
	return &cobra.Command{
		Use:   "status NAME",
		Short: "Show live GitOps status for a tenant cluster",
		Long: `Show the live GitOps status for a tenant cluster.

Queries butler-server for the cluster's current GitOps state: whether GitOps
is enabled, the provider, repository, branch, path, and sync status. This
reflects the live Flux/Argo state, not only the declared spec.

Exit codes:
  0  status retrieved
  1  client-side error or unexpected server error

Examples:
  butlerctl cluster gitops status my-cluster
  butlerctl cluster gitops status my-cluster -n team-payments -o json`,
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeClusterNames,
		RunE: func(cmd *cobra.Command, args []string) error {
			ns, _ := cmd.Flags().GetString("namespace")
			outputFormat, _ := cmd.Flags().GetString("output")
			return runGitopsStatus(cmd.Context(), os.Stdout, args[0], ns, outputFormat)
		},
	}
}

func runGitopsStatus(ctx context.Context, out io.Writer, name, namespace, outputFormat string) error {
	sh, err := serverhttp.New()
	if err != nil {
		return err
	}

	var st gitOpsStatus
	path := fmt.Sprintf("/api/clusters/%s/%s/gitops/status", namespace, name)
	if err := sh.Get(ctx, path, &st); err != nil {
		return translateGitopsError(err)
	}

	switch outputFormat {
	case "json":
		return output.PrintJSON(out, st)
	case "yaml":
		return output.PrintYAML(out, st)
	case "", "table":
		return printGitopsStatus(out, st)
	default:
		return fmt.Errorf("unsupported output format %q (use json or yaml)", outputFormat)
	}
}

// printGitopsStatus renders the status as a human-readable field/value table.
func printGitopsStatus(out io.Writer, st gitOpsStatus) error {
	if !st.Enabled {
		fmt.Fprintln(out, "GitOps is not enabled on this cluster.")
		fmt.Fprintln(out, "Enable it with: butlerctl cluster gitops enable <cluster> --repo <url>")
		return nil
	}

	t := output.NewTable(out, "FIELD", "VALUE")
	t.AddRow("Enabled", "true")
	t.AddRow("Provider", st.Provider)
	t.AddRow("Repository", st.Repository)
	t.AddRow("Branch", st.Branch)
	if st.Path != "" {
		t.AddRow("Path", st.Path)
	}
	if st.Status != "" {
		t.AddRow("Status", st.Status)
	}
	if st.Version != "" {
		t.AddRow("Version", st.Version)
	}
	return t.Flush()
}

// translateGitopsError maps butler-server responses to actionable text for the
// cluster gitops commands. Mirrors the cert-rotate command's translation but
// with GitOps-generic wording (the package already has a rotation-specific
// translateServerError). Per-verb special cases (such as disable mapping 404
// to success) are handled in the individual command before this is called.
func translateGitopsError(err error) error {
	if errors.Is(err, serverhttp.ErrSessionExpired) {
		fmt.Fprintln(os.Stderr, "Butler session expired. Run 'butlerctl login' to re-authenticate.")
		return err
	}
	var se *serverhttp.ServerError
	if errors.As(err, &se) {
		switch {
		case se.IsForbidden():
			return fmt.Errorf("forbidden: %s", se.Message)
		case se.IsNotFound():
			return fmt.Errorf("not found: %s", se.Message)
		case se.IsConflict():
			return fmt.Errorf("conflict: %s", se.Message)
		case se.IsBadRequest():
			return fmt.Errorf("invalid request: %s", se.Message)
		}
	}
	return err
}
