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

func TestBuildEnableRequest(t *testing.T) {
	req := buildEnableRequest(&enableOptions{
		repository: "https://github.com/acme/clusters",
		branch:     "main",
		path:       "clusters/prod",
		provider:   "github",
		private:    true,
	})
	if req.Repository != "https://github.com/acme/clusters" {
		t.Errorf("Repository = %q", req.Repository)
	}
	if req.Branch != "main" || req.Path != "clusters/prod" || req.Provider != "github" || !req.Private {
		t.Errorf("request fields not mapped: %+v", req)
	}
}

func TestPrintEnableResult(t *testing.T) {
	var buf bytes.Buffer
	res := enableResult{
		Success:       true,
		Provider:      "fluxcd",
		RepositoryURL: "https://github.com/acme/clusters",
		Path:          "clusters/prod",
		Version:       "v2.4.0",
	}
	if err := printEnableResult(&buf, res); err != nil {
		t.Fatalf("printEnableResult: %v", err)
	}
	out := buf.String()
	for _, want := range []string{"GitOps enabled", "fluxcd", "https://github.com/acme/clusters", "clusters/prod", "v2.4.0"} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected output to contain %q, got %q", want, out)
		}
	}
}
