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
	"testing"

	"github.com/butlerdotdev/butler/internal/common/client"
)

func TestGitopsConfigExtraction(t *testing.T) {
	tests := []struct {
		name       string
		obj        map[string]interface{}
		wantConfig gitopsConfig
		wantEmpty  bool
	}{
		{
			name: "full gitops config",
			obj: map[string]interface{}{
				"spec": map[string]interface{}{
					"addons": map[string]interface{}{
						"gitops": map[string]interface{}{
							"provider": "fluxcd",
							"version":  "v2.7.5",
							"repository": map[string]interface{}{
								"url":    "https://github.com/acme/clusters",
								"branch": "main",
								"path":   "clusters/prod",
							},
						},
					},
				},
			},
			wantConfig: gitopsConfig{
				Provider:   "fluxcd",
				Version:    "v2.7.5",
				Repository: "https://github.com/acme/clusters",
				Branch:     "main",
				Path:       "clusters/prod",
			},
		},
		{
			name: "no gitops config",
			obj: map[string]interface{}{
				"spec": map[string]interface{}{
					"addons": map[string]interface{}{},
				},
			},
			wantEmpty: true,
		},
		{
			name: "no addons at all",
			obj: map[string]interface{}{
				"spec": map[string]interface{}{},
			},
			wantEmpty: true,
		},
		{
			name: "partial config with provider only",
			obj: map[string]interface{}{
				"spec": map[string]interface{}{
					"addons": map[string]interface{}{
						"gitops": map[string]interface{}{
							"provider": "argocd",
						},
					},
				},
			},
			wantConfig: gitopsConfig{
				Provider: "argocd",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := gitopsConfig{
				Provider:   client.GetNestedString(tt.obj, "spec", "addons", "gitops", "provider"),
				Version:    client.GetNestedString(tt.obj, "spec", "addons", "gitops", "version"),
				Repository: client.GetNestedString(tt.obj, "spec", "addons", "gitops", "repository", "url"),
				Branch:     client.GetNestedString(tt.obj, "spec", "addons", "gitops", "repository", "branch"),
				Path:       client.GetNestedString(tt.obj, "spec", "addons", "gitops", "repository", "path"),
			}

			isEmpty := cfg.Provider == "" && cfg.Version == "" && cfg.Repository == ""

			if tt.wantEmpty && !isEmpty {
				t.Errorf("expected empty config, got %+v", cfg)
			}
			if !tt.wantEmpty && isEmpty {
				t.Errorf("expected non-empty config, got empty")
			}
			if !tt.wantEmpty && cfg != tt.wantConfig {
				t.Errorf("config = %+v, want %+v", cfg, tt.wantConfig)
			}
		})
	}
}
