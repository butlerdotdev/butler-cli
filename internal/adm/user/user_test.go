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

package user

import (
	"testing"

	"github.com/butlerdotdev/butler/internal/common/client"
)

func TestUserListFieldExtraction(t *testing.T) {
	// Validates that field extraction from User objects produces
	// the expected table row values.
	tests := []struct {
		name            string
		obj             map[string]interface{}
		wantName        string
		wantEmail       string
		wantDisplayName string
		wantPhase       string
		wantAuthType    string
		wantAdmin       bool
	}{
		{
			name: "active internal admin user",
			obj: map[string]interface{}{
				"metadata": map[string]interface{}{
					"name": "john-doe",
				},
				"spec": map[string]interface{}{
					"email":           "john.doe@example.com",
					"displayName":    "John Doe",
					"authType":       "internal",
					"isPlatformAdmin": true,
				},
				"status": map[string]interface{}{
					"phase": "Active",
				},
			},
			wantName:        "john-doe",
			wantEmail:       "john.doe@example.com",
			wantDisplayName: "John Doe",
			wantPhase:       "Active",
			wantAuthType:    "internal",
			wantAdmin:       true,
		},
		{
			name: "SSO user",
			obj: map[string]interface{}{
				"metadata": map[string]interface{}{
					"name": "jane-smith",
				},
				"spec": map[string]interface{}{
					"email":       "jane.smith@example.com",
					"displayName": "Jane Smith",
					"authType":    "sso",
					"ssoProvider": "Google",
				},
				"status": map[string]interface{}{
					"phase": "Active",
				},
			},
			wantName:        "jane-smith",
			wantEmail:       "jane.smith@example.com",
			wantDisplayName: "Jane Smith",
			wantPhase:       "Active",
			wantAuthType:    "sso",
			wantAdmin:       false,
		},
		{
			name: "pending user with minimal fields",
			obj: map[string]interface{}{
				"metadata": map[string]interface{}{
					"name": "new-user",
				},
				"spec": map[string]interface{}{
					"email":    "new@example.com",
					"authType": "internal",
				},
				"status": map[string]interface{}{
					"phase": "Pending",
				},
			},
			wantName:        "new-user",
			wantEmail:       "new@example.com",
			wantDisplayName: "",
			wantPhase:       "Pending",
			wantAuthType:    "internal",
			wantAdmin:       false,
		},
		{
			name: "disabled user",
			obj: map[string]interface{}{
				"metadata": map[string]interface{}{
					"name": "disabled-user",
				},
				"spec": map[string]interface{}{
					"email":    "disabled@example.com",
					"authType": "internal",
					"disabled": true,
				},
				"status": map[string]interface{}{
					"phase": "Disabled",
				},
			},
			wantName:        "disabled-user",
			wantEmail:       "disabled@example.com",
			wantDisplayName: "",
			wantPhase:       "Disabled",
			wantAuthType:    "internal",
			wantAdmin:       false,
		},
		{
			name: "user with no status",
			obj: map[string]interface{}{
				"metadata": map[string]interface{}{
					"name": "no-status",
				},
				"spec": map[string]interface{}{
					"email":    "nostatus@example.com",
					"authType": "internal",
				},
			},
			wantName:        "no-status",
			wantEmail:       "nostatus@example.com",
			wantDisplayName: "",
			wantPhase:       "",
			wantAuthType:    "internal",
			wantAdmin:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotName := client.GetNestedString(tt.obj, "metadata", "name")
			gotEmail := client.GetNestedString(tt.obj, "spec", "email")
			gotDisplayName := client.GetNestedString(tt.obj, "spec", "displayName")
			gotPhase := client.GetNestedString(tt.obj, "status", "phase")
			gotAuthType := client.GetNestedString(tt.obj, "spec", "authType")
			gotAdmin := client.GetNestedBool(tt.obj, "spec", "isPlatformAdmin")

			if gotName != tt.wantName {
				t.Errorf("name: got %q, want %q", gotName, tt.wantName)
			}
			if gotEmail != tt.wantEmail {
				t.Errorf("email: got %q, want %q", gotEmail, tt.wantEmail)
			}
			if gotDisplayName != tt.wantDisplayName {
				t.Errorf("displayName: got %q, want %q", gotDisplayName, tt.wantDisplayName)
			}
			if gotPhase != tt.wantPhase {
				t.Errorf("phase: got %q, want %q", gotPhase, tt.wantPhase)
			}
			if gotAuthType != tt.wantAuthType {
				t.Errorf("authType: got %q, want %q", gotAuthType, tt.wantAuthType)
			}
			if gotAdmin != tt.wantAdmin {
				t.Errorf("isPlatformAdmin: got %v, want %v", gotAdmin, tt.wantAdmin)
			}
		})
	}
}

func TestUserGetFieldExtraction(t *testing.T) {
	obj := map[string]interface{}{
		"metadata": map[string]interface{}{
			"name": "john-doe",
		},
		"spec": map[string]interface{}{
			"email":           "john.doe@example.com",
			"displayName":    "John Doe",
			"authType":       "sso",
			"ssoProvider":    "Google",
			"isPlatformAdmin": true,
			"disabled":       false,
		},
		"status": map[string]interface{}{
			"phase":         "Active",
			"lastLoginTime": "2026-04-08T10:30:00Z",
			"loginCount":    int64(42),
			"teams": []interface{}{
				map[string]interface{}{"name": "engineering", "role": "admin"},
				map[string]interface{}{"name": "platform", "role": "operator"},
			},
		},
	}

	stringTests := []struct {
		name   string
		fields []string
		want   string
	}{
		{
			name:   "email",
			fields: []string{"spec", "email"},
			want:   "john.doe@example.com",
		},
		{
			name:   "display name",
			fields: []string{"spec", "displayName"},
			want:   "John Doe",
		},
		{
			name:   "auth type",
			fields: []string{"spec", "authType"},
			want:   "sso",
		},
		{
			name:   "SSO provider",
			fields: []string{"spec", "ssoProvider"},
			want:   "Google",
		},
		{
			name:   "phase",
			fields: []string{"status", "phase"},
			want:   "Active",
		},
		{
			name:   "last login",
			fields: []string{"status", "lastLoginTime"},
			want:   "2026-04-08T10:30:00Z",
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

	// Validate bool fields
	isAdmin := client.GetNestedBool(obj, "spec", "isPlatformAdmin")
	if !isAdmin {
		t.Error("isPlatformAdmin: got false, want true")
	}
	disabled := client.GetNestedBool(obj, "spec", "disabled")
	if disabled {
		t.Error("disabled: got true, want false")
	}

	// Validate int64 fields
	loginCount := client.GetNestedInt64(obj, "status", "loginCount")
	if loginCount != 42 {
		t.Errorf("loginCount: got %d, want 42", loginCount)
	}
}

func TestUserTeamMembershipExtraction(t *testing.T) {
	obj := map[string]interface{}{
		"status": map[string]interface{}{
			"teams": []interface{}{
				map[string]interface{}{"name": "engineering", "role": "admin"},
				map[string]interface{}{"name": "platform", "role": "operator"},
				map[string]interface{}{"name": "qa", "role": "viewer"},
			},
		},
	}

	teams, found := unstructuredNestedSlice(obj, "status", "teams")
	if !found {
		t.Fatal("expected to find status.teams")
	}
	if len(teams) != 3 {
		t.Fatalf("expected 3 teams, got %d", len(teams))
	}

	expectedTeams := []struct {
		name string
		role string
	}{
		{"engineering", "admin"},
		{"platform", "operator"},
		{"qa", "viewer"},
	}

	for i, expected := range expectedTeams {
		tMap, ok := teams[i].(map[string]interface{})
		if !ok {
			t.Fatalf("teams[%d] is not a map", i)
		}
		name := client.GetNestedString(tMap, "name")
		role := client.GetNestedString(tMap, "role")
		if name != expected.name {
			t.Errorf("teams[%d].name = %q, want %q", i, name, expected.name)
		}
		if role != expected.role {
			t.Errorf("teams[%d].role = %q, want %q", i, role, expected.role)
		}
	}
}

func TestUserTeamMembershipMissing(t *testing.T) {
	obj := map[string]interface{}{
		"status": map[string]interface{}{
			"phase": "Active",
		},
	}

	teams, found := unstructuredNestedSlice(obj, "status", "teams")
	if found {
		t.Errorf("expected not found, got found with %d teams", len(teams))
	}
}

func TestEmailToResourceName(t *testing.T) {
	tests := []struct {
		email string
		want  string
	}{
		{
			email: "john.doe@example.com",
			want:  "john-doe",
		},
		{
			email: "admin@butler.dev",
			want:  "admin",
		},
		{
			email: "UPPER.CASE@example.com",
			want:  "upper-case",
		},
		{
			email: "user+tag@example.com",
			want:  "user-tag",
		},
		{
			email: "user_name@example.com",
			want:  "user-name",
		},
		{
			email: "simple",
			want:  "simple",
		},
		{
			email: "a.b.c.d@example.com",
			want:  "a-b-c-d",
		},
	}

	for _, tt := range tests {
		t.Run(tt.email, func(t *testing.T) {
			got := emailToResourceName(tt.email)
			if got != tt.want {
				t.Errorf("emailToResourceName(%q) = %q, want %q", tt.email, got, tt.want)
			}
		})
	}
}

func TestUnstructuredNestedSlice(t *testing.T) {
	tests := []struct {
		name      string
		obj       map[string]interface{}
		fields    []string
		wantLen   int
		wantFound bool
	}{
		{
			name: "teams slice",
			obj: map[string]interface{}{
				"status": map[string]interface{}{
					"teams": []interface{}{
						map[string]interface{}{"name": "eng", "role": "admin"},
					},
				},
			},
			fields:    []string{"status", "teams"},
			wantLen:   1,
			wantFound: true,
		},
		{
			name: "empty slice",
			obj: map[string]interface{}{
				"status": map[string]interface{}{
					"teams": []interface{}{},
				},
			},
			fields:    []string{"status", "teams"},
			wantLen:   0,
			wantFound: true,
		},
		{
			name: "missing field",
			obj: map[string]interface{}{
				"status": map[string]interface{}{},
			},
			fields:    []string{"status", "teams"},
			wantLen:   0,
			wantFound: false,
		},
		{
			name: "path through non-map",
			obj: map[string]interface{}{
				"status": "not-a-map",
			},
			fields:    []string{"status", "teams"},
			wantLen:   0,
			wantFound: false,
		},
		{
			name: "value is not a slice",
			obj: map[string]interface{}{
				"status": map[string]interface{}{
					"phase": "Active",
				},
			},
			fields:    []string{"status", "phase"},
			wantLen:   0,
			wantFound: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, found := unstructuredNestedSlice(tt.obj, tt.fields...)
			if found != tt.wantFound {
				t.Errorf("found = %v, want %v", found, tt.wantFound)
			}
			if len(got) != tt.wantLen {
				t.Errorf("len(slice) = %d, want %d", len(got), tt.wantLen)
			}
		})
	}
}
