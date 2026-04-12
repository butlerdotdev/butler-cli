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

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"

	"github.com/butlerdotdev/butler/internal/tui/styles"
)

// wizardShell is a tea.Model that renders a dashboard-style header at the
// top and a key legend at the bottom, wrapping a huh.Form (or any other
// tea.Model) in between. It's how the wizard achieves the same visual
// identity as the dashboard TUI even though huh renders its own content.
//
// The shell is a lightweight composition: it forwards all messages to the
// inner content, intercepts tea.WindowSizeMsg to reserve space for the
// chrome, and detects huh.Form completion so the program quits cleanly.
type wizardShell struct {
	content tea.Model
	form    *huh.Form // non-nil when content is a huh.Form; used to detect State
	title   string    // shown in the header bar

	width  int
	height int
}

// newFormShell wraps a huh.Form in a wizardShell. The form is stored by
// pointer so the shell can inspect form.State after the program quits.
func newFormShell(form *huh.Form, title string) *wizardShell {
	return &wizardShell{
		content: form,
		form:    form,
		title:   title,
	}
}

// Init delegates to the wrapped content's Init.
func (s *wizardShell) Init() tea.Cmd {
	return s.content.Init()
}

// Update handles window sizing (reserves space for header and footer,
// forwards a reduced size to the content), form completion detection,
// and general message forwarding.
func (s *wizardShell) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if size, ok := msg.(tea.WindowSizeMsg); ok {
		s.width = size.Width
		s.height = size.Height
		// Reserve 2 header lines + 1 spacer + 2 footer lines = 5 total.
		inner := tea.WindowSizeMsg{
			Width:  size.Width,
			Height: size.Height - 5,
		}
		if inner.Height < 1 {
			inner.Height = 1
		}
		var cmd tea.Cmd
		s.content, cmd = s.content.Update(inner)
		return s, cmd
	}

	var cmd tea.Cmd
	s.content, cmd = s.content.Update(msg)

	// If the wrapped content is a huh.Form and it has reached a terminal
	// state, quit the program so wizard.Run can inspect form.State and
	// decide what to do next.
	if s.form != nil && s.form.State != huh.StateNormal {
		return s, tea.Quit
	}

	return s, cmd
}

// View composes the header, content, and footer into a single frame.
func (s *wizardShell) View() string {
	var b strings.Builder

	// Header bar — identical style to the dashboard's top bar.
	header := styles.HeaderStyle.Render(" " + s.title + " ")
	b.WriteString(header)
	b.WriteString("\n\n")

	// Content body (huh form, discovery model, etc.).
	b.WriteString(s.content.View())

	// Pad to push the footer to the bottom of the allotted area.
	contentLines := strings.Count(s.content.View(), "\n") + 1
	// Header (2) + content + spacer (1) + footer (1) = total we want == s.height
	// So pad until content fills the space above the footer.
	padTarget := s.height - 3 - 1 // header + footer lines
	for i := contentLines; i < padTarget; i++ {
		b.WriteString("\n")
	}
	b.WriteString("\n")

	// Key legend — same format as the dashboard's bottom bar.
	b.WriteString(renderWizardLegend())

	return b.String()
}

// renderWizardLegend returns the wizard's key legend in the exact style
// used by the dashboard's bottom bar: bold gray keys over dim separators.
func renderWizardLegend() string {
	dim := styles.DimStyle
	keyStyle := styles.KeyLegendStyle
	return dim.Render("  ") +
		keyStyle.Render("j/k") + dim.Render(":navigate  ") +
		keyStyle.Render("enter") + dim.Render(":next  ") +
		keyStyle.Render("/") + dim.Render(":filter  ") +
		keyStyle.Render("esc") + dim.Render(":back  ") +
		keyStyle.Render("ctrl+c") + dim.Render(":quit")
}

// runFormShell wraps a huh.Form in a wizardShell and runs the resulting
// tea.Program in alt-screen mode. Inspect form.State after this returns
// to determine whether the user submitted or aborted.
func runFormShell(form *huh.Form, title string) error {
	shell := newFormShell(form, title)
	_, err := tea.NewProgram(shell, tea.WithAltScreen()).Run()
	return err
}
