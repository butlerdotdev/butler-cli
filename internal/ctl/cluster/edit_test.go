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

package cluster

import (
	"encoding/json"
	"testing"
)

func TestEditOptions_HasInlineFlags(t *testing.T) {
	tests := []struct {
		name         string
		changedFlags map[string]bool
		want         bool
	}{
		{
			name:         "no flags set triggers interactive mode",
			changedFlags: map[string]bool{},
			want:         false,
		},
		{
			name:         "nil map triggers interactive mode",
			changedFlags: nil,
			want:         false,
		},
		{
			name:         "workers-replicas triggers inline mode",
			changedFlags: map[string]bool{"workers-replicas": true},
			want:         true,
		},
		{
			name:         "workers-cpu triggers inline mode",
			changedFlags: map[string]bool{"workers-cpu": true},
			want:         true,
		},
		{
			name:         "workers-memory triggers inline mode",
			changedFlags: map[string]bool{"workers-memory": true},
			want:         true,
		},
		{
			name:         "kubernetes-version triggers inline mode",
			changedFlags: map[string]bool{"kubernetes-version": true},
			want:         true,
		},
		{
			name: "multiple flags trigger inline mode",
			changedFlags: map[string]bool{
				"workers-replicas":   true,
				"kubernetes-version": true,
			},
			want: true,
		},
		{
			name:         "unrelated flag does not trigger inline mode",
			changedFlags: map[string]bool{"namespace": true},
			want:         false,
		},
		{
			name: "unrelated flag alongside edit flag triggers inline mode",
			changedFlags: map[string]bool{
				"namespace":        true,
				"workers-replicas": true,
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := &editOptions{changedFlags: tt.changedFlags}
			got := opts.hasInlineFlags()
			if got != tt.want {
				t.Errorf("hasInlineFlags() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestEditOptions_BuildEditPatch(t *testing.T) {
	tests := []struct {
		name         string
		opts         editOptions
		wantPatchJSON string
	}{
		{
			name: "workers-replicas only",
			opts: editOptions{
				workersReplicas: 5,
				changedFlags:    map[string]bool{"workers-replicas": true},
			},
			wantPatchJSON: `{"spec":{"workers":{"replicas":5}}}`,
		},
		{
			name: "workers-replicas set to zero",
			opts: editOptions{
				workersReplicas: 0,
				changedFlags:    map[string]bool{"workers-replicas": true},
			},
			wantPatchJSON: `{"spec":{"workers":{"replicas":0}}}`,
		},
		{
			name: "kubernetes-version only",
			opts: editOptions{
				kubernetesVersion: "v1.32.0",
				changedFlags:      map[string]bool{"kubernetes-version": true},
			},
			wantPatchJSON: `{"spec":{"kubernetesVersion":"v1.32.0"}}`,
		},
		{
			name: "workers-cpu only",
			opts: editOptions{
				workersCPU:   8,
				changedFlags: map[string]bool{"workers-cpu": true},
			},
			wantPatchJSON: `{"spec":{"workers":{"machineTemplate":{"cpu":8}}}}`,
		},
		{
			name: "workers-memory only",
			opts: editOptions{
				workersMemory: 16,
				changedFlags:  map[string]bool{"workers-memory": true},
			},
			wantPatchJSON: `{"spec":{"workers":{"machineTemplate":{"memoryGiB":16}}}}`,
		},
		{
			name: "replicas and cpu together",
			opts: editOptions{
				workersReplicas: 3,
				workersCPU:      4,
				changedFlags: map[string]bool{
					"workers-replicas": true,
					"workers-cpu":      true,
				},
			},
			wantPatchJSON: `{"spec":{"workers":{"machineTemplate":{"cpu":4},"replicas":3}}}`,
		},
		{
			name: "all worker flags and kubernetes version",
			opts: editOptions{
				workersReplicas:   3,
				workersCPU:        4,
				workersMemory:     16,
				kubernetesVersion: "v1.31.0",
				changedFlags: map[string]bool{
					"workers-replicas":   true,
					"workers-cpu":        true,
					"workers-memory":     true,
					"kubernetes-version": true,
				},
			},
			wantPatchJSON: `{"spec":{"kubernetesVersion":"v1.31.0","workers":{"machineTemplate":{"cpu":4,"memoryGiB":16},"replicas":3}}}`,
		},
		{
			name: "no changed flags produces empty spec",
			opts: editOptions{
				workersReplicas: 5,
				changedFlags:    map[string]bool{},
			},
			wantPatchJSON: `{"spec":{}}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			patch := tt.opts.buildEditPatch()
			got, err := json.Marshal(patch)
			if err != nil {
				t.Fatalf("failed to marshal patch: %v", err)
			}
			if string(got) != tt.wantPatchJSON {
				t.Errorf("buildEditPatch() =\n  %s\nwant:\n  %s", string(got), tt.wantPatchJSON)
			}
		})
	}
}
