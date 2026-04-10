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

package components

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/butlerdotdev/butler/internal/tui/styles"
)

// Table is a scrollable, filterable table component.
type Table struct {
	headers   []string
	rows      [][]string
	filtered  []int // indices into rows
	cursor    int
	offset    int
	height    int
	width     int
	filter    string
	filtering bool
	colWidths []int
}

// NewTable creates a table with the given headers.
func NewTable(headers []string) Table {
	return Table{
		headers: headers,
		height:  20,
	}
}

// SetRows replaces the table data and resets the cursor.
func (t *Table) SetRows(rows [][]string) {
	t.rows = rows
	t.recalcWidths()
	t.applyFilter()
	if t.cursor >= len(t.filtered) {
		t.cursor = max(0, len(t.filtered)-1)
	}
	t.clampOffset()
}

// SetSize updates the available render dimensions.
func (t *Table) SetSize(width, height int) {
	t.width = width
	// Reserve 2 lines for header and filter bar
	t.height = max(1, height-2)
	t.clampOffset()
}

// SelectedRow returns the currently selected row data, or nil.
func (t *Table) SelectedRow() []string {
	if len(t.filtered) == 0 || t.cursor < 0 || t.cursor >= len(t.filtered) {
		return nil
	}
	return t.rows[t.filtered[t.cursor]]
}

// Filtering returns whether the filter input is active.
func (t *Table) Filtering() bool {
	return t.filtering
}

// Update handles key events for navigation, filtering, and selection.
func (t *Table) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if t.filtering {
			return t.updateFilter(msg)
		}

		switch {
		case key.Matches(msg, key.NewBinding(key.WithKeys("up", "k"))):
			t.moveUp()
		case key.Matches(msg, key.NewBinding(key.WithKeys("down", "j"))):
			t.moveDown()
		case key.Matches(msg, key.NewBinding(key.WithKeys("/"))):
			t.filtering = true
		case key.Matches(msg, key.NewBinding(key.WithKeys("G"))):
			t.cursor = max(0, len(t.filtered)-1)
			t.clampOffset()
		case key.Matches(msg, key.NewBinding(key.WithKeys("g"))):
			t.cursor = 0
			t.clampOffset()
		}
	}
	return nil
}

func (t *Table) updateFilter(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "enter", "esc":
		t.filtering = false
		if msg.String() == "esc" {
			t.filter = ""
			t.applyFilter()
		}
	case "backspace":
		if len(t.filter) > 0 {
			t.filter = t.filter[:len(t.filter)-1]
			t.applyFilter()
		}
	default:
		if len(msg.String()) == 1 {
			t.filter += msg.String()
			t.applyFilter()
		}
	}
	return nil
}

func (t *Table) moveUp() {
	if t.cursor > 0 {
		t.cursor--
		t.clampOffset()
	}
}

func (t *Table) moveDown() {
	if t.cursor < len(t.filtered)-1 {
		t.cursor++
		t.clampOffset()
	}
}

func (t *Table) clampOffset() {
	if t.cursor < t.offset {
		t.offset = t.cursor
	}
	if t.cursor >= t.offset+t.height {
		t.offset = t.cursor - t.height + 1
	}
	if t.offset < 0 {
		t.offset = 0
	}
}

func (t *Table) applyFilter() {
	t.filtered = t.filtered[:0]
	lower := strings.ToLower(t.filter)
	for i, row := range t.rows {
		if t.filter == "" {
			t.filtered = append(t.filtered, i)
			continue
		}
		for _, col := range row {
			if strings.Contains(strings.ToLower(col), lower) {
				t.filtered = append(t.filtered, i)
				break
			}
		}
	}
	if t.cursor >= len(t.filtered) {
		t.cursor = max(0, len(t.filtered)-1)
	}
	t.offset = 0
}

func (t *Table) recalcWidths() {
	t.colWidths = make([]int, len(t.headers))
	for i, h := range t.headers {
		t.colWidths[i] = len(h)
	}
	for _, row := range t.rows {
		for i, col := range row {
			if i < len(t.colWidths) && len(col) > t.colWidths[i] {
				t.colWidths[i] = len(col)
			}
		}
	}
}

// View renders the table.
func (t *Table) View() string {
	var b strings.Builder

	// Header row
	headerParts := make([]string, len(t.headers))
	for i, h := range t.headers {
		w := t.colWidths[i] + 2
		headerParts[i] = styles.TableHeaderStyle.Render(padRight(h, w))
	}
	b.WriteString(strings.Join(headerParts, ""))
	b.WriteString("\n")

	// Data rows
	if len(t.filtered) == 0 {
		b.WriteString(styles.HelpStyle.Render("  No items found"))
		b.WriteString("\n")
	} else {
		end := min(t.offset+t.height, len(t.filtered))
		for vi := t.offset; vi < end; vi++ {
			row := t.rows[t.filtered[vi]]
			style := styles.TableRowStyle
			if vi == t.cursor {
				style = styles.SelectedRowStyle
			}

			parts := make([]string, len(t.headers))
			for ci := range t.headers {
				w := t.colWidths[ci] + 2
				val := ""
				if ci < len(row) {
					val = row[ci]
				}
				if vi == t.cursor {
					parts[ci] = style.Render(padRight(val, w))
				} else {
					parts[ci] = renderCell(val, w, style)
				}
			}
			b.WriteString(strings.Join(parts, ""))
			b.WriteString("\n")
		}
	}

	// Filter bar
	if t.filtering {
		b.WriteString(fmt.Sprintf("/%s_", t.filter))
	} else if t.filter != "" {
		b.WriteString(styles.HelpStyle.Render(fmt.Sprintf("filter: %s", t.filter)))
	}

	return b.String()
}

// renderCell applies phase-aware coloring to certain cell values.
func renderCell(val string, width int, base lipgloss.Style) string {
	switch val {
	case "Ready":
		return styles.BadgeReady.Render(padRight(val, width))
	case "Provisioning", "Installing", "Updating":
		return styles.BadgeProvisioning.Render(padRight(val, width))
	case "Failed":
		return styles.BadgeFailed.Render(padRight(val, width))
	case "Deleting":
		return styles.BadgeDeleting.Render(padRight(val, width))
	default:
		return base.Render(padRight(val, width))
	}
}

func padRight(s string, width int) string {
	if len(s) >= width {
		return s
	}
	return s + strings.Repeat(" ", width-len(s))
}
