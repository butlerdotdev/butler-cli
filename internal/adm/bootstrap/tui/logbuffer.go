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

package tui

import (
	"strings"
	"sync"
)

// LogBuffer is a thread-safe ring buffer for formatted log lines.
type LogBuffer struct {
	mu    sync.Mutex
	lines []string
	max   int
}

// NewLogBuffer creates a ring buffer that retains up to max lines.
func NewLogBuffer(max int) *LogBuffer {
	return &LogBuffer{
		lines: make([]string, 0, max),
		max:   max,
	}
}

// Write appends a line, evicting the oldest if full.
func (b *LogBuffer) Write(line string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.lines) >= b.max {
		b.lines = b.lines[1:]
	}
	b.lines = append(b.lines, line)
}

// Lines returns a snapshot of all buffered lines.
func (b *LogBuffer) Lines() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]string, len(b.lines))
	copy(out, b.lines)
	return out
}

// Content returns all buffered lines joined by newlines.
func (b *LogBuffer) Content() string {
	lines := b.Lines()
	return strings.Join(lines, "\n")
}

// Len returns the current number of buffered lines.
func (b *LogBuffer) Len() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.lines)
}
