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

package orchestrator

import (
	"encoding/json"
	"testing"
)

func TestBuildTalosSpec_WithTimeServers(t *testing.T) {
	cfg := &Config{
		Talos: TalosConfig{
			Version:     "v1.12.4",
			Schematic:   "abc123",
			TimeServers: []string{"10.92.92.2", "10.92.92.4"},
		},
	}

	spec := buildTalosSpec(cfg)

	if spec["version"] != "v1.12.4" {
		t.Errorf("version = %v", spec["version"])
	}
	if spec["schematic"] != "abc123" {
		t.Errorf("schematic = %v", spec["schematic"])
	}

	patches, ok := spec["configPatches"].([]interface{})
	if !ok || len(patches) != 1 {
		t.Fatalf("configPatches = %v, want 1-element slice", spec["configPatches"])
	}

	patch := patches[0].(map[string]interface{})
	if patch["op"] != "add" {
		t.Errorf("op = %v, want add", patch["op"])
	}
	if patch["path"] != "/machine/time" {
		t.Errorf("path = %v, want /machine/time", patch["path"])
	}

	// Value should be valid JSON with a servers array.
	valueStr, ok := patch["value"].(string)
	if !ok {
		t.Fatalf("value is not a string: %T", patch["value"])
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(valueStr), &parsed); err != nil {
		t.Fatalf("value is not valid JSON: %v", err)
	}

	servers, ok := parsed["servers"].([]interface{})
	if !ok || len(servers) != 2 {
		t.Fatalf("servers = %v, want 2-element array", parsed["servers"])
	}
	if servers[0] != "10.92.92.2" || servers[1] != "10.92.92.4" {
		t.Errorf("servers = %v", servers)
	}
}

func TestBuildTalosSpec_WithoutTimeServers(t *testing.T) {
	cfg := &Config{
		Talos: TalosConfig{
			Version:   "v1.12.4",
			Schematic: "abc123",
		},
	}

	spec := buildTalosSpec(cfg)

	if _, ok := spec["configPatches"]; ok {
		t.Error("configPatches should not be present when TimeServers is empty")
	}
}
