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
	"bytes"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/butlerdotdev/butler/internal/common/serverhttp"
)

func TestPrintGitopsStatus(t *testing.T) {
	t.Run("not enabled", func(t *testing.T) {
		var buf bytes.Buffer
		if err := printGitopsStatus(&buf, gitOpsStatus{Enabled: false}); err != nil {
			t.Fatalf("printGitopsStatus: %v", err)
		}
		out := buf.String()
		if !strings.Contains(out, "not enabled") {
			t.Fatalf("expected not-enabled message, got %q", out)
		}
		if !strings.Contains(out, "gitops enable") {
			t.Fatalf("expected enable hint, got %q", out)
		}
	})

	t.Run("enabled", func(t *testing.T) {
		var buf bytes.Buffer
		st := gitOpsStatus{
			Enabled:    true,
			Provider:   "fluxcd",
			Repository: "https://github.com/acme/clusters",
			Branch:     "main",
			Path:       "clusters/prod",
			Status:     "Ready",
			Version:    "v2.4.0",
		}
		if err := printGitopsStatus(&buf, st); err != nil {
			t.Fatalf("printGitopsStatus: %v", err)
		}
		out := buf.String()
		for _, want := range []string{"fluxcd", "https://github.com/acme/clusters", "main", "clusters/prod", "Ready", "v2.4.0"} {
			if !strings.Contains(out, want) {
				t.Fatalf("expected output to contain %q, got %q", want, out)
			}
		}
	})
}

func TestTranslateGitopsError(t *testing.T) {
	tests := []struct {
		name       string
		in         error
		wantIsExp  bool
		wantSubstr string
	}{
		{name: "session expired", in: serverhttp.ErrSessionExpired, wantIsExp: true},
		{name: "forbidden", in: &serverhttp.ServerError{StatusCode: http.StatusForbidden, Message: "not a team operator"}, wantSubstr: "forbidden: not a team operator"},
		{name: "not found", in: &serverhttp.ServerError{StatusCode: http.StatusNotFound, Message: "cluster not found"}, wantSubstr: "not found: cluster not found"},
		{name: "conflict", in: &serverhttp.ServerError{StatusCode: http.StatusConflict, Message: "already enabled"}, wantSubstr: "conflict: already enabled"},
		{name: "bad request", in: &serverhttp.ServerError{StatusCode: http.StatusBadRequest, Message: "repository required"}, wantSubstr: "invalid request: repository required"},
		{name: "passthrough", in: errors.New("connection refused"), wantSubstr: "connection refused"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := translateGitopsError(tt.in)
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
