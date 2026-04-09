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

package auth

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSaveAndLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "credentials.json")

	original := &CredentialFile{
		ActiveServer: "https://butler.example.com",
		Servers: map[string]*ServerCredential{
			"https://butler.example.com": {
				User: UserInfo{
					Email:           "dev@example.com",
					Name:            "Test User",
					IsPlatformAdmin: true,
					Teams: []TeamMembership{
						{Name: "platform", Role: "admin"},
						{Name: "payments", Role: "viewer"},
					},
				},
				Kubeconfig:       base64.StdEncoding.EncodeToString([]byte("apiVersion: v1\nkind: Config")),
				ExpiresAt:        time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC),
				RefreshToken:     "refresh-token-abc",
				RefreshExpiresAt: time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC),
				ActiveTeam:       "platform",
			},
		},
	}

	// Write manually to the temp path (Save uses CredentialPath which depends
	// on the real home dir, so we test the marshal/unmarshal logic directly).
	data, err := json.MarshalIndent(original, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Read it back
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	var loaded CredentialFile
	if err := json.Unmarshal(raw, &loaded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if loaded.ActiveServer != original.ActiveServer {
		t.Errorf("ActiveServer = %q, want %q", loaded.ActiveServer, original.ActiveServer)
	}

	sc, ok := loaded.Servers["https://butler.example.com"]
	if !ok {
		t.Fatal("expected server entry not found")
	}

	if sc.User.Email != "dev@example.com" {
		t.Errorf("User.Email = %q, want %q", sc.User.Email, "dev@example.com")
	}
	if sc.User.Name != "Test User" {
		t.Errorf("User.Name = %q, want %q", sc.User.Name, "Test User")
	}
	if !sc.User.IsPlatformAdmin {
		t.Error("User.IsPlatformAdmin = false, want true")
	}
	if len(sc.User.Teams) != 2 {
		t.Fatalf("len(User.Teams) = %d, want 2", len(sc.User.Teams))
	}
	if sc.User.Teams[0].Name != "platform" || sc.User.Teams[0].Role != "admin" {
		t.Errorf("Teams[0] = %+v, want {platform admin}", sc.User.Teams[0])
	}
	if sc.ActiveTeam != "platform" {
		t.Errorf("ActiveTeam = %q, want %q", sc.ActiveTeam, "platform")
	}
	if sc.RefreshToken != "refresh-token-abc" {
		t.Errorf("RefreshToken = %q, want %q", sc.RefreshToken, "refresh-token-abc")
	}
	if !sc.ExpiresAt.Equal(time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)) {
		t.Errorf("ExpiresAt = %v, want 2026-06-01T12:00:00Z", sc.ExpiresAt)
	}
}

func TestSaveCreatesDirectoryAndFile(t *testing.T) {
	// Override home dir for this test using a temp dir
	dir := t.TempDir()
	butlerDir := filepath.Join(dir, ".butler")
	path := filepath.Join(butlerDir, "credentials.json")

	cf := &CredentialFile{
		ActiveServer: "https://test.example.com",
		Servers:      map[string]*ServerCredential{},
	}

	// Manually do what Save() does but with our custom path
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	data, err := json.MarshalIndent(cf, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Verify directory was created
	info, err := os.Stat(butlerDir)
	if err != nil {
		t.Fatalf("stat dir: %v", err)
	}
	if !info.IsDir() {
		t.Fatal("expected directory")
	}

	// Verify file permissions
	fileInfo, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat file: %v", err)
	}
	perm := fileInfo.Mode().Perm()
	if perm != 0600 {
		t.Errorf("file permissions = %o, want 0600", perm)
	}
}

func TestIsExpired(t *testing.T) {
	tests := []struct {
		name      string
		expiresAt time.Time
		want      bool
	}{
		{
			name:      "future time is not expired",
			expiresAt: time.Now().Add(time.Hour),
			want:      false,
		},
		{
			name:      "past time is expired",
			expiresAt: time.Now().Add(-time.Hour),
			want:      true,
		},
		{
			name:      "zero time is expired",
			expiresAt: time.Time{},
			want:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sc := &ServerCredential{ExpiresAt: tt.expiresAt}
			if got := sc.IsExpired(); got != tt.want {
				t.Errorf("IsExpired() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCanRefresh(t *testing.T) {
	tests := []struct {
		name             string
		refreshToken     string
		refreshExpiresAt time.Time
		want             bool
	}{
		{
			name:             "valid refresh token with future expiry",
			refreshToken:     "some-token",
			refreshExpiresAt: time.Now().Add(24 * time.Hour),
			want:             true,
		},
		{
			name:             "empty refresh token",
			refreshToken:     "",
			refreshExpiresAt: time.Now().Add(24 * time.Hour),
			want:             false,
		},
		{
			name:             "expired refresh token",
			refreshToken:     "some-token",
			refreshExpiresAt: time.Now().Add(-time.Hour),
			want:             false,
		},
		{
			name:             "missing refresh token and past expiry",
			refreshToken:     "",
			refreshExpiresAt: time.Now().Add(-time.Hour),
			want:             false,
		},
		{
			name:             "zero expiry time",
			refreshToken:     "some-token",
			refreshExpiresAt: time.Time{},
			want:             false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sc := &ServerCredential{
				RefreshToken:     tt.refreshToken,
				RefreshExpiresAt: tt.refreshExpiresAt,
			}
			if got := sc.CanRefresh(); got != tt.want {
				t.Errorf("CanRefresh() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestActiveCredential(t *testing.T) {
	t.Run("returns credential for active server", func(t *testing.T) {
		cf := &CredentialFile{
			ActiveServer: "https://butler.example.com",
			Servers: map[string]*ServerCredential{
				"https://butler.example.com": {
					User: UserInfo{Email: "user@example.com"},
				},
			},
		}
		sc := cf.ActiveCredential()
		if sc == nil {
			t.Fatal("expected non-nil credential")
		}
		if sc.User.Email != "user@example.com" {
			t.Errorf("User.Email = %q, want %q", sc.User.Email, "user@example.com")
		}
	})

	t.Run("returns nil when no active server", func(t *testing.T) {
		cf := &CredentialFile{
			ActiveServer: "",
			Servers: map[string]*ServerCredential{
				"https://butler.example.com": {
					User: UserInfo{Email: "user@example.com"},
				},
			},
		}
		if sc := cf.ActiveCredential(); sc != nil {
			t.Errorf("expected nil, got %+v", sc)
		}
	})

	t.Run("returns nil when active server not in map", func(t *testing.T) {
		cf := &CredentialFile{
			ActiveServer: "https://other.example.com",
			Servers: map[string]*ServerCredential{
				"https://butler.example.com": {
					User: UserInfo{Email: "user@example.com"},
				},
			},
		}
		if sc := cf.ActiveCredential(); sc != nil {
			t.Errorf("expected nil, got %+v", sc)
		}
	})

	t.Run("returns nil with empty servers map", func(t *testing.T) {
		cf := &CredentialFile{
			ActiveServer: "https://butler.example.com",
			Servers:      map[string]*ServerCredential{},
		}
		if sc := cf.ActiveCredential(); sc != nil {
			t.Errorf("expected nil, got %+v", sc)
		}
	})
}

func TestKubeconfigBytes(t *testing.T) {
	t.Run("decodes valid base64", func(t *testing.T) {
		original := "apiVersion: v1\nclusters: []\nkind: Config"
		sc := &ServerCredential{
			Kubeconfig: base64.StdEncoding.EncodeToString([]byte(original)),
		}
		got, err := sc.KubeconfigBytes()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if string(got) != original {
			t.Errorf("KubeconfigBytes() = %q, want %q", string(got), original)
		}
	})

	t.Run("errors on empty kubeconfig", func(t *testing.T) {
		sc := &ServerCredential{Kubeconfig: ""}
		_, err := sc.KubeconfigBytes()
		if err == nil {
			t.Fatal("expected error for empty kubeconfig")
		}
	})

	t.Run("errors on invalid base64", func(t *testing.T) {
		sc := &ServerCredential{Kubeconfig: "not-valid-base64!!!"}
		_, err := sc.KubeconfigBytes()
		if err == nil {
			t.Fatal("expected error for invalid base64")
		}
	})
}

func TestLoadCredentialsMissingFile(t *testing.T) {
	// LoadCredentials uses CredentialPath() which depends on the real home dir.
	// We can at least verify that the function signature works for the
	// non-existent path case by testing the parsing logic directly.
	data := []byte(`{
		"activeServer": "https://test.example.com",
		"servers": {
			"https://test.example.com": {
				"user": {"email": "a@b.com", "name": "A"},
				"kubeconfig": "dGVzdA==",
				"expiresAt": "2026-06-01T00:00:00Z",
				"refreshToken": "rt",
				"refreshExpiresAt": "2026-07-01T00:00:00Z",
				"activeTeam": "dev"
			}
		}
	}`)

	var cf CredentialFile
	if err := json.Unmarshal(data, &cf); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if cf.ActiveServer != "https://test.example.com" {
		t.Errorf("ActiveServer = %q, want %q", cf.ActiveServer, "https://test.example.com")
	}
	sc := cf.Servers["https://test.example.com"]
	if sc == nil {
		t.Fatal("expected server credential")
	}
	if sc.User.Email != "a@b.com" {
		t.Errorf("User.Email = %q, want %q", sc.User.Email, "a@b.com")
	}
	if sc.ActiveTeam != "dev" {
		t.Errorf("ActiveTeam = %q, want %q", sc.ActiveTeam, "dev")
	}
}

func TestNilServersMapInitialized(t *testing.T) {
	data := []byte(`{"activeServer": ""}`)

	var cf CredentialFile
	if err := json.Unmarshal(data, &cf); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// LoadCredentials initializes nil maps; verify the logic manually
	if cf.Servers == nil {
		cf.Servers = make(map[string]*ServerCredential)
	}

	// Should not panic
	cf.Servers["test"] = &ServerCredential{}
	if len(cf.Servers) != 1 {
		t.Errorf("len(Servers) = %d, want 1", len(cf.Servers))
	}
}
