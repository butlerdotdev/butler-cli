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

// Package authtest provides shared helpers for exercising the CLI login flow
// in tests. It is imported only by login package tests, so it is not linked
// into the shipped binaries. The helper runs the real runLogin against a mock
// device-flow server so the credential-save path (including session-token
// persistence) is covered for both butleradm and butlerctl from one place.
package authtest

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/butlerdotdev/butler/internal/common/auth"
	"github.com/butlerdotdev/butler/internal/common/log"
)

// LoginFunc matches the unexported runLogin in the login packages.
type LoginFunc func(ctx context.Context, logger *log.Logger, serverURL string, noBrowser bool) error

// MockTokenResponse configures what the mock /api/auth/cli/token endpoint
// returns. When IncludeSession is false, the session_token and
// session_expires_at fields are omitted entirely, simulating a server (or a
// regression) that does not supply a session token.
type MockTokenResponse struct {
	SessionToken     string
	SessionExpiresAt time.Time
	IncludeSession   bool
}

// RunMockLogin starts a mock device-flow server, runs the given login flow
// against it under a temp HOME, and returns the saved ServerCredential. The
// mock always reports a platform admin so butleradm's admin gate is satisfied.
func RunMockLogin(t *testing.T, run LoginFunc, mock MockTokenResponse) *auth.ServerCredential {
	t.Helper()

	mux := http.NewServeMux()
	mux.HandleFunc("/api/auth/cli/device", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(t, w, map[string]any{
			"device_code":      "test-device-code",
			"user_code":        "TEST-CODE",
			"verification_uri": "http://example.test/auth/device",
			"expires_in":       60,
			"interval":         1,
		})
	})
	mux.HandleFunc("/api/auth/cli/token", func(w http.ResponseWriter, _ *http.Request) {
		resp := map[string]any{
			"user": map[string]any{
				"email":           "admin@test.local",
				"name":            "Test Admin",
				"teams":           []any{},
				"isPlatformAdmin": true,
			},
			"kubeconfig":         "fake-kubeconfig",
			"expires_at":         time.Now().Add(time.Hour).Format(time.RFC3339),
			"refresh_token":      "test-refresh-token",
			"refresh_expires_at": time.Now().Add(24 * time.Hour).Format(time.RFC3339),
		}
		if mock.IncludeSession {
			resp["session_token"] = mock.SessionToken
			resp["session_expires_at"] = mock.SessionExpiresAt.Format(time.RFC3339)
		}
		writeJSON(t, w, resp)
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	t.Setenv("HOME", t.TempDir())

	logger := log.NewWithWriter("authtest", slog.LevelError, io.Discard)
	if err := run(context.Background(), logger, srv.URL, true); err != nil {
		t.Fatalf("login flow failed: %v", err)
	}

	creds, err := auth.LoadCredentials()
	if err != nil {
		t.Fatalf("load credentials: %v", err)
	}
	sc := creds.Servers[srv.URL]
	if sc == nil {
		t.Fatalf("no credential saved for %s", srv.URL)
	}
	return sc
}

func writeJSON(t *testing.T, w http.ResponseWriter, v any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		t.Errorf("encode mock response: %v", err)
	}
}
