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
	"context"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/butlerdotdev/butler/internal/common/client"
	"github.com/butlerdotdev/butler/internal/common/output"
	"github.com/butlerdotdev/butler/internal/tui/components"
	"github.com/butlerdotdev/butler/internal/tui/styles"
)

const providerNamespace = "butler-system"

// providerActionMode tracks the current interaction state.
type providerActionMode int

const (
	providerModeNormal  providerActionMode = iota
	providerModeConfirm                    // y/n confirmation
	providerModeResult                     // success/error message displayed
)

type providerListMsg struct {
	rows [][]string
	err  error
}

// providerActionResultMsg carries the result of an async provider action.
type providerActionResultMsg struct {
	err error
}

// ProviderListView displays ProviderConfig resources.
type ProviderListView struct {
	client  *client.Client
	Admin   bool
	table   components.Table
	loading bool
	err     error
	width   int
	height  int

	// Action state
	mode          providerActionMode
	confirmMsg    string
	resultMsg     string
	resultIsError bool
}

// NewProviderListView creates the provider list view.
func NewProviderListView(c *client.Client, admin bool) ProviderListView {
	return ProviderListView{
		client:  c,
		Admin:   admin,
		table:   components.NewTable([]string{"NAME", "PROVIDER", "VALIDATED", "ENDPOINT", "AGE"}),
		loading: true,
	}
}

// IsFiltering returns true if the table is in filter mode.
func (v *ProviderListView) IsFiltering() bool {
	return v.table.Filtering()
}

// IsInActionMode returns true when a provider action prompt is active.
func (v *ProviderListView) IsInActionMode() bool {
	return v.mode != providerModeNormal
}

// Init fetches provider data.
func (v ProviderListView) Init() tea.Cmd {
	return v.fetch()
}

// Update handles messages.
func (v ProviderListView) Update(msg tea.Msg) (ProviderListView, tea.Cmd) {
	switch msg := msg.(type) {
	case providerListMsg:
		v.loading = false
		v.err = msg.err
		if msg.err == nil {
			v.table.SetRows(msg.rows)
		}
		return v, nil

	case providerActionResultMsg:
		v.loading = false
		if msg.err != nil {
			v.mode = providerModeResult
			v.resultMsg = msg.err.Error()
			v.resultIsError = true
		} else {
			v.mode = providerModeResult
			v.resultMsg = "Provider deleted. Press any key to refresh."
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
		case providerModeConfirm:
			return v.updateConfirmMode(msg)
		case providerModeResult:
			return v.updateResultMode(msg)
		default:
			return v.updateNormalMode(msg)
		}
	}
	return v, nil
}

// updateNormalMode handles keys when no action is active.
func (v ProviderListView) updateNormalMode(msg tea.KeyMsg) (ProviderListView, tea.Cmd) {
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
			v.mode = providerModeConfirm
			v.confirmMsg = fmt.Sprintf("Delete provider %s? (y/n)", row[0])
			return v, nil
		}
	default:
		cmd := v.table.Update(msg)
		return v, cmd
	}
	return v, nil
}

// updateConfirmMode handles keys when a y/n prompt is active.
func (v ProviderListView) updateConfirmMode(msg tea.KeyMsg) (ProviderListView, tea.Cmd) {
	switch msg.String() {
	case "y", "Y":
		v.loading = true
		row := v.table.SelectedRow()
		providerName := ""
		if row != nil && len(row) > 0 {
			providerName = row[0]
		}
		c := v.client
		return v, func() tea.Msg {
			ctx := context.Background()
			err := c.Dynamic.Resource(client.ProviderConfigGVR).Namespace(providerNamespace).Delete(
				ctx, providerName, metav1.DeleteOptions{},
			)
			if err != nil {
				return providerActionResultMsg{err: fmt.Errorf("deleting provider: %w", err)}
			}
			return providerActionResultMsg{}
		}
	case "n", "N", "esc":
		v.mode = providerModeNormal
		return v, nil
	}
	return v, nil
}

// updateResultMode handles keys when showing a result message.
func (v ProviderListView) updateResultMode(msg tea.KeyMsg) (ProviderListView, tea.Cmd) {
	v.mode = providerModeNormal
	v.resultMsg = ""
	v.loading = true
	return v, v.fetch()
}

// KeyLegend returns the action keys available for the provider view.
func (v *ProviderListView) KeyLegend() string {
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

// View renders the provider list.
func (v ProviderListView) View() string {
	if v.loading {
		return "  Loading providers..."
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
func (v ProviderListView) renderActionPrompt() string {
	switch v.mode {
	case providerModeConfirm:
		return styles.ActionConfirmStyle.Render(v.confirmMsg)
	case providerModeResult:
		if v.resultIsError {
			return styles.ActionErrorStyle.Render("Error: " + v.resultMsg)
		}
		return styles.ActionSuccessStyle.Render(v.resultMsg)
	}
	return ""
}

func (v ProviderListView) fetch() tea.Cmd {
	c := v.client
	return func() tea.Msg {
		list, err := c.Dynamic.Resource(client.ProviderConfigGVR).Namespace(providerNamespace).List(context.Background(), metav1.ListOptions{})
		if err != nil {
			return providerListMsg{err: fmt.Errorf("listing ProviderConfigs: %w", err)}
		}

		rows := make([][]string, len(list.Items))
		for i, pc := range list.Items {
			provider := client.GetNestedString(pc.Object, "spec", "provider")
			validated := client.GetNestedBool(pc.Object, "status", "validated")

			var endpoint string
			switch provider {
			case "nutanix":
				endpoint = client.GetNestedString(pc.Object, "spec", "nutanix", "endpoint")
			case "harvester":
				endpoint = "(in-cluster)"
			case "proxmox":
				endpoint = client.GetNestedString(pc.Object, "spec", "proxmox", "endpoint")
			case "gcp":
				endpoint = fmt.Sprintf("%s/%s",
					client.GetNestedString(pc.Object, "spec", "gcp", "projectID"),
					client.GetNestedString(pc.Object, "spec", "gcp", "region"))
			case "aws":
				endpoint = client.GetNestedString(pc.Object, "spec", "aws", "region")
			case "azure":
				endpoint = client.GetNestedString(pc.Object, "spec", "azure", "subscriptionID")
			default:
				endpoint = "-"
			}

			validatedStr := "No"
			if validated {
				validatedStr = "Ready"
			}

			rows[i] = []string{
				pc.GetName(),
				provider,
				validatedStr,
				endpoint,
				output.FormatAge(pc.GetCreationTimestamp().Time),
			}
		}
		return providerListMsg{rows: rows}
	}
}
