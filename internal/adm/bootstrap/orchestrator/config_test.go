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

package orchestrator

import (
	"strings"
	"testing"
)

// TestValidateMTU covers the shared validator used by both LoadConfig
// and the wizard's buildConfig. Each case is a single (mtu, jumboFrames)
// tuple; the table is organized by what the case is proving:
//
//   - zero: no patch, no validation needed
//   - bounds: below 576 / above 9000 always fails
//   - standard: 576..1500 always passes regardless of jumbo flag
//   - jumbo gate: 1501..9000 passes only when jumboFrames=true
//
// The substring check on the error message enforces the specific error
// phrasing users will see; keeping it asserted prevents regressions that
// would hurt error-message quality.
func TestValidateMTU(t *testing.T) {
	tests := []struct {
		name        string
		mtu         int
		jumboFrames bool
		wantErr     bool
		errContains string
	}{
		{"zero, jumbo false", 0, false, false, ""},
		{"zero, jumbo true", 0, true, false, ""},

		{"below minimum", 575, false, true, "below the minimum of 576"},
		{"exactly minimum", 576, false, false, ""},
		{"typical tunnel", 1380, false, false, ""},
		{"standard Ethernet", 1500, false, false, ""},

		{"just above standard without opt-in", 1501, false, true, "set network.jumboFrames: true"},
		{"just above standard with opt-in", 1501, true, false, ""},
		{"common jumbo without opt-in", 9000, false, true, "set network.jumboFrames: true"},
		{"common jumbo with opt-in", 9000, true, false, ""},
		{"exactly maximum with opt-in", 9000, true, false, ""},

		{"above maximum without opt-in", 9001, false, true, "exceeds the supported maximum of 9000"},
		{"above maximum with opt-in", 9001, true, true, "exceeds the supported maximum of 9000"},

		// Jumbo flag on a sub-1500 MTU is a no-op, not an error. Let
		// operators flip jumbo on in YAML without forcing them to also
		// bump MTU — the flag simply doesn't gate anything in that case.
		{"jumbo flag with standard MTU", 1500, true, false, ""},
		{"negative MTU rejected as below minimum", -1, false, true, "below the minimum of 576"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateMTU(tt.mtu, tt.jumboFrames)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ValidateMTU(%d, %v) = nil, want error containing %q", tt.mtu, tt.jumboFrames, tt.errContains)
				}
				if !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("ValidateMTU(%d, %v) error = %q, want substring %q", tt.mtu, tt.jumboFrames, err.Error(), tt.errContains)
				}
				return
			}
			if err != nil {
				t.Errorf("ValidateMTU(%d, %v) = %v, want nil", tt.mtu, tt.jumboFrames, err)
			}
		})
	}
}
