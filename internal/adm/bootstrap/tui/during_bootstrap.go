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
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/butlerdotdev/butler/internal/adm/bootstrap/orchestrator"
)

type panel int

const (
	panelPhases panel = iota
	panelMachines
	panelAddons
	panelLogs
	panelCount // sentinel for modular arithmetic
)

// duringBootstrapModel displays the multi-panel bootstrap progress view.
type duringBootstrapModel struct {
	phases   phaseProgressModel
	machines machineTableModel
	addons   addonChecklistModel
	logs     logViewportModel

	activePanel    panel
	provider       string
	clusterName    string
	startTime      time.Time
	width          int
	height         int
	confirmingQuit bool
}

func newDuringBootstrapModel(provider, clusterName string, logBuf *LogBuffer) duringBootstrapModel {
	return duringBootstrapModel{
		phases:      newPhaseProgressModel(),
		machines:    newMachineTableModel(),
		addons:      newAddonChecklistModel(),
		logs:        newLogViewportModel(logBuf),
		provider:    provider,
		clusterName: clusterName,
		startTime:   time.Now(),
		width:       80,
		height:      24,
	}
}

func (m duringBootstrapModel) Init() tea.Cmd {
	return m.phases.Init()
}

func (m duringBootstrapModel) Update(msg tea.Msg) (duringBootstrapModel, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		if m.confirmingQuit {
			switch {
			case key.Matches(msg, keys.Yes):
				m.confirmingQuit = false
				return m, tea.Quit
			case key.Matches(msg, keys.No), key.Matches(msg, keys.Cancel):
				m.confirmingQuit = false
				return m, nil
			}
			return m, nil
		}

		if m.logs.IsFiltering() {
			switch {
			case key.Matches(msg, keys.Cancel):
				m.logs.StopFilter()
				return m, nil
			case key.Matches(msg, keys.Confirm):
				m.logs.StopFilter()
				return m, nil
			default:
				var cmd tea.Cmd
				m.logs, cmd = m.logs.Update(msg)
				return m, cmd
			}
		}

		switch {
		case key.Matches(msg, keys.Quit), key.Matches(msg, keys.ForceQuit):
			m.confirmingQuit = true
			return m, nil
		case key.Matches(msg, keys.TabNext):
			m.cyclePanel(1)
		case key.Matches(msg, keys.TabPrev):
			m.cyclePanel(-1)
		case key.Matches(msg, keys.ToggleLogs):
			m.logs.ToggleVisible()
		case key.Matches(msg, keys.Filter):
			if m.activePanel == panelLogs {
				cmd := m.logs.StartFilter()
				return m, cmd
			}
		}

	case tickMsg:
		m.logs.Refresh()
		cmds = append(cmds, tickCmd(200*time.Millisecond))

	case orchestratorEventMsg:
		m.handleEvent(orchestrator.Event(msg))
	}

	// Update sub-models
	var cmd tea.Cmd
	m.phases, cmd = m.phases.Update(msg)
	cmds = append(cmds, cmd)
	m.machines, cmd = m.machines.Update(msg)
	cmds = append(cmds, cmd)
	m.logs, cmd = m.logs.Update(msg)
	cmds = append(cmds, cmd)

	return m, tea.Batch(cmds...)
}

func (m *duringBootstrapModel) handleEvent(e orchestrator.Event) {
	switch e.Type {
	case orchestrator.EventPhaseChange:
		m.phases.SetPhase(e.Phase)
	case orchestrator.EventBootstrapStatus:
		if e.Status != nil {
			m.machines.SetMachines(e.Status.Machines)
			m.addons.SetAddons(e.Status.AddonsInstalled)
		}
	case orchestrator.EventFailed:
		m.phases.SetFailed()
	case orchestrator.EventComplete:
		m.phases.SetCompleted()
	}
}

func (m *duringBootstrapModel) cyclePanel(dir int) {
	// Unfocus current
	m.setFocus(m.activePanel, false)
	// Cycle
	m.activePanel = panel((int(m.activePanel) + dir + int(panelCount)) % int(panelCount))
	// Skip logs panel if hidden
	if m.activePanel == panelLogs && !m.logs.IsVisible() {
		m.activePanel = panel((int(m.activePanel) + dir + int(panelCount)) % int(panelCount))
	}
	// Focus new
	m.setFocus(m.activePanel, true)
}

func (m *duringBootstrapModel) setFocus(p panel, focused bool) {
	switch p {
	case panelMachines:
		m.machines.SetFocused(focused)
	case panelLogs:
		m.logs.SetFocused(focused)
	}
}

// SetSize updates layout dimensions.
func (m *duringBootstrapModel) SetSize(w, h int) {
	m.width = w
	m.height = h
	contentWidth := w - 4 // Account for border
	m.machines.SetWidth(contentWidth - 4)
	m.addons.SetWidth(contentWidth - 4)

	// Allocate log viewport height: roughly 1/4 of available space
	logH := h / 4
	if logH < 3 {
		logH = 3
	}
	m.logs.SetSize(contentWidth-4, logH)
}

func (m duringBootstrapModel) View() string {
	contentWidth := m.width - 4

	var b strings.Builder

	// Title bar with elapsed time
	elapsed := time.Since(m.startTime).Round(time.Second)
	titleText := fmt.Sprintf("Butler Bootstrap -- %s", m.provider)
	elapsedText := dimStyle.Render(fmt.Sprintf("%s", elapsed))
	padding := contentWidth - lipgloss.Width(titleText) - lipgloss.Width(elapsedText) - 2
	if padding < 1 {
		padding = 1
	}
	title := titleBarStyle.Width(contentWidth).Render(
		titleText + strings.Repeat(" ", padding) + elapsedText)
	b.WriteString(title + "\n")

	// Quit confirmation overlay
	if m.confirmingQuit {
		b.WriteString("\n")
		b.WriteString(failureStyle.Render("  Abort bootstrap? This will clean up the KIND cluster.") + "\n")
		b.WriteString(dimStyle.Render("  Press y to confirm, n to cancel") + "\n\n")
	}

	// Phases panel
	panelStyle := m.panelBorder(panelPhases, contentWidth)
	b.WriteString(panelStyle.Render(
		sectionStyle.Render("Phases") + "\n" +
			m.phases.View()))
	b.WriteString("\n")

	// Machines panel
	panelStyle = m.panelBorder(panelMachines, contentWidth)
	machineHeader := sectionStyle.Render("Machines")
	if s := m.machines.summary(); s != "" {
		machineHeader += "  " + s
	}
	b.WriteString(panelStyle.Render(machineHeader + "\n" + m.machines.View()))
	b.WriteString("\n")

	// Addons panel
	panelStyle = m.panelBorder(panelAddons, contentWidth)
	addonHeader := sectionStyle.Render("Addons")
	if s := m.addons.summary(); s != "" {
		addonHeader += "  " + dimStyle.Render(s)
	}
	b.WriteString(panelStyle.Render(addonHeader + "\n" + m.addons.View()))
	b.WriteString("\n")

	// Logs panel (toggleable)
	if m.logs.IsVisible() {
		panelStyle = m.panelBorder(panelLogs, contentWidth)
		logHeader := sectionStyle.Render("Logs")
		logHeader += dimStyle.Render(" [l toggle]")
		b.WriteString(panelStyle.Render(logHeader + "\n" + m.logs.View()))
		b.WriteString("\n")
	}

	// Help bar
	help := helpBarStyle.Render("q quit  Tab panels  l logs  / filter  j/k scroll")
	b.WriteString(help)

	return b.String()
}

func (m duringBootstrapModel) panelBorder(p panel, width int) lipgloss.Style {
	if p == m.activePanel {
		return activeBorder.Width(width)
	}
	return normalBorder.Width(width)
}
