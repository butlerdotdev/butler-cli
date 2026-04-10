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

	tea "github.com/charmbracelet/bubbletea"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/butlerdotdev/butler/internal/common/client"
	"github.com/butlerdotdev/butler/internal/tui/components"
)

type addonListMsg struct {
	rows [][]string
	err  error
}

// AddonCatalogView displays AddonDefinitions.
type AddonCatalogView struct {
	client  *client.Client
	table   components.Table
	loading bool
	err     error
	width   int
	height  int
}

// NewAddonCatalogView creates the addon catalog view.
func NewAddonCatalogView(c *client.Client) AddonCatalogView {
	return AddonCatalogView{
		client:  c,
		table:   components.NewTable([]string{"NAME", "DISPLAY NAME", "CATEGORY", "VERSION", "PLATFORM"}),
		loading: true,
	}
}

// Init fetches addon definitions.
func (v AddonCatalogView) Init() tea.Cmd {
	return v.fetch()
}

// Update handles messages.
func (v AddonCatalogView) Update(msg tea.Msg) (AddonCatalogView, tea.Cmd) {
	switch msg := msg.(type) {
	case addonListMsg:
		v.loading = false
		v.err = msg.err
		if msg.err == nil {
			v.table.SetRows(msg.rows)
		}
		return v, nil

	case tea.WindowSizeMsg:
		v.width = msg.Width
		v.height = msg.Height
		v.table.SetSize(msg.Width, msg.Height-4)
		return v, nil

	case tea.KeyMsg:
		if v.table.Filtering() {
			cmd := v.table.Update(msg)
			return v, cmd
		}
		switch msg.String() {
		case "r":
			v.loading = true
			return v, v.fetch()
		default:
			cmd := v.table.Update(msg)
			return v, cmd
		}
	}
	return v, nil
}

// View renders the addon catalog.
func (v AddonCatalogView) View() string {
	if v.loading {
		return "  Loading addon catalog..."
	}
	if v.err != nil {
		return fmt.Sprintf("  Error: %v", v.err)
	}
	return v.table.View()
}

func (v AddonCatalogView) fetch() tea.Cmd {
	c := v.client
	return func() tea.Msg {
		list, err := c.Dynamic.Resource(client.AddonDefinitionGVR).List(context.Background(), metav1.ListOptions{})
		if err != nil {
			return addonListMsg{err: fmt.Errorf("listing AddonDefinitions: %w", err)}
		}

		rows := make([][]string, len(list.Items))
		for i, ad := range list.Items {
			platform := ""
			if client.GetNestedBool(ad.Object, "spec", "platform") {
				platform = "Yes"
			}
			rows[i] = []string{
				ad.GetName(),
				client.GetNestedString(ad.Object, "spec", "displayName"),
				client.GetNestedString(ad.Object, "spec", "category"),
				client.GetNestedString(ad.Object, "spec", "chart", "defaultVersion"),
				platform,
			}
		}
		return addonListMsg{rows: rows}
	}
}
