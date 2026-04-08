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

package output

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"sigs.k8s.io/yaml"
)

type testCluster struct {
	Name    string `json:"name"`
	Workers int    `json:"workers"`
	Phase   string `json:"phase"`
}

func TestPrintJSON(t *testing.T) {
	tests := []struct {
		name      string
		data      interface{}
		wantErr   bool
		checkFunc func(t *testing.T, output string)
	}{
		{
			name: "struct produces valid JSON",
			data: testCluster{
				Name:    "prod-cluster",
				Workers: 3,
				Phase:   "Ready",
			},
			wantErr: false,
			checkFunc: func(t *testing.T, output string) {
				var decoded testCluster
				if err := json.Unmarshal([]byte(output), &decoded); err != nil {
					t.Fatalf("output is not valid JSON: %v", err)
				}
				if decoded.Name != "prod-cluster" {
					t.Errorf("name = %q, want %q", decoded.Name, "prod-cluster")
				}
				if decoded.Workers != 3 {
					t.Errorf("workers = %d, want %d", decoded.Workers, 3)
				}
				if decoded.Phase != "Ready" {
					t.Errorf("phase = %q, want %q", decoded.Phase, "Ready")
				}
			},
		},
		{
			name:    "nil data produces JSON null",
			data:    nil,
			wantErr: false,
			checkFunc: func(t *testing.T, output string) {
				trimmed := strings.TrimSpace(output)
				if trimmed != "null" {
					t.Errorf("got %q, want %q", trimmed, "null")
				}
			},
		},
		{
			name: "slice of structs",
			data: []testCluster{
				{Name: "cluster-a", Workers: 1, Phase: "Ready"},
				{Name: "cluster-b", Workers: 5, Phase: "Provisioning"},
			},
			wantErr: false,
			checkFunc: func(t *testing.T, output string) {
				var decoded []testCluster
				if err := json.Unmarshal([]byte(output), &decoded); err != nil {
					t.Fatalf("output is not valid JSON: %v", err)
				}
				if len(decoded) != 2 {
					t.Fatalf("got %d items, want 2", len(decoded))
				}
				if decoded[0].Name != "cluster-a" {
					t.Errorf("first item name = %q, want %q", decoded[0].Name, "cluster-a")
				}
			},
		},
		{
			name:    "empty struct",
			data:    testCluster{},
			wantErr: false,
			checkFunc: func(t *testing.T, output string) {
				var decoded testCluster
				if err := json.Unmarshal([]byte(output), &decoded); err != nil {
					t.Fatalf("output is not valid JSON: %v", err)
				}
				if decoded.Name != "" {
					t.Errorf("name = %q, want empty", decoded.Name)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			err := PrintJSON(&buf, tt.data)
			if (err != nil) != tt.wantErr {
				t.Fatalf("PrintJSON() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.checkFunc != nil {
				tt.checkFunc(t, buf.String())
			}
		})
	}
}

func TestPrintYAML(t *testing.T) {
	tests := []struct {
		name      string
		data      interface{}
		wantErr   bool
		checkFunc func(t *testing.T, output string)
	}{
		{
			name: "struct produces valid YAML",
			data: testCluster{
				Name:    "prod-cluster",
				Workers: 3,
				Phase:   "Ready",
			},
			wantErr: false,
			checkFunc: func(t *testing.T, output string) {
				var decoded testCluster
				if err := yaml.Unmarshal([]byte(output), &decoded); err != nil {
					t.Fatalf("output is not valid YAML: %v", err)
				}
				if decoded.Name != "prod-cluster" {
					t.Errorf("name = %q, want %q", decoded.Name, "prod-cluster")
				}
				if decoded.Workers != 3 {
					t.Errorf("workers = %d, want %d", decoded.Workers, 3)
				}
				if decoded.Phase != "Ready" {
					t.Errorf("phase = %q, want %q", decoded.Phase, "Ready")
				}
			},
		},
		{
			name:    "nil data produces YAML null",
			data:    nil,
			wantErr: false,
			checkFunc: func(t *testing.T, output string) {
				trimmed := strings.TrimSpace(output)
				if trimmed != "null" {
					t.Errorf("got %q, want %q", trimmed, "null")
				}
			},
		},
		{
			name: "slice of structs",
			data: []testCluster{
				{Name: "cluster-a", Workers: 1, Phase: "Ready"},
				{Name: "cluster-b", Workers: 5, Phase: "Provisioning"},
			},
			wantErr: false,
			checkFunc: func(t *testing.T, output string) {
				var decoded []testCluster
				if err := yaml.Unmarshal([]byte(output), &decoded); err != nil {
					t.Fatalf("output is not valid YAML: %v", err)
				}
				if len(decoded) != 2 {
					t.Fatalf("got %d items, want 2", len(decoded))
				}
				if decoded[1].Phase != "Provisioning" {
					t.Errorf("second item phase = %q, want %q", decoded[1].Phase, "Provisioning")
				}
			},
		},
		{
			name: "YAML output contains expected field names",
			data: testCluster{
				Name:    "my-cluster",
				Workers: 2,
				Phase:   "Pending",
			},
			wantErr: false,
			checkFunc: func(t *testing.T, output string) {
				if !strings.Contains(output, "name: my-cluster") {
					t.Errorf("YAML output missing expected field: %s", output)
				}
				if !strings.Contains(output, "workers: 2") {
					t.Errorf("YAML output missing expected field: %s", output)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			err := PrintYAML(&buf, tt.data)
			if (err != nil) != tt.wantErr {
				t.Fatalf("PrintYAML() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.checkFunc != nil {
				tt.checkFunc(t, buf.String())
			}
		})
	}
}

func TestParseFormat(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected Format
		wantErr  bool
	}{
		{name: "table", input: "table", expected: FormatTable},
		{name: "empty defaults to table", input: "", expected: FormatTable},
		{name: "json", input: "json", expected: FormatJSON},
		{name: "yaml", input: "yaml", expected: FormatYAML},
		{name: "wide", input: "wide", expected: FormatWide},
		{name: "case insensitive", input: "JSON", expected: FormatJSON},
		{name: "unknown format", input: "xml", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseFormat(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ParseFormat(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
			if !tt.wantErr && got != tt.expected {
				t.Errorf("ParseFormat(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}
