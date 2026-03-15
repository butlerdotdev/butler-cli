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
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/butlerdotdev/butler/internal/adm/bootstrap/orchestrator"
)

type viewState int

const (
	viewPreBootstrap viewState = iota
	viewDuringBootstrap
	viewPostBootstrap
)

// bootstrapModel is the root bubbletea model that manages view transitions.
type bootstrapModel struct {
	pre    preBootstrapModel
	during duringBootstrapModel
	post   postBootstrapModel

	activeView       viewState
	eventCh          <-chan orchestrator.Event
	startFn          func() // Called when user confirms pre-bootstrap
	cfg              *orchestrator.Config
	skipCleanup      bool
	skipPreBootstrap bool
	startTime        time.Time
	lastStatus       *orchestrator.BootstrapSnapshot
	err              error
	width            int
	height           int
	quitting         bool
}

// ModelConfig holds the configuration needed to construct a bootstrapModel.
type ModelConfig struct {
	Cfg              *orchestrator.Config
	EventCh          <-chan orchestrator.Event
	LogBuf           *LogBuffer
	StartFn          func() // Starts the orchestrator goroutine
	SkipCleanup      bool
	SkipPreBootstrap bool // Skip pre-bootstrap view (wizard already confirmed)
}

// NewModel creates the root bubbletea model.
func NewModel(mc ModelConfig) bootstrapModel {
	initialView := viewPreBootstrap
	if mc.SkipPreBootstrap {
		initialView = viewDuringBootstrap
	}
	return bootstrapModel{
		pre:              newPreBootstrapModel(mc.Cfg),
		during:           newDuringBootstrapModel(mc.Cfg.Provider, mc.Cfg.Cluster.Name, mc.LogBuf),
		post:             newPostBootstrapModel(mc.Cfg.Cluster.Name, mc.Cfg.Provider, mc.Cfg.Cluster.Topology),
		activeView:       initialView,
		eventCh:          mc.EventCh,
		startFn:          mc.StartFn,
		cfg:              mc.Cfg,
		skipCleanup:      mc.SkipCleanup,
		skipPreBootstrap: mc.SkipPreBootstrap,
		width:            80,
		height:           24,
	}
}

func (m bootstrapModel) Init() tea.Cmd {
	if m.skipPreBootstrap {
		m.startTime = time.Now()
		if m.startFn != nil {
			m.startFn()
		}
		return tea.Batch(
			m.during.Init(),
			waitForEvent(m.eventCh),
			tickCmd(200*time.Millisecond),
			tea.WindowSize(),
		)
	}
	return tea.Batch(
		m.pre.Init(),
		tea.WindowSize(),
	)
}

func (m bootstrapModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.pre.SetSize(msg.Width, msg.Height)
		m.during.SetSize(msg.Width, msg.Height)
		m.post.SetSize(msg.Width, msg.Height)
		return m, nil

	case tea.KeyMsg:
		// Global quit handling
		if m.activeView == viewPostBootstrap {
			if key.Matches(msg, keys.Quit) || key.Matches(msg, keys.ForceQuit) || key.Matches(msg, keys.Confirm) {
				m.quitting = true
				return m, tea.Quit
			}
		}

		if m.activeView == viewPreBootstrap {
			switch {
			case key.Matches(msg, keys.Quit), key.Matches(msg, keys.ForceQuit):
				m.quitting = true
				return m, tea.Quit
			case key.Matches(msg, keys.Confirm):
				if m.pre.AllPassed() {
					return m, m.transitionToDuring()
				}
			}
		}

		// During-bootstrap key handling is delegated to the during model

	case bootstrapDoneMsg:
		m.err = msg.err
		return m, m.transitionToPost()

	case orchestratorEventMsg:
		e := orchestrator.Event(msg)
		if e.Status != nil {
			m.lastStatus = e.Status
		}
		if e.Type == orchestrator.EventComplete {
			m.during.handleEvent(e)
			return m, nil
		}
		if e.Type == orchestrator.EventFailed {
			m.during.handleEvent(e)
			return m, nil
		}
	}

	// Delegate to active view
	switch m.activeView {
	case viewPreBootstrap:
		var cmd tea.Cmd
		m.pre, cmd = m.pre.Update(msg)
		cmds = append(cmds, cmd)

	case viewDuringBootstrap:
		var cmd tea.Cmd
		m.during, cmd = m.during.Update(msg)
		cmds = append(cmds, cmd)

	case viewPostBootstrap:
		// Post view is static, no updates needed
	}

	return m, tea.Batch(cmds...)
}

func (m *bootstrapModel) transitionToDuring() tea.Cmd {
	m.activeView = viewDuringBootstrap
	m.startTime = time.Now()

	// Start the orchestrator
	if m.startFn != nil {
		m.startFn()
	}

	return tea.Batch(
		m.during.Init(),
		waitForEvent(m.eventCh),
		tickCmd(200*time.Millisecond),
	)
}

func (m *bootstrapModel) transitionToPost() tea.Cmd {
	m.activeView = viewPostBootstrap
	elapsed := time.Since(m.startTime)

	if m.err != nil {
		phase := ""
		reason := ""
		message := m.err.Error()
		if m.lastStatus != nil {
			phase = m.lastStatus.Phase
			reason = m.lastStatus.FailureReason
			if m.lastStatus.FailureMessage != "" {
				message = m.lastStatus.FailureMessage
			}
		}
		m.post.SetFailure(phase, reason, message, elapsed, m.skipCleanup)
	} else {
		var creds *orchestrator.ClusterCredentials
		// Find creds from the last complete event
		if m.lastStatus != nil && m.lastStatus.Phase == "Ready" {
			// Creds are passed via bootstrapDoneMsg, not status
		}
		_ = creds // Will be set by the tui.go wiring
		m.post.SetSuccess(nil, elapsed)
	}

	return nil
}

func (m bootstrapModel) View() string {
	if m.quitting {
		return ""
	}

	switch m.activeView {
	case viewPreBootstrap:
		return m.pre.View()
	case viewDuringBootstrap:
		return m.during.View()
	case viewPostBootstrap:
		return m.post.View()
	default:
		return ""
	}
}
