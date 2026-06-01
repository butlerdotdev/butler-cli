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

package cluster

import (
	"bytes"
	"strings"
	"testing"
)

func TestValidateRotateOptions(t *testing.T) {
	cases := []struct {
		name        string
		clusterName string
		opts        rotateOptions
		wantErr     bool
		wantSubstr  string
	}{
		{
			name:        "kubeconfigs simple",
			clusterName: "tc",
			opts:        rotateOptions{rotationType: rotateTypeKubeconfigs},
		},
		{
			name:        "all simple",
			clusterName: "tc",
			opts:        rotateOptions{rotationType: rotateTypeAll},
		},
		{
			name:        "ca without acknowledge fails",
			clusterName: "tc",
			opts:        rotateOptions{rotationType: rotateTypeCA},
			wantErr:     true,
			wantSubstr:  "--ca-acknowledge",
		},
		{
			name:        "ca with acknowledge interactive OK",
			clusterName: "tc",
			opts:        rotateOptions{rotationType: rotateTypeCA, caAcknowledge: true},
		},
		{
			name:        "ca with yes but missing name confirm fails",
			clusterName: "production",
			opts: rotateOptions{
				rotationType:  rotateTypeCA,
				caAcknowledge: true,
				yes:           true,
			},
			wantErr:    true,
			wantSubstr: "--cluster-name-confirm=production",
		},
		{
			name:        "ca with yes and wrong name confirm fails",
			clusterName: "production",
			opts: rotateOptions{
				rotationType:       rotateTypeCA,
				caAcknowledge:      true,
				yes:                true,
				clusterNameConfirm: "different-name",
			},
			wantErr:    true,
			wantSubstr: "--cluster-name-confirm=production",
		},
		{
			name:        "ca with yes and matching name confirm OK",
			clusterName: "production",
			opts: rotateOptions{
				rotationType:       rotateTypeCA,
				caAcknowledge:      true,
				yes:                true,
				clusterNameConfirm: "production",
			},
		},
		{
			name:        "invalid type fails",
			clusterName: "tc",
			opts:        rotateOptions{rotationType: "everything"},
			wantErr:     true,
			wantSubstr:  "kubeconfigs, all, ca",
		},
		{
			name:        "empty type fails",
			clusterName: "tc",
			opts:        rotateOptions{rotationType: ""},
			wantErr:     true,
			wantSubstr:  "kubeconfigs, all, ca",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateRotateOptions(tc.clusterName, &tc.opts)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("err = nil, want error containing %q", tc.wantSubstr)
				}
				if !strings.Contains(err.Error(), tc.wantSubstr) {
					t.Errorf("err = %q, want substring %q", err.Error(), tc.wantSubstr)
				}
				return
			}
			if err != nil {
				t.Fatalf("err = %v, want nil", err)
			}
		})
	}
}

func TestConfirmRotation(t *testing.T) {
	cases := []struct {
		name        string
		clusterName string
		opts        rotateOptions
		input       string
		wantErr     bool
		wantSubstr  string
	}{
		{
			name:        "kubeconfigs y accepted",
			clusterName: "tc",
			opts:        rotateOptions{rotationType: rotateTypeKubeconfigs},
			input:       "y\n",
		},
		{
			name:        "kubeconfigs Y accepted",
			clusterName: "tc",
			opts:        rotateOptions{rotationType: rotateTypeKubeconfigs},
			input:       "Y\n",
		},
		{
			name:        "kubeconfigs yes accepted",
			clusterName: "tc",
			opts:        rotateOptions{rotationType: rotateTypeKubeconfigs},
			input:       "yes\n",
		},
		{
			name:        "kubeconfigs blank rejected",
			clusterName: "tc",
			opts:        rotateOptions{rotationType: rotateTypeKubeconfigs},
			input:       "\n",
			wantErr:     true,
			wantSubstr:  "cancelled",
		},
		{
			name:        "kubeconfigs n rejected",
			clusterName: "tc",
			opts:        rotateOptions{rotationType: rotateTypeKubeconfigs},
			input:       "n\n",
			wantErr:     true,
			wantSubstr:  "cancelled",
		},
		{
			name:        "all y accepted",
			clusterName: "tc",
			opts:        rotateOptions{rotationType: rotateTypeAll},
			input:       "y\n",
		},
		{
			name:        "ca correct name accepted",
			clusterName: "production",
			opts:        rotateOptions{rotationType: rotateTypeCA, caAcknowledge: true},
			input:       "production\n",
		},
		{
			name:        "ca correct name with surrounding space accepted",
			clusterName: "production",
			opts:        rotateOptions{rotationType: rotateTypeCA, caAcknowledge: true},
			input:       "  production  \n",
		},
		{
			name:        "ca wrong name rejected",
			clusterName: "production",
			opts:        rotateOptions{rotationType: rotateTypeCA, caAcknowledge: true},
			input:       "staging\n",
			wantErr:     true,
			wantSubstr:  "did not match cluster name",
		},
		{
			name:        "ca empty rejected",
			clusterName: "production",
			opts:        rotateOptions{rotationType: rotateTypeCA, caAcknowledge: true},
			input:       "\n",
			wantErr:     true,
			wantSubstr:  "did not match cluster name",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			err := confirmRotation(tc.clusterName, &tc.opts, strings.NewReader(tc.input), &out)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("err = nil, want error containing %q", tc.wantSubstr)
				}
				if !strings.Contains(err.Error(), tc.wantSubstr) {
					t.Errorf("err = %q, want substring %q", err.Error(), tc.wantSubstr)
				}
				return
			}
			if err != nil {
				t.Fatalf("err = %v, want nil", err)
			}
			// Sanity: the prompt printed something operator-facing.
			if out.Len() == 0 {
				t.Errorf("no prompt output written")
			}
		})
	}
}
