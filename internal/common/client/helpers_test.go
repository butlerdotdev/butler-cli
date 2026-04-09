// Copyright 2026 The Butler Authors.
// SPDX-License-Identifier: Apache-2.0

package client

import "testing"

func TestGetNestedString(t *testing.T) {
	obj := map[string]interface{}{
		"metadata": map[string]interface{}{
			"name":      "test-cluster",
			"namespace": "butler-tenants",
		},
		"status": map[string]interface{}{
			"phase": "Ready",
		},
		"emptyField": "",
	}

	tests := []struct {
		name     string
		obj      map[string]interface{}
		fields   []string
		expected string
	}{
		{
			name:     "top-level field",
			obj:      obj,
			fields:   []string{"emptyField"},
			expected: "",
		},
		{
			name:     "nested field",
			obj:      obj,
			fields:   []string{"metadata", "name"},
			expected: "test-cluster",
		},
		{
			name:     "deeply nested field",
			obj:      obj,
			fields:   []string{"status", "phase"},
			expected: "Ready",
		},
		{
			name:     "missing path returns empty string",
			obj:      obj,
			fields:   []string{"metadata", "labels", "app"},
			expected: "",
		},
		{
			name:     "nil map returns empty string",
			obj:      nil,
			fields:   []string{"metadata", "name"},
			expected: "",
		},
		{
			name:     "empty field returns empty string",
			obj:      obj,
			fields:   []string{"emptyField"},
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GetNestedString(tt.obj, tt.fields...)
			if got != tt.expected {
				t.Errorf("GetNestedString() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestGetNestedInt64(t *testing.T) {
	obj := map[string]interface{}{
		"spec": map[string]interface{}{
			"workers": map[string]interface{}{
				"replicas": int64(3),
			},
		},
		"count": int64(42),
	}

	tests := []struct {
		name     string
		obj      map[string]interface{}
		fields   []string
		expected int64
	}{
		{
			name:     "nested int64 field",
			obj:      obj,
			fields:   []string{"spec", "workers", "replicas"},
			expected: 3,
		},
		{
			name:     "top-level int64 field",
			obj:      obj,
			fields:   []string{"count"},
			expected: 42,
		},
		{
			name:     "missing path returns zero",
			obj:      obj,
			fields:   []string{"spec", "nonexistent"},
			expected: 0,
		},
		{
			name:     "nil map returns zero",
			obj:      nil,
			fields:   []string{"count"},
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GetNestedInt64(tt.obj, tt.fields...)
			if got != tt.expected {
				t.Errorf("GetNestedInt64() = %d, want %d", got, tt.expected)
			}
		})
	}
}

func TestGetNestedBool(t *testing.T) {
	obj := map[string]interface{}{
		"spec": map[string]interface{}{
			"enabled": true,
			"nested": map[string]interface{}{
				"disabled": false,
			},
		},
	}

	tests := []struct {
		name     string
		obj      map[string]interface{}
		fields   []string
		expected bool
	}{
		{
			name:     "true value",
			obj:      obj,
			fields:   []string{"spec", "enabled"},
			expected: true,
		},
		{
			name:     "false value",
			obj:      obj,
			fields:   []string{"spec", "nested", "disabled"},
			expected: false,
		},
		{
			name:     "missing path returns false",
			obj:      obj,
			fields:   []string{"spec", "nonexistent"},
			expected: false,
		},
		{
			name:     "nil map returns false",
			obj:      nil,
			fields:   []string{"spec", "enabled"},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GetNestedBool(tt.obj, tt.fields...)
			if got != tt.expected {
				t.Errorf("GetNestedBool() = %v, want %v", got, tt.expected)
			}
		})
	}
}
