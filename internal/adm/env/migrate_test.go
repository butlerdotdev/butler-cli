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
