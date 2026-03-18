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

package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/butlerdotdev/butler/internal/adm/bootstrap/orchestrator"
)

// machineTableModel displays per-machine provisioning status.
type machineTableModel struct {
	table    table.Model
	machines []orchestrator.MachineStatus
	width    int
	focused  bool
}

func newMachineTableModel() machineTableModel {
	columns := []table.Column{
		{Title: "NAME", Width: 25},
		{Title: "ROLE", Width: 15},
		{Title: "PHASE", Width: 12},
		{Title: "IP", Width: 16},
		{Title: "TALOS", Width: 5},
	}

	t := table.New(
		table.WithColumns(columns),
		table.WithRows([]table.Row{}),
		table.WithFocused(false),
		table.WithHeight(4),
	)

	s := table.DefaultStyles()
	s.Header = s.Header.
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(colorBorder).
		BorderBottom(true).
		Bold(true)
	s.Selected = s.Selected.
		Foreground(colorText).
		Background(colorBg).
		Bold(false)
	t.SetStyles(s)

	return machineTableModel{
		table: t,
		width: 80,
	}
}

func (m machineTableModel) Init() tea.Cmd {
	return nil
}

func (m machineTableModel) Update(msg tea.Msg) (machineTableModel, tea.Cmd) {
	if !m.focused {
		return m, nil
	}
	var cmd tea.Cmd
	m.table, cmd = m.table.Update(msg)
	return m, cmd
}

// SetMachines updates the table rows from a machine status list.
func (m *machineTableModel) SetMachines(machines []orchestrator.MachineStatus) {
	m.machines = machines
	rows := make([]table.Row, len(machines))
	for i, ms := range machines {
		ip := ms.IPAddress
		if ip == "" {
			ip = "-"
		}
		talos := "-"
		if ms.TalosConfigured {
			talos = "Yes"
		}
		rows[i] = table.Row{
			ms.Name,
			ms.Role,
			ms.Phase,
			ip,
			talos,
		}
	}
	m.table.SetRows(rows)
	// Adjust height to fit content (min 1, max 8)
	h := len(rows)
	if h < 1 {
		h = 1
	}
	if h > 8 {
		h = 8
	}
	m.table.SetHeight(h)
}

// SetFocused controls whether the table responds to key input.
func (m *machineTableModel) SetFocused(focused bool) {
	m.focused = focused
	m.table.Focus()
	if !focused {
		m.table.Blur()
	}
}

// SetWidth adjusts column widths proportionally.
func (m *machineTableModel) SetWidth(w int) {
	m.width = w
	// Proportional column widths
	nameW := w * 30 / 100
	roleW := w * 18 / 100
	phaseW := w * 15 / 100
	ipW := w * 22 / 100
	talosW := w * 8 / 100
	m.table.SetColumns([]table.Column{
		{Title: "NAME", Width: nameW},
		{Title: "ROLE", Width: roleW},
		{Title: "PHASE", Width: phaseW},
		{Title: "IP", Width: ipW},
		{Title: "TALOS", Width: talosW},
	})
}

func (m machineTableModel) View() string {
	if len(m.machines) == 0 {
		return phasePending.Render("  Waiting for machines...")
	}
	return m.table.View()
}

// summary returns a one-line summary like "3/5 Running".
func (m machineTableModel) summary() string {
	if len(m.machines) == 0 {
		return ""
	}
	running := 0
	for _, ms := range m.machines {
		if ms.Phase == "Running" {
			running++
		}
	}
	var b strings.Builder
	b.WriteString(machineRunning.Render(strings.Repeat("*", running)))
	b.WriteString(machinePending.Render(strings.Repeat("*", len(m.machines)-running)))
	return b.String()
}
