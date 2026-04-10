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

type teamListMsg struct {
	rows [][]string
	err  error
}

// TeamListView displays Team resources.
type TeamListView struct {
	client  *client.Client
	table   components.Table
	loading bool
	err     error
	width   int
	height  int
}

// NewTeamListView creates the team list view.
func NewTeamListView(c *client.Client) TeamListView {
	return TeamListView{
		client:  c,
		table:   components.NewTable([]string{"NAME", "DISPLAY NAME", "PHASE", "CLUSTERS", "MEMBERS", "NAMESPACE", "AGE"}),
		loading: true,
	}
}

// Init fetches team data.
func (v TeamListView) Init() tea.Cmd {
	return v.fetch()
}

// Update handles messages.
func (v TeamListView) Update(msg tea.Msg) (TeamListView, tea.Cmd) {
	switch msg := msg.(type) {
	case teamListMsg:
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

// View renders the team list.
func (v TeamListView) View() string {
	if v.loading {
		return "  Loading teams..."
	}
	if v.err != nil {
		return fmt.Sprintf("  Error: %v", v.err)
	}
	return v.table.View()
}

func (v TeamListView) fetch() tea.Cmd {
	c := v.client
	return func() tea.Msg {
		list, err := c.Dynamic.Resource(client.TeamGVR).List(context.Background(), metav1.ListOptions{})
		if err != nil {
			return teamListMsg{err: fmt.Errorf("listing Teams: %w", err)}
		}

		rows := make([][]string, len(list.Items))
		for i, tm := range list.Items {
			rows[i] = []string{
				tm.GetName(),
				client.GetNestedString(tm.Object, "spec", "displayName"),
				client.GetNestedString(tm.Object, "status", "phase"),
				fmt.Sprintf("%d", client.GetNestedInt64(tm.Object, "status", "clusterCount")),
				fmt.Sprintf("%d", client.GetNestedInt64(tm.Object, "status", "memberCount")),
				client.GetNestedString(tm.Object, "status", "namespace"),
				output.FormatAge(tm.GetCreationTimestamp().Time),
			}
		}
		return teamListMsg{rows: rows}
	}
}
