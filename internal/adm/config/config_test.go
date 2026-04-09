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

package config

import (
	"encoding/json"
	"testing"
)

func TestSetNestedField(t *testing.T) {
	tests := []struct {
		name     string
		keys     []string
		value    interface{}
		wantJSON string
	}{
		{
			name:     "single key",
			keys:     []string{"mode"},
			value:    "LoadBalancer",
			wantJSON: `{"mode":"LoadBalancer"}`,
		},
		{
			name:     "two-level path",
			keys:     []string{"spec", "defaultProvider"},
			value:    "harvester-prod",
			wantJSON: `{"spec":{"defaultProvider":"harvester-prod"}}`,
		},
		{
			name:     "three-level path",
			keys:     []string{"spec", "multiTenancy", "enabled"},
			value:    true,
			wantJSON: `{"spec":{"multiTenancy":{"enabled":true}}}`,
		},
		{
			name:     "boolean false value",
			keys:     []string{"spec", "multiTenancy", "enabled"},
			value:    false,
			wantJSON: `{"spec":{"multiTenancy":{"enabled":false}}}`,
		},
		{
			name:     "string value at deep path",
			keys:     []string{"spec", "gitProvider", "type"},
			value:    "github",
			wantJSON: `{"spec":{"gitProvider":{"type":"github"}}}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := make(map[string]interface{})
			setNestedField(m, tt.keys, tt.value)

			got, err := json.Marshal(m)
			if err != nil {
				t.Fatalf("failed to marshal result: %v", err)
			}
			if string(got) != tt.wantJSON {
				t.Errorf("setNestedField() =\n  %s\nwant:\n  %s", string(got), tt.wantJSON)
			}
		})
	}
}

func TestSetNestedField_MultipleKeys(t *testing.T) {
	// Verify that multiple calls to setNestedField on the same map merge
	// correctly rather than overwriting sibling keys.
	m := make(map[string]interface{})
	setNestedField(m, []string{"spec", "mode"}, "LoadBalancer")
	setNestedField(m, []string{"spec", "defaultProvider"}, "harvester-prod")

	got, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("failed to marshal result: %v", err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(got, &result); err != nil {
		t.Fatalf("failed to unmarshal result: %v", err)
	}

	spec, ok := result["spec"].(map[string]interface{})
	if !ok {
		t.Fatal("expected spec to be a map")
	}
	if spec["mode"] != "LoadBalancer" {
		t.Errorf("spec.mode = %v, want LoadBalancer", spec["mode"])
	}
	if spec["defaultProvider"] != "harvester-prod" {
		t.Errorf("spec.defaultProvider = %v, want harvester-prod", spec["defaultProvider"])
	}
}

func TestOrDefault(t *testing.T) {
	tests := []struct {
		name string
		s    string
		def  string
		want string
	}{
		{
			name: "non-empty returns value",
			s:    "Ready",
			def:  "<unknown>",
			want: "Ready",
		},
		{
			name: "empty returns default",
			s:    "",
			def:  "<not set>",
			want: "<not set>",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := orDefault(tt.s, tt.def)
			if got != tt.want {
				t.Errorf("orDefault(%q, %q) = %q, want %q", tt.s, tt.def, got, tt.want)
			}
		})
	}
}

func TestRunSet_KVParsing(t *testing.T) {
	// Test the KEY=VALUE parsing and value type coercion that happens
	// at the start of runSet. We replicate the parsing logic here since
	// runSet itself needs a K8s client, but the parsing is the interesting
	// part to validate.
	tests := []struct {
		name      string
		kvPairs   []string
		wantJSON  string
		wantErr   bool
		errSubstr string
	}{
		{
			name:     "simple string value",
			kvPairs:  []string{"spec.defaultProvider=harvester-prod"},
			wantJSON: `{"spec":{"defaultProvider":"harvester-prod"}}`,
		},
		{
			name:     "boolean true",
			kvPairs:  []string{"spec.multiTenancy.enabled=true"},
			wantJSON: `{"spec":{"multiTenancy":{"enabled":true}}}`,
		},
		{
			name:     "boolean false",
			kvPairs:  []string{"spec.multiTenancy.enabled=false"},
			wantJSON: `{"spec":{"multiTenancy":{"enabled":false}}}`,
		},
		{
			name:     "boolean TRUE case-insensitive",
			kvPairs:  []string{"spec.multiTenancy.enabled=TRUE"},
			wantJSON: `{"spec":{"multiTenancy":{"enabled":true}}}`,
		},
		{
			name:     "boolean False case-insensitive",
			kvPairs:  []string{"spec.multiTenancy.enabled=False"},
			wantJSON: `{"spec":{"multiTenancy":{"enabled":false}}}`,
		},
		{
			name:    "multiple pairs",
			kvPairs: []string{"spec.mode=LoadBalancer", "spec.defaultProvider=nutanix-prod"},
			// Both should end up under spec
			wantJSON: `{"spec":{"defaultProvider":"nutanix-prod","mode":"LoadBalancer"}}`,
		},
		{
			name:      "missing equals sign",
			kvPairs:   []string{"spec.mode"},
			wantErr:   true,
			errSubstr: "expected KEY=VALUE format",
		},
		{
			name:     "value containing equals sign",
			kvPairs:  []string{"spec.annotation=key=value"},
			wantJSON: `{"spec":{"annotation":"key=value"}}`,
		},
		{
			name:     "empty value after equals",
			kvPairs:  []string{"spec.mode="},
			wantJSON: `{"spec":{"mode":""}}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			patch, err := parseKVPairs(tt.kvPairs)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if tt.errSubstr != "" {
					if got := err.Error(); !contains(got, tt.errSubstr) {
						t.Errorf("error %q does not contain %q", got, tt.errSubstr)
					}
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			got, err := json.Marshal(patch)
			if err != nil {
				t.Fatalf("failed to marshal patch: %v", err)
			}
			if string(got) != tt.wantJSON {
				t.Errorf("parseKVPairs() =\n  %s\nwant:\n  %s", string(got), tt.wantJSON)
			}
		})
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
