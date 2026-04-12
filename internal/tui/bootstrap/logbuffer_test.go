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

import (
	"fmt"
	"sync"
	"testing"
)

func TestLogBuffer_Empty(t *testing.T) {
	b := NewLogBuffer(10)
	if b.Len() != 0 {
		t.Fatalf("expected 0, got %d", b.Len())
	}
	if len(b.Lines()) != 0 {
		t.Fatalf("expected empty lines")
	}
	if b.Content() != "" {
		t.Fatalf("expected empty content")
	}
}

func TestLogBuffer_WriteAndRead(t *testing.T) {
	b := NewLogBuffer(10)
	b.Write("line1")
	b.Write("line2")
	b.Write("line3")

	if b.Len() != 3 {
		t.Fatalf("expected 3, got %d", b.Len())
	}

	lines := b.Lines()
	if len(lines) != 3 || lines[0] != "line1" || lines[2] != "line3" {
		t.Fatalf("unexpected lines: %v", lines)
	}

	if b.Content() != "line1\nline2\nline3" {
		t.Fatalf("unexpected content: %q", b.Content())
	}
}

func TestLogBuffer_Eviction(t *testing.T) {
	b := NewLogBuffer(3)
	for i := 0; i < 5; i++ {
		b.Write(fmt.Sprintf("line%d", i))
	}

	if b.Len() != 3 {
		t.Fatalf("expected 3, got %d", b.Len())
	}

	lines := b.Lines()
	if lines[0] != "line2" || lines[1] != "line3" || lines[2] != "line4" {
		t.Fatalf("expected [line2 line3 line4], got %v", lines)
	}
}

func TestLogBuffer_MaxOne(t *testing.T) {
	b := NewLogBuffer(1)
	b.Write("a")
	b.Write("b")

	if b.Len() != 1 {
		t.Fatalf("expected 1, got %d", b.Len())
	}
	if b.Lines()[0] != "b" {
		t.Fatalf("expected 'b', got %q", b.Lines()[0])
	}
}

func TestLogBuffer_LinesCopy(t *testing.T) {
	b := NewLogBuffer(10)
	b.Write("original")

	lines := b.Lines()
	lines[0] = "modified"

	if b.Lines()[0] != "original" {
		t.Fatal("Lines() should return a copy, not a reference")
	}
}

func TestLogBuffer_ConcurrentAccess(t *testing.T) {
	b := NewLogBuffer(100)
	var wg sync.WaitGroup

	// 10 writers
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				b.Write(fmt.Sprintf("writer%d-line%d", id, j))
			}
		}(i)
	}

	// 5 readers
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				_ = b.Lines()
				_ = b.Len()
				_ = b.Content()
			}
		}()
	}

	wg.Wait()

	// Just verify it didn't panic or deadlock. The ring buffer
	// should have exactly 100 lines (the max), from various writers.
	if b.Len() != 100 {
		t.Fatalf("expected 100, got %d", b.Len())
	}
}
