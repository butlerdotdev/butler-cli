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
	"strings"
	"testing"
)

func TestBuildExportRequest(t *testing.T) {
	req := buildExportRequest(&exportOptions{
		repository:    "https://github.com/acme/clusters",
		branch:        "main",
		createPR:      true,
		prTitle:       "GitOps export",
		commitMessage: "export cluster",
		env:           "prd",
		clusterName:   "my-cluster",
	})
	if req.Repository != "https://github.com/acme/clusters" || req.Branch != "main" {
		t.Errorf("repo/branch not mapped: %+v", req)
	}
	if !req.CreatePR || req.PRTitle != "GitOps export" || req.CommitMessage != "export cluster" {
		t.Errorf("PR/commit fields not mapped: %+v", req)
	}
	if req.Env != "prd" || req.ClusterName != "my-cluster" {
		t.Errorf("env/clusterName not mapped: %+v", req)
	}
}

func TestPrintExportResult(t *testing.T) {
	t.Run("pull request", func(t *testing.T) {
		var buf bytes.Buffer
		res := exportResult{Success: true, Mode: "feature-branch-mr", FilesCount: 12, PRURL: "https://github.com/acme/clusters/pull/7"}
		if err := printExportResult(&buf, res); err != nil {
			t.Fatalf("printExportResult: %v", err)
		}
		out := buf.String()
		for _, want := range []string{"12 files", "feature-branch-mr", "Pull request:", "pull/7"} {
			if !strings.Contains(out, want) {
				t.Fatalf("expected output to contain %q, got %q", want, out)
			}
		}
	})

	t.Run("direct push", func(t *testing.T) {
		var buf bytes.Buffer
		res := exportResult{Success: true, Mode: "direct-push", FilesCount: 5, Branch: "main", CommitSHA: "abc123"}
		if err := printExportResult(&buf, res); err != nil {
			t.Fatalf("printExportResult: %v", err)
		}
		out := buf.String()
		for _, want := range []string{"5 files", "direct-push", "Branch: main", "Commit: abc123"} {
			if !strings.Contains(out, want) {
				t.Fatalf("expected output to contain %q, got %q", want, out)
			}
		}
		if strings.Contains(out, "Pull request:") {
			t.Errorf("direct-push should not print a PR URL")
		}
	})
}
