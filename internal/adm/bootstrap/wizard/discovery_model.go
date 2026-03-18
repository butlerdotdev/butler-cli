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

package wizard

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/butlerdotdev/butler/internal/adm/bootstrap/wizard/discovery"
)

// discoveryModel connects to a provider and fetches root resources concurrently.
type discoveryModel struct {
	disc     discovery.ProviderDiscovery
	provider string
	tasks    []discoveryTask
	spinner  spinner.Model

	results map[string][]discovery.ProviderResource

	connected bool
	connErr   error
	done      bool
	err       error
	width     int
}

type taskStatus int

const (
	taskPending taskStatus = iota
	taskLoading
	taskDone
	taskFailed
)

type discoveryTask struct {
	resourceType string
	label        string
	status       taskStatus
	count        int
	err          error
}

type connectDoneMsg struct {
	err error
}

type resourceFetchedMsg struct {
	resourceType string
	results      []discovery.ProviderResource
	err          error
}

func newDiscoveryModel(provider string, disc discovery.ProviderDiscovery) discoveryModel {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(brandGreen)

	// Build task list from root resource types (those with no parent).
	var tasks []discoveryTask
	for _, rt := range disc.ResourceTypes() {
		if rt.ParentType == "" {
			tasks = append(tasks, discoveryTask{
				resourceType: rt.Type,
				label:        rt.Label + "s",
				status:       taskPending,
			})
		}
	}

	return discoveryModel{
		disc:     disc,
		provider: provider,
		tasks:    tasks,
		spinner:  s,
		results:  make(map[string][]discovery.ProviderResource),
		width:    80,
	}
}

func connectCmd(disc discovery.ProviderDiscovery) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		err := disc.Connect(ctx)
		return connectDoneMsg{err: err}
	}
}

func fetchResourceCmd(disc discovery.ProviderDiscovery, resourceType string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		results, err := disc.FetchResource(ctx, resourceType, "")
		return resourceFetchedMsg{
			resourceType: resourceType,
			results:      results,
			err:          err,
		}
	}
}

func (m discoveryModel) Init() tea.Cmd {
	return tea.Batch(m.spinner.Tick, connectCmd(m.disc))
}

func (m discoveryModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			m.done = true
			m.err = fmt.Errorf("discovery cancelled")
			return m, tea.Quit
		}

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd

	case connectDoneMsg:
		if msg.err != nil {
			m.connErr = msg.err
			m.done = true
			m.err = fmt.Errorf("connection failed: %w", msg.err)
			return m, tea.Quit
		}
		m.connected = true

		var cmds []tea.Cmd
		for i := range m.tasks {
			m.tasks[i].status = taskLoading
			cmds = append(cmds, fetchResourceCmd(m.disc, m.tasks[i].resourceType))
		}
		return m, tea.Batch(cmds...)

	case resourceFetchedMsg:
		for i := range m.tasks {
			if m.tasks[i].resourceType == msg.resourceType {
				if msg.err != nil {
					m.tasks[i].status = taskFailed
					m.tasks[i].err = msg.err
				} else {
					m.tasks[i].status = taskDone
					m.tasks[i].count = len(msg.results)
					m.results[msg.resourceType] = msg.results
				}
				break
			}
		}

		allDone := true
		hasFailure := false
		for _, t := range m.tasks {
			if t.status == taskPending || t.status == taskLoading {
				allDone = false
			}
			if t.status == taskFailed {
				hasFailure = true
			}
		}

		if allDone {
			m.done = true
			if hasFailure {
				var msgs []string
				for _, t := range m.tasks {
					if t.status == taskFailed {
						msgs = append(msgs, fmt.Sprintf("%s: %v", t.label, t.err))
					}
				}
				m.err = fmt.Errorf("discovery failed:\n  %s", strings.Join(msgs, "\n  "))
			}
			return m, tea.Quit
		}
	}

	return m, nil
}

func (m discoveryModel) View() string {
	var b strings.Builder

	title := lipgloss.NewStyle().
		Foreground(brandBlue).
		Bold(true)

	dim := lipgloss.NewStyle().Foreground(brandGray)
	green := lipgloss.NewStyle().Foreground(brandGreen)
	red := lipgloss.NewStyle().Foreground(brandRed)

	b.WriteString("\n")
	b.WriteString(title.Render(fmt.Sprintf("  Discovering %s Resources", providerDisplayName(m.provider))))
	b.WriteString("\n\n")

	if !m.connected && m.connErr == nil {
		b.WriteString(fmt.Sprintf("  %s Connecting...\n", m.spinner.View()))
	} else if m.connErr != nil {
		b.WriteString(fmt.Sprintf("  %s Connection failed: %v\n", red.Render(iconFailed), m.connErr))
	} else {
		b.WriteString(fmt.Sprintf("  %s Connected\n", green.Render(iconCompleted)))
	}

	b.WriteString("\n")

	for _, t := range m.tasks {
		switch t.status {
		case taskPending:
			b.WriteString(fmt.Sprintf("  %s %s\n", dim.Render(iconPending), dim.Render(t.label)))
		case taskLoading:
			b.WriteString(fmt.Sprintf("  %s %s\n", m.spinner.View(), t.label))
		case taskDone:
			countStr := dim.Render(fmt.Sprintf("found %d", t.count))
			b.WriteString(fmt.Sprintf("  %s %-20s %s\n", green.Render(iconCompleted), t.label, countStr))
		case taskFailed:
			errStr := dim.Render(t.err.Error())
			b.WriteString(fmt.Sprintf("  %s %-20s %s\n", red.Render(iconFailed), t.label, errStr))
		}
	}

	b.WriteString("\n")

	if !m.done {
		b.WriteString(dim.Render("  Press q to cancel"))
		b.WriteString("\n")
	}

	return b.String()
}

// Results returns discovered resources keyed by type.
func (m discoveryModel) Results() map[string][]discovery.ProviderResource {
	return m.results
}

func (m discoveryModel) Err() error {
	return m.err
}

const (
	iconCompleted = "\u2713" // check
	iconFailed    = "\u2717" // x
	iconPending   = "\u25CB" // circle
)

func providerDisplayName(provider string) string {
	switch provider {
	case "harvester":
		return "Harvester"
	case "nutanix":
		return "Nutanix"
	case "gcp":
		return "GCP"
	case "aws":
		return "AWS"
	case "azure":
		return "Azure"
	default:
		return strings.ToUpper(provider)
	}
}

// runDiscovery runs provider discovery between form1 and form2.
func runDiscovery(provider string, disc discovery.ProviderDiscovery) (map[string][]discovery.ProviderResource, error) {
	m := newDiscoveryModel(provider, disc)
	p := tea.NewProgram(m)

	finalModel, err := p.Run()
	if err != nil {
		return nil, fmt.Errorf("discovery UI error: %w", err)
	}

	result := finalModel.(discoveryModel)
	if result.Err() != nil {
		return nil, result.Err()
	}

	return result.Results(), nil
}
