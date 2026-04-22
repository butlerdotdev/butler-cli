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

package client

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"testing"
	"time"

	"github.com/butlerdotdev/butler/internal/common/auth"
	"k8s.io/client-go/rest"
)

// writeCredentialFile places a credentials.json under a temp HOME and returns
// a cleanup function. The function relies on os.UserHomeDir honoring the HOME
// env var on Unix systems; on Windows we skip.
func writeCredentialFile(t *testing.T, cf *auth.CredentialFile) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("credential-file override requires HOME env semantics; skipping on windows")
	}

	home := t.TempDir()
	t.Setenv("HOME", home)

	if cf == nil {
		return
	}
	dir := filepath.Join(home, ".butler")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	data, err := json.MarshalIndent(cf, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "credentials.json"), data, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func TestApplyImpersonation_SetsPayloadWhenEmailPresent(t *testing.T) {
	writeCredentialFile(t, &auth.CredentialFile{
		ActiveServer: "https://butler.example.com",
		Servers: map[string]*auth.ServerCredential{
			"https://butler.example.com": {
				User: auth.UserInfo{
					Email: "alice@example.com",
					Name:  "Alice",
				},
				ExpiresAt: time.Now().Add(1 * time.Hour),
			},
		},
	})

	cfg := &rest.Config{}
	applyImpersonationFromCredentials(cfg)

	if cfg.Impersonate.UserName != "alice@example.com" {
		t.Errorf("UserName = %q, want %q", cfg.Impersonate.UserName, "alice@example.com")
	}

	groups := append([]string(nil), cfg.Impersonate.Groups...)
	sort.Strings(groups)
	wantGroups := []string{"butler-api-users", "system:authenticated"}
	if len(groups) != len(wantGroups) {
		t.Fatalf("Groups len = %d, want %d (%v)", len(groups), len(wantGroups), groups)
	}
	for i, g := range wantGroups {
		if groups[i] != g {
			t.Errorf("Groups[%d] = %q, want %q", i, groups[i], g)
		}
	}
}

func TestApplyImpersonation_RawKubeconfigShortCircuit(t *testing.T) {
	// No active credential at all (empty credential file).
	writeCredentialFile(t, &auth.CredentialFile{
		Servers: map[string]*auth.ServerCredential{},
	})

	cfg := &rest.Config{}
	applyImpersonationFromCredentials(cfg)

	if cfg.Impersonate.UserName != "" {
		t.Errorf("UserName = %q, want empty (raw-kubeconfig short-circuit)", cfg.Impersonate.UserName)
	}
	if len(cfg.Impersonate.Groups) != 0 {
		t.Errorf("Groups = %v, want empty (raw-kubeconfig short-circuit)", cfg.Impersonate.Groups)
	}

	// Active server set but User.Email empty: still short-circuit.
	writeCredentialFile(t, &auth.CredentialFile{
		ActiveServer: "https://butler.example.com",
		Servers: map[string]*auth.ServerCredential{
			"https://butler.example.com": {
				User:      auth.UserInfo{Email: ""},
				ExpiresAt: time.Now().Add(1 * time.Hour),
			},
		},
	})

	cfg = &rest.Config{}
	applyImpersonationFromCredentials(cfg)

	if cfg.Impersonate.UserName != "" {
		t.Errorf("UserName = %q, want empty when email is empty", cfg.Impersonate.UserName)
	}
	if len(cfg.Impersonate.Groups) != 0 {
		t.Errorf("Groups = %v, want empty when email is empty", cfg.Impersonate.Groups)
	}
}

func TestApplyImpersonation_LowercasesAndTrimsEmail(t *testing.T) {
	writeCredentialFile(t, &auth.CredentialFile{
		ActiveServer: "https://butler.example.com",
		Servers: map[string]*auth.ServerCredential{
			"https://butler.example.com": {
				User: auth.UserInfo{
					Email: "  Alice@Example.COM  ",
				},
				ExpiresAt: time.Now().Add(1 * time.Hour),
			},
		},
	})

	cfg := &rest.Config{}
	applyImpersonationFromCredentials(cfg)

	if cfg.Impersonate.UserName != "alice@example.com" {
		t.Errorf("UserName = %q, want %q (canonicalized)", cfg.Impersonate.UserName, "alice@example.com")
	}
}
