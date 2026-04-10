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

// Package styles defines the Butler brand lipgloss styles shared across
// the TUI components and views.
package styles

import "github.com/charmbracelet/lipgloss"

// Butler brand palette.
var (
	ColorPrimary = lipgloss.Color("#22c55e") // Butler green
	ColorAccent  = lipgloss.Color("#4ade80") // Light green
	ColorAdmin   = lipgloss.Color("#8b5cf6") // Violet
	ColorDanger  = lipgloss.Color("#ef4444") // Red
	ColorWarning = lipgloss.Color("#eab308") // Yellow
	ColorInfo    = lipgloss.Color("#3b82f6") // Blue
	ColorMuted   = lipgloss.Color("#737373") // Gray
	ColorText    = lipgloss.Color("#fafafa") // Primary text
	ColorSurface = lipgloss.Color("#1c1c1c") // Dark surface
	ColorBorder  = lipgloss.Color("#404040") // Border gray
)

// Header bar across the top of the dashboard.
var HeaderStyle = lipgloss.NewStyle().
	Background(ColorPrimary).
	Foreground(lipgloss.Color("#000000")).
	Bold(true).
	Padding(0, 1)

// Tab bar styles.
var (
	TabStyle = lipgloss.NewStyle().
			Foreground(ColorMuted).
			Padding(0, 2)

	ActiveTabStyle = lipgloss.NewStyle().
			Foreground(ColorPrimary).
			Bold(true).
			Padding(0, 2).
			Underline(true)
)

// Table styles.
var (
	TableHeaderStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(ColorInfo)

	TableRowStyle = lipgloss.NewStyle().
			Foreground(ColorText)

	SelectedRowStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(ColorPrimary)
)

// Status badge styles.
var (
	BadgeReady        = lipgloss.NewStyle().Foreground(ColorPrimary).Bold(true)
	BadgeProvisioning = lipgloss.NewStyle().Foreground(ColorWarning)
	BadgeFailed       = lipgloss.NewStyle().Foreground(ColorDanger).Bold(true)
	BadgePending      = lipgloss.NewStyle().Foreground(ColorMuted)
	BadgeDeleting     = lipgloss.NewStyle().Foreground(ColorAdmin)
)

// Detail pane styles.
var (
	LabelStyle   = lipgloss.NewStyle().Foreground(ColorInfo).Width(20)
	ValueStyle   = lipgloss.NewStyle().Foreground(ColorText)
	SectionStyle = lipgloss.NewStyle().Bold(true).Foreground(ColorAccent).MarginTop(1)
)

// Help text.
var HelpStyle = lipgloss.NewStyle().Foreground(ColorMuted)

// ColorizePhase returns a styled phase string.
func ColorizePhase(phase string) string {
	switch phase {
	case "Ready":
		return BadgeReady.Render(phase)
	case "Provisioning", "Installing", "Updating":
		return BadgeProvisioning.Render(phase)
	case "Failed":
		return BadgeFailed.Render(phase)
	case "Deleting":
		return BadgeDeleting.Render(phase)
	default:
		return BadgePending.Render(phase)
	}
}

// WarningBannerStyle renders a warning banner.
var WarningBannerStyle = lipgloss.NewStyle().
	Foreground(ColorWarning).
	Background(lipgloss.Color("#3a2a00")).
	Bold(true)

// DimStyle renders dimmed help text.
var DimStyle = lipgloss.NewStyle().
	Foreground(lipgloss.Color("#555555"))

// KeyLegendStyle renders key names in the legend bar.
var KeyLegendStyle = lipgloss.NewStyle().
	Foreground(lipgloss.Color("#888888")).
	Bold(true)

// Action prompt styles for inline input, confirmation, and result display.
var (
	ActionPromptStyle = lipgloss.NewStyle().
				Foreground(ColorInfo).
				Bold(true)

	ActionInputStyle = lipgloss.NewStyle().
				Foreground(ColorText)

	ActionSuccessStyle = lipgloss.NewStyle().
				Foreground(ColorPrimary).
				Bold(true)

	ActionErrorStyle = lipgloss.NewStyle().
				Foreground(ColorDanger).
				Bold(true)

	ActionConfirmStyle = lipgloss.NewStyle().
				Foreground(ColorWarning).
				Bold(true)
)
