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

package network

import (
	"fmt"
	"testing"

	"github.com/butlerdotdev/butler/internal/common/client"
)

func TestUtilizationCalculation(t *testing.T) {
	// The utilization calculation in runGet computes a percentage from
	// totalIPs and allocatedIPs extracted from the NetworkPool status.
	// This test validates the arithmetic using the same formula.
	tests := []struct {
		name         string
		totalIPs     int64
		allocatedIPs int64
		wantPct      string
		wantAvail    int64
	}{
		{
			name:         "empty pool",
			totalIPs:     512,
			allocatedIPs: 0,
			wantPct:      "0.0%",
			wantAvail:    512,
		},
		{
			name:         "fully allocated",
			totalIPs:     256,
			allocatedIPs: 256,
			wantPct:      "100.0%",
			wantAvail:    0,
		},
		{
			name:         "half allocated",
			totalIPs:     100,
			allocatedIPs: 50,
			wantPct:      "50.0%",
			wantAvail:    50,
		},
		{
			name:         "fractional percentage",
			totalIPs:     300,
			allocatedIPs: 100,
			wantPct:      "33.3%",
			wantAvail:    200,
		},
		{
			name:         "zero total IPs",
			totalIPs:     0,
			allocatedIPs: 0,
			wantPct:      "0.0%",
			wantAvail:    0,
		},
		{
			name:         "one IP allocated of one",
			totalIPs:     1,
			allocatedIPs: 1,
			wantPct:      "100.0%",
			wantAvail:    0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			available := tt.totalIPs - tt.allocatedIPs
			if available != tt.wantAvail {
				t.Errorf("available = %d, want %d", available, tt.wantAvail)
			}

			var utilization string
			if tt.totalIPs > 0 {
				pct := float64(tt.allocatedIPs) / float64(tt.totalIPs) * 100
				utilization = fmt.Sprintf("%.1f%%", pct)
			} else {
				utilization = "0.0%"
			}
			if utilization != tt.wantPct {
				t.Errorf("utilization = %q, want %q", utilization, tt.wantPct)
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
			name: "slice at top level",
			obj: map[string]interface{}{
				"items": []interface{}{"a", "b", "c"},
			},
			fields:    []string{"items"},
			wantLen:   3,
			wantFound: true,
		},
		{
			name: "nested slice",
			obj: map[string]interface{}{
				"spec": map[string]interface{}{
					"reserved": []interface{}{
						map[string]interface{}{"start": "10.0.0.1", "end": "10.0.0.10"},
					},
				},
			},
			fields:    []string{"spec", "reserved"},
			wantLen:   1,
			wantFound: true,
		},
		{
			name: "empty slice",
			obj: map[string]interface{}{
				"spec": map[string]interface{}{
					"reserved": []interface{}{},
				},
			},
			fields:    []string{"spec", "reserved"},
			wantLen:   0,
			wantFound: true,
		},
		{
			name: "missing field",
			obj: map[string]interface{}{
				"spec": map[string]interface{}{},
			},
			fields:    []string{"spec", "reserved"},
			wantLen:   0,
			wantFound: false,
		},
		{
			name: "path through non-map",
			obj: map[string]interface{}{
				"spec": "not-a-map",
			},
			fields:    []string{"spec", "reserved"},
			wantLen:   0,
			wantFound: false,
		},
		{
			name: "value is not a slice",
			obj: map[string]interface{}{
				"spec": map[string]interface{}{
					"cidr": "10.40.0.0/23",
				},
			},
			fields:    []string{"spec", "cidr"},
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

func TestNetworkPoolFieldExtraction(t *testing.T) {
	// Validates that GetNestedString/GetNestedInt64 correctly extract
	// fields from a NetworkPool-shaped unstructured object.
	obj := map[string]interface{}{
		"metadata": map[string]interface{}{
			"name":      "node-pool",
			"namespace": "butler-system",
		},
		"spec": map[string]interface{}{
			"cidr": "10.40.0.0/23",
			"reserved": []interface{}{
				map[string]interface{}{
					"start":       "10.40.0.1",
					"end":         "10.40.0.10",
					"description": "infrastructure",
				},
			},
			"tenantAllocation": map[string]interface{}{
				"start": "10.40.0.20",
				"end":   "10.40.1.254",
			},
		},
		"status": map[string]interface{}{
			"totalIPs":      int64(512),
			"allocatedIPs":  int64(40),
			"fragmentation": "low",
		},
	}

	tests := []struct {
		name   string
		fields []string
		want   string
	}{
		{
			name:   "cidr",
			fields: []string{"spec", "cidr"},
			want:   "10.40.0.0/23",
		},
		{
			name:   "fragmentation",
			fields: []string{"status", "fragmentation"},
			want:   "low",
		},
		{
			name:   "tenant allocation start",
			fields: []string{"spec", "tenantAllocation", "start"},
			want:   "10.40.0.20",
		},
		{
			name:   "tenant allocation end",
			fields: []string{"spec", "tenantAllocation", "end"},
			want:   "10.40.1.254",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := client.GetNestedString(obj, tt.fields...)
			if got != tt.want {
				t.Errorf("GetNestedString(%v) = %q, want %q", tt.fields, got, tt.want)
			}
		})
	}

	// Validate int64 fields
	totalIPs := client.GetNestedInt64(obj, "status", "totalIPs")
	if totalIPs != 512 {
		t.Errorf("totalIPs = %d, want 512", totalIPs)
	}
	allocatedIPs := client.GetNestedInt64(obj, "status", "allocatedIPs")
	if allocatedIPs != 40 {
		t.Errorf("allocatedIPs = %d, want 40", allocatedIPs)
	}

	// Validate reserved range extraction
	reserved, found := unstructuredNestedSlice(obj, "spec", "reserved")
	if !found {
		t.Fatal("expected to find spec.reserved slice")
	}
	if len(reserved) != 1 {
		t.Fatalf("expected 1 reserved range, got %d", len(reserved))
	}
	rMap, ok := reserved[0].(map[string]interface{})
	if !ok {
		t.Fatal("expected reserved[0] to be a map")
	}
	if start := client.GetNestedString(rMap, "start"); start != "10.40.0.1" {
		t.Errorf("reserved[0].start = %q, want %q", start, "10.40.0.1")
	}
	if desc := client.GetNestedString(rMap, "description"); desc != "infrastructure" {
		t.Errorf("reserved[0].description = %q, want %q", desc, "infrastructure")
	}
}
