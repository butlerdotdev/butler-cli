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

func TestBuildTalosSpec_WithMTU(t *testing.T) {
	cfg := &Config{
		Talos: TalosConfig{
			Version:   "v1.12.4",
			Schematic: "abc123",
		},
		Network: NetworkConfig{
			MTU: 1380,
		},
	}

	spec := buildTalosSpec(cfg)

	patches, ok := spec["configPatches"].([]interface{})
	if !ok || len(patches) != 1 {
		t.Fatalf("configPatches = %v, want 1-element slice", spec["configPatches"])
	}

	patch := patches[0].(map[string]interface{})
	if patch["op"] != "add" {
		t.Errorf("op = %v, want add", patch["op"])
	}
	// The patch targets the full interfaces array (not a deep index).
	// Talos's default generated config has no interfaces array, so
	// patching a deeper path fails RFC6902 validation.
	if patch["path"] != "/machine/network/interfaces" {
		t.Errorf("path = %v, want /machine/network/interfaces", patch["path"])
	}

	// Value must be a JSON array string with a single interface entry
	// whose deviceSelector matches any physical NIC.
	valueStr, ok := patch["value"].(string)
	if !ok {
		t.Fatalf("value is not a string: %T", patch["value"])
	}
	var parsed []map[string]interface{}
	if err := json.Unmarshal([]byte(valueStr), &parsed); err != nil {
		t.Fatalf("value %q is not valid JSON: %v", valueStr, err)
	}
	if len(parsed) != 1 {
		t.Fatalf("interfaces length = %d, want 1", len(parsed))
	}
	iface := parsed[0]
	// MTU round-trips through JSON as float64.
	mtu, ok := iface["mtu"].(float64)
	if !ok || int(mtu) != 1380 {
		t.Errorf("mtu = %v, want 1380", iface["mtu"])
	}
	sel, ok := iface["deviceSelector"].(map[string]interface{})
	if !ok {
		t.Fatalf("deviceSelector = %v, want map", iface["deviceSelector"])
	}
	if phys, _ := sel["physical"].(bool); !phys {
		t.Errorf("deviceSelector.physical = %v, want true", sel["physical"])
	}
	// dhcp:true must be present. Once an interfaces entry exists, Talos
	// stops auto-enabling DHCP on physical NICs, so the MTU override has
	// to carry addressing itself or the node boots with no IP lease.
	if dhcp, _ := iface["dhcp"].(bool); !dhcp {
		t.Errorf("dhcp = %v, want true", iface["dhcp"])
	}
}

func TestBuildTalosSpec_WithoutMTU(t *testing.T) {
	cfg := &Config{
		Talos: TalosConfig{
			Version:   "v1.12.4",
			Schematic: "abc123",
		},
		Network: NetworkConfig{MTU: 0},
	}

	spec := buildTalosSpec(cfg)

	if _, ok := spec["configPatches"]; ok {
		t.Error("configPatches should not be present when MTU is zero and no other patches are set")
	}
}

func TestBuildTalosSpec_MTUAndTimeServers(t *testing.T) {
	cfg := &Config{
		Talos: TalosConfig{
			Version:     "v1.12.4",
			Schematic:   "abc123",
			TimeServers: []string{"10.92.92.2"},
		},
		Network: NetworkConfig{MTU: 1380},
	}

	spec := buildTalosSpec(cfg)

	patches, ok := spec["configPatches"].([]interface{})
	if !ok || len(patches) != 2 {
		t.Fatalf("configPatches = %v, want 2-element slice", spec["configPatches"])
	}

	// Both patches must be present, but order between them isn't
	// load-bearing: the bootstrap controller ships each patch as its
	// own --config-patch arg, independent of the others. Assert on the
	// set of paths, not their indexes, so future reorderings of
	// buildTalosSpec don't break the test.
	paths := make(map[string]bool, len(patches))
	for _, p := range patches {
		paths[p.(map[string]interface{})["path"].(string)] = true
	}
	if !paths["/machine/time"] {
		t.Errorf("missing /machine/time patch, got paths %v", paths)
	}
	if !paths["/machine/network/interfaces"] {
		t.Errorf("missing /machine/network/interfaces patch, got paths %v", paths)
	}
}
