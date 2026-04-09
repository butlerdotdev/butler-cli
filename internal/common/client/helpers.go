// Copyright 2026 The Butler Authors.
// SPDX-License-Identifier: Apache-2.0

package client

import (
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// GetNestedString extracts a string from nested map fields.
func GetNestedString(obj map[string]interface{}, fields ...string) string {
	val, _, _ := unstructured.NestedString(obj, fields...)
	return val
}

// GetNestedInt64 extracts an int64 from nested map fields.
func GetNestedInt64(obj map[string]interface{}, fields ...string) int64 {
	val, _, _ := unstructured.NestedInt64(obj, fields...)
	return val
}

// GetNestedBool extracts a bool from nested map fields.
func GetNestedBool(obj map[string]interface{}, fields ...string) bool {
	val, _, _ := unstructured.NestedBool(obj, fields...)
	return val
}
