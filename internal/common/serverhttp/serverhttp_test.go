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

package serverhttp

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/butlerdotdev/butler/internal/common/auth"
)

// setupClient stands up an httptest server and writes a credential file in a
// temporary HOME so a freshly constructed serverhttp.Client points at the
// test server with a valid session token and a refresh token. Refresh
// requests against /api/auth/cli/refresh are handled by the test server too;
// the refreshHandler argument lets each test choose how refresh behaves
// (succeed with a new token, return 401, etc.). Use nil for tests that
// should not trigger a refresh.
func setupClient(t *testing.T, mainHandler http.HandlerFunc, refreshHandler http.HandlerFunc) (*Client, *httptest.Server) {
	t.Helper()

	mux := http.NewServeMux()
	mux.HandleFunc("/", mainHandler)
	if refreshHandler != nil {
		mux.HandleFunc("/api/auth/cli/refresh", refreshHandler)
	} else {
		mux.HandleFunc("/api/auth/cli/refresh", func(w http.ResponseWriter, _ *http.Request) {
			t.Errorf("unexpected refresh call")
			http.Error(w, `{"error":"unexpected"}`, http.StatusInternalServerError)
		})
	}
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	home := t.TempDir()
	t.Setenv("HOME", home)

	cf := &auth.CredentialFile{
		ActiveServer: srv.URL,
		Servers: map[string]*auth.ServerCredential{
			srv.URL: {
				User:             auth.UserInfo{Email: "test@example.com", Name: "Test"},
				Kubeconfig:       "fake-kubeconfig",
				ExpiresAt:        time.Now().Add(time.Hour),
				SessionToken:     "initial-session-token",
				SessionExpiresAt: time.Now().Add(time.Hour),
				RefreshToken:     "initial-refresh-token",
				RefreshExpiresAt: time.Now().Add(7 * 24 * time.Hour),
			},
		},
	}
	if err := cf.Save(); err != nil {
		t.Fatalf("save initial credentials: %v", err)
	}

	c, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c, srv
}

func TestClient_Get_Success(t *testing.T) {
	var gotBearer string
	c, _ := setupClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotBearer = r.Header.Get("Authorization")
		if r.Method != http.MethodGet {
			t.Errorf("method = %s, want GET", r.Method)
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{"hello": "world"})
	}, nil)

	var out map[string]string
	if err := c.Get(context.Background(), "/api/example", &out); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if out["hello"] != "world" {
		t.Errorf("response = %v, want {hello:world}", out)
	}
	if gotBearer != "Bearer initial-session-token" {
		t.Errorf("authorization header = %q, want Bearer initial-session-token", gotBearer)
	}
}

func TestClient_Post_Success_WithBody(t *testing.T) {
	var gotBody map[string]any
	c, _ := setupClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("content-type = %q, want application/json", ct)
		}
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(map[string]string{"ack": "yes"})
	}, nil)

	in := map[string]any{"type": "kubeconfigs", "acknowledge": false}
	var out map[string]string
	if err := c.Post(context.Background(), "/api/example", in, &out); err != nil {
		t.Fatalf("Post: %v", err)
	}
	if gotBody["type"] != "kubeconfigs" {
		t.Errorf("server saw body type = %v, want kubeconfigs", gotBody["type"])
	}
	if out["ack"] != "yes" {
		t.Errorf("response = %v, want {ack:yes}", out)
	}
}

func TestClient_Delete_Success(t *testing.T) {
	var gotBearer string
	c, _ := setupClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotBearer = r.Header.Get("Authorization")
		if r.Method != http.MethodDelete {
			t.Errorf("method = %s, want DELETE", r.Method)
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "message": "removed"})
	}, nil)

	var out map[string]any
	if err := c.Delete(context.Background(), "/api/example", &out); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if out["success"] != true {
		t.Errorf("response = %v, want {success:true}", out)
	}
	if gotBearer != "Bearer initial-session-token" {
		t.Errorf("authorization header = %q, want Bearer initial-session-token", gotBearer)
	}
}

func TestServerError_StatusAccessors(t *testing.T) {
	cases := []struct {
		name           string
		status         int
		body           string
		wantForbidden  bool
		wantConflict   bool
		wantNotFound   bool
		wantBadRequest bool
		wantMessage    string
	}{
		{"forbidden", 403, `{"error":"forbidden: not a team admin"}`, true, false, false, false, "forbidden: not a team admin"},
		{"conflict", 409, `{"error":"rotation already in progress"}`, false, true, false, false, "rotation already in progress"},
		{"not found", 404, `{"error":"cluster not found: tc/foo"}`, false, false, true, false, "cluster not found: tc/foo"},
		{"bad request", 400, `{"error":"invalid rotation type"}`, false, false, false, true, "invalid rotation type"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := tc.body
			c, _ := setupClient(t, func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(body))
			}, nil)

			err := c.Post(context.Background(), "/api/example", map[string]string{"x": "y"}, nil)
			var se *ServerError
			if !errors.As(err, &se) {
				t.Fatalf("err = %T %v, want *ServerError", err, err)
			}
			if se.IsForbidden() != tc.wantForbidden {
				t.Errorf("IsForbidden() = %v, want %v", se.IsForbidden(), tc.wantForbidden)
			}
			if se.IsConflict() != tc.wantConflict {
				t.Errorf("IsConflict() = %v, want %v", se.IsConflict(), tc.wantConflict)
			}
			if se.IsNotFound() != tc.wantNotFound {
				t.Errorf("IsNotFound() = %v, want %v", se.IsNotFound(), tc.wantNotFound)
			}
			if se.IsBadRequest() != tc.wantBadRequest {
				t.Errorf("IsBadRequest() = %v, want %v", se.IsBadRequest(), tc.wantBadRequest)
			}
			if se.IsInvalidSession() {
				t.Errorf("IsInvalidSession() = true on non-401")
			}
			if se.Message != tc.wantMessage {
				t.Errorf("Message = %q, want %q", se.Message, tc.wantMessage)
			}
		})
	}
}

func TestClient_InvalidSession_RefreshSucceeds_RetriesOnce(t *testing.T) {
	var mainCalls int32
	var refreshCalls int32

	main := func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&mainCalls, 1)
		bearer := r.Header.Get("Authorization")
		if n == 1 {
			// First call: original token. Reject with the documented envelope.
			if bearer != "Bearer initial-session-token" {
				t.Errorf("first call bearer = %q, want initial token", bearer)
			}
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"invalid session"}`))
			return
		}
		// Second call: post-refresh. Expect the new token.
		if bearer != "Bearer refreshed-session-token" {
			t.Errorf("second call bearer = %q, want refreshed token", bearer)
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{"ok": "yes"})
	}

	refresh := func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&refreshCalls, 1)
		resp := auth.TokenResponse{
			User:             auth.UserInfo{Email: "test@example.com"},
			Kubeconfig:       "fresh-kubeconfig",
			ExpiresAt:        time.Now().Add(time.Hour),
			SessionToken:     "refreshed-session-token",
			SessionExpiresAt: time.Now().Add(time.Hour),
			RefreshToken:     "refreshed-refresh-token",
			RefreshExpiresAt: time.Now().Add(7 * 24 * time.Hour),
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(resp)
	}

	c, _ := setupClient(t, main, refresh)

	var out map[string]string
	if err := c.Get(context.Background(), "/api/example", &out); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if out["ok"] != "yes" {
		t.Errorf("response = %v, want {ok:yes}", out)
	}
	if got := atomic.LoadInt32(&mainCalls); got != 2 {
		t.Errorf("main call count = %d, want 2 (one + retry)", got)
	}
	if got := atomic.LoadInt32(&refreshCalls); got != 1 {
		t.Errorf("refresh call count = %d, want 1", got)
	}

	// Confirm credentials file was persisted with the new tokens.
	reloaded, err := auth.LoadCredentials()
	if err != nil {
		t.Fatalf("reload credentials: %v", err)
	}
	sc := reloaded.ActiveCredential()
	if sc == nil {
		t.Fatal("no active credential after refresh")
	}
	if sc.SessionToken != "refreshed-session-token" {
		t.Errorf("persisted session token = %q, want refreshed-session-token", sc.SessionToken)
	}
	if sc.RefreshToken != "refreshed-refresh-token" {
		t.Errorf("persisted refresh token = %q, want refreshed-refresh-token", sc.RefreshToken)
	}
}

func TestClient_InvalidSession_RefreshFails_ReturnsErrSessionExpired(t *testing.T) {
	var mainCalls int32
	main := func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&mainCalls, 1)
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"invalid session"}`))
	}
	refresh := func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"refresh_token_expired"}`))
	}

	c, _ := setupClient(t, main, refresh)
	err := c.Get(context.Background(), "/api/example", nil)
	if err == nil {
		t.Fatal("err = nil, want ErrSessionExpired")
	}
	if !errors.Is(err, ErrSessionExpired) {
		t.Errorf("err = %v, want errors.Is(err, ErrSessionExpired)", err)
	}
	if got := atomic.LoadInt32(&mainCalls); got != 1 {
		t.Errorf("main call count = %d, want 1 (no retry when refresh fails)", got)
	}
}

func TestClient_NonInvalidSession_401_DoesNotRefresh(t *testing.T) {
	var mainCalls int32
	main := func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&mainCalls, 1)
		w.WriteHeader(http.StatusUnauthorized)
		// Different envelope: the CLI must not paper over this with a refresh.
		_, _ = w.Write([]byte(`{"error":"missing bearer"}`))
	}

	c, _ := setupClient(t, main, nil)
	err := c.Get(context.Background(), "/api/example", nil)
	var se *ServerError
	if !errors.As(err, &se) {
		t.Fatalf("err = %T %v, want *ServerError", err, err)
	}
	if se.IsInvalidSession() {
		t.Errorf("IsInvalidSession() = true on 'missing bearer' envelope")
	}
	if se.Message != "missing bearer" {
		t.Errorf("Message = %q, want missing bearer", se.Message)
	}
	if got := atomic.LoadInt32(&mainCalls); got != 1 {
		t.Errorf("main call count = %d, want 1 (no retry on non-invalid-session 401)", got)
	}
}

func TestServerError_IsInvalidSession_PositiveOnly(t *testing.T) {
	se := &ServerError{StatusCode: http.StatusUnauthorized, Message: "invalid session"}
	if !se.IsInvalidSession() {
		t.Errorf("IsInvalidSession() = false on 401 + invalid session")
	}
	other := &ServerError{StatusCode: http.StatusUnauthorized, Message: "something else"}
	if other.IsInvalidSession() {
		t.Errorf("IsInvalidSession() = true on 401 with different message")
	}
	wrongStatus := &ServerError{StatusCode: http.StatusForbidden, Message: "invalid session"}
	if wrongStatus.IsInvalidSession() {
		t.Errorf("IsInvalidSession() = true on non-401 status")
	}
}

func TestNew_MissingSessionToken_Errors(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	cf := &auth.CredentialFile{
		ActiveServer: "http://test",
		Servers: map[string]*auth.ServerCredential{
			"http://test": {
				User:         auth.UserInfo{Email: "test@example.com"},
				ExpiresAt:    time.Now().Add(time.Hour),
				RefreshToken: "x",
			},
		},
	}
	if err := cf.Save(); err != nil {
		t.Fatalf("save: %v", err)
	}
	_, err := New()
	if err == nil {
		t.Fatal("New() succeeded with empty SessionToken; want error")
	}
}

func TestNewWithTimeout_SetsTimeout(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cf := &auth.CredentialFile{
		ActiveServer: "http://test",
		Servers: map[string]*auth.ServerCredential{
			"http://test": {
				User:         auth.UserInfo{Email: "test@example.com"},
				ExpiresAt:    time.Now().Add(time.Hour),
				SessionToken: "tok",
				RefreshToken: "x",
			},
		},
	}
	if err := cf.Save(); err != nil {
		t.Fatalf("save: %v", err)
	}

	c, err := NewWithTimeout(90 * time.Second)
	if err != nil {
		t.Fatalf("NewWithTimeout: %v", err)
	}
	if c.http.Timeout != 90*time.Second {
		t.Fatalf("timeout = %v, want 90s", c.http.Timeout)
	}

	// New keeps the default.
	d, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if d.http.Timeout != DefaultTimeout {
		t.Fatalf("New timeout = %v, want %v", d.http.Timeout, DefaultTimeout)
	}
}
