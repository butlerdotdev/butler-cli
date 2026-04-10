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

package views

import (
	"context"
	"strings"
	"time"

	"github.com/butlerdotdev/butler/internal/common/output"
)

// apiTimeout is the maximum duration for any Kubernetes API call made from
// a TUI view. A hung apiserver must not freeze the dashboard.
const apiTimeout = 30 * time.Second

// apiContext returns a background context with the standard TUI API timeout.
// Callers MUST defer the returned cancel function.
func apiContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), apiTimeout)
}

// formatAge parses an RFC3339 timestamp and returns a human-readable age.
func formatAge(ts string) string {
	if ts == "" {
		return "<unknown>"
	}
	t, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		t, err = time.Parse("2006-01-02T15:04:05Z", ts)
		if err != nil {
			return "<unknown>"
		}
	}
	return output.FormatAge(t)
}

// padRight pads a string to the given width with spaces.
func padRight(s string, width int) string {
	if len(s) >= width {
		return s
	}
	return s + strings.Repeat(" ", width-len(s))
}
