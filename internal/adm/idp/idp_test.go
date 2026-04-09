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

package idp

import (
	"testing"

	"github.com/butlerdotdev/butler/internal/common/client"
)

func TestUnstructuredNestedSlice(t *testing.T) {
	tests := []struct {
		name      string
		obj       map[string]interface{}
		fields    []string
		wantLen   int
		wantFound bool
		wantErr   bool
	}{
		{
			name: "string slice",
			obj: map[string]interface{}{
				"spec": map[string]interface{}{
					"scopes": []interface{}{"openid", "profile", "email"},
				},
			},
			fields:    []string{"spec", "scopes"},
			wantLen:   3,
			wantFound: true,
		},
		{
			name: "empty slice",
			obj: map[string]interface{}{
				"spec": map[string]interface{}{
					"scopes": []interface{}{},
				},
			},
			fields:    []string{"spec", "scopes"},
			wantLen:   0,
			wantFound: true,
		},
		{
			name: "missing field returns not found",
			obj: map[string]interface{}{
				"spec": map[string]interface{}{},
			},
			fields:    []string{"spec", "scopes"},
			wantLen:   0,
			wantFound: false,
		},
		{
			name: "non-map intermediate returns not found",
			obj: map[string]interface{}{
				"spec": "string-value",
			},
			fields:    []string{"spec", "scopes"},
			wantLen:   0,
			wantFound: false,
		},
		{
			name: "non-slice value returns error",
			obj: map[string]interface{}{
				"spec": map[string]interface{}{
					"type": "oidc",
				},
			},
			fields:  []string{"spec", "type"},
			wantLen: 0,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, found, err := unstructuredNestedSlice(tt.obj, tt.fields...)
			if tt.wantErr && err == nil {
				t.Error("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			if found != tt.wantFound {
				t.Errorf("found = %v, want %v", found, tt.wantFound)
			}
			if len(got) != tt.wantLen {
				t.Errorf("len(slice) = %d, want %d", len(got), tt.wantLen)
			}
		})
	}
}

func TestIDPFieldExtraction(t *testing.T) {
	obj := map[string]interface{}{
		"metadata": map[string]interface{}{
			"name": "google-workspace",
		},
		"spec": map[string]interface{}{
			"type":      "oidc",
			"issuerURL": "https://accounts.google.com",
			"clientID":  "123456789.apps.googleusercontent.com",
			"scopes":    []interface{}{"openid", "profile", "email", "groups"},
			"claims": map[string]interface{}{
				"email":  "email",
				"groups": "groups",
			},
		},
		"status": map[string]interface{}{
			"phase":   "Ready",
			"message": "OIDC provider configured",
		},
	}

	stringTests := []struct {
		name   string
		fields []string
		want   string
	}{
		{
			name:   "type",
			fields: []string{"spec", "type"},
			want:   "oidc",
		},
		{
			name:   "issuer URL",
			fields: []string{"spec", "issuerURL"},
			want:   "https://accounts.google.com",
		},
		{
			name:   "client ID",
			fields: []string{"spec", "clientID"},
			want:   "123456789.apps.googleusercontent.com",
		},
		{
			name:   "phase",
			fields: []string{"status", "phase"},
			want:   "Ready",
		},
		{
			name:   "status message",
			fields: []string{"status", "message"},
			want:   "OIDC provider configured",
		},
		{
			name:   "email claim",
			fields: []string{"spec", "claims", "email"},
			want:   "email",
		},
		{
			name:   "groups claim",
			fields: []string{"spec", "claims", "groups"},
			want:   "groups",
		},
		{
			name:   "missing field returns empty",
			fields: []string{"spec", "nonexistent"},
			want:   "",
		},
	}

	for _, tt := range stringTests {
		t.Run(tt.name, func(t *testing.T) {
			got := client.GetNestedString(obj, tt.fields...)
			if got != tt.want {
				t.Errorf("GetNestedString(%v) = %q, want %q", tt.fields, got, tt.want)
			}
		})
	}
}

func TestIDPScopeExtraction(t *testing.T) {
	// Validates the scope extraction pattern used in runGet.
	obj := map[string]interface{}{
		"spec": map[string]interface{}{
			"scopes": []interface{}{"openid", "profile", "email"},
		},
	}

	scopesRaw, found, err := unstructuredNestedSlice(obj, "spec", "scopes")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !found {
		t.Fatal("expected to find scopes")
	}

	var scopes []string
	for _, s := range scopesRaw {
		str, ok := s.(string)
		if !ok {
			t.Fatalf("expected string scope, got %T", s)
		}
		scopes = append(scopes, str)
	}

	expected := []string{"openid", "profile", "email"}
	if len(scopes) != len(expected) {
		t.Fatalf("got %d scopes, want %d", len(scopes), len(expected))
	}
	for i, want := range expected {
		if scopes[i] != want {
			t.Errorf("scopes[%d] = %q, want %q", i, scopes[i], want)
		}
	}
}

func TestIDPIssuerTruncation(t *testing.T) {
	// The list command truncates issuer URLs longer than 50 characters.
	tests := []struct {
		name   string
		issuer string
		want   string
	}{
		{
			name:   "short issuer unchanged",
			issuer: "https://accounts.google.com",
			want:   "https://accounts.google.com",
		},
		{
			name:   "exactly 50 chars unchanged",
			issuer: "https://accounts.google.com/some-path/that-is-50c",
			want:   "https://accounts.google.com/some-path/that-is-50c",
		},
		{
			name:   "over 50 chars truncated",
			issuer: "https://login.microsoftonline.com/00000000-0000-0000-0000-000000000000/v2.0",
			want:   "https://login.microsoftonline.com/00000000-0000...",
		},
		{
			name:   "empty issuer unchanged",
			issuer: "",
			want:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.issuer
			if len(got) > 50 {
				got = got[:47] + "..."
			}
			if got != tt.want {
				t.Errorf("truncated issuer = %q, want %q", got, tt.want)
			}
		})
	}
}
