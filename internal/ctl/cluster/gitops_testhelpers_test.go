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
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/butlerdotdev/butler/internal/common/auth"
)

// startGitopsTestServer starts an httptest server with the given handler mux
// and writes an active credential under a temp HOME pointing at it, so
// serverhttp.New / NewWithTimeout pick it up. Shared by the cluster gitops
// roundtrip tests (disable, preview).
func startGitopsTestServer(t *testing.T, mux *http.ServeMux) {
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
