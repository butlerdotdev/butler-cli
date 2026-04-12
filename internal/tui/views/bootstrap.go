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
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/butlerdotdev/butler/internal/common/client"
	"github.com/butlerdotdev/butler/internal/tui/bootstrap"
	"github.com/butlerdotdev/butler/internal/tui/components"
	"github.com/butlerdotdev/butler/internal/tui/styles"
)

// BootstrapView is the launcher for the interactive bootstrap flow. It is
// tab 0 of the dashboard and is only shown in admin mode. When the user
// presses Enter, the dashboard quits with a Requested flag set; the entry
// point loop in internal/tui/cmd.go catches the flag, runs the wizard and
// bootstrap progress TUI, then relaunches the dashboard with the new
// kubeconfig.
type BootstrapView struct {
	client      *client.Client // may be nil before first bootstrap
	contextName string
	width       int
	height      int

	// Prerequisite check state. Checks run asynchronously in Init and
	// arrive as a prereqResultMsg. Enter is gated on required checks
	// passing. 'r' re-runs the checks.
	checks        []bootstrap.Check
	checksLoaded  bool
	checksRunning bool

	// Requested is set to true when the user presses Enter on this view.
	// The app's Update method checks this and returns tea.Quit so cmd.go
	// can take over the bootstrap flow outside the Bubbletea program.
	Requested bool
}

// prereqResultMsg carries the outcome of a prerequisite check run.
type prereqResultMsg []bootstrap.Check

// NewBootstrapView constructs the tab 0 launcher view. client may be nil
// when the dashboard starts without a valid kubeconfig — the view renders
// a "no cluster connected" state in that case.
func NewBootstrapView(c *client.Client, contextName string) BootstrapView {
	return BootstrapView{
		client:      c,
		contextName: contextName,
	}
}

// Init kicks off the prerequisite checks asynchronously.
func (v BootstrapView) Init() tea.Cmd {
	return runPrereqChecks
}

// runPrereqChecks is a tea.Cmd that runs the bootstrap prerequisite checks
// and returns the result as a prereqResultMsg. Runs docker+kubectl as
// required and kind as optional (warning-only) since kind is only needed
// for --local dev mode image loading.
func runPrereqChecks() tea.Msg {
	return prereqResultMsg(bootstrap.CheckAll(false))
}

// Update handles key events and prereq results.
func (v BootstrapView) Update(msg tea.Msg) (BootstrapView, tea.Cmd) {
	switch msg := msg.(type) {
	case prereqResultMsg:
		v.checks = []bootstrap.Check(msg)
		v.checksLoaded = true
		v.checksRunning = false
		return v, nil
	case tea.WindowSizeMsg:
		v.width = msg.Width
		v.height = msg.Height
		return v, nil
	case tea.KeyMsg:
		switch msg.String() {
		case "enter":
			// Gate wizard launch on required prereqs passing.
			if !v.checksLoaded || !bootstrap.AllPassed(v.checks) {
				return v, nil
			}
			v.Requested = true
			return v, tea.Quit
		case "r":
			v.checksRunning = true
			v.checksLoaded = false
			return v, runPrereqChecks
		}
	}
	return v, nil
}

// IsFiltering satisfies the dashboard's active-view interface.
func (v BootstrapView) IsFiltering() bool { return false }

// IsInActionMode satisfies the dashboard's active-view interface.
func (v BootstrapView) IsInActionMode() bool { return false }

// View renders the bootstrap launcher panel.
func (v BootstrapView) View() string {
	var b strings.Builder

	// Cluster status panel.
	var statusLines []components.DetailField
	if v.client == nil {
		statusLines = []components.DetailField{
			{Label: "Status", Value: "Not connected"},
			{Label: "Cluster", Value: "None"},
			{Label: "Context", Value: "—"},
		}
	} else {
		statusLines = []components.DetailField{
			{Label: "Status", Value: "Connected"},
			{Label: "Context", Value: v.contextName},
		}
	}

	statusPane := components.DetailPane{
		Sections: []components.DetailSection{
			{Title: "Current Management Cluster", Fields: statusLines},
		},
	}
	b.WriteString(statusPane.View())
	b.WriteString("\n\n")

	// Prerequisites panel.
	b.WriteString(styles.SectionStyle.Render("Prerequisites"))
	b.WriteString("\n\n")
	b.WriteString(v.renderPrereqs())
	b.WriteString("\n")

	// Launcher panel.
	launcherTitle := styles.SectionStyle.Render("Bootstrap a New Management Cluster")
	b.WriteString(launcherTitle)
	b.WriteString("\n\n")

	dim := lipgloss.NewStyle().Foreground(styles.ColorMuted)
	accent := lipgloss.NewStyle().Foreground(styles.ColorPrimary).Bold(true)
	warn := lipgloss.NewStyle().Foreground(styles.ColorDanger).Bold(true)

	b.WriteString(dim.Render("The wizard will collect credentials, discover provider"))
	b.WriteString("\n")
	b.WriteString(dim.Render("resources, ask for cluster sizing, and launch a live"))
	b.WriteString("\n")
	b.WriteString(dim.Render("bootstrap progress view."))
	b.WriteString("\n\n")

	switch {
	case !v.checksLoaded:
		b.WriteString(dim.Render("  Checking prerequisites..."))
	case !bootstrap.AllPassed(v.checks):
		b.WriteString(warn.Render("  Prerequisites failed. Fix the issues above, then press r to recheck."))
	default:
		b.WriteString(accent.Render("  Press enter to launch the bootstrap wizard."))
		b.WriteString("\n")
		b.WriteString(dim.Render("  In the wizard: j/k navigate  enter next  esc back  ctrl+c cancel"))
	}
	b.WriteString("\n")

	return b.String()
}

// renderPrereqs renders the list of prerequisite checks with colored icons.
func (v BootstrapView) renderPrereqs() string {
	if !v.checksLoaded {
		return styles.DimStyle.Render("  running...")
	}

	ok := lipgloss.NewStyle().Foreground(styles.ColorPrimary).Bold(true)
	fail := lipgloss.NewStyle().Foreground(styles.ColorDanger).Bold(true)
	warn := lipgloss.NewStyle().Foreground(styles.ColorWarning).Bold(true)
	dim := styles.DimStyle

	var b strings.Builder
	for _, c := range v.checks {
		var icon, name string
		switch {
		case c.Passed:
			icon = ok.Render("✓")
			name = ok.Render(c.Name)
		case c.Optional:
			icon = warn.Render("!")
			name = warn.Render(c.Name + " (optional)")
		default:
			icon = fail.Render("✗")
			name = fail.Render(c.Name)
		}
		b.WriteString("  ")
		b.WriteString(icon)
		b.WriteString("  ")
		b.WriteString(name)
		b.WriteString(dim.Render(" — " + c.Detail))
		b.WriteString("\n")
	}
	return b.String()
}

// KeyLegend returns the action keys for the status bar.
func (v BootstrapView) KeyLegend() string {
	dim := styles.DimStyle
	keyStyle := styles.KeyLegendStyle
	return dim.Render("  ") +
		keyStyle.Render("enter") + dim.Render(":launch wizard  ") +
		keyStyle.Render("r") + dim.Render(":recheck prereqs  ") +
		keyStyle.Render("?") + dim.Render(":help  ") +
		keyStyle.Render("q") + dim.Render(":quit")
}
