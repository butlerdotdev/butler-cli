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

package addon

import (
	"testing"

	"github.com/butlerdotdev/butler/internal/common/client"
)

func TestCatalogTableRows(t *testing.T) {
	// Verify that field extraction from AddonDefinition objects produces
	// the expected table row values.
	tests := []struct {
		name        string
		obj         map[string]interface{}
		wantName    string
		wantDisplay string
		wantCat     string
		wantVersion string
		wantPlatStr string
	}{
		{
			name: "full addon definition",
			obj: map[string]interface{}{
				"metadata": map[string]interface{}{
					"name": "cilium",
				},
				"spec": map[string]interface{}{
					"displayName": "Cilium CNI",
					"category":    "networking",
					"platform":    true,
					"chart": map[string]interface{}{
						"defaultVersion": "1.16.0",
					},
				},
			},
			wantName:    "cilium",
			wantDisplay: "Cilium CNI",
			wantCat:     "networking",
			wantVersion: "1.16.0",
			wantPlatStr: "Yes",
		},
		{
			name: "non-platform addon without display name",
			obj: map[string]interface{}{
				"metadata": map[string]interface{}{
					"name": "longhorn",
				},
				"spec": map[string]interface{}{
					"category": "storage",
					"chart": map[string]interface{}{
						"defaultVersion": "1.7.0",
					},
				},
			},
			wantName:    "longhorn",
			wantDisplay: "",
			wantCat:     "storage",
			wantVersion: "1.7.0",
			wantPlatStr: "",
		},
		{
			name: "addon with no version",
			obj: map[string]interface{}{
				"metadata": map[string]interface{}{
					"name": "custom-addon",
				},
				"spec": map[string]interface{}{
					"displayName": "Custom",
					"category":    "other",
					"chart":       map[string]interface{}{},
				},
			},
			wantName:    "custom-addon",
			wantDisplay: "Custom",
			wantCat:     "other",
			wantVersion: "",
			wantPlatStr: "",
		},
		{
			name: "empty spec",
			obj: map[string]interface{}{
				"metadata": map[string]interface{}{
					"name": "empty",
				},
				"spec": map[string]interface{}{},
			},
			wantName:    "empty",
			wantDisplay: "",
			wantCat:     "",
			wantVersion: "",
			wantPlatStr: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotName := client.GetNestedString(tt.obj, "metadata", "name")
			gotDisplay := client.GetNestedString(tt.obj, "spec", "displayName")
			gotCat := client.GetNestedString(tt.obj, "spec", "category")
			gotVersion := client.GetNestedString(tt.obj, "spec", "chart", "defaultVersion")
			gotPlatform := client.GetNestedBool(tt.obj, "spec", "platform")

			platformStr := ""
			if gotPlatform {
				platformStr = "Yes"
			}

			if gotName != tt.wantName {
				t.Errorf("name: got %q, want %q", gotName, tt.wantName)
			}
			if gotDisplay != tt.wantDisplay {
				t.Errorf("displayName: got %q, want %q", gotDisplay, tt.wantDisplay)
			}
			if gotCat != tt.wantCat {
				t.Errorf("category: got %q, want %q", gotCat, tt.wantCat)
			}
			if gotVersion != tt.wantVersion {
				t.Errorf("version: got %q, want %q", gotVersion, tt.wantVersion)
			}
			if platformStr != tt.wantPlatStr {
				t.Errorf("platform: got %q, want %q", platformStr, tt.wantPlatStr)
			}
		})
	}
}

func TestManagementAddonTableRows(t *testing.T) {
	// Verify that field extraction from ManagementAddon objects produces
	// the expected table row values.
	tests := []struct {
		name         string
		obj          map[string]interface{}
		wantName     string
		wantAddon    string
		wantVersion  string
		wantInstall  string
		wantPhase    string
		wantPaused   bool
		wantMessage  string
		wantHelmName string
	}{
		{
			name: "installed addon",
			obj: map[string]interface{}{
				"metadata": map[string]interface{}{
					"name": "cilium",
				},
				"spec": map[string]interface{}{
					"addon":   "cilium",
					"version": "1.16.0",
				},
				"status": map[string]interface{}{
					"phase":            "Installed",
					"installedVersion": "1.16.0",
					"helmRelease": map[string]interface{}{
						"name":      "cilium",
						"namespace": "kube-system",
						"status":    "deployed",
					},
				},
			},
			wantName:     "cilium",
			wantAddon:    "cilium",
			wantVersion:  "1.16.0",
			wantInstall:  "1.16.0",
			wantPhase:    "Installed",
			wantPaused:   false,
			wantMessage:  "",
			wantHelmName: "cilium",
		},
		{
			name: "paused addon with message",
			obj: map[string]interface{}{
				"metadata": map[string]interface{}{
					"name": "longhorn",
				},
				"spec": map[string]interface{}{
					"addon":   "longhorn",
					"version": "1.7.0",
					"paused":  true,
				},
				"status": map[string]interface{}{
					"phase":   "Failed",
					"message": "helm install timed out",
				},
			},
			wantName:     "longhorn",
			wantAddon:    "longhorn",
			wantVersion:  "1.7.0",
			wantInstall:  "",
			wantPhase:    "Failed",
			wantPaused:   true,
			wantMessage:  "helm install timed out",
			wantHelmName: "",
		},
		{
			name: "pending addon with no status",
			obj: map[string]interface{}{
				"metadata": map[string]interface{}{
					"name": "cert-manager",
				},
				"spec": map[string]interface{}{
					"addon":   "cert-manager",
					"version": "v1.16.2",
				},
			},
			wantName:     "cert-manager",
			wantAddon:    "cert-manager",
			wantVersion:  "v1.16.2",
			wantInstall:  "",
			wantPhase:    "",
			wantPaused:   false,
			wantMessage:  "",
			wantHelmName: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotName := client.GetNestedString(tt.obj, "metadata", "name")
			gotAddon := client.GetNestedString(tt.obj, "spec", "addon")
			gotVersion := client.GetNestedString(tt.obj, "spec", "version")
			gotPaused := client.GetNestedBool(tt.obj, "spec", "paused")
			gotInstall := client.GetNestedString(tt.obj, "status", "installedVersion")
			gotPhase := client.GetNestedString(tt.obj, "status", "phase")
			gotMessage := client.GetNestedString(tt.obj, "status", "message")
			gotHelmName := client.GetNestedString(tt.obj, "status", "helmRelease", "name")

			if gotName != tt.wantName {
				t.Errorf("name: got %q, want %q", gotName, tt.wantName)
			}
			if gotAddon != tt.wantAddon {
				t.Errorf("addon: got %q, want %q", gotAddon, tt.wantAddon)
			}
			if gotVersion != tt.wantVersion {
				t.Errorf("version: got %q, want %q", gotVersion, tt.wantVersion)
			}
			if gotPaused != tt.wantPaused {
				t.Errorf("paused: got %v, want %v", gotPaused, tt.wantPaused)
			}
			if gotInstall != tt.wantInstall {
				t.Errorf("installedVersion: got %q, want %q", gotInstall, tt.wantInstall)
			}
			if gotPhase != tt.wantPhase {
				t.Errorf("phase: got %q, want %q", gotPhase, tt.wantPhase)
			}
			if gotMessage != tt.wantMessage {
				t.Errorf("message: got %q, want %q", gotMessage, tt.wantMessage)
			}
			if gotHelmName != tt.wantHelmName {
				t.Errorf("helmRelease.name: got %q, want %q", gotHelmName, tt.wantHelmName)
			}
		})
	}
}
