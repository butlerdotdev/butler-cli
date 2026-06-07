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
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/butlerdotdev/butler/internal/common/log"
	"github.com/butlerdotdev/butler/internal/common/serverhttp"
	"github.com/spf13/cobra"
)

type configClearOptions struct {
	yes     bool
	confirm string
}

// newConfigClearCmd creates `gitops config clear`.
func newConfigClearCmd(_ *log.Logger) *cobra.Command {
	opts := &configClearOptions{}

	cmd := &cobra.Command{
		Use:   "clear",
		Args:  cobra.NoArgs,
		Short: "Remove the platform Git provider configuration",
		Long: `Remove the platform Git provider configuration and its stored credential.

This is destructive and irreversible: the credential is deleted, and any
cluster relying on this provider for GitOps operations will be affected. A
typed confirmation is required. Interactively you are prompted to type the
configured organization (or the word "confirm" when no organization is set).
Non-interactively, pass the same value via --confirm together with --yes.

Clearing when nothing is configured is not an error (exit 0).

Exit codes:
  0  configuration cleared, or nothing was configured
  1  client-side error, confirmation mismatch, or unexpected server error

Examples:
  # Interactive (prompts for the typed confirmation)
  butleradm gitops config clear

  # Non-interactive
  butleradm gitops config clear --yes --confirm butlerdotdev`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runConfigClear(cmd.Context(), os.Stdin, os.Stdout, opts)
		},
	}

	cmd.Flags().BoolVar(&opts.yes, "yes", false, "skip the interactive confirmation prompt (requires --confirm)")
	cmd.Flags().StringVar(&opts.confirm, "confirm", "", "typed confirmation value for non-interactive use with --yes")

	return cmd
}

// clearConfirmTarget is the value the operator must type to confirm a clear:
// the configured organization when present, otherwise the literal "confirm".
func clearConfirmTarget(cfg gitProviderConfig) string {
	if cfg.Organization != "" {
		return cfg.Organization
	}
	return "confirm"
}

// confirmClear prompts for and reads the typed confirmation value.
func confirmClear(target string, in io.Reader, out io.Writer) error {
	fmt.Fprintln(out, "This removes the platform Git provider configuration and its stored credential.")
	fmt.Fprintln(out, "Any cluster relying on it for GitOps operations will be affected.")
	fmt.Fprintf(out, "Type %q to confirm: ", target)

	line, _ := bufio.NewReader(in).ReadString('\n')
	if strings.TrimSpace(line) != target {
		return fmt.Errorf("confirmation did not match %q; aborted", target)
	}
	return nil
}

// runConfigClear fetches the current config to derive the confirmation target,
// gates on the typed confirmation, then deletes the provider configuration. A
// 404 (nothing configured) is mapped to success.
func runConfigClear(ctx context.Context, in io.Reader, out io.Writer, opts *configClearOptions) error {
	sh, err := serverhttp.New()
	if err != nil {
		return err
	}

	var cfg gitProviderConfig
	if err := sh.Get(ctx, "/api/gitops/config", &cfg); err != nil {
		return translateServerError(err)
	}
	target := clearConfirmTarget(cfg)

	if opts.yes {
		if opts.confirm != target {
			return fmt.Errorf("clearing the Git provider requires --confirm=%q to proceed with --yes", target)
		}
	} else if err := confirmClear(target, in, out); err != nil {
		return err
	}

	if err := sh.Delete(ctx, "/api/gitops/config", nil); err != nil {
		var se *serverhttp.ServerError
		if errors.As(err, &se) && se.IsNotFound() {
			fmt.Fprintln(out, "No Git provider configured; nothing to clear.")
			return nil
		}
		return translateServerError(err)
	}

	fmt.Fprintln(out, "Git provider configuration cleared.")
	return nil
}
