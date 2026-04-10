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
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/butlerdotdev/butler/internal/common/client"
	"github.com/butlerdotdev/butler/internal/common/output"
	"github.com/butlerdotdev/butler/internal/tui/components"
	"github.com/butlerdotdev/butler/internal/tui/styles"
)

const networkNamespace = "butler-system"

// networkActionMode tracks the current interaction state.
type networkActionMode int

const (
	networkModeNormal  networkActionMode = iota
	networkModeConfirm                   // y/n confirmation
	networkModeResult                    // success/error message displayed
)

type networkListMsg struct {
	rows [][]string
	err  error
}

// networkActionResultMsg carries the result of an async network action.
type networkActionResultMsg struct {
	err error
}

// NetworkListView displays NetworkPool resources.
type NetworkListView struct {
	client  *client.Client
	Admin   bool
	table   components.Table
	loading bool
	err     error
	width   int
	height  int

	// Action state
	mode          networkActionMode
	confirmMsg    string
	resultMsg     string
	resultIsError bool
}

// NewNetworkListView creates the network pool list view.
func NewNetworkListView(c *client.Client, admin bool) NetworkListView {
	return NetworkListView{
		client:  c,
		Admin:   admin,
		table:   components.NewTable([]string{"NAME", "CIDR", "TOTAL", "ALLOCATED", "AVAILABLE", "UTILIZATION", "AGE"}),
		loading: true,
	}
}

// IsFiltering returns true if the table is in filter mode.
func (v *NetworkListView) IsFiltering() bool {
	return v.table.Filtering()
}

// IsInActionMode returns true when a network action prompt is active.
func (v *NetworkListView) IsInActionMode() bool {
	return v.mode != networkModeNormal
}

// Init fetches network pool data.
func (v NetworkListView) Init() tea.Cmd {
	return v.fetch()
}

// Update handles messages.
func (v NetworkListView) Update(msg tea.Msg) (NetworkListView, tea.Cmd) {
	switch msg := msg.(type) {
	case networkListMsg:
		v.loading = false
		v.err = msg.err
		if msg.err == nil {
			v.table.SetRows(msg.rows)
		}
		return v, nil

	case networkActionResultMsg:
		v.loading = false
		if msg.err != nil {
			v.mode = networkModeResult
			v.resultMsg = msg.err.Error()
			v.resultIsError = true
		} else {
			v.mode = networkModeResult
			v.resultMsg = "Network pool deleted. Press any key to refresh."
			v.resultIsError = false
		}
		return v, nil

	case tea.WindowSizeMsg:
		v.width = msg.Width
		v.height = msg.Height
		v.table.SetSize(msg.Width, msg.Height-4)
		return v, nil

	case tea.KeyMsg:
		switch v.mode {
		case networkModeConfirm:
			return v.updateConfirmMode(msg)
		case networkModeResult:
			return v.updateResultMode(msg)
		default:
			return v.updateNormalMode(msg)
		}
	}
	return v, nil
}

// updateNormalMode handles keys when no action is active.
func (v NetworkListView) updateNormalMode(msg tea.KeyMsg) (NetworkListView, tea.Cmd) {
	if v.table.Filtering() {
		cmd := v.table.Update(msg)
		return v, cmd
	}
	switch msg.String() {
	case "r":
		v.loading = true
		return v, v.fetch()
	case "d":
		if !v.Admin {
			return v, nil
		}
		row := v.table.SelectedRow()
		if row != nil && len(row) > 0 {
			v.mode = networkModeConfirm
			v.confirmMsg = fmt.Sprintf("Delete network pool %s? (y/n)", row[0])
			return v, nil
		}
	default:
		cmd := v.table.Update(msg)
		return v, cmd
	}
	return v, nil
}

// updateConfirmMode handles keys when a y/n prompt is active.
func (v NetworkListView) updateConfirmMode(msg tea.KeyMsg) (NetworkListView, tea.Cmd) {
	switch msg.String() {
	case "y", "Y":
		v.loading = true
		row := v.table.SelectedRow()
		poolName := ""
		if row != nil && len(row) > 0 {
			poolName = row[0]
		}
		c := v.client
		return v, func() tea.Msg {
			ctx, cancel := apiContext()
			defer cancel()
			err := c.Dynamic.Resource(client.NetworkPoolGVR).Namespace(networkNamespace).Delete(
				ctx, poolName, metav1.DeleteOptions{},
			)
			if err != nil {
				return networkActionResultMsg{err: fmt.Errorf("deleting network pool: %w", err)}
			}
			return networkActionResultMsg{}
		}
	case "n", "N", "esc":
		v.mode = networkModeNormal
		return v, nil
	}
	return v, nil
}

// updateResultMode handles keys when showing a result message.
func (v NetworkListView) updateResultMode(msg tea.KeyMsg) (NetworkListView, tea.Cmd) {
	v.mode = networkModeNormal
	v.resultMsg = ""
	v.loading = true
	return v, v.fetch()
}

// KeyLegend returns the action keys available for the network view.
func (v *NetworkListView) KeyLegend() string {
	dimStyle := styles.DimStyle
	keyStyle := styles.KeyLegendStyle

	legend := dimStyle.Render("  ") +
		keyStyle.Render("j/k") + dimStyle.Render(":navigate  ")

	if v.Admin {
		legend += keyStyle.Render("d") + dimStyle.Render(":delete  ")
	}

	legend += keyStyle.Render("/") + dimStyle.Render(":filter  ") +
		keyStyle.Render("r") + dimStyle.Render(":refresh  ") +
		keyStyle.Render("?") + dimStyle.Render(":help  ") +
		keyStyle.Render("q") + dimStyle.Render(":quit")

	return legend
}

// View renders the network pool list.
func (v NetworkListView) View() string {
	if v.loading {
		return "  Loading network pools..."
	}
	if v.err != nil {
		return fmt.Sprintf("  Error: %v", v.err)
	}

	var b strings.Builder
	b.WriteString(v.table.View())

	// Action prompt/result overlay
	if prompt := v.renderActionPrompt(); prompt != "" {
		b.WriteString("\n")
		b.WriteString(prompt)
	}

	return b.String()
}

// renderActionPrompt renders the inline prompt for the current action mode.
func (v NetworkListView) renderActionPrompt() string {
	switch v.mode {
	case networkModeConfirm:
		return styles.ActionConfirmStyle.Render(v.confirmMsg)
	case networkModeResult:
		if v.resultIsError {
			return styles.ActionErrorStyle.Render("Error: " + v.resultMsg)
		}
		return styles.ActionSuccessStyle.Render(v.resultMsg)
	}
	return ""
}

func (v NetworkListView) fetch() tea.Cmd {
	c := v.client
	return func() tea.Msg {
		ctx, cancel := apiContext()
		defer cancel()
		list, err := c.Dynamic.Resource(client.NetworkPoolGVR).Namespace(networkNamespace).List(ctx, metav1.ListOptions{})
		if err != nil {
			return networkListMsg{err: fmt.Errorf("listing NetworkPools: %w", err)}
		}

		rows := make([][]string, len(list.Items))
		for i, np := range list.Items {
			totalIPs := client.GetNestedInt64(np.Object, "status", "totalIPs")
			allocatedIPs := client.GetNestedInt64(np.Object, "status", "allocatedIPs")
			availableIPs := totalIPs - allocatedIPs

			utilization := "0.0%"
			if totalIPs > 0 {
				utilization = fmt.Sprintf("%.1f%%", float64(allocatedIPs)/float64(totalIPs)*100)
			}

			rows[i] = []string{
				np.GetName(),
				client.GetNestedString(np.Object, "spec", "cidr"),
				fmt.Sprintf("%d", totalIPs),
				fmt.Sprintf("%d", allocatedIPs),
				fmt.Sprintf("%d", availableIPs),
				utilization,
				output.FormatAge(np.GetCreationTimestamp().Time),
			}
		}
		return networkListMsg{rows: rows}
	}
}
