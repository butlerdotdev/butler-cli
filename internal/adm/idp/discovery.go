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

package idp

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/butlerdotdev/butler/internal/common/log"
	"github.com/butlerdotdev/butler/internal/common/output"
	"github.com/butlerdotdev/butler/internal/common/serverhttp"
	"github.com/spf13/cobra"
)

// testDiscoveryRequest mirrors butler-server's TestDiscoveryRequest
// (POST /api/admin/identity-providers/test).
type testDiscoveryRequest struct {
	IssuerURL string `json:"issuerURL"`
}

// testDiscoveryResponse mirrors butler-server's TestDiscoveryResponse. Both
// the test (issuer URL) and validate (existing IdP) endpoints return it. The
// underlying probe is a read-only OIDC discovery fetch, so neither verb
// mutates anything.
type testDiscoveryResponse struct {
	Valid                 bool   `json:"valid"`
	Message               string `json:"message"`
	AuthorizationEndpoint string `json:"authorizationEndpoint,omitempty"`
	TokenEndpoint         string `json:"tokenEndpoint,omitempty"`
	UserInfoEndpoint      string `json:"userInfoEndpoint,omitempty"`
	JWKSURI               string `json:"jwksURI,omitempty"`
}

// --- test ---

func newTestCmd(_ *log.Logger) *cobra.Command {
	var (
		issuerURL    string
		outputFormat string
	)

	cmd := &cobra.Command{
		Use:   "test",
		Short: "Test OIDC discovery for an issuer URL",
		Long: `Probe an OIDC issuer's discovery document before creating an identity
provider. The server fetches the issuer's .well-known/openid-configuration and
reports whether discovery succeeds and which endpoints it found.

This is a read-only probe: it does not create or change any resource.

Examples:
  butleradm idp test --issuer-url https://accounts.google.com
  butleradm idp test --issuer-url https://example.okta.com -o json`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTest(cmd.Context(), os.Stdout, issuerURL, outputFormat)
		},
	}

	cmd.Flags().StringVar(&issuerURL, "issuer-url", "", "OIDC issuer URL to probe (required)")
	cmd.Flags().StringVarP(&outputFormat, "output", "o", "", "output format (table, json, yaml)")
	_ = cmd.MarkFlagRequired("issuer-url")

	return cmd
}

func runTest(ctx context.Context, out io.Writer, issuerURL, outputFormat string) error {
	sh, err := serverhttp.New()
	if err != nil {
		return err
	}
	var res testDiscoveryResponse
	if err := sh.Post(ctx, "/api/admin/identity-providers/test", testDiscoveryRequest{IssuerURL: issuerURL}, &res); err != nil {
		return translateDiscoveryError(err)
	}
	return renderDiscovery(out, res, outputFormat)
}

// --- validate ---

func newValidateCmd(_ *log.Logger) *cobra.Command {
	var outputFormat string

	cmd := &cobra.Command{
		Use:   "validate NAME",
		Short: "Test OIDC discovery for an existing identity provider",
		Long: `Probe the OIDC discovery document of an existing identity provider, using
the issuer URL already configured on it. Reports whether discovery succeeds and
which endpoints it found.

This is a read-only probe: it does not change the identity provider.

Examples:
  butleradm idp validate google-workspace
  butleradm idp validate okta -o json`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runValidate(cmd.Context(), os.Stdout, args[0], outputFormat)
		},
	}

	cmd.Flags().StringVarP(&outputFormat, "output", "o", "", "output format (table, json, yaml)")

	return cmd
}

func runValidate(ctx context.Context, out io.Writer, name, outputFormat string) error {
	sh, err := serverhttp.New()
	if err != nil {
		return err
	}
	path := fmt.Sprintf("/api/admin/identity-providers/%s/validate", name)
	var res testDiscoveryResponse
	if err := sh.Post(ctx, path, struct{}{}, &res); err != nil {
		return translateDiscoveryError(err)
	}
	return renderDiscovery(out, res, outputFormat)
}

// --- shared rendering + error translation ---

func renderDiscovery(out io.Writer, res testDiscoveryResponse, outputFormat string) error {
	switch outputFormat {
	case "json":
		return output.PrintJSON(out, res)
	case "yaml":
		return output.PrintYAML(out, res)
	case "", "table":
		return printDiscovery(out, res)
	default:
		return fmt.Errorf("unsupported output format %q (use table, json or yaml)", outputFormat)
	}
}

func printDiscovery(out io.Writer, res testDiscoveryResponse) error {
	status := "INVALID"
	if res.Valid {
		status = "VALID"
	}
	fmt.Fprintf(out, "Discovery: %s\n", status)
	if res.Message != "" {
		fmt.Fprintf(out, "Message: %s\n", res.Message)
	}
	if !res.Valid {
		return nil
	}
	t := output.NewTable(out, "ENDPOINT", "URL")
	if res.AuthorizationEndpoint != "" {
		t.AddRow("authorization", res.AuthorizationEndpoint)
	}
	if res.TokenEndpoint != "" {
		t.AddRow("token", res.TokenEndpoint)
	}
	if res.UserInfoEndpoint != "" {
		t.AddRow("userinfo", res.UserInfoEndpoint)
	}
	if res.JWKSURI != "" {
		t.AddRow("jwks", res.JWKSURI)
	}
	return t.Flush()
}

func translateDiscoveryError(err error) error {
	if errors.Is(err, serverhttp.ErrSessionExpired) {
		fmt.Fprintln(os.Stderr, "Butler session expired. Run 'butleradm login' to re-authenticate.")
		return err
	}
	var se *serverhttp.ServerError
	if errors.As(err, &se) {
		switch {
		case se.IsForbidden():
			return fmt.Errorf("forbidden (identity provider operations require platform admin): %s", se.Message)
		case se.IsNotFound():
			return fmt.Errorf("not found: %s", se.Message)
		case se.IsBadRequest():
			return fmt.Errorf("invalid request: %s", se.Message)
		}
	}
	return err
}
