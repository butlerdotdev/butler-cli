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

package team

import (
	"testing"

	"github.com/butlerdotdev/butler/internal/common/client"
)

func TestTeamListFieldExtraction(t *testing.T) {
	// Validates that field extraction from Team objects produces
	// the expected table row values.
	tests := []struct {
		name             string
		obj              map[string]interface{}
		wantName         string
		wantDisplayName  string
		wantPhase        string
		wantClusterCount int64
		wantMemberCount  int64
		wantNamespace    string
	}{
		{
			name: "ready team with members",
			obj: map[string]interface{}{
				"metadata": map[string]interface{}{
					"name": "engineering",
				},
				"spec": map[string]interface{}{
					"displayName": "Engineering Team",
					"description": "The engineering team",
					"access": map[string]interface{}{
						"users": []interface{}{
							map[string]interface{}{"name": "admin@example.com", "role": "admin"},
							map[string]interface{}{"name": "dev@example.com", "role": "operator"},
						},
					},
				},
				"status": map[string]interface{}{
					"phase":        "Ready",
					"namespace":    "team-engineering",
					"clusterCount": int64(3),
					"memberCount":  int64(2),
				},
			},
			wantName:         "engineering",
			wantDisplayName:  "Engineering Team",
			wantPhase:        "Ready",
			wantClusterCount: 3,
			wantMemberCount:  2,
			wantNamespace:    "team-engineering",
		},
		{
			name: "pending team with no clusters",
			obj: map[string]interface{}{
				"metadata": map[string]interface{}{
					"name": "new-team",
				},
				"spec": map[string]interface{}{},
				"status": map[string]interface{}{
					"phase":     "Pending",
					"namespace": "team-new-team",
				},
			},
			wantName:         "new-team",
			wantDisplayName:  "",
			wantPhase:        "Pending",
			wantClusterCount: 0,
			wantMemberCount:  0,
			wantNamespace:    "team-new-team",
		},
		{
			name: "team with no status",
			obj: map[string]interface{}{
				"metadata": map[string]interface{}{
					"name": "empty-team",
				},
				"spec": map[string]interface{}{
					"displayName": "Empty",
				},
			},
			wantName:         "empty-team",
			wantDisplayName:  "Empty",
			wantPhase:        "",
			wantClusterCount: 0,
			wantMemberCount:  0,
			wantNamespace:    "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotName := client.GetNestedString(tt.obj, "metadata", "name")
			gotDisplayName := client.GetNestedString(tt.obj, "spec", "displayName")
			gotPhase := client.GetNestedString(tt.obj, "status", "phase")
			gotClusterCount := client.GetNestedInt64(tt.obj, "status", "clusterCount")
			gotMemberCount := client.GetNestedInt64(tt.obj, "status", "memberCount")
			gotNamespace := client.GetNestedString(tt.obj, "status", "namespace")

			if gotName != tt.wantName {
				t.Errorf("name: got %q, want %q", gotName, tt.wantName)
			}
			if gotDisplayName != tt.wantDisplayName {
				t.Errorf("displayName: got %q, want %q", gotDisplayName, tt.wantDisplayName)
			}
			if gotPhase != tt.wantPhase {
				t.Errorf("phase: got %q, want %q", gotPhase, tt.wantPhase)
			}
			if gotClusterCount != tt.wantClusterCount {
				t.Errorf("clusterCount: got %d, want %d", gotClusterCount, tt.wantClusterCount)
			}
			if gotMemberCount != tt.wantMemberCount {
				t.Errorf("memberCount: got %d, want %d", gotMemberCount, tt.wantMemberCount)
			}
			if gotNamespace != tt.wantNamespace {
				t.Errorf("namespace: got %q, want %q", gotNamespace, tt.wantNamespace)
			}
		})
	}
}

func TestTeamGetFieldExtraction(t *testing.T) {
	obj := map[string]interface{}{
		"metadata": map[string]interface{}{
			"name": "platform",
		},
		"spec": map[string]interface{}{
			"displayName": "Platform Team",
			"description": "Manages the Butler platform",
			"access": map[string]interface{}{
				"users": []interface{}{
					map[string]interface{}{"name": "admin@butler.dev", "role": "admin"},
					map[string]interface{}{"name": "ops@butler.dev", "role": "operator"},
					map[string]interface{}{"name": "viewer@butler.dev", "role": "viewer"},
				},
				"groups": []interface{}{
					map[string]interface{}{
						"name":             "platform-engineers",
						"role":             "admin",
						"identityProvider": "google-workspace",
					},
				},
			},
			"resourceLimits": map[string]interface{}{
				"maxClusters":        int64(10),
				"maxTotalNodes":      int64(50),
				"maxNodesPerCluster": int64(10),
			},
		},
		"status": map[string]interface{}{
			"phase":        "Ready",
			"namespace":    "team-platform",
			"clusterCount": int64(5),
			"memberCount":  int64(3),
			"quotaStatus":  "OK",
			"quotaMessage": "",
			"resourceUsage": map[string]interface{}{
				"clusters":   int64(5),
				"totalNodes": int64(20),
			},
		},
	}

	// Validate string fields
	stringTests := []struct {
		name   string
		fields []string
		want   string
	}{
		{
			name:   "display name",
			fields: []string{"spec", "displayName"},
			want:   "Platform Team",
		},
		{
			name:   "description",
			fields: []string{"spec", "description"},
			want:   "Manages the Butler platform",
		},
		{
			name:   "phase",
			fields: []string{"status", "phase"},
			want:   "Ready",
		},
		{
			name:   "namespace",
			fields: []string{"status", "namespace"},
			want:   "team-platform",
		},
		{
			name:   "quota status",
			fields: []string{"status", "quotaStatus"},
			want:   "OK",
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

	// Validate int64 fields
	int64Tests := []struct {
		name   string
		fields []string
		want   int64
	}{
		{
			name:   "cluster count",
			fields: []string{"status", "clusterCount"},
			want:   5,
		},
		{
			name:   "member count",
			fields: []string{"status", "memberCount"},
			want:   3,
		},
		{
			name:   "max clusters",
			fields: []string{"spec", "resourceLimits", "maxClusters"},
			want:   10,
		},
		{
			name:   "max total nodes",
			fields: []string{"spec", "resourceLimits", "maxTotalNodes"},
			want:   50,
		},
		{
			name:   "max nodes per cluster",
			fields: []string{"spec", "resourceLimits", "maxNodesPerCluster"},
			want:   10,
		},
		{
			name:   "usage clusters",
			fields: []string{"status", "resourceUsage", "clusters"},
			want:   5,
		},
		{
			name:   "usage total nodes",
			fields: []string{"status", "resourceUsage", "totalNodes"},
			want:   20,
		},
	}

	for _, tt := range int64Tests {
		t.Run(tt.name, func(t *testing.T) {
			got := client.GetNestedInt64(obj, tt.fields...)
			if got != tt.want {
				t.Errorf("GetNestedInt64(%v) = %d, want %d", tt.fields, got, tt.want)
			}
		})
	}
}

func TestTeamMemberExtraction(t *testing.T) {
	obj := map[string]interface{}{
		"spec": map[string]interface{}{
			"access": map[string]interface{}{
				"users": []interface{}{
					map[string]interface{}{"name": "admin@example.com", "role": "admin"},
					map[string]interface{}{"name": "dev@example.com", "role": "operator"},
					map[string]interface{}{"name": "viewer@example.com", "role": "viewer"},
				},
				"groups": []interface{}{
					map[string]interface{}{
						"name":             "engineers",
						"role":             "operator",
						"identityProvider": "google",
					},
					map[string]interface{}{
						"name": "viewers",
						"role": "viewer",
					},
				},
			},
		},
	}

	// Validate users extraction
	users, found := unstructuredNestedSlice(obj, "spec", "access", "users")
	if !found {
		t.Fatal("expected to find spec.access.users")
	}
	if len(users) != 3 {
		t.Fatalf("expected 3 users, got %d", len(users))
	}

	expectedUsers := []struct {
		email string
		role  string
	}{
		{"admin@example.com", "admin"},
		{"dev@example.com", "operator"},
		{"viewer@example.com", "viewer"},
	}

	for i, expected := range expectedUsers {
		uMap, ok := users[i].(map[string]interface{})
		if !ok {
			t.Fatalf("users[%d] is not a map", i)
		}
		email := client.GetNestedString(uMap, "name")
		role := client.GetNestedString(uMap, "role")
		if email != expected.email {
			t.Errorf("users[%d].name = %q, want %q", i, email, expected.email)
		}
		if role != expected.role {
			t.Errorf("users[%d].role = %q, want %q", i, role, expected.role)
		}
	}

	// Validate groups extraction
	groups, found := unstructuredNestedSlice(obj, "spec", "access", "groups")
	if !found {
		t.Fatal("expected to find spec.access.groups")
	}
	if len(groups) != 2 {
		t.Fatalf("expected 2 groups, got %d", len(groups))
	}

	gMap, ok := groups[0].(map[string]interface{})
	if !ok {
		t.Fatal("groups[0] is not a map")
	}
	if name := client.GetNestedString(gMap, "name"); name != "engineers" {
		t.Errorf("groups[0].name = %q, want %q", name, "engineers")
	}
	if role := client.GetNestedString(gMap, "role"); role != "operator" {
		t.Errorf("groups[0].role = %q, want %q", role, "operator")
	}
	if idp := client.GetNestedString(gMap, "identityProvider"); idp != "google" {
		t.Errorf("groups[0].identityProvider = %q, want %q", idp, "google")
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
			name: "users slice",
			obj: map[string]interface{}{
				"spec": map[string]interface{}{
					"access": map[string]interface{}{
						"users": []interface{}{
							map[string]interface{}{"name": "a@b.com", "role": "admin"},
						},
					},
				},
			},
			fields:    []string{"spec", "access", "users"},
			wantLen:   1,
			wantFound: true,
		},
		{
			name: "empty slice",
			obj: map[string]interface{}{
				"spec": map[string]interface{}{
					"access": map[string]interface{}{
						"users": []interface{}{},
					},
				},
			},
			fields:    []string{"spec", "access", "users"},
			wantLen:   0,
			wantFound: true,
		},
		{
			name: "missing field",
			obj: map[string]interface{}{
				"spec": map[string]interface{}{},
			},
			fields:    []string{"spec", "access", "users"},
			wantLen:   0,
			wantFound: false,
		},
		{
			name: "path through non-map",
			obj: map[string]interface{}{
				"spec": "not-a-map",
			},
			fields:    []string{"spec", "access"},
			wantLen:   0,
			wantFound: false,
		},
		{
			name: "value is not a slice",
			obj: map[string]interface{}{
				"spec": map[string]interface{}{
					"displayName": "Engineering",
				},
			},
			fields:    []string{"spec", "displayName"},
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
