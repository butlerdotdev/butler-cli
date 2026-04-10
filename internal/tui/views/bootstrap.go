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

	// Requested is set to true when the user presses Enter on this view.
	// The app's Update method checks this and returns tea.Quit so cmd.go
	// can take over the bootstrap flow outside the Bubbletea program.
	Requested bool
}

// NewBootstrapView constructs the tab 0 launcher view. client may be nil
// when the dashboard starts without a valid kubeconfig — the view renders
// a "no cluster connected" state in that case.
func NewBootstrapView(c *client.Client, contextName string) BootstrapView {
	return BootstrapView{
		client:      c,
		contextName: contextName,
	}
}

// Init is a no-op — the bootstrap view has no async data to fetch.
func (v BootstrapView) Init() tea.Cmd {
	return nil
}

// Update handles Enter to request the bootstrap flow. Other keys are ignored
// so global dashboard navigation (1-7, q, ?) can still flow through.
func (v BootstrapView) Update(msg tea.Msg) (BootstrapView, tea.Cmd) {
	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		if keyMsg.String() == "enter" {
			v.Requested = true
			return v, tea.Quit
		}
	}
	if sizeMsg, ok := msg.(tea.WindowSizeMsg); ok {
		v.width = sizeMsg.Width
		v.height = sizeMsg.Height
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

	// Launcher panel.
	launcherTitle := styles.SectionStyle.Render("Bootstrap a New Management Cluster")
	b.WriteString(launcherTitle)
	b.WriteString("\n\n")

	dim := lipgloss.NewStyle().Foreground(styles.ColorMuted)
	accent := lipgloss.NewStyle().Foreground(styles.ColorPrimary).Bold(true)

	b.WriteString(dim.Render("The bootstrap wizard walks you through creating a Butler"))
	b.WriteString("\n")
	b.WriteString(dim.Render("management cluster on Harvester or Nutanix. It will:"))
	b.WriteString("\n\n")
	b.WriteString(dim.Render("  1. Collect provider credentials"))
	b.WriteString("\n")
	b.WriteString(dim.Render("  2. Discover available namespaces/networks/images"))
	b.WriteString("\n")
	b.WriteString(dim.Render("  3. Ask for cluster sizing and networking"))
	b.WriteString("\n")
	b.WriteString(dim.Render("  4. Sync the Talos image if needed"))
	b.WriteString("\n")
	b.WriteString(dim.Render("  5. Launch the bootstrap with a live progress view"))
	b.WriteString("\n\n")

	if v.client == nil {
		b.WriteString(accent.Render("  No management cluster connected."))
		b.WriteString("\n\n")
	}

	b.WriteString(accent.Render("  Press enter to launch the bootstrap wizard."))
	b.WriteString("\n")
	b.WriteString(dim.Render("  In the wizard: j/k navigate  enter next  esc back  ctrl+c cancel"))
	b.WriteString("\n")

	return b.String()
}

// KeyLegend returns the action keys for the status bar.
func (v BootstrapView) KeyLegend() string {
	dim := styles.DimStyle
	keyStyle := styles.KeyLegendStyle
	return dim.Render("  ") +
		keyStyle.Render("enter") + dim.Render(":launch wizard  ") +
		keyStyle.Render("?") + dim.Render(":help  ") +
		keyStyle.Render("q") + dim.Render(":quit")
}

