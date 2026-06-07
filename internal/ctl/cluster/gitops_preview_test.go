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
	"bytes"
	"context"
	"net/http"
	"strings"
	"testing"
)

func TestPrintPreview(t *testing.T) {
	var buf bytes.Buffer
	res := previewResult{
		ClusterName: "my-cluster",
		Files: map[string]string{
			"clusters/my-cluster/flux-system/gotk-sync.yaml":  "...",
			"clusters/my-cluster/apps/prd/kustomization.yaml": "...",
		},
		Summary: previewSummary{FileCount: 2, Collisions: 0, Failures: 0},
	}
	if err := printPreview(&buf, res); err != nil {
		t.Fatalf("printPreview: %v", err)
	}
	out := buf.String()
	for _, want := range []string{"my-cluster", "Files: 2", "gotk-sync.yaml", "kustomization.yaml"} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected output to contain %q, got %q", want, out)
		}
	}
	// Files should be sorted: apps path sorts before flux-system path.
	if strings.Index(out, "apps/prd") > strings.Index(out, "flux-system") {
		t.Errorf("file paths not sorted")
	}
}

// Roundtrip: CLI is the first consumer of preview-cluster (no console parity),
// so exercise the full POST path against a mock server.
func TestRunGitopsPreview_RoundTrip(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/clusters/team-x/c1/gitops/preview-cluster", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"clusterName":"c1","files":{"clusters/c1/x.yaml":"data"},"summary":{"fileCount":1,"collisions":0,"failures":0}}`))
	})
	startGitopsTestServer(t, mux)

	var buf bytes.Buffer
	err := runGitopsPreview(context.Background(), &buf, "c1", "team-x", "", &previewOptions{})
	if err != nil {
		t.Fatalf("runGitopsPreview: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "c1") || !strings.Contains(out, "Files: 1") || !strings.Contains(out, "clusters/c1/x.yaml") {
		t.Fatalf("unexpected preview output: %q", out)
	}
}
