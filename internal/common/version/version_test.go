// Copyright 2026 The Butler Authors.
// SPDX-License-Identifier: Apache-2.0

package version

import "testing"

func TestDefaultValues(t *testing.T) {
	tests := []struct {
		name     string
		got      string
		expected string
	}{
		{
			name:     "Version defaults to dev",
			got:      Version,
			expected: "dev",
		},
		{
			name:     "Commit defaults to unknown",
			got:      Commit,
			expected: "unknown",
		},
		{
			name:     "Date defaults to unknown",
			got:      Date,
			expected: "unknown",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.expected {
				t.Errorf("got %q, want %q", tt.got, tt.expected)
			}
		})
	}
}
