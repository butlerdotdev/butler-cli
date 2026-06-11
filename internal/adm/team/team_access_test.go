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

package team

import (
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// teamObj builds a Team with a resourceVersion, a status, and the given users
// and groups, so edit* functions can be checked for status/rv/sibling
// preservation against a realistic object.
func teamObj(users, groups []interface{}) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "butler.butlerlabs.dev/v1alpha1",
		"kind":       "Team",
		"metadata": map[string]interface{}{
			"name":            "engineering",
			"resourceVersion": "4242",
		},
		"spec": map[string]interface{}{
			"access": map[string]interface{}{
				"users":  users,
				"groups": groups,
			},
		},
		"status": map[string]interface{}{
			"phase":       "Ready",
			"memberCount": int64(2),
		},
	}}
}

func usersField(t *testing.T, obj *unstructured.Unstructured) []interface{} {
	t.Helper()
	u, _, _ := unstructured.NestedSlice(obj.Object, "spec", "access", "users")
	return u
}

func groupsField(t *testing.T, obj *unstructured.Unstructured) []interface{} {
	t.Helper()
	g, _, _ := unstructured.NestedSlice(obj.Object, "spec", "access", "groups")
	return g
}

func assertStatusAndRVPreserved(t *testing.T, obj *unstructured.Unstructured) {
	t.Helper()
	if rv := obj.GetResourceVersion(); rv != "4242" {
		t.Errorf("resourceVersion clobbered: got %q, want 4242", rv)
	}
	phase, found, _ := unstructured.NestedString(obj.Object, "status", "phase")
	if !found || phase != "Ready" {
		t.Errorf("status.phase clobbered: found=%v phase=%q (want Ready)", found, phase)
	}
}

func TestValidateRole(t *testing.T) {
	for _, r := range []string{"admin", "operator", "viewer"} {
		if err := validateRole(r); err != nil {
			t.Errorf("validateRole(%q) = %v, want nil", r, err)
		}
	}
	for _, r := range []string{"", "owner", "Admin", "root"} {
		if err := validateRole(r); err == nil {
			t.Errorf("validateRole(%q) = nil, want error", r)
		}
	}
}

// TestEditMemberRole is load-bearing: changing one member's role must leave the
// OTHER member untouched and must preserve status + resourceVersion.
func TestEditMemberRole(t *testing.T) {
	obj := teamObj([]interface{}{
		map[string]interface{}{"name": "alice@x.com", "role": "viewer"},
		map[string]interface{}{"name": "bob@x.com", "role": "operator"},
	}, nil)

	if err := editMemberRole(obj, "alice@x.com", "admin"); err != nil {
		t.Fatal(err)
	}

	users := usersField(t, obj)
	got := map[string]string{}
	for _, u := range users {
		m := u.(map[string]interface{})
		got[m["name"].(string)] = m["role"].(string)
	}
	if got["alice@x.com"] != "admin" {
		t.Errorf("alice role = %q, want admin", got["alice@x.com"])
	}
	if got["bob@x.com"] != "operator" {
		t.Errorf("bob (sibling) role = %q, want operator (untouched)", got["bob@x.com"])
	}
	if len(users) != 2 {
		t.Errorf("user count = %d, want 2 (no entry dropped)", len(users))
	}
	assertStatusAndRVPreserved(t, obj)
}

func TestEditMemberRoleNotFound(t *testing.T) {
	obj := teamObj([]interface{}{
		map[string]interface{}{"name": "alice@x.com", "role": "viewer"},
	}, nil)
	if err := editMemberRole(obj, "ghost@x.com", "admin"); err == nil {
		t.Error("expected not-found error for non-member, got nil")
	}
}

func TestEditAddGroup(t *testing.T) {
	obj := teamObj(nil, []interface{}{
		map[string]interface{}{"name": "eng", "role": "viewer"},
	})
	if err := editAddGroup(obj, "platform", "operator", "okta"); err != nil {
		t.Fatal(err)
	}
	groups := groupsField(t, obj)
	if len(groups) != 2 {
		t.Fatalf("group count = %d, want 2", len(groups))
	}
	added := groups[1].(map[string]interface{})
	if added["name"] != "platform" || added["role"] != "operator" || added["identityProvider"] != "okta" {
		t.Errorf("added group wrong: %v", added)
	}
	assertStatusAndRVPreserved(t, obj)

	// Duplicate on name+idp errors.
	if err := editAddGroup(obj, "platform", "viewer", "okta"); err == nil {
		t.Error("expected duplicate error, got nil")
	}
	// Same name, different idp (empty) is NOT a duplicate.
	if err := editAddGroup(obj, "platform", "viewer", ""); err != nil {
		t.Errorf("same-name different-idp should be allowed: %v", err)
	}
}

// TestFindGroupIndexAmbiguity is the load-bearing D2 path: two rules share a
// name, and without --identity-provider the lookup must ERROR, never pick one.
func TestFindGroupIndexAmbiguity(t *testing.T) {
	groups := []interface{}{
		map[string]interface{}{"name": "eng", "role": "viewer", "identityProvider": "okta"},
		map[string]interface{}{"name": "eng", "role": "admin", "identityProvider": "google"},
	}
	// No idp given: must error (ambiguous).
	if _, err := findGroupIndex(groups, "eng", ""); err == nil {
		t.Error("ambiguous group with no idp must error, got nil")
	}
	// idp given: resolves to the right one.
	idx, err := findGroupIndex(groups, "eng", "google")
	if err != nil || idx != 1 {
		t.Errorf("findGroupIndex(eng, google) = (%d, %v), want (1, nil)", idx, err)
	}
	// Unknown group: not-found error.
	if _, err := findGroupIndex(groups, "missing", ""); err == nil {
		t.Error("unknown group must error, got nil")
	}
}

// TestEditGroupRoleAmbiguityDoesNotMutate confirms the D2 ambiguity path fails
// safe: an ambiguous update errors and leaves both rules unchanged.
func TestEditGroupRoleAmbiguityDoesNotMutate(t *testing.T) {
	obj := teamObj(nil, []interface{}{
		map[string]interface{}{"name": "eng", "role": "viewer", "identityProvider": "okta"},
		map[string]interface{}{"name": "eng", "role": "viewer", "identityProvider": "google"},
	})
	if err := editGroupRole(obj, "eng", "", "admin"); err == nil {
		t.Fatal("ambiguous update-group-role must error, got nil")
	}
	for _, g := range groupsField(t, obj) {
		if g.(map[string]interface{})["role"] != "viewer" {
			t.Errorf("ambiguous update must not mutate any rule, got %v", g)
		}
	}

	// Disambiguated: changes only the targeted rule.
	if err := editGroupRole(obj, "eng", "google", "admin"); err != nil {
		t.Fatal(err)
	}
	roles := map[string]string{}
	for _, g := range groupsField(t, obj) {
		m := g.(map[string]interface{})
		roles[m["identityProvider"].(string)] = m["role"].(string)
	}
	if roles["google"] != "admin" || roles["okta"] != "viewer" {
		t.Errorf("disambiguated update wrong: %v", roles)
	}
	assertStatusAndRVPreserved(t, obj)
}

func TestEditRemoveGroup(t *testing.T) {
	obj := teamObj(nil, []interface{}{
		map[string]interface{}{"name": "eng", "role": "viewer"},
		map[string]interface{}{"name": "platform", "role": "admin"},
	})
	if err := editRemoveGroup(obj, "eng", ""); err != nil {
		t.Fatal(err)
	}
	groups := groupsField(t, obj)
	if len(groups) != 1 || groups[0].(map[string]interface{})["name"] != "platform" {
		t.Errorf("after remove, want only platform, got %v", groups)
	}
	assertStatusAndRVPreserved(t, obj)

	if err := editRemoveGroup(obj, "missing", ""); err == nil {
		t.Error("removing unknown group must error, got nil")
	}
}
