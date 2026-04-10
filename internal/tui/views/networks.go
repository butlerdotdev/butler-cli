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

type networkListMsg struct {
	rows [][]string
	err  error
}

// NetworkListView displays NetworkPool resources.
type NetworkListView struct {
	client  *client.Client
	table   components.Table
	loading bool
	err     error
	width   int
	height  int
}

// NewNetworkListView creates the network pool list view.
func NewNetworkListView(c *client.Client) NetworkListView {
	return NetworkListView{
		client:  c,
		table:   components.NewTable([]string{"NAME", "CIDR", "TOTAL", "ALLOCATED", "AVAILABLE", "UTILIZATION", "AGE"}),
		loading: true,
	}
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

// View renders the network pool list.
func (v NetworkListView) View() string {
	if v.loading {
		return "  Loading network pools..."
	}
	if v.err != nil {
		return fmt.Sprintf("  Error: %v", v.err)
	}
	return v.table.View()
}

func (v NetworkListView) fetch() tea.Cmd {
	c := v.client
	return func() tea.Msg {
		list, err := c.Dynamic.Resource(client.NetworkPoolGVR).Namespace("butler-system").List(context.Background(), metav1.ListOptions{})
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
