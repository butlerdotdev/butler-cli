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
	"strings"
	"testing"
)

func TestValidateRepoFullName(t *testing.T) {
	tests := []struct {
		name    string
		repo    string
		wantErr bool
	}{
		{"owner/repo", "acme/clusters", false},
		{"gitlab subgroup", "group/subgroup/clusters", false},
		{"https url", "https://github.com/acme/clusters", true},
		{"ssh url", "git@github.com:acme/clusters", true},
		{"no slash", "acme", true},
		{"trailing slash", "acme/", true},
		{"empty", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := validateRepoFullName(tt.repo); (err != nil) != tt.wantErr {
				t.Errorf("validateRepoFullName(%q) err=%v wantErr=%v", tt.repo, err, tt.wantErr)
			}
		})
	}
}

// TestConfirmMgmtDisable is the load-bearing guard test. Management disable
// tears down the platform's own Flux, so it must proceed ONLY with the exact
// typed phrase, and must NOT fire on a habitual "yes" or the cluster name.
func TestConfirmMgmtDisable(t *testing.T) {
	tests := []struct {
		name    string
		flag    string // --confirm value
		stdin   string // interactive input when flag is empty
		wantErr bool   // true = refuses (does NOT proceed)
	}{
		{name: "exact phrase by flag", flag: disableManageConfirm, wantErr: false},
		{name: "wrong flag", flag: "nope", wantErr: true},
		{name: "cluster-name flag does not fire", flag: "management", wantErr: true},
		{name: "exact phrase typed", flag: "", stdin: disableManageConfirm + "\n", wantErr: false},
		{name: "habitual yes does not fire", flag: "", stdin: "yes\n", wantErr: true},
		{name: "cluster name typed does not fire", flag: "", stdin: "management\n", wantErr: true},
		{name: "empty input", flag: "", stdin: "\n", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := &disableOptions{confirm: tt.flag}
			err := confirmMgmtDisable(opts, strings.NewReader(tt.stdin), &bytes.Buffer{})
			if (err != nil) != tt.wantErr {
				t.Errorf("confirmMgmtDisable(flag=%q stdin=%q) err=%v, wantErr=%v", tt.flag, tt.stdin, err, tt.wantErr)
			}
		})
	}
}

func TestConfirmMgmtEnable(t *testing.T) {
	tests := []struct {
		name    string
		yes     bool
		stdin   string
		wantErr bool
	}{
		{name: "yes flag skips prompt", yes: true, wantErr: false},
		{name: "typed yes proceeds", yes: false, stdin: "yes\n", wantErr: false},
		{name: "declined", yes: false, stdin: "no\n", wantErr: true},
		{name: "empty declines", yes: false, stdin: "\n", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := &enableOptions{yes: tt.yes}
			err := confirmMgmtEnable(opts, strings.NewReader(tt.stdin), &bytes.Buffer{})
			if (err != nil) != tt.wantErr {
				t.Errorf("confirmMgmtEnable(yes=%v stdin=%q) err=%v, wantErr=%v", tt.yes, tt.stdin, err, tt.wantErr)
			}
		})
	}
}

func TestBuildExportRequest(t *testing.T) {
	opts := &exportOptions{repository: "acme/clusters", branch: "main", createPR: true, prTitle: "x", env: "prd"}
	req := buildExportRequest(opts)
	if req.Repository != "acme/clusters" || req.Branch != "main" || !req.CreatePR || req.PRTitle != "x" || req.Env != "prd" {
		t.Errorf("buildExportRequest mismatch: %+v", req)
	}
}

func TestPrintMgmtStatus(t *testing.T) {
	var out bytes.Buffer
	if err := printMgmtStatus(&out, mgmtGitOpsStatus{Enabled: true, Provider: "fluxcd", Repository: "acme/clusters", Branch: "main", Status: "Healthy"}); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"fluxcd", "acme/clusters", "Healthy"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("status output missing %q: %s", want, out.String())
		}
	}

	var off bytes.Buffer
	_ = printMgmtStatus(&off, mgmtGitOpsStatus{Enabled: false})
	if !strings.Contains(off.String(), "not enabled") {
		t.Errorf("disabled status should say not enabled: %s", off.String())
	}
}

func TestPrintExportResult(t *testing.T) {
	var out bytes.Buffer
	if err := printExportResult(&out, exportResult{FilesCount: 18, Mode: "feature-branch-mr", PRURL: "https://x/pull/1"}); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"18 files", "feature-branch-mr", "pull/1"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("export output missing %q: %s", want, out.String())
		}
	}
}
