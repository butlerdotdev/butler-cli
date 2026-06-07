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
	"context"
	"net/http"
	"strings"
	"testing"
)

func TestConfirmGitopsDisable(t *testing.T) {
	t.Run("match", func(t *testing.T) {
		var buf bytes.Buffer
		if err := confirmGitopsDisable("my-cluster", strings.NewReader("my-cluster\n"), &buf); err != nil {
			t.Fatalf("expected match, got %v", err)
		}
	})
	t.Run("mismatch", func(t *testing.T) {
		var buf bytes.Buffer
		err := confirmGitopsDisable("my-cluster", strings.NewReader("wrong\n"), &buf)
		if err == nil || !strings.Contains(err.Error(), "did not match") {
			t.Fatalf("expected mismatch error, got %v", err)
		}
	})
}

func gitopsDisableMux(t *testing.T, status int, deleteCalled *bool) *http.ServeMux {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/clusters/team-x/c1/gitops", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("method = %s, want DELETE", r.Method)
		}
		if deleteCalled != nil {
			*deleteCalled = true
		}
		w.WriteHeader(status)
		if status >= 400 {
			_, _ = w.Write([]byte(`{"error":"gitops not enabled"}`))
		} else {
			_, _ = w.Write([]byte(`{"success":true,"message":"disabled"}`))
		}
	})
	return mux
}

func TestRunGitopsDisable_Interactive200(t *testing.T) {
	startGitopsTestServer(t, gitopsDisableMux(t, http.StatusOK, nil))

	var buf bytes.Buffer
	err := runGitopsDisable(context.Background(), strings.NewReader("c1\n"), &buf, "c1", "team-x", "", &disableOptions{})
	if err != nil {
		t.Fatalf("runGitopsDisable: %v", err)
	}
	if !strings.Contains(buf.String(), "disabled on c1") {
		t.Fatalf("expected disabled message, got %q", buf.String())
	}
}

func TestRunGitopsDisable_404MappedToSuccess(t *testing.T) {
	startGitopsTestServer(t, gitopsDisableMux(t, http.StatusNotFound, nil))

	var buf bytes.Buffer
	err := runGitopsDisable(context.Background(), strings.NewReader(""), &buf, "c1", "team-x", "", &disableOptions{yes: true, confirm: "c1"})
	if err != nil {
		t.Fatalf("expected 404 mapped to success, got %v", err)
	}
	if !strings.Contains(buf.String(), "nothing to disable") {
		t.Fatalf("expected nothing-to-disable message, got %q", buf.String())
	}
}

func TestRunGitopsDisable_YesWrongConfirm(t *testing.T) {
	deleteCalled := false
	startGitopsTestServer(t, gitopsDisableMux(t, http.StatusOK, &deleteCalled))

	var buf bytes.Buffer
	err := runGitopsDisable(context.Background(), strings.NewReader(""), &buf, "c1", "team-x", "", &disableOptions{yes: true, confirm: "wrong"})
	if err == nil || !strings.Contains(err.Error(), "--confirm") {
		t.Fatalf("expected confirm-mismatch error, got %v", err)
	}
	if deleteCalled {
		t.Errorf("DELETE must not be called when confirmation fails")
	}
}
