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
	"strings"
	"testing"
)

func TestIsValidClusterName(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{
			name:     "simple valid name",
			input:    "my-cluster",
			expected: true,
		},
		{
			name:     "alphanumeric only",
			input:    "cluster1",
			expected: true,
		},
		{
			name:     "single character",
			input:    "a",
			expected: true,
		},
		{
			name:     "max length 63 chars",
			input:    strings.Repeat("a", 63),
			expected: true,
		},
		{
			name:     "exceeds 63 chars",
			input:    strings.Repeat("a", 64),
			expected: false,
		},
		{
			name:     "empty string",
			input:    "",
			expected: false,
		},
		{
			name:     "contains dots",
			input:    "my.cluster",
			expected: false,
		},
		{
			name:     "contains uppercase",
			input:    "MyCluster",
			expected: false,
		},
		{
			name:     "starts with hyphen",
			input:    "-cluster",
			expected: false,
		},
		{
			name:     "ends with hyphen",
			input:    "cluster-",
			expected: false,
		},
		{
			name:     "contains underscore",
			input:    "my_cluster",
			expected: false,
		},
		{
			name:     "contains spaces",
			input:    "my cluster",
			expected: false,
		},
		{
			name:     "hyphens in middle",
			input:    "my-prod-cluster-01",
			expected: true,
		},
		{
			name:     "numeric only",
			input:    "12345",
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isValidClusterName(tt.input)
			if got != tt.expected {
				t.Errorf("isValidClusterName(%q) = %v, want %v", tt.input, got, tt.expected)
			}
		})
	}
}

func TestIsValidIP(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{
			name:     "valid IPv4",
			input:    "10.127.14.40",
			expected: true,
		},
		{
			name:     "valid loopback",
			input:    "127.0.0.1",
			expected: true,
		},
		{
			name:     "valid zeros",
			input:    "0.0.0.0",
			expected: true,
		},
		{
			name:     "valid max values",
			input:    "255.255.255.255",
			expected: true,
		},
		{
			name:     "octet over 255",
			input:    "256.0.0.1",
			expected: false,
		},
		{
			name:     "too few octets",
			input:    "10.0.1",
			expected: false,
		},
		{
			name:     "too many octets",
			input:    "10.0.0.1.5",
			expected: false,
		},
		{
			name:     "empty string",
			input:    "",
			expected: false,
		},
		{
			name:     "IPv6 address",
			input:    "::1",
			expected: false,
		},
		{
			name:     "non-numeric octet",
			input:    "10.abc.0.1",
			expected: false,
		},
		{
			name:     "negative octet",
			input:    "10.-1.0.1",
			expected: false,
		},
		{
			name:     "hostname instead of IP",
			input:    "example.com",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isValidIP(tt.input)
			if got != tt.expected {
				t.Errorf("isValidIP(%q) = %v, want %v", tt.input, got, tt.expected)
			}
		})
	}
}

func TestCreateOptionsValidate(t *testing.T) {
	validOpts := func() *CreateOptions {
		return &CreateOptions{
			Name:              "test-cluster",
			Namespace:         "butler-tenants",
			Workers:           3,
			CPU:               4,
			MemoryMB:          8192,
			DiskGB:            50,
			KubernetesVersion: "v1.30.2",
			LBPoolStart:       "10.127.14.40",
			LBPoolEnd:         "10.127.14.50",
		}
	}

	tests := []struct {
		name      string
		modify    func(*CreateOptions)
		wantErr   bool
		errSubstr string
	}{
		{
			name:    "valid options",
			modify:  func(o *CreateOptions) {},
			wantErr: false,
		},
		{
			name:      "empty name",
			modify:    func(o *CreateOptions) { o.Name = "" },
			wantErr:   true,
			errSubstr: "cluster name is required",
		},
		{
			name:      "invalid name with uppercase",
			modify:    func(o *CreateOptions) { o.Name = "MyCluster" },
			wantErr:   true,
			errSubstr: "invalid cluster name",
		},
		{
			name:      "invalid name with dots",
			modify:    func(o *CreateOptions) { o.Name = "my.cluster" },
			wantErr:   true,
			errSubstr: "invalid cluster name",
		},
		{
			name:      "workers below minimum",
			modify:    func(o *CreateOptions) { o.Workers = 0 },
			wantErr:   true,
			errSubstr: "workers must be between 1 and 10",
		},
		{
			name:      "workers above maximum",
			modify:    func(o *CreateOptions) { o.Workers = 11 },
			wantErr:   true,
			errSubstr: "workers must be between 1 and 10",
		},
		{
			name:    "workers at minimum boundary",
			modify:  func(o *CreateOptions) { o.Workers = 1 },
			wantErr: false,
		},
		{
			name:    "workers at maximum boundary",
			modify:  func(o *CreateOptions) { o.Workers = 10 },
			wantErr: false,
		},
		{
			name:      "CPU below minimum",
			modify:    func(o *CreateOptions) { o.CPU = 0 },
			wantErr:   true,
			errSubstr: "cpu must be between 1 and 128",
		},
		{
			name:      "memory below minimum",
			modify:    func(o *CreateOptions) { o.MemoryMB = 1024 },
			wantErr:   true,
			errSubstr: "memory must be at least 2048MB",
		},
		{
			name:      "disk below minimum",
			modify:    func(o *CreateOptions) { o.DiskGB = 10 },
			wantErr:   true,
			errSubstr: "disk must be at least 20GB",
		},
		{
			name:      "kubernetes version missing v prefix",
			modify:    func(o *CreateOptions) { o.KubernetesVersion = "1.30.2" },
			wantErr:   true,
			errSubstr: "must start with 'v'",
		},
		{
			name:      "missing lb pool start",
			modify:    func(o *CreateOptions) { o.LBPoolStart = "" },
			wantErr:   true,
			errSubstr: "load balancer IP pool is required",
		},
		{
			name:      "missing lb pool end",
			modify:    func(o *CreateOptions) { o.LBPoolEnd = "" },
			wantErr:   true,
			errSubstr: "load balancer IP pool is required",
		},
		{
			name:      "invalid lb pool start IP",
			modify:    func(o *CreateOptions) { o.LBPoolStart = "not-an-ip" },
			wantErr:   true,
			errSubstr: "invalid IP address for --lb-pool-start",
		},
		{
			name:      "invalid lb pool end IP",
			modify:    func(o *CreateOptions) { o.LBPoolEnd = "999.999.999.999" },
			wantErr:   true,
			errSubstr: "invalid IP address for --lb-pool-end",
		},
		{
			name: "filename skips all validation",
			modify: func(o *CreateOptions) {
				o.Filename = "cluster.yaml"
				o.Name = "" // would normally fail
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := validOpts()
			tt.modify(opts)

			err := opts.Validate()
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if tt.errSubstr != "" && !strings.Contains(err.Error(), tt.errSubstr) {
					t.Errorf("error %q does not contain %q", err.Error(), tt.errSubstr)
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}
		})
	}
}
