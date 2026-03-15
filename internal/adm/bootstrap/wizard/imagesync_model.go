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

// imageSyncModel is a bubbletea model that uploads an OS image to a provider
// and polls until it is ready. Used when the user selects "Sync new image
// from Butler Image Factory" in the resource selection form.
type imageSyncModel struct {
	disc        discovery.ProviderDiscovery
	artifactURL string
	displayName string
	spinner     spinner.Model

	phase       syncPhase
	syncID      string
	providerRef string
	pollCount   int
	err         error
	done        bool
}

type syncPhase int

const (
	syncUploading syncPhase = iota
	syncPolling
	syncComplete
	syncFailed
)

// Image sync messages.
type syncStartedMsg struct {
	syncID string
	err    error
}

type syncPollMsg struct {
	ready       bool
	providerRef string
	err         error
}

type syncTickMsg struct{}

func newImageSyncModel(disc discovery.ProviderDiscovery, artifactURL, displayName string) imageSyncModel {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(brandGreen)

	return imageSyncModel{
		disc:        disc,
		artifactURL: artifactURL,
		displayName: displayName,
		spinner:     s,
		phase:       syncUploading,
	}
}

func startSyncCmd(disc discovery.ProviderDiscovery, artifactURL, displayName string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		syncID, err := disc.SyncImage(ctx, artifactURL, displayName)
		return syncStartedMsg{syncID: syncID, err: err}
	}
}

func pollSyncCmd(disc discovery.ProviderDiscovery, syncID string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		ready, ref, err := disc.PollImageSync(ctx, syncID)
		return syncPollMsg{ready: ready, providerRef: ref, err: err}
	}
}

func syncTickCmd() tea.Cmd {
	return tea.Tick(5*time.Second, func(_ time.Time) tea.Msg {
		return syncTickMsg{}
	})
}

func (m imageSyncModel) Init() tea.Cmd {
	return tea.Batch(
		m.spinner.Tick,
		startSyncCmd(m.disc, m.artifactURL, m.displayName),
	)
}

func (m imageSyncModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			m.done = true
			m.err = fmt.Errorf("image sync cancelled")
			return m, tea.Quit
		}

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd

	case syncStartedMsg:
		if msg.err != nil {
			m.phase = syncFailed
			m.err = fmt.Errorf("upload failed: %w", msg.err)
			m.done = true
			return m, tea.Quit
		}
		m.syncID = msg.syncID
		m.phase = syncPolling
		return m, syncTickCmd()

	case syncTickMsg:
		if m.phase != syncPolling {
			return m, nil
		}
		return m, pollSyncCmd(m.disc, m.syncID)

	case syncPollMsg:
		if msg.err != nil {
			m.phase = syncFailed
			m.err = fmt.Errorf("sync failed: %w", msg.err)
			m.done = true
			return m, tea.Quit
		}
		m.pollCount++
		if msg.ready {
			m.phase = syncComplete
			m.providerRef = msg.providerRef
			m.done = true
			return m, tea.Quit
		}
		// Keep polling every 5 seconds.
		return m, syncTickCmd()
	}

	return m, nil
}

func (m imageSyncModel) View() string {
	var b strings.Builder

	title := lipgloss.NewStyle().
		Foreground(brandBlue).
		Bold(true)

	dim := lipgloss.NewStyle().Foreground(brandGray)
	green := lipgloss.NewStyle().Foreground(brandGreen)
	red := lipgloss.NewStyle().Foreground(brandRed)

	b.WriteString("\n")
	b.WriteString(title.Render("  Syncing Image from Butler Image Factory"))
	b.WriteString("\n\n")

	b.WriteString(dim.Render("  Image:    "))
	b.WriteString(m.displayName)
	b.WriteString("\n")

	b.WriteString(dim.Render("  Source:   "))
	// Truncate long URLs for display.
	url := m.artifactURL
	if len(url) > 60 {
		url = url[:57] + "..."
	}
	b.WriteString(dim.Render(url))
	b.WriteString("\n\n")

	switch m.phase {
	case syncUploading:
		b.WriteString(fmt.Sprintf("  %s Uploading image to provider...\n", m.spinner.View()))
	case syncPolling:
		dots := strings.Repeat(".", (m.pollCount%3)+1)
		b.WriteString(fmt.Sprintf("  %s Waiting for image to be ready%s\n", m.spinner.View(), dots))
		if m.pollCount > 0 {
			b.WriteString(dim.Render(fmt.Sprintf("    Checked %d time(s)", m.pollCount)))
			b.WriteString("\n")
		}
	case syncComplete:
		b.WriteString(fmt.Sprintf("  %s Image ready: %s\n", green.Render(iconCompleted), m.providerRef))
	case syncFailed:
		b.WriteString(fmt.Sprintf("  %s %v\n", red.Render(iconFailed), m.err))
	}

	b.WriteString("\n")

	if !m.done {
		b.WriteString(dim.Render("  Press q to cancel"))
		b.WriteString("\n")
	}

	return b.String()
}

// ProviderRef returns the provider image reference after a successful sync.
func (m imageSyncModel) ProviderRef() string {
	return m.providerRef
}

// Err returns any error that occurred during the sync.
func (m imageSyncModel) Err() error {
	return m.err
}

// runImageSync runs the image sync model as a bubbletea program and returns
// the provider image reference on success.
func runImageSync(disc discovery.ProviderDiscovery, artifactURL, displayName string) (string, error) {
	m := newImageSyncModel(disc, artifactURL, displayName)
	p := tea.NewProgram(m)

	finalModel, err := p.Run()
	if err != nil {
		return "", fmt.Errorf("image sync UI error: %w", err)
	}

	result := finalModel.(imageSyncModel)
	if result.Err() != nil {
		return "", result.Err()
	}

	return result.ProviderRef(), nil
}
