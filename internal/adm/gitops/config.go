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

// gitProviderConfig mirrors butler-server's gitops.GitProviderConfigResponse
// JSON shape (GET /api/gitops/config). We do not import butler-server types;
// the HTTP contract is the boundary, matching the cert-rotate command's
// mirror of RotationEvent.
type gitProviderConfig struct {
	Configured   bool   `json:"configured"`
	Type         string `json:"type,omitempty"`
	URL          string `json:"url,omitempty"`
	Organization string `json:"organization,omitempty"`
	Username     string `json:"username,omitempty"`
}

// newConfigCmd creates the `gitops config` parent command.
func newConfigCmd(logger *log.Logger) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Args:  cobra.NoArgs,
		RunE:  output.ShowHelp,
		Short: "Manage the platform Git provider configuration",
		Long: `Manage the Git provider used for GitOps operations.

The Git provider is stored platform-wide on the management cluster (a
credential Secret plus a ConfigMap) and is a prerequisite for enabling
GitOps on tenant or management clusters.

Examples:
  butleradm gitops config get
  butleradm gitops config get -o yaml`,
	}

	cmd.AddCommand(newConfigGetCmd(logger))
	cmd.AddCommand(newConfigSetCmd(logger))
	cmd.AddCommand(newConfigClearCmd(logger))

	return cmd
}

// saveGitProviderRequest mirrors butler-server's gitops.SaveGitProviderRequest
// (POST /api/gitops/config). The token is sent in the body and never logged.
type saveGitProviderRequest struct {
	Type         string `json:"type"`
	Token        string `json:"token"`
	URL          string `json:"url,omitempty"`
	Organization string `json:"organization,omitempty"`
}

type configSetOptions struct {
	providerType  string
	url           string
	organization  string
	tokenFromFile string
	tokenFromEnv  string
	outputFormat  string
}

// newConfigSetCmd creates `gitops config set`.
func newConfigSetCmd(_ *log.Logger) *cobra.Command {
	opts := &configSetOptions{}

	cmd := &cobra.Command{
		Use:   "set",
		Args:  cobra.NoArgs,
		Short: "Configure the platform Git provider",
		Long: `Configure the Git provider used for GitOps operations.

The token is read from a file (--token-from-file) or an environment variable
(--token-from-env); it is never accepted directly on the command line, to keep
it out of shell history. Exactly one token source is required. butler-server
validates the token before storing it as a Secret on the management cluster.

Exit codes:
  0  provider configured
  1  client-side validation error or server error (including a rejected token)

Examples:
  # Token from a file (recommended; keep the file at mode 0600)
  butleradm gitops config set --type github --organization butlerdotdev \
    --token-from-file ~/.config/gh-token

  # Token from an environment variable
  butleradm gitops config set --type gitlab --url https://gitlab.example.com \
    --token-from-env GITLAB_TOKEN`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runConfigSet(cmd.Context(), os.Stdout, opts)
		},
	}

	cmd.Flags().StringVar(&opts.providerType, "type", "", "git provider type: github, gitlab, or bitbucket (required)")
	cmd.Flags().StringVar(&opts.url, "url", "", "provider API URL (defaults per type when omitted)")
	cmd.Flags().StringVar(&opts.organization, "organization", "", "organization or group")
	cmd.Flags().StringVar(&opts.tokenFromFile, "token-from-file", "", "read the token from this file")
	cmd.Flags().StringVar(&opts.tokenFromEnv, "token-from-env", "", "read the token from this environment variable")
	cmd.Flags().StringVarP(&opts.outputFormat, "output", "o", "", "output format (json, yaml)")
	_ = cmd.MarkFlagRequired("type")

	return cmd
}

// validateConfigSet enforces client-side gates before any token read or
// network call: a known provider type, and exactly one token source.
func validateConfigSet(opts *configSetOptions) error {
	switch opts.providerType {
	case "github", "gitlab", "bitbucket":
	case "":
		return fmt.Errorf("--type is required (github, gitlab, or bitbucket)")
	default:
		return fmt.Errorf("invalid --type %q (use github, gitlab, or bitbucket)", opts.providerType)
	}

	switch {
	case opts.tokenFromFile != "" && opts.tokenFromEnv != "":
		return fmt.Errorf("specify only one of --token-from-file or --token-from-env")
	case opts.tokenFromFile == "" && opts.tokenFromEnv == "":
		return fmt.Errorf("a token source is required: --token-from-file or --token-from-env")
	}

	return nil
}

// resolveToken reads the token from the configured source. It never echoes or
// logs the token value.
func resolveToken(opts *configSetOptions) (string, error) {
	if opts.tokenFromEnv != "" {
		tok := os.Getenv(opts.tokenFromEnv)
		if tok == "" {
			return "", fmt.Errorf("environment variable %q is empty or unset", opts.tokenFromEnv)
		}
		return tok, nil
	}

	info, err := os.Stat(opts.tokenFromFile)
	if err != nil {
		return "", fmt.Errorf("reading token file: %w", err)
	}
	if info.Mode().Perm()&0o077 != 0 {
		fmt.Fprintf(os.Stderr, "Warning: token file %s is group/world-readable (%#o); tighten it to 0600.\n", opts.tokenFromFile, info.Mode().Perm())
	}
	data, err := os.ReadFile(opts.tokenFromFile)
	if err != nil {
		return "", fmt.Errorf("reading token file: %w", err)
	}
	tok := strings.TrimSpace(string(data))
	if tok == "" {
		return "", fmt.Errorf("token file %q is empty", opts.tokenFromFile)
	}
	return tok, nil
}

// runConfigSet validates input, resolves the token, and POSTs the provider
// configuration to butler-server, which validates the token before storing it.
func runConfigSet(ctx context.Context, out io.Writer, opts *configSetOptions) error {
	if err := validateConfigSet(opts); err != nil {
		return err
	}

	token, err := resolveToken(opts)
	if err != nil {
		return err
	}

	sh, err := serverhttp.New()
	if err != nil {
		return err
	}

	req := saveGitProviderRequest{
		Type:         opts.providerType,
		Token:        token,
		URL:          opts.url,
		Organization: opts.organization,
	}

	var cfg gitProviderConfig
	if err := sh.Post(ctx, "/api/gitops/config", req, &cfg); err != nil {
		return translateServerError(err)
	}

	switch opts.outputFormat {
	case "json":
		return output.PrintJSON(out, cfg)
	case "yaml":
		return output.PrintYAML(out, cfg)
	case "", "table":
		fmt.Fprintln(out, "Git provider configured.")
		return printConfigTable(out, cfg)
	default:
		return fmt.Errorf("unsupported output format %q (use json or yaml)", opts.outputFormat)
	}
}

// newConfigGetCmd creates `gitops config get`.
func newConfigGetCmd(_ *log.Logger) *cobra.Command {
	var outputFormat string

	cmd := &cobra.Command{
		Use:   "get",
		Args:  cobra.NoArgs,
		Short: "Show the configured Git provider",
		Long: `Show the platform Git provider configuration.

Reads GET /api/gitops/config from butler-server and reports whether a
provider is configured, its type, URL, organization, and the authenticated
username (when the stored token validates). The token is never returned.

Exit codes:
  0  configuration retrieved (whether or not a provider is configured)
  1  client-side error or unexpected server error

Examples:
  # Human-readable summary
  butleradm gitops config get

  # Machine-readable
  butleradm gitops config get -o json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runConfigGet(cmd.Context(), os.Stdout, outputFormat)
		},
	}

	cmd.Flags().StringVarP(&outputFormat, "output", "o", "", "output format (json, yaml)")

	return cmd
}

// runConfigGet fetches the Git provider config and renders it to out.
func runConfigGet(ctx context.Context, out io.Writer, outputFormat string) error {
	sh, err := serverhttp.New()
	if err != nil {
		return err
	}

	var cfg gitProviderConfig
	if err := sh.Get(ctx, "/api/gitops/config", &cfg); err != nil {
		return translateServerError(err)
	}

	switch outputFormat {
	case "json":
		return output.PrintJSON(out, cfg)
	case "yaml":
		return output.PrintYAML(out, cfg)
	case "", "table":
		return printConfigTable(out, cfg)
	default:
		return fmt.Errorf("unsupported output format %q (use json or yaml)", outputFormat)
	}
}

// printConfigTable renders the config as a human-readable field/value table.
func printConfigTable(out io.Writer, cfg gitProviderConfig) error {
	if !cfg.Configured {
		fmt.Fprintln(out, "No Git provider configured. Configure one with:")
		fmt.Fprintln(out, "  butleradm gitops config set --type github|gitlab|bitbucket --url <url> --organization <org> --token-from-file <path>")
		return nil
	}

	t := output.NewTable(out, "FIELD", "VALUE")
	t.AddRow("Type", cfg.Type)
	t.AddRow("URL", cfg.URL)
	if cfg.Organization != "" {
		t.AddRow("Organization", cfg.Organization)
	}
	if cfg.Username != "" {
		t.AddRow("Username", cfg.Username)
	}
	return t.Flush()
}

// translateServerError maps butler-server responses to actionable text so the
// operator sees guidance rather than raw status codes. It mirrors the
// cert-rotate command's translateServerError (the Feature 2 consistency
// contract). Pattern-match on the serverhttp.ServerError accessors; never
// string-match the message.
func translateServerError(err error) error {
	if errors.Is(err, serverhttp.ErrSessionExpired) {
		fmt.Fprintln(os.Stderr, "Butler session expired. Run 'butleradm login' to re-authenticate.")
		return err
	}
	var se *serverhttp.ServerError
	if errors.As(err, &se) {
		switch {
		case se.IsForbidden():
			return fmt.Errorf("forbidden: %s", se.Message)
		case se.IsNotFound():
			return fmt.Errorf("not found: %s", se.Message)
		case se.IsBadRequest():
			return fmt.Errorf("invalid request: %s", se.Message)
		case se.IsConflict():
			return fmt.Errorf("conflict: %s", se.Message)
		}
	}
	return err
}
