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

import "github.com/charmbracelet/lipgloss"

// Butler brand palette (matches butler-console CSS custom properties)
var (
	colorBrand   = lipgloss.Color("#22c55e") // emerald green
	colorSuccess = lipgloss.Color("#22c55e")
	colorWarning = lipgloss.Color("#eab308") // yellow
	colorError   = lipgloss.Color("#ef4444") // red
	colorInfo    = lipgloss.Color("#3b82f6") // blue
	colorMuted   = lipgloss.Color("#a3a3a3") // gray
	colorBorder = lipgloss.Color("#404040") // dark gray
	colorText   = lipgloss.Color("#fafafa") // primary text
	colorBg     = lipgloss.Color("#0a0a0a") // near-black background
)

// Panel border styles
var (
	activeBorder  = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(colorBrand).Padding(0, 1)
	normalBorder  = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(colorBorder).Padding(0, 1)
	titleBarStyle = lipgloss.NewStyle().Background(colorBrand).Foreground(colorBg).Bold(true).Padding(0, 1)
)

// Phase indicator styles
var (
	phaseCompleted = lipgloss.NewStyle().Foreground(colorSuccess)
	phaseActive    = lipgloss.NewStyle().Foreground(colorWarning).Bold(true)
	phasePending   = lipgloss.NewStyle().Foreground(colorMuted)
	phaseFailed    = lipgloss.NewStyle().Foreground(colorError).Bold(true)
)

// Machine status styles
var (
	machineRunning = lipgloss.NewStyle().Foreground(colorSuccess)
	machinePending = lipgloss.NewStyle().Foreground(colorMuted)
)

// Addon styles
var (
	addonInstalled = lipgloss.NewStyle().Foreground(colorSuccess)
	addonPending   = lipgloss.NewStyle().Foreground(colorMuted)
)

// Status icons (Unicode)
const (
	iconCompleted = "\u2713" // ✓
	iconPending   = "\u25CB" // ○
	iconFailed    = "\u2717" // ✗
)

// Post-bootstrap and generic styles
var (
	successStyle = lipgloss.NewStyle().Foreground(colorSuccess).Bold(true)
	failureStyle = lipgloss.NewStyle().Foreground(colorError).Bold(true)
	labelStyle   = lipgloss.NewStyle().Foreground(colorInfo).Width(18)
	valueStyle   = lipgloss.NewStyle().Foreground(colorText)
	dimStyle     = lipgloss.NewStyle().Foreground(colorMuted)
	helpBarStyle = lipgloss.NewStyle().Foreground(colorMuted)
	sectionStyle = lipgloss.NewStyle().Bold(true).Foreground(colorInfo)
)
