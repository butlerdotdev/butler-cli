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

package bootstrap

import "testing"

func TestAllPassed(t *testing.T) {
	tests := []struct {
		name   string
		checks []Check
		want   bool
	}{
		{
			name:   "empty",
			checks: []Check{},
			want:   true,
		},
		{
			name: "all required pass",
			checks: []Check{
				{Name: "a", Passed: true},
				{Name: "b", Passed: true},
			},
			want: true,
		},
		{
			name: "one required fails",
			checks: []Check{
				{Name: "a", Passed: true},
				{Name: "b", Passed: false},
			},
			want: false,
		},
		{
			name: "required passes optional fails",
			checks: []Check{
				{Name: "a", Passed: true},
				{Name: "b", Passed: false, Optional: true},
			},
			want: true,
		},
		{
			name: "all optional all fail",
			checks: []Check{
				{Name: "a", Passed: false, Optional: true},
				{Name: "b", Passed: false, Optional: true},
			},
			want: true,
		},
		{
			name: "all required fail",
			checks: []Check{
				{Name: "a", Passed: false},
				{Name: "b", Passed: false},
			},
			want: false,
		},
		{
			name: "mixed: required pass + optional fail + required fail",
			checks: []Check{
				{Name: "a", Passed: true},
				{Name: "b", Passed: false, Optional: true},
				{Name: "c", Passed: false},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := AllPassed(tt.checks); got != tt.want {
				t.Errorf("AllPassed() = %v, want %v", got, tt.want)
			}
		})
	}
}
