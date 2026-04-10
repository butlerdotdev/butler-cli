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
	"github.com/butlerdotdev/butler/internal/common/output"
	"github.com/butlerdotdev/butler/internal/tui/components"
)

const providerNamespace = "butler-system"

type providerListMsg struct {
	rows [][]string
	err  error
}

// ProviderListView displays ProviderConfig resources.
type ProviderListView struct {
	client  *client.Client
	table   components.Table
	loading bool
	err     error
	width   int
	height  int
}

// NewProviderListView creates the provider list view.
func NewProviderListView(c *client.Client) ProviderListView {
	return ProviderListView{
		client:  c,
		table:   components.NewTable([]string{"NAME", "PROVIDER", "VALIDATED", "ENDPOINT", "AGE"}),
		loading: true,
	}
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

// View renders the provider list.
func (v ProviderListView) View() string {
	if v.loading {
		return "  Loading providers..."
	}
	if v.err != nil {
		return fmt.Sprintf("  Error: %v", v.err)
	}
	return v.table.View()
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
