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

package idp

import (
	"bytes"
	"net/http"
	"strings"
	"testing"

	"github.com/butlerdotdev/butler/internal/common/serverhttp"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// TestMergeIdPSpec is the load-bearing update correctness test. A spec update
// must replace spec but PRESERVE the existing status (controller-managed
// conditions) and resourceVersion, so it never clobbers status and the Update
// carries the resourceVersion the API server requires.
func TestMergeIdPSpec(t *testing.T) {
	existing := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "butler.butlerlabs.dev/v1alpha1",
		"kind":       "IdentityProvider",
		"metadata": map[string]interface{}{
			"name":            "okta",
			"resourceVersion": "98765",
		},
		"spec": map[string]interface{}{
			"type": "oidc",
			"oidc": map[string]interface{}{"issuerURL": "https://old.okta.com"},
		},
		"status": map[string]interface{}{
			"conditions": []interface{}{
				map[string]interface{}{"type": "Ready", "status": "True"},
			},
		},
	}}

	newSpec := map[string]interface{}{
		"type": "oidc",
		"oidc": map[string]interface{}{"issuerURL": "https://new.okta.com"},
	}

	if err := mergeIdPSpec(existing, newSpec); err != nil {
		t.Fatal(err)
	}

	// Spec replaced.
	got, _, _ := unstructured.NestedString(existing.Object, "spec", "oidc", "issuerURL")
	if got != "https://new.okta.com" {
		t.Errorf("spec not updated: issuerURL = %q, want https://new.okta.com", got)
	}

	// resourceVersion preserved (Update needs it for optimistic concurrency).
	if rv := existing.GetResourceVersion(); rv != "98765" {
		t.Errorf("resourceVersion clobbered: got %q, want 98765", rv)
	}

	// Status conditions preserved untouched.
	conds, found, _ := unstructured.NestedSlice(existing.Object, "status", "conditions")
	if !found || len(conds) != 1 {
		t.Fatalf("status.conditions clobbered: found=%v len=%d (want 1)", found, len(conds))
	}
	cond, _ := conds[0].(map[string]interface{})
	if cond["type"] != "Ready" || cond["status"] != "True" {
		t.Errorf("status condition altered: %v", cond)
	}
}

func TestMergeIdPSpecEmptyExisting(t *testing.T) {
	if err := mergeIdPSpec(&unstructured.Unstructured{}, map[string]interface{}{"type": "oidc"}); err == nil {
		t.Error("expected error merging into empty object, got nil")
	}
}

func TestPrintDiscovery(t *testing.T) {
	var out bytes.Buffer
	res := testDiscoveryResponse{
		Valid:                 true,
		Message:               "OIDC discovery successful",
		AuthorizationEndpoint: "https://idp/authorize",
		TokenEndpoint:         "https://idp/token",
	}
	if err := printDiscovery(&out, res); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"VALID", "https://idp/authorize", "https://idp/token"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("discovery output missing %q:\n%s", want, out.String())
		}
	}

	var bad bytes.Buffer
	_ = printDiscovery(&bad, testDiscoveryResponse{Valid: false, Message: "OIDC discovery failed: no such host"})
	if !strings.Contains(bad.String(), "INVALID") || !strings.Contains(bad.String(), "no such host") {
		t.Errorf("invalid-discovery output should report INVALID + reason:\n%s", bad.String())
	}
}

func TestTranslateDiscoveryError(t *testing.T) {
	// 403 names the platform-admin requirement.
	forbidden := &serverhttp.ServerError{StatusCode: http.StatusForbidden, Message: "admin required"}
	if got := translateDiscoveryError(forbidden).Error(); !strings.Contains(got, "platform admin") {
		t.Errorf("403 should mention platform admin: %s", got)
	}
	// 404 maps to not found.
	nf := &serverhttp.ServerError{StatusCode: http.StatusNotFound, Message: "identity provider \"ghost\" not found"}
	if got := translateDiscoveryError(nf).Error(); !strings.Contains(got, "not found") {
		t.Errorf("404 should map to not found: %s", got)
	}
	// 400 maps to invalid request.
	br := &serverhttp.ServerError{StatusCode: http.StatusBadRequest, Message: "issuerURL is required"}
	if got := translateDiscoveryError(br).Error(); !strings.Contains(got, "invalid request") {
		t.Errorf("400 should map to invalid request: %s", got)
	}
}
