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
	"strings"
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

func TestApplyMigrationMutation_SetsLabelAndAnnotation(t *testing.T) {
	item := makeTC("alpha", "")
	applyMigrationMutation(&item, "prod")

	if got := item.GetLabels()[EnvironmentLabel]; got != "prod" {
		t.Errorf("env label = %q, want prod", got)
	}
	if got := item.GetAnnotations()[MigrationOperationAnnotation]; got != "true" {
		t.Errorf("migration-operation annotation = %q, want \"true\"", got)
	}
}

func TestApplyMigrationMutation_PreservesExistingMetadata(t *testing.T) {
	item := makeTC("alpha", "staging")
	item.SetAnnotations(map[string]string{"custom.example.com/foo": "bar"})
	existingLabels := item.GetLabels()
	existingLabels["app.kubernetes.io/managed-by"] = "butler"
	item.SetLabels(existingLabels)

	applyMigrationMutation(&item, "prod")

	labels := item.GetLabels()
	if got := labels["app.kubernetes.io/managed-by"]; got != "butler" {
		t.Errorf("existing label clobbered: got %q, want butler", got)
	}
	if got := labels[EnvironmentLabel]; got != "prod" {
		t.Errorf("env label after relabel = %q, want prod", got)
	}

	annotations := item.GetAnnotations()
	if got := annotations["custom.example.com/foo"]; got != "bar" {
		t.Errorf("existing annotation clobbered: got %q, want bar", got)
	}
	if got := annotations[MigrationOperationAnnotation]; got != "true" {
		t.Errorf("migration-operation annotation = %q, want \"true\"", got)
	}
}

func TestTeamStatusNamespace_ReadsFromStatus(t *testing.T) {
	tm := unstructured.Unstructured{}
	tm.SetName("payments")
	// Real controller convention: namespace is the bare team name, not
	// "team-<name>". The CLI must read status.namespace, not concatenate.
	if err := unstructured.SetNestedField(tm.Object, "payments", "status", "namespace"); err != nil {
		t.Fatalf("set status.namespace: %v", err)
	}
	got, err := teamStatusNamespace(&tm)
	if err != nil {
		t.Fatalf("teamStatusNamespace returned error: %v", err)
	}
	if got != "payments" {
		t.Errorf("teamStatusNamespace = %q, want payments (bare team name from status; a team- prefix would regress)", got)
	}
}

func TestTeamStatusNamespace_ErrorsWhenUnreconciled(t *testing.T) {
	tm := unstructured.Unstructured{}
	tm.SetName("newly-created")
	// status.namespace unset: controller has not yet reconciled.
	_, err := teamStatusNamespace(&tm)
	if err == nil {
		t.Fatal("expected error when status.namespace is unset, got nil")
	}
	// Error must name the team so operators can see which Team blocked the
	// command; the test asserts the team name is in the message.
	if !strings.Contains(err.Error(), "newly-created") {
		t.Errorf("error = %q, expected to contain team name %q", err.Error(), "newly-created")
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

func TestParseAccessGroups(t *testing.T) {
	got, err := parseAccessGroups([]string{
		"sre:admin",
		"developers:viewer:okta",
	})
	if err != nil {
		t.Fatalf("parseAccessGroups returned error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 groups, got %d", len(got))
	}
	sre := got[0].(map[string]interface{})
	if sre["name"] != "sre" || sre["role"] != "admin" {
		t.Errorf("sre = %v, want sre/admin", sre)
	}
	if _, hasIdP := sre["identityProvider"]; hasIdP {
		t.Errorf("sre unexpectedly has identityProvider: %v", sre)
	}
	devs := got[1].(map[string]interface{})
	if devs["identityProvider"] != "okta" {
		t.Errorf("developers identityProvider = %v, want okta", devs["identityProvider"])
	}

	// Same name from two different IdPs is allowed.
	if _, err := parseAccessGroups([]string{"admins:admin:okta", "admins:viewer:google"}); err != nil {
		t.Errorf("parseAccessGroups rejected same-name-different-idp case: %v", err)
	}

	bad := [][]string{
		{"no-role"},
		{":admin"},                      // missing name
		{"sre:"},                        // empty role in create
		{"sre:bogus"},                   // invalid role
		{"sre:admin", "sre:viewer"},     // duplicate (same name, empty idp)
	}
	for _, pairs := range bad {
		if _, err := parseAccessGroups(pairs); err == nil {
			t.Errorf("parseAccessGroups(%v) returned nil error, expected failure", pairs)
		}
	}
}

func TestMergeAccessGroupPatches(t *testing.T) {
	existing := []interface{}{
		map[string]interface{}{"name": "sre", "role": "admin"},
		map[string]interface{}{"name": "developers", "role": "viewer", "identityProvider": "okta"},
	}

	// Overwrite sre's role, remove okta-developers, add a google-developers.
	merged, err := mergeAccessGroupPatches(existing, []string{
		"sre:operator",
		"developers::okta",
		"developers:viewer:google",
	})
	if err != nil {
		t.Fatalf("mergeAccessGroupPatches returned error: %v", err)
	}
	if len(merged) != 2 {
		t.Fatalf("expected 2 groups after merge, got %d (%v)", len(merged), merged)
	}

	type gk struct{ name, idp, role string }
	seen := map[gk]bool{}
	for _, g := range merged {
		m := g.(map[string]interface{})
		name, _ := m["name"].(string)
		role, _ := m["role"].(string)
		idp, _ := m["identityProvider"].(string)
		seen[gk{name, idp, role}] = true
	}
	if !seen[gk{"sre", "", "operator"}] {
		t.Errorf("expected sre to be operator, got %v", seen)
	}
	if !seen[gk{"developers", "google", "viewer"}] {
		t.Errorf("expected google-developers to be viewer, got %v", seen)
	}
	if seen[gk{"developers", "okta", "viewer"}] {
		t.Errorf("okta-developers should have been removed, got %v", seen)
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
