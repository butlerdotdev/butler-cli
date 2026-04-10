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

	"github.com/charmbracelet/lipgloss"

	"github.com/butlerdotdev/butler/internal/tui/styles"
)

// StatusBar renders a bottom bar with view name, context, and help hint.
type StatusBar struct {
	ViewName string
	Context  string
	Width    int
}

// View renders the status bar.
func (s StatusBar) View() string {
	style := lipgloss.NewStyle().
		Background(styles.ColorSurface).
		Foreground(styles.ColorText).
		Padding(0, 1)

	left := style.Render(s.ViewName)
	right := styles.HelpStyle.Render("? help")
	center := styles.HelpStyle.Render(s.Context)

	leftLen := lipgloss.Width(left)
	rightLen := lipgloss.Width(right)
	centerLen := lipgloss.Width(center)

	gap := s.Width - leftLen - rightLen - centerLen
	if gap < 2 {
		gap = 2
	}
	leftGap := gap / 2
	rightGap := gap - leftGap

	return fmt.Sprintf("%s%s%s%s%s",
		left,
		strings.Repeat(" ", leftGap),
		center,
		strings.Repeat(" ", rightGap),
		right,
	)
}
