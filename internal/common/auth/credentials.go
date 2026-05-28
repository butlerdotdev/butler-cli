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

// Package auth provides credential storage and device flow authentication
// for the Butler CLI. Credentials are stored at ~/.butler/credentials.json
// with 0600 permissions.
package auth

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// CredentialFile holds authentication state for all known Butler servers.
// It is persisted to ~/.butler/credentials.json.
type CredentialFile struct {
	ActiveServer string                       `json:"activeServer"`
	Servers      map[string]*ServerCredential `json:"servers"`
}

// ServerCredential holds session state for a single Butler server.
//
// Two tokens live here. Kubeconfig embeds a Kubernetes ServiceAccount token
// for direct K8s API access (ADR-002 path). SessionToken is the butler-server
// session JWT for protected HTTP endpoints (cert rotation, GitOps lifecycle,
// provider images/networks, IdP create); see ADR-016's 2026-05-28 amendment.
// The two expiries default-align at 24h but are independent timers.
type ServerCredential struct {
	User             UserInfo  `json:"user"`
	Kubeconfig       string    `json:"kubeconfig"`        // base64-encoded management cluster kubeconfig
	ExpiresAt        time.Time `json:"expiresAt"`         // SA token expiry
	SessionToken     string    `json:"sessionToken"`      // butler-server session JWT (HS256)
	SessionExpiresAt time.Time `json:"sessionExpiresAt"`  // session JWT expiry
	RefreshToken     string    `json:"refreshToken"`      // opaque refresh token
	RefreshExpiresAt time.Time `json:"refreshExpiresAt"`  // refresh token expiry
	ActiveTeam       string    `json:"activeTeam"`        // currently selected team
}

// SessionTokenExpired reports whether the butler-server session JWT has expired.
// Independent of IsExpired (which tracks the SA token).
func (c *ServerCredential) SessionTokenExpired() bool {
	if c.SessionExpiresAt.IsZero() {
		return true
	}
	return time.Now().After(c.SessionExpiresAt)
}

// UserInfo describes the authenticated user.
type UserInfo struct {
	Email           string           `json:"email"`
	Name            string           `json:"name"`
	Teams           []TeamMembership `json:"teams"`
	IsPlatformAdmin bool             `json:"isPlatformAdmin"`
}

// TeamMembership records the user's role within a team.
type TeamMembership struct {
	Name string `json:"name"`
	Role string `json:"role"`
}

// CredentialPath returns the path to the credential file (~/.butler/credentials.json).
func CredentialPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".butler", "credentials.json")
}

// LoadCredentials reads and parses the credential file. If the file does not
// exist, an empty CredentialFile is returned (not an error).
func LoadCredentials() (*CredentialFile, error) {
	path := CredentialPath()
	if path == "" {
		return nil, fmt.Errorf("unable to determine home directory")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &CredentialFile{
				Servers: make(map[string]*ServerCredential),
			}, nil
		}
		return nil, fmt.Errorf("reading credential file: %w", err)
	}

	var cf CredentialFile
	if err := json.Unmarshal(data, &cf); err != nil {
		return nil, fmt.Errorf("parsing credential file: %w", err)
	}

	if cf.Servers == nil {
		cf.Servers = make(map[string]*ServerCredential)
	}

	return &cf, nil
}

// Save writes the credential file to disk with 0600 permissions, creating
// the ~/.butler/ directory if it does not exist.
func (f *CredentialFile) Save() error {
	path := CredentialPath()
	if path == "" {
		return fmt.Errorf("unable to determine home directory")
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("creating directory %s: %w", dir, err)
	}

	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling credentials: %w", err)
	}

	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("writing credential file: %w", err)
	}

	return nil
}

// ActiveCredential returns the credential for the active server,
// or nil if no active server is set or the server is not found.
func (f *CredentialFile) ActiveCredential() *ServerCredential {
	if f.ActiveServer == "" {
		return nil
	}
	return f.Servers[f.ActiveServer]
}

// IsExpired reports whether the access token has expired.
func (c *ServerCredential) IsExpired() bool {
	return time.Now().After(c.ExpiresAt)
}

// CanRefresh reports whether the refresh token is present and has not expired.
func (c *ServerCredential) CanRefresh() bool {
	if c.RefreshToken == "" {
		return false
	}
	return time.Now().Before(c.RefreshExpiresAt)
}

// KubeconfigBytes returns the base64-decoded kubeconfig bytes.
func (c *ServerCredential) KubeconfigBytes() ([]byte, error) {
	if c.Kubeconfig == "" {
		return nil, fmt.Errorf("no kubeconfig stored in credential")
	}
	data, err := base64.StdEncoding.DecodeString(c.Kubeconfig)
	if err != nil {
		return nil, fmt.Errorf("decoding kubeconfig: %w", err)
	}
	return data, nil
}
