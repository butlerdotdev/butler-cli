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
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/butlerdotdev/butler/internal/common/auth"
	"github.com/butlerdotdev/butler/internal/common/serverhttp"
)

func TestTranslateServerError(t *testing.T) {
	tests := []struct {
		name       string
		in         error
		wantIsExp  bool   // expect errors.Is(result, ErrSessionExpired)
		wantSubstr string // expect result message to contain this (when not session-expired)
	}{
		{name: "session expired", in: serverhttp.ErrSessionExpired, wantIsExp: true},
		{name: "forbidden", in: &serverhttp.ServerError{StatusCode: http.StatusForbidden, Message: "insufficient role"}, wantSubstr: "forbidden: insufficient role"},
		{name: "not found", in: &serverhttp.ServerError{StatusCode: http.StatusNotFound, Message: "no config"}, wantSubstr: "not found: no config"},
		{name: "bad request", in: &serverhttp.ServerError{StatusCode: http.StatusBadRequest, Message: "bad token"}, wantSubstr: "invalid request: bad token"},
		{name: "conflict", in: &serverhttp.ServerError{StatusCode: http.StatusConflict, Message: "in progress"}, wantSubstr: "conflict: in progress"},
		{name: "passthrough", in: errors.New("connection refused"), wantSubstr: "connection refused"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := translateServerError(tt.in)
			if tt.wantIsExp {
				if !errors.Is(got, serverhttp.ErrSessionExpired) {
					t.Fatalf("expected ErrSessionExpired, got %v", got)
				}
				return
			}
			if got == nil || !strings.Contains(got.Error(), tt.wantSubstr) {
				t.Fatalf("expected error containing %q, got %v", tt.wantSubstr, got)
			}
		})
	}
}

func TestPrintConfigTable(t *testing.T) {
	t.Run("not configured", func(t *testing.T) {
		var buf bytes.Buffer
		if err := printConfigTable(&buf, gitProviderConfig{Configured: false}); err != nil {
			t.Fatalf("printConfigTable: %v", err)
		}
		if !strings.Contains(buf.String(), "No Git provider configured") {
			t.Fatalf("expected not-configured message, got %q", buf.String())
		}
	})

	t.Run("configured", func(t *testing.T) {
		var buf bytes.Buffer
		cfg := gitProviderConfig{
			Configured:   true,
			Type:         "github",
			URL:          "https://api.github.com",
			Organization: "butlerdotdev",
			Username:     "octocat",
		}
		if err := printConfigTable(&buf, cfg); err != nil {
			t.Fatalf("printConfigTable: %v", err)
		}
		out := buf.String()
		for _, want := range []string{"github", "https://api.github.com", "butlerdotdev", "octocat"} {
			if !strings.Contains(out, want) {
				t.Fatalf("expected output to contain %q, got %q", want, out)
			}
		}
	})
}

// startTestServer starts an httptest server with the given handler mux and
// writes an active credential under a temp HOME pointing at it, so
// serverhttp.New picks it up. Mirrors internal/common/serverhttp setupClient.
func startTestServer(t *testing.T, mux *http.ServeMux) {
	t.Helper()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	t.Setenv("HOME", t.TempDir())
	cf := &auth.CredentialFile{
		ActiveServer: srv.URL,
		Servers: map[string]*auth.ServerCredential{
			srv.URL: {
				User:             auth.UserInfo{Email: "admin@example.com", Name: "Admin"},
				SessionToken:     "test-session-token",
				SessionExpiresAt: time.Now().Add(time.Hour),
				ExpiresAt:        time.Now().Add(time.Hour),
				RefreshToken:     "test-refresh-token",
				RefreshExpiresAt: time.Now().Add(7 * 24 * time.Hour),
			},
		},
	}
	if err := cf.Save(); err != nil {
		t.Fatalf("save credentials: %v", err)
	}
}

// configGetMux serves the given JSON on GET /api/gitops/config.
func configGetMux(configJSON string) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/gitops/config", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(configJSON))
	})
	return mux
}

func TestRunConfigGet_RoundTrip(t *testing.T) {
	startTestServer(t, configGetMux(`{"configured":true,"type":"github","url":"https://api.github.com","organization":"butlerdotdev","username":"octocat"}`))

	var buf bytes.Buffer
	if err := runConfigGet(context.Background(), &buf, "json"); err != nil {
		t.Fatalf("runConfigGet: %v", err)
	}
	out := buf.String()
	for _, want := range []string{`"configured": true`, "github", "octocat"} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected JSON output to contain %q, got %q", want, out)
		}
	}
}

func TestRunConfigGet_UnsupportedFormat(t *testing.T) {
	// A valid server and credential exist, so the GET succeeds and execution
	// reaches the output-format switch. An unknown format must be rejected
	// there with a specific message.
	startTestServer(t, configGetMux(`{"configured":true,"type":"github"}`))

	var buf bytes.Buffer
	err := runConfigGet(context.Background(), &buf, "xml")
	if err == nil || !strings.Contains(err.Error(), "unsupported output format") {
		t.Fatalf("expected unsupported-format error, got %v", err)
	}
}

func TestValidateConfigSet(t *testing.T) {
	tests := []struct {
		name       string
		opts       configSetOptions
		wantErr    bool
		wantSubstr string
	}{
		{name: "valid github + file", opts: configSetOptions{providerType: "github", tokenFromFile: "/tmp/t"}},
		{name: "valid gitlab + env", opts: configSetOptions{providerType: "gitlab", tokenFromEnv: "TOK"}},
		{name: "missing type", opts: configSetOptions{tokenFromEnv: "TOK"}, wantErr: true, wantSubstr: "--type is required"},
		{name: "invalid type", opts: configSetOptions{providerType: "svn", tokenFromEnv: "TOK"}, wantErr: true, wantSubstr: "invalid --type"},
		{name: "both token sources", opts: configSetOptions{providerType: "github", tokenFromFile: "/tmp/t", tokenFromEnv: "TOK"}, wantErr: true, wantSubstr: "only one of"},
		{name: "no token source", opts: configSetOptions{providerType: "github"}, wantErr: true, wantSubstr: "token source is required"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateConfigSet(&tt.opts)
			if tt.wantErr {
				if err == nil || !strings.Contains(err.Error(), tt.wantSubstr) {
					t.Fatalf("expected error containing %q, got %v", tt.wantSubstr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestResolveToken(t *testing.T) {
	t.Run("from env", func(t *testing.T) {
		t.Setenv("GITOPS_TEST_TOKEN", "env-token")
		got, err := resolveToken(&configSetOptions{tokenFromEnv: "GITOPS_TEST_TOKEN"})
		if err != nil || got != "env-token" {
			t.Fatalf("got %q, err %v; want env-token", got, err)
		}
	})

	t.Run("env unset", func(t *testing.T) {
		_, err := resolveToken(&configSetOptions{tokenFromEnv: "GITOPS_TEST_TOKEN_UNSET"})
		if err == nil || !strings.Contains(err.Error(), "empty or unset") {
			t.Fatalf("expected empty-or-unset error, got %v", err)
		}
	})

	t.Run("from file with trailing newline", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "token")
		if err := os.WriteFile(path, []byte("file-token\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		got, err := resolveToken(&configSetOptions{tokenFromFile: path})
		if err != nil || got != "file-token" {
			t.Fatalf("got %q, err %v; want file-token (trimmed)", got, err)
		}
	})

	t.Run("empty file", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "empty")
		if err := os.WriteFile(path, []byte("  \n"), 0o600); err != nil {
			t.Fatal(err)
		}
		_, err := resolveToken(&configSetOptions{tokenFromFile: path})
		if err == nil || !strings.Contains(err.Error(), "empty") {
			t.Fatalf("expected empty-file error, got %v", err)
		}
	})

	t.Run("missing file", func(t *testing.T) {
		_, err := resolveToken(&configSetOptions{tokenFromFile: filepath.Join(t.TempDir(), "nope")})
		if err == nil || !strings.Contains(err.Error(), "reading token file") {
			t.Fatalf("expected reading-token-file error, got %v", err)
		}
	})
}
