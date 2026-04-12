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

package wizard

import (
	"strings"
	"testing"
)

func TestValidateClusterName(t *testing.T) {
	tests := []struct {
		input   string
		wantErr bool
	}{
		{"", true},
		{"a", true},                              // too short for two-anchor regex
		{"ab", false},                             // minimum valid
		{"my-cluster", false},
		{"butler-mgmt-01", false},
		{"My-Cluster", true},                      // uppercase
		{"1cluster", true},                        // starts with digit
		{"-cluster", true},                        // starts with hyphen
		{"cluster-", true},                        // ends with hyphen
		{"ab cd", true},                           // space
		{"ab.cd", true},                           // dot
		{"ab_cd", true},                           // underscore
		{strings.Repeat("a", 63), false},          // exactly 63
		{strings.Repeat("a", 64), true},           // too long
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			err := validateClusterName(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateClusterName(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
		})
	}
}

func TestValidateCIDR(t *testing.T) {
	tests := []struct {
		input   string
		wantErr bool
	}{
		{"", true},
		{"10.0.0.0/8", false},
		{"192.168.1.0/24", false},
		{"10.0.0.0/33", true},
		{"not-a-cidr", true},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			err := validateCIDR(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateCIDR(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
		})
	}
}

func TestValidateIP(t *testing.T) {
	tests := []struct {
		input   string
		wantErr bool
	}{
		{"", true},
		{"10.0.0.1", false},
		{"::1", false},
		{"not-an-ip", true},
		{"10.0.0.256", true},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			err := validateIP(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateIP(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
		})
	}
}

func TestValidateIntRange(t *testing.T) {
	v := validateIntRange(1, 100)
	tests := []struct {
		input   string
		wantErr bool
	}{
		{"1", false},
		{"100", false},
		{"50", false},
		{"0", true},
		{"101", true},
		{"", true},
		{"abc", true},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			err := v(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateIntRange(1,100)(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
		})
	}
}

func TestValidateWorkerReplicas(t *testing.T) {
	tests := []struct {
		name     string
		topology string
		input    string
		wantErr  bool
	}{
		{"ha valid", "ha", "3", false},
		{"ha too few", "ha", "2", true},
		{"ha minimum", "ha", "1", true},
		{"ha range low", "ha", "0", true},
		{"ha range high", "ha", "101", true},
		{"single-node 1", "single-node", "1", false},
		{"single-node 2", "single-node", "2", false},
		{"not a number", "ha", "abc", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &wizardState{Topology: tt.topology}
			v := validateWorkerReplicas(s)
			err := v(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidateWorkerDiskGB(t *testing.T) {
	tests := []struct {
		name     string
		topology string
		input    string
		wantErr  bool
	}{
		{"ha valid", "ha", "50", false},
		{"ha too small", "ha", "49", true},
		{"ha 25", "ha", "25", true},
		{"ha 100", "ha", "100", false},
		{"ha range low", "ha", "19", true},
		{"ha range high", "ha", "4097", true},
		{"single-node 20", "single-node", "20", false},
		{"single-node 25", "single-node", "25", false},
		{"not a number", "ha", "abc", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &wizardState{Topology: tt.topology}
			v := validateWorkerDiskGB(s)
			err := v(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
