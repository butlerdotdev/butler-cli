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

package bootstrap

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
)

// phaseItem represents a single orchestrator phase with its display name.
type phaseItem struct {
	id   string // Internal phase identifier
	name string // Display name
}

// orchestratorPhases defines the ordered list of phases shown in the progress panel.
var orchestratorPhases = []phaseItem{
	{id: "CreatingKIND", name: "Creating KIND cluster"},
	{id: "DeployingCRDs", name: "Deploying CRDs"},
	{id: "DeployingControllers", name: "Deploying controllers"},
	{id: "CreatingBootstrap", name: "Creating bootstrap resources"},
	{id: "WatchingBootstrap", name: "Waiting for cluster bootstrap"},
	{id: "SavingCredentials", name: "Saving credentials"},
	{id: "CleaningUp", name: "Cleaning up"},
}

type phaseState int

const (
	phaseStatePending phaseState = iota
	phaseStateActive
	phaseStateCompleted
	phaseStateFailed
)

// phaseProgressModel displays the orchestrator phase checklist.
type phaseProgressModel struct {
	phases       []phaseItem
	states       map[string]phaseState
	startTimes   map[string]time.Time
	endTimes     map[string]time.Time
	currentPhase string
	spinner      spinner.Model
	failed       bool
}

func newPhaseProgressModel() phaseProgressModel {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = phaseActive

	return phaseProgressModel{
		phases:     orchestratorPhases,
		states:     make(map[string]phaseState),
		startTimes: make(map[string]time.Time),
		endTimes:   make(map[string]time.Time),
		spinner:    s,
	}
}

func (m phaseProgressModel) Init() tea.Cmd {
	return m.spinner.Tick
}

func (m phaseProgressModel) Update(msg tea.Msg) (phaseProgressModel, tea.Cmd) {
	switch msg := msg.(type) {
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	}
	return m, nil
}

// SetPhase transitions to the given phase, completing all preceding phases.
func (m *phaseProgressModel) SetPhase(phaseID string) {
	now := time.Now()
	found := false
	for _, p := range m.phases {
		if p.id == phaseID {
			found = true
			m.states[p.id] = phaseStateActive
			m.startTimes[p.id] = now
			m.currentPhase = p.id
			break
		}
		// Complete all preceding phases
		if m.states[p.id] != phaseStateCompleted {
			m.states[p.id] = phaseStateCompleted
			if _, ok := m.endTimes[p.id]; !ok {
				m.endTimes[p.id] = now
			}
		}
	}
	if !found {
		m.currentPhase = phaseID
	}
}

// SetFailed marks the current phase as failed.
func (m *phaseProgressModel) SetFailed() {
	m.failed = true
	if m.currentPhase != "" {
		m.states[m.currentPhase] = phaseStateFailed
		m.endTimes[m.currentPhase] = time.Now()
	}
}

// SetCompleted marks all phases as completed.
func (m *phaseProgressModel) SetCompleted() {
	now := time.Now()
	for _, p := range m.phases {
		if m.states[p.id] != phaseStateFailed {
			m.states[p.id] = phaseStateCompleted
			if _, ok := m.endTimes[p.id]; !ok {
				m.endTimes[p.id] = now
			}
		}
	}
}

func (m phaseProgressModel) View() string {
	var b strings.Builder
	for _, p := range m.phases {
		state := m.states[p.id]
		var icon, name, elapsed string

		switch state {
		case phaseStateCompleted:
			icon = phaseCompleted.Render(iconCompleted)
			name = phaseCompleted.Render(p.name)
		case phaseStateActive:
			icon = m.spinner.View()
			name = phaseActive.Render(p.name)
		case phaseStateFailed:
			icon = phaseFailed.Render(iconFailed)
			name = phaseFailed.Render(p.name)
		default:
			icon = phasePending.Render(iconPending)
			name = phasePending.Render(p.name)
		}

		if start, ok := m.startTimes[p.id]; ok {
			end := time.Now()
			if e, ok := m.endTimes[p.id]; ok {
				end = e
			}
			d := end.Sub(start).Round(time.Second)
			elapsed = dimStyle.Render(fmt.Sprintf("  %s", d))
		}

		b.WriteString(fmt.Sprintf("  %s %s%s\n", icon, name, elapsed))
	}
	return b.String()
}
