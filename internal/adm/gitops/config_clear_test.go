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
	"net/http"
	"strings"
	"testing"
)

func TestClearConfirmTarget(t *testing.T) {
	if got := clearConfirmTarget(gitProviderConfig{Organization: "acme"}); got != "acme" {
		t.Errorf("with org: got %q, want acme", got)
	}
	if got := clearConfirmTarget(gitProviderConfig{Configured: true}); got != "confirm" {
		t.Errorf("no org: got %q, want confirm", got)
	}
}

func TestConfirmClear(t *testing.T) {
	t.Run("match", func(t *testing.T) {
		var buf bytes.Buffer
		if err := confirmClear("acme", strings.NewReader("acme\n"), &buf); err != nil {
			t.Fatalf("expected match, got %v", err)
		}
	})
	t.Run("mismatch", func(t *testing.T) {
		var buf bytes.Buffer
		err := confirmClear("acme", strings.NewReader("nope\n"), &buf)
		if err == nil || !strings.Contains(err.Error(), "did not match") {
			t.Fatalf("expected mismatch error, got %v", err)
		}
	})
}

// clearMux serves GET /api/gitops/config (current state) and DELETE
// /api/gitops/config (deleteStatus). A DELETE is recorded in *deleteCalled.
func clearMux(t *testing.T, getJSON string, deleteStatus int, deleteCalled *bool) *http.ServeMux {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/gitops/config", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(getJSON))
		case http.MethodDelete:
			if deleteCalled != nil {
				*deleteCalled = true
			}
			w.WriteHeader(deleteStatus)
			if deleteStatus >= 400 {
				_, _ = w.Write([]byte(`{"error":"no Git provider configured"}`))
			}
		default:
			t.Errorf("unexpected method %s", r.Method)
		}
	})
	return mux
}

func TestRunConfigClear_Interactive204(t *testing.T) {
	startTestServer(t, clearMux(t, `{"configured":true,"type":"github","organization":"butlerdotdev"}`, http.StatusNoContent, nil))

	var buf bytes.Buffer
	// Interactive: the typed value must equal the org.
	err := runConfigClear(context.Background(), strings.NewReader("butlerdotdev\n"), &buf, &configClearOptions{})
	if err != nil {
		t.Fatalf("runConfigClear: %v", err)
	}
	if !strings.Contains(buf.String(), "cleared") {
		t.Fatalf("expected cleared message, got %q", buf.String())
	}
}

func TestRunConfigClear_404MappedToSuccess(t *testing.T) {
	startTestServer(t, clearMux(t, `{"configured":false}`, http.StatusNotFound, nil))

	var buf bytes.Buffer
	// Not configured: target is the literal "confirm".
	err := runConfigClear(context.Background(), strings.NewReader(""), &buf, &configClearOptions{yes: true, confirm: "confirm"})
	if err != nil {
		t.Fatalf("expected 404 mapped to success, got %v", err)
	}
	if !strings.Contains(buf.String(), "nothing to clear") {
		t.Fatalf("expected nothing-to-clear message, got %q", buf.String())
	}
}

func TestRunConfigClear_YesWrongConfirm(t *testing.T) {
	deleteCalled := false
	startTestServer(t, clearMux(t, `{"configured":true,"type":"github","organization":"butlerdotdev"}`, http.StatusNoContent, &deleteCalled))

	var buf bytes.Buffer
	err := runConfigClear(context.Background(), strings.NewReader(""), &buf, &configClearOptions{yes: true, confirm: "wrong"})
	if err == nil || !strings.Contains(err.Error(), "--confirm") {
		t.Fatalf("expected confirm-mismatch error, got %v", err)
	}
	if deleteCalled {
		t.Errorf("DELETE must not be called when confirmation fails")
	}
}
