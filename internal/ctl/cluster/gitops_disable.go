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
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/butlerdotdev/butler/internal/common/log"
	"github.com/butlerdotdev/butler/internal/common/output"
	"github.com/butlerdotdev/butler/internal/common/serverhttp"
	"github.com/spf13/cobra"
)

// disableResult mirrors butler-server's gitops.DisableGitOpsResponse.
type disableResult struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

type disableOptions struct {
	yes     bool
	confirm string
}

// newGitopsDisableCmd creates `cluster gitops disable`.
func newGitopsDisableCmd(_ *log.Logger) *cobra.Command {
	opts := &disableOptions{}

	cmd := &cobra.Command{
		Use:   "disable NAME",
		Short: "Disable GitOps on a tenant cluster",
		Long: `Disable GitOps on a tenant cluster.

This removes the GitOps engine (Flux) from the cluster. It is destructive and
requires a typed confirmation: type the cluster name interactively, or pass
--confirm <name> together with --yes for non-interactive use.

Disabling when GitOps is not enabled is not an error (exit 0).

Exit codes:
  0  GitOps disabled, or it was not enabled
  1  client-side error, confirmation mismatch, or unexpected server error

Examples:
  butlerctl cluster gitops disable my-cluster
  butlerctl cluster gitops disable my-cluster --yes --confirm my-cluster`,
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeClusterNames,
		RunE: func(cmd *cobra.Command, args []string) error {
			ns, _ := cmd.Flags().GetString("namespace")
			outputFormat, _ := cmd.Flags().GetString("output")
			return runGitopsDisable(cmd.Context(), os.Stdin, os.Stdout, args[0], ns, outputFormat, opts)
		},
	}

	cmd.Flags().BoolVar(&opts.yes, "yes", false, "skip the interactive confirmation prompt (requires --confirm)")
	cmd.Flags().StringVar(&opts.confirm, "confirm", "", "typed confirmation value (the cluster name) for non-interactive use with --yes")

	return cmd
}

// confirmGitopsDisable prompts for and reads the typed cluster-name confirmation.
func confirmGitopsDisable(name string, in io.Reader, out io.Writer) error {
	fmt.Fprintf(out, "This removes the GitOps engine (Flux) from cluster %s.\n", name)
	fmt.Fprintf(out, "Type %q to confirm: ", name)

	line, _ := bufio.NewReader(in).ReadString('\n')
	if strings.TrimSpace(line) != name {
		return fmt.Errorf("confirmation did not match %q; aborted", name)
	}
	return nil
}

func runGitopsDisable(ctx context.Context, in io.Reader, out io.Writer, name, namespace, outputFormat string, opts *disableOptions) error {
	if opts.yes {
		if opts.confirm != name {
			return fmt.Errorf("disabling GitOps requires --confirm=%q to proceed with --yes", name)
		}
	} else if err := confirmGitopsDisable(name, in, out); err != nil {
		return err
	}

	sh, err := serverhttp.New()
	if err != nil {
		return err
	}

	var res disableResult
	path := fmt.Sprintf("/api/clusters/%s/%s/gitops", namespace, name)
	if err := sh.Delete(ctx, path, &res); err != nil {
		var se *serverhttp.ServerError
		if errors.As(err, &se) && se.IsNotFound() {
			fmt.Fprintf(out, "GitOps is not enabled on %s; nothing to disable.\n", name)
			return nil
		}
		return translateGitopsError(err)
	}

	switch outputFormat {
	case "json":
		return output.PrintJSON(out, res)
	case "yaml":
		return output.PrintYAML(out, res)
	default:
		fmt.Fprintf(out, "GitOps disabled on %s.\n", name)
		return nil
	}
}
