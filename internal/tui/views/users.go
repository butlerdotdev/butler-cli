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

type userListMsg struct {
	rows [][]string
	err  error
}

// UserListView displays User resources.
type UserListView struct {
	client  *client.Client
	table   components.Table
	loading bool
	err     error
	width   int
	height  int
}

// NewUserListView creates the user list view.
func NewUserListView(c *client.Client) UserListView {
	return UserListView{
		client:  c,
		table:   components.NewTable([]string{"NAME", "EMAIL", "PHASE", "AUTH TYPE", "ADMIN", "AGE"}),
		loading: true,
	}
}

// Init fetches user data.
func (v UserListView) Init() tea.Cmd {
	return v.fetch()
}

// Update handles messages.
func (v UserListView) Update(msg tea.Msg) (UserListView, tea.Cmd) {
	switch msg := msg.(type) {
	case userListMsg:
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

// View renders the user list.
func (v UserListView) View() string {
	if v.loading {
		return "  Loading users..."
	}
	if v.err != nil {
		return fmt.Sprintf("  Error: %v", v.err)
	}
	return v.table.View()
}

func (v UserListView) fetch() tea.Cmd {
	c := v.client
	return func() tea.Msg {
		list, err := c.Dynamic.Resource(client.UserGVR).List(context.Background(), metav1.ListOptions{})
		if err != nil {
			return userListMsg{err: fmt.Errorf("listing Users: %w", err)}
		}

		rows := make([][]string, len(list.Items))
		for i, usr := range list.Items {
			adminStr := ""
			if client.GetNestedBool(usr.Object, "spec", "isPlatformAdmin") {
				adminStr = "Yes"
			}
			rows[i] = []string{
				usr.GetName(),
				client.GetNestedString(usr.Object, "spec", "email"),
				client.GetNestedString(usr.Object, "status", "phase"),
				client.GetNestedString(usr.Object, "spec", "authType"),
				adminStr,
				output.FormatAge(usr.GetCreationTimestamp().Time),
			}
		}
		return userListMsg{rows: rows}
	}
}
