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

	"github.com/butlerdotdev/butler/internal/adm/bootstrap/orchestrator"
)

// postBootstrapModel displays the bootstrap result summary.
type postBootstrapModel struct {
	success     bool
	clusterName string
	provider    string
	topology    string
	creds       *orchestrator.ClusterCredentials
	elapsed     time.Duration
	// Failure info
	failPhase   string
	failReason  string
	failMessage string
	skipCleanup bool

	width int
}

func newPostBootstrapModel(clusterName, provider, topology string) postBootstrapModel {
	return postBootstrapModel{
		clusterName: clusterName,
		provider:    provider,
		topology:    topology,
		width:       80,
	}
}

// SetSuccess configures the model for a successful bootstrap.
func (m *postBootstrapModel) SetSuccess(creds *orchestrator.ClusterCredentials, elapsed time.Duration) {
	m.success = true
	m.creds = creds
	m.elapsed = elapsed
}

// SetFailure configures the model for a failed bootstrap.
func (m *postBootstrapModel) SetFailure(phase, reason, message string, elapsed time.Duration, skipCleanup bool) {
	m.success = false
	m.failPhase = phase
	m.failReason = reason
	m.failMessage = message
	m.elapsed = elapsed
	m.skipCleanup = skipCleanup
}

// SetSize sets the available render width.
func (m *postBootstrapModel) SetSize(w, h int) {
	m.width = w
}

func (m postBootstrapModel) View() string {
	contentWidth := m.width - 4

	if m.success {
		return m.viewSuccess(contentWidth)
	}
	return m.viewFailure(contentWidth)
}

func (m postBootstrapModel) viewSuccess(width int) string {
	var b strings.Builder

	title := titleBarStyle.Width(width).
		Background(colorSuccess).
		Render("Bootstrap Complete!")
	b.WriteString(title + "\n\n")

	b.WriteString(fmt.Sprintf("  %s %s\n", labelStyle.Render("Cluster:"), valueStyle.Render(m.clusterName)))
	b.WriteString(fmt.Sprintf("  %s %s\n", labelStyle.Render("Provider:"), valueStyle.Render(m.provider)))
	b.WriteString(fmt.Sprintf("  %s %s\n", labelStyle.Render("Topology:"), valueStyle.Render(m.topology)))
	b.WriteString(fmt.Sprintf("  %s %s\n", labelStyle.Render("Duration:"), valueStyle.Render(m.elapsed.Round(time.Second).String())))

	kubeconfigPath := fmt.Sprintf("~/.butler/%s-kubeconfig", m.clusterName)
	talosconfigPath := fmt.Sprintf("~/.butler/%s-talosconfig", m.clusterName)
	b.WriteString(fmt.Sprintf("  %s %s\n", labelStyle.Render("Kubeconfig:"), valueStyle.Render(kubeconfigPath)))
	b.WriteString(fmt.Sprintf("  %s %s\n", labelStyle.Render("Talosconfig:"), valueStyle.Render(talosconfigPath)))

	if m.creds != nil && m.creds.ConsoleURL != "" {
		b.WriteString(fmt.Sprintf("  %s %s\n", labelStyle.Render("Console URL:"), valueStyle.Render(m.creds.ConsoleURL)))
	}

	if m.creds != nil && len(m.creds.ControlPlaneIPs) > 0 {
		b.WriteString(fmt.Sprintf("  %s %s\n", labelStyle.Render("CP Endpoint:"),
			valueStyle.Render(strings.Join(m.creds.ControlPlaneIPs, ", "))))
	}

	b.WriteString("\n")
	b.WriteString(sectionStyle.Render("  Next Steps:") + "\n")
	b.WriteString(dimStyle.Render(fmt.Sprintf("    export KUBECONFIG=%s\n", kubeconfigPath)))
	b.WriteString(dimStyle.Render("    kubectl get nodes\n"))
	if m.creds != nil && len(m.creds.ControlPlaneIPs) > 0 {
		b.WriteString(dimStyle.Render(fmt.Sprintf("    talosctl health --nodes %s\n", m.creds.ControlPlaneIPs[0])))
	}

	b.WriteString("\n")
	b.WriteString(helpBarStyle.Render("  Press q or Enter to exit"))

	return normalBorder.Width(width).Render(b.String())
}

func (m postBootstrapModel) viewFailure(width int) string {
	var b strings.Builder

	title := titleBarStyle.Width(width).
		Background(colorError).
		Render("Bootstrap Failed")
	b.WriteString(title + "\n\n")

	b.WriteString(fmt.Sprintf("  %s %s\n", labelStyle.Render("Phase:"), phaseFailed.Render(m.failPhase)))
	b.WriteString(fmt.Sprintf("  %s %s\n", labelStyle.Render("Reason:"), valueStyle.Render(m.failReason)))
	b.WriteString(fmt.Sprintf("  %s %s\n", labelStyle.Render("Message:"), valueStyle.Render(m.failMessage)))
	b.WriteString(fmt.Sprintf("  %s %s\n", labelStyle.Render("Duration:"), valueStyle.Render(m.elapsed.Round(time.Second).String())))

	b.WriteString("\n")
	b.WriteString(sectionStyle.Render("  Debug:") + "\n")
	if m.skipCleanup {
		b.WriteString(dimStyle.Render("    KIND cluster preserved (--skip-cleanup was set)\n"))
		b.WriteString(dimStyle.Render("    kubectl --kubeconfig /tmp/butler-bootstrap-kubeconfig get clusterbootstraps -n butler-system\n"))
		b.WriteString(dimStyle.Render("    kubectl --kubeconfig /tmp/butler-bootstrap-kubeconfig get machinerequests -n butler-system\n"))
	} else {
		b.WriteString(dimStyle.Render("    KIND cluster was cleaned up. Rerun with --skip-cleanup to preserve for debugging.\n"))
	}

	b.WriteString("\n")
	b.WriteString(helpBarStyle.Render("  Press q or Enter to exit"))

	return normalBorder.Width(width).Render(b.String())
}
