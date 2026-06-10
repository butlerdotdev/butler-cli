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

package provider

import (
	"bytes"
	"net/http"
	"strings"
	"testing"

	"github.com/butlerdotdev/butler/internal/common/serverhttp"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestPrintProviderDetail(t *testing.T) {
	pc := &unstructured.Unstructured{Object: map[string]interface{}{
		"metadata": map[string]interface{}{"name": "nutanix", "namespace": "butler-system"},
		"spec": map[string]interface{}{
			"provider":       "nutanix",
			"nutanix":        map[string]interface{}{"endpoint": "https://pc.example.com"},
			"credentialsRef": map[string]interface{}{"name": "nutanix-creds"},
		},
		"status": map[string]interface{}{"validated": true},
	}}

	var out bytes.Buffer
	if err := printProviderDetail(&out, pc); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"nutanix", "https://pc.example.com", "nutanix-creds", "true"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("detail output missing %q:\n%s", want, out.String())
		}
	}
}

// TestMergeProviderSpec is the load-bearing update correctness test. A spec
// update must replace spec but PRESERVE the existing status and
// resourceVersion, so it never clobbers validation status and the Update
// carries the resourceVersion the API server requires.
func TestMergeProviderSpec(t *testing.T) {
	existing := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "butler.butlerlabs.dev/v1alpha1",
		"kind":       "ProviderConfig",
		"metadata": map[string]interface{}{
			"name":            "nutanix",
			"namespace":       "butler-system",
			"resourceVersion": "12345",
		},
		"spec": map[string]interface{}{
			"provider": "nutanix",
			"nutanix":  map[string]interface{}{"endpoint": "https://old.example.com"},
		},
		"status": map[string]interface{}{
			"validated":          true,
			"lastValidationTime": "2026-06-01T00:00:00Z",
		},
	}}

	newSpec := map[string]interface{}{
		"provider": "nutanix",
		"nutanix":  map[string]interface{}{"endpoint": "https://new.example.com"},
	}

	if err := mergeProviderSpec(existing, newSpec); err != nil {
		t.Fatal(err)
	}

	// Spec must be replaced with the new value.
	gotEndpoint, _, _ := unstructured.NestedString(existing.Object, "spec", "nutanix", "endpoint")
	if gotEndpoint != "https://new.example.com" {
		t.Errorf("spec not updated: endpoint = %q, want https://new.example.com", gotEndpoint)
	}

	// resourceVersion must be preserved (Update needs it for optimistic concurrency).
	if rv := existing.GetResourceVersion(); rv != "12345" {
		t.Errorf("resourceVersion clobbered: got %q, want 12345", rv)
	}

	// Status must be preserved untouched.
	validated, found, _ := unstructured.NestedBool(existing.Object, "status", "validated")
	if !found || !validated {
		t.Errorf("status.validated clobbered: found=%v validated=%v (want true)", found, validated)
	}
	lvt, _, _ := unstructured.NestedString(existing.Object, "status", "lastValidationTime")
	if lvt != "2026-06-01T00:00:00Z" {
		t.Errorf("status.lastValidationTime clobbered: got %q", lvt)
	}
}

func TestMergeProviderSpecEmptyExisting(t *testing.T) {
	if err := mergeProviderSpec(&unstructured.Unstructured{}, map[string]interface{}{"provider": "x"}); err == nil {
		t.Error("expected error merging into empty object, got nil")
	}
}

// TestTranslateDiscoveryError covers the load-bearing UX path: the 400 the
// server returns for cloud/proxmox providers must become a friendly message
// that names the provider context and points at the static config fields,
// not a raw 400 dump.
func TestTranslateDiscoveryError(t *testing.T) {
	notSupported := &serverhttp.ServerError{
		StatusCode: http.StatusBadRequest,
		Message:    "image listing not supported for provider: aws",
	}
	err := translateDiscoveryError(notSupported, "aws-prod")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	msg := err.Error()
	for _, want := range []string{"aws", "harvester", "nutanix", "awsVpcId"} {
		if !strings.Contains(msg, want) {
			t.Errorf("friendly message missing %q:\n%s", want, msg)
		}
	}
	// It must NOT be a bare "butler-server returned 400" dump.
	if strings.Contains(msg, "butler-server returned 400") {
		t.Errorf("message is a raw 400 dump, not the friendly form:\n%s", msg)
	}

	// A 404 maps to a not-found message naming the provider.
	nf := &serverhttp.ServerError{StatusCode: http.StatusNotFound, Message: "provider not found"}
	if got := translateDiscoveryError(nf, "ghost").Error(); !strings.Contains(got, "ghost") {
		t.Errorf("404 message should name the provider: %s", got)
	}
}
