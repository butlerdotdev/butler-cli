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

func TestPrintDiscovery(t *testing.T) {
	t.Run("with releases and engine", func(t *testing.T) {
		var buf bytes.Buffer
		res := discoveryResult{
			GitOpsEngine: &gitopsEngineStatus{Provider: "fluxcd", Installed: true, Ready: true, Version: "v2.4.0"},
			Matched: []discoveredRelease{
				{Name: "cilium", Namespace: "kube-system", Chart: "cilium", ChartVersion: "1.15.0", Status: "deployed"},
			},
			Unmatched: []discoveredRelease{
				{Name: "custom-app", Namespace: "apps", Chart: "custom", ChartVersion: "0.1.0", Status: "deployed"},
			},
		}
		if err := printDiscovery(&buf, res); err != nil {
			t.Fatalf("printDiscovery: %v", err)
		}
		out := buf.String()
		for _, want := range []string{"fluxcd", "Matched", "cilium", "kube-system", "Unmatched", "custom-app"} {
			if !strings.Contains(out, want) {
				t.Fatalf("expected output to contain %q, got %q", want, out)
			}
		}
	})

	t.Run("empty", func(t *testing.T) {
		var buf bytes.Buffer
		if err := printDiscovery(&buf, discoveryResult{}); err != nil {
			t.Fatalf("printDiscovery: %v", err)
		}
		out := buf.String()
		if !strings.Contains(out, "Matched (Butler addons): 0") || !strings.Contains(out, "Unmatched: 0") {
			t.Fatalf("expected zero-count sections, got %q", out)
		}
	})
}
