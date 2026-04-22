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

package env

import (
	"sort"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// makeTC builds a TenantCluster-like unstructured with a given name and
// environment label (pass "" to leave unlabeled).
func makeTC(name, envLabel string) unstructured.Unstructured {
	tc := unstructured.Unstructured{}
	tc.SetName(name)
	if envLabel != "" {
		tc.SetLabels(map[string]string{EnvironmentLabel: envLabel})
	}
	return tc
}

// targetNames returns the Name fields of a slice of MigrationTargets,
// sorted for deterministic assertions.
func targetNames(targets []MigrationTarget) []string {
	out := make([]string, 0, len(targets))
	for _, t := range targets {
		out = append(out, t.Name)
	}
	sort.Strings(out)
	return out
}

func TestSelectMigrationTargets_AllPicksUnlabeledByDefault(t *testing.T) {
	items := []unstructured.Unstructured{
		makeTC("alpha", ""),            // unlabeled -> included
		makeTC("beta", ""),             // unlabeled -> included
		makeTC("gamma", "staging"),     // labeled, different env -> skipped (no --relabel)
		makeTC("delta", "prod"),        // already target env -> skipped
	}

	got := SelectMigrationTargets(items, nil, true, false, "prod")
	names := targetNames(got)
	want := []string{"alpha", "beta"}
	if len(names) != len(want) {
		t.Fatalf("got %v, want %v", names, want)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Errorf("targets[%d] = %q, want %q", i, names[i], want[i])
		}
	}

	// PreviousEnv must be empty for the unlabeled targets.
	for _, g := range got {
		if g.PreviousEnv != "" {
			t.Errorf("target %s PreviousEnv = %q, want empty (unlabeled)", g.Name, g.PreviousEnv)
		}
	}
}

func TestSelectMigrationTargets_RelabelIncludesMismatchedLabels(t *testing.T) {
	items := []unstructured.Unstructured{
		makeTC("alpha", ""),        // unlabeled -> included under relabel too
		makeTC("beta", "staging"),  // labeled different -> included under relabel
		makeTC("gamma", "prod"),    // already target env -> skipped
	}

	got := SelectMigrationTargets(items, nil, true, true, "prod")
	names := targetNames(got)
	want := []string{"alpha", "beta"}
	if len(names) != len(want) {
		t.Fatalf("got %v, want %v", names, want)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Errorf("targets[%d] = %q, want %q", i, names[i], want[i])
		}
	}

	// Verify PreviousEnv is correctly reported for the relabel case.
	prevByName := map[string]string{}
	for _, g := range got {
		prevByName[g.Name] = g.PreviousEnv
	}
	if prevByName["alpha"] != "" {
		t.Errorf("alpha PreviousEnv = %q, want empty", prevByName["alpha"])
	}
	if prevByName["beta"] != "staging" {
		t.Errorf("beta PreviousEnv = %q, want staging", prevByName["beta"])
	}
}

func TestSelectMigrationTargets_NamedClustersRespectRelabelGate(t *testing.T) {
	items := []unstructured.Unstructured{
		makeTC("alpha", ""),
		makeTC("beta", "staging"),
		makeTC("gamma", "prod"),
		makeTC("delta", ""),
	}

	// Without --relabel, beta is named but labeled differently; it must be
	// filtered out. alpha is unlabeled and should be included.
	got := SelectMigrationTargets(items, []string{"alpha", "beta"}, false, false, "prod")
	names := targetNames(got)
	if len(names) != 1 || names[0] != "alpha" {
		t.Fatalf("without relabel: got %v, want [alpha]", names)
	}

	// With --relabel, beta is included even though it was labeled staging.
	got = SelectMigrationTargets(items, []string{"alpha", "beta"}, false, true, "prod")
	names = targetNames(got)
	want := []string{"alpha", "beta"}
	if len(names) != len(want) {
		t.Fatalf("with relabel: got %v, want %v", names, want)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Errorf("targets[%d] = %q, want %q", i, names[i], want[i])
		}
	}
}

func TestSelectMigrationTargets_EmptyWhenNothingMatches(t *testing.T) {
	items := []unstructured.Unstructured{
		makeTC("alpha", "prod"),
		makeTC("beta", "prod"),
	}

	got := SelectMigrationTargets(items, nil, true, false, "prod")
	if len(got) != 0 {
		t.Fatalf("expected empty target list, got %v", targetNames(got))
	}

	got = SelectMigrationTargets(items, nil, true, true, "prod")
	if len(got) != 0 {
		t.Fatalf("expected empty target list under --relabel when all match, got %v", targetNames(got))
	}
}

func TestSelectMigrationTargets_NoFlagsReturnsEmpty(t *testing.T) {
	items := []unstructured.Unstructured{makeTC("alpha", "")}

	// No names, no --all: selection must return empty slice so the caller
	// can reject with a helpful error message.
	got := SelectMigrationTargets(items, nil, false, false, "prod")
	if len(got) != 0 {
		t.Fatalf("expected empty selection with no flags, got %v", targetNames(got))
	}
}

func TestValidateEnvName(t *testing.T) {
	ok := []string{"staging", "prod", "env-1", "a", "A", "dev.1", "user_sandbox"}
	for _, name := range ok {
		if err := validateEnvName(name); err != nil {
			t.Errorf("validateEnvName(%q) returned unexpected error: %v", name, err)
		}
	}

	bad := []string{"", "-leading", "trailing-", "bad name", "bad/name", "bad@char"}
	for _, name := range bad {
		if err := validateEnvName(name); err == nil {
			t.Errorf("validateEnvName(%q) returned nil, expected error", name)
		}
	}
}

func TestParseClusterDefaults(t *testing.T) {
	got, err := parseClusterDefaults([]string{
		"kubernetesVersion=v1.31.0",
		"workerCount=3",
		"workerCPU=4",
		"workerMemoryGi=16",
		"workerDiskGi=100",
	})
	if err != nil {
		t.Fatalf("parseClusterDefaults returned error: %v", err)
	}
	if got["kubernetesVersion"] != "v1.31.0" {
		t.Errorf("kubernetesVersion = %v, want v1.31.0", got["kubernetesVersion"])
	}
	if got["workerCount"] != int64(3) {
		t.Errorf("workerCount = %v (%T), want int64(3)", got["workerCount"], got["workerCount"])
	}
	if got["workerCPU"] != int64(4) {
		t.Errorf("workerCPU = %v, want int64(4)", got["workerCPU"])
	}
	if got["workerMemoryGi"] != int64(16) {
		t.Errorf("workerMemoryGi = %v, want int64(16)", got["workerMemoryGi"])
	}
	if got["workerDiskGi"] != int64(100) {
		t.Errorf("workerDiskGi = %v, want int64(100)", got["workerDiskGi"])
	}

	bad := [][]string{
		{"unknownKey=foo"},           // unknown key
		{"workerCount=notanumber"},   // non-integer numeric
		{"=v1.31.0"},                 // missing key
		{"kubernetesVersion"},        // missing '='
		{"kubernetesVersion="},       // empty value in create semantics
	}
	for _, pairs := range bad {
		if _, err := parseClusterDefaults(pairs); err == nil {
			t.Errorf("parseClusterDefaults(%v) returned nil error, expected failure", pairs)
		}
	}
}

func TestParseAccessUsers(t *testing.T) {
	got, err := parseAccessUsers([]string{
		"alice@example.com:admin",
		"bob@example.com:operator",
		"carol@example.com:viewer",
	})
	if err != nil {
		t.Fatalf("parseAccessUsers returned error: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("expected 3 users, got %d", len(got))
	}
	first := got[0].(map[string]interface{})
	if first["name"] != "alice@example.com" || first["role"] != "admin" {
		t.Errorf("first user = %v, want alice/admin", first)
	}

	bad := [][]string{
		{"no-colon"},
		{":admin"},                          // missing email
		{"alice@example.com:"},              // empty role in create
		{"alice@example.com:superadmin"},    // invalid role
		{"alice@example.com:admin", "alice@example.com:viewer"}, // duplicate
	}
	for _, pairs := range bad {
		if _, err := parseAccessUsers(pairs); err == nil {
			t.Errorf("parseAccessUsers(%v) returned nil error, expected failure", pairs)
		}
	}
}

func TestMergeAccessUserPatches(t *testing.T) {
	existing := []interface{}{
		map[string]interface{}{"name": "alice@example.com", "role": "admin"},
		map[string]interface{}{"name": "bob@example.com", "role": "viewer"},
	}

	// Overwrite alice's role, remove bob, add carol.
	merged, err := mergeAccessUserPatches(existing, []string{
		"alice@example.com:operator",
		"bob@example.com:",
		"carol@example.com:viewer",
	})
	if err != nil {
		t.Fatalf("mergeAccessUserPatches returned error: %v", err)
	}
	if len(merged) != 2 {
		t.Fatalf("expected 2 users, got %d (%v)", len(merged), merged)
	}

	roleByEmail := map[string]string{}
	for _, e := range merged {
		m := e.(map[string]interface{})
		roleByEmail[m["name"].(string)] = m["role"].(string)
	}
	if roleByEmail["alice@example.com"] != "operator" {
		t.Errorf("alice role = %q, want operator", roleByEmail["alice@example.com"])
	}
	if _, stillThere := roleByEmail["bob@example.com"]; stillThere {
		t.Errorf("bob should have been removed, got %v", roleByEmail)
	}
	if roleByEmail["carol@example.com"] != "viewer" {
		t.Errorf("carol role = %q, want viewer", roleByEmail["carol@example.com"])
	}

	// Invalid role still rejected on merge.
	if _, err := mergeAccessUserPatches(nil, []string{"x@y.com:bogus"}); err == nil {
		t.Errorf("merge with invalid role returned nil error, expected failure")
	}
}

func TestMergeClusterDefaultPatches(t *testing.T) {
	existing := map[string]interface{}{
		"kubernetesVersion": "v1.30.0",
		"workerCount":       int64(2),
	}

	// Mixed: overwrite one key, clear another, set a new one.
	merged, err := mergeClusterDefaultPatches(existing, []string{
		"kubernetesVersion=v1.31.0",
		"workerCount=",
		"workerCPU=8",
	})
	if err != nil {
		t.Fatalf("mergeClusterDefaultPatches returned error: %v", err)
	}
	if merged["kubernetesVersion"] != "v1.31.0" {
		t.Errorf("kubernetesVersion = %v, want v1.31.0", merged["kubernetesVersion"])
	}
	if _, still := merged["workerCount"]; still {
		t.Errorf("workerCount should have been cleared; merged = %v", merged)
	}
	if merged["workerCPU"] != int64(8) {
		t.Errorf("workerCPU = %v, want int64(8)", merged["workerCPU"])
	}

	// Unknown key still rejected on update merge.
	if _, err := mergeClusterDefaultPatches(nil, []string{"unknown=x"}); err == nil {
		t.Errorf("merge with unknown key returned nil error, expected failure")
	}
}
