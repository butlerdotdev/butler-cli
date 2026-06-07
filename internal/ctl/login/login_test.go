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

package login

import (
	"testing"
	"time"

	"github.com/butlerdotdev/butler/internal/common/authtest"
)

// TestLogin_PersistsSessionToken mirrors the butleradm regression guard.
// butlerctl already persisted the session token; this locks that behavior in
// so a future refactor cannot regress it on either binary unnoticed.
func TestLogin_PersistsSessionToken(t *testing.T) {
	exp := time.Now().Add(time.Hour)

	t.Run("session token present is persisted", func(t *testing.T) {
		sc := authtest.RunMockLogin(t, runLogin, authtest.MockTokenResponse{
			SessionToken:     "sess-butlerctl-xyz789",
			SessionExpiresAt: exp,
			IncludeSession:   true,
		})
		if sc.SessionToken != "sess-butlerctl-xyz789" {
			t.Fatalf("SessionToken = %q, want sess-butlerctl-xyz789", sc.SessionToken)
		}
	})

	t.Run("session expiry is persisted", func(t *testing.T) {
		sc := authtest.RunMockLogin(t, runLogin, authtest.MockTokenResponse{
			SessionToken:     "sess-butlerctl-xyz789",
			SessionExpiresAt: exp,
			IncludeSession:   true,
		})
		if sc.SessionExpiresAt.IsZero() {
			t.Fatal("SessionExpiresAt is zero, want the server-provided expiry")
		}
		if sc.SessionExpiresAt.Unix() != exp.Unix() {
			t.Fatalf("SessionExpiresAt = %v, want %v", sc.SessionExpiresAt, exp)
		}
	})

	t.Run("missing session token yields empty", func(t *testing.T) {
		sc := authtest.RunMockLogin(t, runLogin, authtest.MockTokenResponse{
			IncludeSession: false,
		})
		if sc.SessionToken != "" {
			t.Fatalf("SessionToken = %q, want empty when server sends none", sc.SessionToken)
		}
	})
}
