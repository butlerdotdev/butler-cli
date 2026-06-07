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

package gitops

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/butlerdotdev/butler/internal/common/log"
	"github.com/butlerdotdev/butler/internal/common/output"
	"github.com/butlerdotdev/butler/internal/common/serverhttp"
	"github.com/spf13/cobra"
)

// repository mirrors butler-server's gitops.Repository
// (GET /api/gitops/repos). The HTTP contract is the boundary.
type repository struct {
	Name          string `json:"name"`
	FullName      string `json:"fullName"`
	Description   string `json:"description,omitempty"`
	DefaultBranch string `json:"defaultBranch"`
	Private       bool   `json:"private"`
	CloneURL      string `json:"cloneUrl"`
	SSHURL        string `json:"sshUrl"`
	HTMLURL       string `json:"htmlUrl"`
	UpdatedAt     string `json:"updatedAt,omitempty"`
}

// newRepositoriesCmd creates the `gitops repositories` parent command.
func newRepositoriesCmd(logger *log.Logger) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "repositories",
		Aliases: []string{"repos"},
		Args:    cobra.NoArgs,
		RunE:    output.ShowHelp,
		Short:   "List repositories the Git provider can access",
		Long: `List repositories available to the configured Git provider.

Requires a Git provider configured via 'butleradm gitops config set'.

Examples:
  butleradm gitops repositories list
  butleradm gitops repos list -o json`,
	}

	cmd.AddCommand(newReposListCmd(logger))

	return cmd
}

// newReposListCmd creates `gitops repositories list`.
func newReposListCmd(_ *log.Logger) *cobra.Command {
	var outputFormat string

	cmd := &cobra.Command{
		Use:   "list",
		Args:  cobra.NoArgs,
		Short: "List repositories",
		Long: `List repositories the configured Git provider credential can access.

Exit codes:
  0  repositories listed
  1  client-side error or server error (including no provider configured)

Examples:
  butleradm gitops repositories list
  butleradm gitops repositories list -o yaml`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runReposList(cmd.Context(), os.Stdout, outputFormat)
		},
	}

	cmd.Flags().StringVarP(&outputFormat, "output", "o", "", "output format (json, yaml)")

	return cmd
}

func runReposList(ctx context.Context, out io.Writer, outputFormat string) error {
	sh, err := serverhttp.New()
	if err != nil {
		return err
	}

	var repos []repository
	if err := sh.Get(ctx, "/api/gitops/repos", &repos); err != nil {
		return translateServerError(err)
	}

	switch outputFormat {
	case "json":
		return output.PrintJSON(out, repos)
	case "yaml":
		return output.PrintYAML(out, repos)
	case "", "table":
		return printReposTable(out, repos)
	default:
		return fmt.Errorf("unsupported output format %q (use json or yaml)", outputFormat)
	}
}

func printReposTable(out io.Writer, repos []repository) error {
	if len(repos) == 0 {
		fmt.Fprintln(out, "No repositories found for the configured Git provider.")
		return nil
	}

	t := output.NewTable(out, "NAME", "FULL NAME", "DEFAULT BRANCH", "PRIVATE")
	for _, r := range repos {
		t.AddRow(r.Name, r.FullName, r.DefaultBranch, fmt.Sprintf("%t", r.Private))
	}
	return t.Flush()
}
