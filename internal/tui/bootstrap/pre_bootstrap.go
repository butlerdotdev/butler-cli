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

	tea "github.com/charmbracelet/bubbletea"

	"github.com/butlerdotdev/butler/internal/adm/bootstrap/orchestrator"
)

// preBootstrapModel shows config summary, prereq checks, and a confirmation prompt.
type preBootstrapModel struct {
	cfg     *orchestrator.Config
	checks  []Check
	width   int
	height  int
	checked bool
}

func newPreBootstrapModel(cfg *orchestrator.Config) preBootstrapModel {
	return preBootstrapModel{
		cfg:   cfg,
		width: 80,
	}
}

func (m preBootstrapModel) Init() tea.Cmd {
	return m.runPrereqChecks
}

// runPrereqChecks runs prerequisite validation and returns results as a message.
func (m preBootstrapModel) runPrereqChecks() tea.Msg {
	checks := CheckAll(false)
	checks = append(checks, m.checkConfig())
	return prereqChecksMsg(checks)
}

type prereqChecksMsg []Check

func (m preBootstrapModel) checkConfig() Check {
	check := Check{Name: "Config valid"}
	if m.cfg == nil {
		check.Detail = "No config loaded"
		return check
	}
	if m.cfg.Cluster.Name == "" {
		check.Detail = "Cluster name is required"
		return check
	}
	if m.cfg.Provider == "" {
		check.Detail = "Provider is required"
		return check
	}
	check.Passed = true
	check.Detail = "Configuration is valid"
	return check
}

func (m preBootstrapModel) Update(msg tea.Msg) (preBootstrapModel, tea.Cmd) {
	switch msg := msg.(type) {
	case prereqChecksMsg:
		m.checks = []Check(msg)
		m.checked = true
	}
	return m, nil
}

// AllPassed returns true if all prerequisite checks passed.
func (m preBootstrapModel) AllPassed() bool {
	if !m.checked {
		return false
	}
	return AllPassed(m.checks)
}

// SetSize sets the available render dimensions.
func (m *preBootstrapModel) SetSize(w, h int) {
	m.width = w
	m.height = h
}

func (m preBootstrapModel) View() string {
	var b strings.Builder
	contentWidth := m.width - 4 // Account for border padding

	// Title
	title := titleBarStyle.Width(contentWidth).Render("Bootstrap Configuration")
	b.WriteString(title + "\n\n")

	// Config summary
	if m.cfg != nil {
		b.WriteString(m.renderConfigSummary(contentWidth))
		b.WriteString("\n")
	}

	// Prerequisites
	b.WriteString(sectionStyle.Render("Prerequisites") + "\n")
	if !m.checked {
		b.WriteString(phasePending.Render("  Checking prerequisites...\n"))
	} else {
		for _, c := range m.checks {
			if c.Passed {
				icon := phaseCompleted.Render(iconCompleted)
				name := phaseCompleted.Render(c.Name)
				b.WriteString(fmt.Sprintf("  %s %s\n", icon, name))
			} else {
				icon := phaseFailed.Render(iconFailed)
				name := phaseFailed.Render(c.Name)
				detail := phaseFailed.Render(" - " + c.Detail)
				b.WriteString(fmt.Sprintf("  %s %s%s\n", icon, name, detail))
			}
		}
	}

	b.WriteString("\n")

	// Action prompt
	if m.AllPassed() {
		b.WriteString(successStyle.Render("  Press Enter to start bootstrap, q to quit"))
	} else if m.checked {
		b.WriteString(failureStyle.Render("  Prerequisites failed. Fix the issues above and try again."))
		b.WriteString("\n")
		b.WriteString(dimStyle.Render("  Press q to quit"))
	}

	return normalBorder.Width(contentWidth).Render(b.String())
}

func (m preBootstrapModel) renderConfigSummary(width int) string {
	var b strings.Builder
	cfg := m.cfg

	b.WriteString(sectionStyle.Render("Cluster") + "\n")
	b.WriteString(fmt.Sprintf("  %s %s\n", labelStyle.Render("Name:"), valueStyle.Render(cfg.Cluster.Name)))
	b.WriteString(fmt.Sprintf("  %s %s\n", labelStyle.Render("Provider:"), valueStyle.Render(cfg.Provider)))
	b.WriteString(fmt.Sprintf("  %s %s\n", labelStyle.Render("Topology:"), valueStyle.Render(cfg.Cluster.Topology)))

	if cfg.IsSingleNode() {
		b.WriteString(fmt.Sprintf("  %s %s\n", labelStyle.Render("Control Plane:"),
			valueStyle.Render(fmt.Sprintf("1 node (%d CPU, %d MB, %d GB)",
				cfg.Cluster.ControlPlane.CPU, cfg.Cluster.ControlPlane.MemoryMB, cfg.Cluster.ControlPlane.DiskGB))))
	} else {
		b.WriteString(fmt.Sprintf("  %s %s\n", labelStyle.Render("Control Plane:"),
			valueStyle.Render(fmt.Sprintf("%d nodes (%d CPU, %d MB, %d GB each)",
				cfg.Cluster.ControlPlane.Replicas, cfg.Cluster.ControlPlane.CPU,
				cfg.Cluster.ControlPlane.MemoryMB, cfg.Cluster.ControlPlane.DiskGB))))
		b.WriteString(fmt.Sprintf("  %s %s\n", labelStyle.Render("Workers:"),
			valueStyle.Render(fmt.Sprintf("%d nodes (%d CPU, %d MB, %d GB each)",
				cfg.Cluster.Workers.Replicas, cfg.Cluster.Workers.CPU,
				cfg.Cluster.Workers.MemoryMB, cfg.Cluster.Workers.DiskGB))))
	}

	b.WriteString("\n")
	b.WriteString(sectionStyle.Render("Network") + "\n")
	b.WriteString(fmt.Sprintf("  %s %s\n", labelStyle.Render("Pod CIDR:"), valueStyle.Render(cfg.Network.PodCIDR)))
	b.WriteString(fmt.Sprintf("  %s %s\n", labelStyle.Render("Service CIDR:"), valueStyle.Render(cfg.Network.ServiceCIDR)))
	if cfg.Network.VIP != "" {
		b.WriteString(fmt.Sprintf("  %s %s\n", labelStyle.Render("VIP:"), valueStyle.Render(cfg.Network.VIP)))
	}

	b.WriteString("\n")
	b.WriteString(sectionStyle.Render("Talos") + "\n")
	b.WriteString(fmt.Sprintf("  %s %s\n", labelStyle.Render("Version:"), valueStyle.Render(cfg.Talos.Version)))
	if cfg.Talos.Schematic != "" {
		b.WriteString(fmt.Sprintf("  %s %s\n", labelStyle.Render("Schematic:"), dimStyle.Render(cfg.Talos.Schematic[:min(16, len(cfg.Talos.Schematic))]+"...")))
	}

	b.WriteString("\n")
	return b.String()
}
