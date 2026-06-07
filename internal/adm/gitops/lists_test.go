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

package gitops

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestPrintReposTable(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		var buf bytes.Buffer
		if err := printReposTable(&buf, nil); err != nil {
			t.Fatalf("printReposTable: %v", err)
		}
		if !strings.Contains(buf.String(), "No repositories found") {
			t.Fatalf("expected empty message, got %q", buf.String())
		}
	})

	t.Run("rows", func(t *testing.T) {
		var buf bytes.Buffer
		repos := []repository{
			{Name: "cluster-config", FullName: "butlerdotdev/cluster-config", DefaultBranch: "main", Private: true},
			{Name: "public-charts", FullName: "butlerdotdev/public-charts", DefaultBranch: "master", Private: false},
		}
		if err := printReposTable(&buf, repos); err != nil {
			t.Fatalf("printReposTable: %v", err)
		}
		out := buf.String()
		for _, want := range []string{"butlerdotdev/cluster-config", "main", "true", "butlerdotdev/public-charts", "master", "false"} {
			if !strings.Contains(out, want) {
				t.Fatalf("expected output to contain %q, got %q", want, out)
			}
		}
	})
}

func TestPrintBranchesTable(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		var buf bytes.Buffer
		if err := printBranchesTable(&buf, nil); err != nil {
			t.Fatalf("printBranchesTable: %v", err)
		}
		if !strings.Contains(buf.String(), "No branches found") {
			t.Fatalf("expected empty message, got %q", buf.String())
		}
	})

	t.Run("rows", func(t *testing.T) {
		var buf bytes.Buffer
		branches := []branch{
			{Name: "main", Default: true, Protected: true},
			{Name: "feature/x", Default: false, Protected: false},
		}
		if err := printBranchesTable(&buf, branches); err != nil {
			t.Fatalf("printBranchesTable: %v", err)
		}
		out := buf.String()
		for _, want := range []string{"main", "true", "feature/x", "false"} {
			if !strings.Contains(out, want) {
				t.Fatalf("expected output to contain %q, got %q", want, out)
			}
		}
	})
}

// runBranchesList guards on an empty --repo before any network call, so the
// error surfaces without a server or credential.
func TestRunBranchesList_RequiresRepo(t *testing.T) {
	var buf bytes.Buffer
	err := runBranchesList(context.Background(), &buf, "", "")
	if err == nil || !strings.Contains(err.Error(), "--repo is required") {
		t.Fatalf("expected --repo required error, got %v", err)
	}
}
