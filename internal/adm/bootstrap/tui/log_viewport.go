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

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
)

// logViewportModel wraps a viewport with optional text filtering.
type logViewportModel struct {
	viewport  viewport.Model
	filter    textinput.Model
	filtering bool
	buf       *LogBuffer
	focused   bool
	visible   bool
	lastLen   int // Track buffer length to detect new lines
}

func newLogViewportModel(buf *LogBuffer) logViewportModel {
	vp := viewport.New(80, 6)
	vp.SetContent("")

	fi := textinput.New()
	fi.Placeholder = "filter..."
	fi.CharLimit = 64
	fi.Width = 30

	return logViewportModel{
		viewport: vp,
		filter:   fi,
		buf:      buf,
		visible:  true,
	}
}

func (m logViewportModel) Init() tea.Cmd {
	return nil
}

func (m logViewportModel) Update(msg tea.Msg) (logViewportModel, tea.Cmd) {
	var cmds []tea.Cmd

	if m.filtering {
		var cmd tea.Cmd
		m.filter, cmd = m.filter.Update(msg)
		cmds = append(cmds, cmd)
		// Refresh content with filter applied
		m.refreshContent()
		return m, tea.Batch(cmds...)
	}

	if m.focused {
		var cmd tea.Cmd
		m.viewport, cmd = m.viewport.Update(msg)
		cmds = append(cmds, cmd)
	}

	return m, tea.Batch(cmds...)
}

// Refresh updates the viewport content from the log buffer.
func (m *logViewportModel) Refresh() {
	newLen := m.buf.Len()
	if newLen == m.lastLen {
		return
	}
	m.lastLen = newLen
	m.refreshContent()
}

func (m *logViewportModel) refreshContent() {
	lines := m.buf.Lines()

	// Apply filter if active
	filterText := m.filter.Value()
	if filterText != "" {
		var filtered []string
		lower := strings.ToLower(filterText)
		for _, line := range lines {
			if strings.Contains(strings.ToLower(line), lower) {
				filtered = append(filtered, line)
			}
		}
		lines = filtered
	}

	content := strings.Join(lines, "\n")
	atBottom := m.viewport.AtBottom()
	m.viewport.SetContent(content)
	if atBottom {
		m.viewport.GotoBottom()
	}
}

// SetSize sets the viewport dimensions.
func (m *logViewportModel) SetSize(w, h int) {
	m.viewport.Width = w
	m.viewport.Height = h
}

// SetFocused controls whether the viewport responds to key input.
func (m *logViewportModel) SetFocused(focused bool) {
	m.focused = focused
}

// SetVisible controls viewport visibility.
func (m *logViewportModel) SetVisible(visible bool) {
	m.visible = visible
}

// ToggleVisible toggles viewport visibility.
func (m *logViewportModel) ToggleVisible() {
	m.visible = !m.visible
}

// IsVisible returns whether the viewport is shown.
func (m logViewportModel) IsVisible() bool {
	return m.visible
}

// StartFilter enters filter mode.
func (m *logViewportModel) StartFilter() tea.Cmd {
	m.filtering = true
	m.filter.SetValue("")
	return m.filter.Focus()
}

// StopFilter exits filter mode.
func (m *logViewportModel) StopFilter() {
	m.filtering = false
	m.filter.Blur()
}

// IsFiltering returns whether filter mode is active.
func (m logViewportModel) IsFiltering() bool {
	return m.filtering
}

func (m logViewportModel) View() string {
	if !m.visible {
		return ""
	}

	var b strings.Builder
	if m.filtering {
		b.WriteString("  / " + m.filter.View() + "\n")
	}
	b.WriteString(m.viewport.View())
	return b.String()
}
