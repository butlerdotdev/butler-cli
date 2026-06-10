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
	"strings"
	"testing"
)

func TestValidateRepoFullName(t *testing.T) {
	tests := []struct {
		name    string
		repo    string
		wantErr bool
		// hint, when set, must appear in the error message.
		hint string
	}{
		{name: "owner/repo", repo: "acme/clusters", wantErr: false},
		{name: "gitlab subgroup", repo: "group/subgroup/clusters", wantErr: false},
		{name: "https url", repo: "https://github.com/acme/clusters", wantErr: true, hint: "not a URL"},
		{name: "ssh url", repo: "git@github.com:acme/clusters", wantErr: true, hint: "not a URL"},
		{name: "no slash", repo: "acme", wantErr: true},
		{name: "trailing slash", repo: "acme/", wantErr: true},
		{name: "leading slash", repo: "/clusters", wantErr: true},
		{name: "empty", repo: "", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateRepoFullName(tt.repo)
			if (err != nil) != tt.wantErr {
				t.Fatalf("validateRepoFullName(%q) err = %v, wantErr %v", tt.repo, err, tt.wantErr)
			}
			if tt.hint != "" && (err == nil || !strings.Contains(err.Error(), tt.hint)) {
				t.Errorf("validateRepoFullName(%q) error = %v, want hint %q", tt.repo, err, tt.hint)
			}
		})
	}
}
