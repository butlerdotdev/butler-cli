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
	"bufio"
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/butlerdotdev/butler/internal/adm/bootstrap/orchestrator"
)

// debugTab identifies which sub-tab of the debug panel is active.
type debugTab int

const (
	debugTabController debugTab = iota
	debugTabProvider
	debugTabCRStatus
	debugTabCount
)

var debugTabNames = []string{"Controller Logs", "Provider Logs", "CR Status"}

// debugPanelModel is a togglable sub-view of the during-bootstrap screen
// that surfaces data the operator would otherwise have to chase via
// `kind export kubeconfig` + `kubectl logs` + `kubectl describe`. It
// streams butler-bootstrap-controller and butler-provider-* pod logs via
// client-go and renders the last ClusterBootstrap CR snapshot received
// over the EventSink.
type debugPanelModel struct {
	active     bool
	tab        debugTab
	width      int
	height     int
	kubeconfig string
	provider   string // harvester, nutanix, etc. — drives provider-log label selector

	// Buffers fed by background log streaming goroutines.
	controllerLogs *LogBuffer
	providerLogs   *LogBuffer

	// Last CR status snapshot (piggybacks on EventBootstrapStatus).
	lastStatus *orchestrator.BootstrapSnapshot

	// Cancels all background streaming goroutines.
	cancel context.CancelFunc

	// Scroll offset for the log viewport (number of lines to skip from top).
	// Tracked per-tab so switching tabs resets scroll cleanly.
	scrollOffset int
}

func newDebugPanelModel(provider string) debugPanelModel {
	return debugPanelModel{
		provider:       provider,
		controllerLogs: NewLogBuffer(2000),
		providerLogs:   NewLogBuffer(2000),
	}
}

// IsActive reports whether the debug panel should render instead of the
// normal during-bootstrap layout.
func (m debugPanelModel) IsActive() bool { return m.active }

// Toggle flips the debug panel on or off.
func (m *debugPanelModel) Toggle() {
	m.active = !m.active
}

// SetSize updates render dimensions.
func (m *debugPanelModel) SetSize(w, h int) {
	m.width = w
	m.height = h
}

// CycleTab moves to the next/previous sub-tab.
func (m *debugPanelModel) CycleTab(dir int) {
	m.tab = debugTab((int(m.tab) + dir + int(debugTabCount)) % int(debugTabCount))
	m.scrollOffset = 0
}

// ScrollBy adjusts the log viewport scroll offset. Negative = up, positive = down.
func (m *debugPanelModel) ScrollBy(delta int) {
	m.scrollOffset += delta
	if m.scrollOffset < 0 {
		m.scrollOffset = 0
	}
}

// SetStatus updates the CR status snapshot displayed on the status tab.
func (m *debugPanelModel) SetStatus(s *orchestrator.BootstrapSnapshot) {
	m.lastStatus = s
}

// Start kicks off background streaming goroutines once the KIND kubeconfig
// path is known (after EventKINDReady). Idempotent — calling twice is a
// no-op.
func (m *debugPanelModel) Start(kubeconfigPath string) {
	if m.cancel != nil || kubeconfigPath == "" {
		return
	}
	m.kubeconfig = kubeconfigPath

	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel

	m.controllerLogs.Write(fmt.Sprintf("[debug] connecting to KIND: %s", kubeconfigPath))

	cfg, err := clientcmd.BuildConfigFromFlags("", kubeconfigPath)
	if err != nil {
		m.controllerLogs.Write(fmt.Sprintf("[debug] failed to load kubeconfig: %v", err))
		return
	}
	client, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		m.controllerLogs.Write(fmt.Sprintf("[debug] failed to build client: %v", err))
		return
	}

	m.controllerLogs.Write("[debug] waiting for butler-bootstrap-controller pod...")
	go streamPodLogs(ctx, client, "butler-system", "app.kubernetes.io/name=butler-bootstrap-controller", m.controllerLogs)

	if m.provider != "" {
		providerLabel := fmt.Sprintf("app.kubernetes.io/name=butler-provider-%s", m.provider)
		m.providerLogs.Write(fmt.Sprintf("[debug] waiting for butler-provider-%s pod...", m.provider))
		go streamPodLogs(ctx, client, "butler-system", providerLabel, m.providerLogs)
	} else {
		m.providerLogs.Write("[debug] no provider configured — provider logs disabled")
	}
}

// Stop cancels any running log streams. Safe to call before Start.
func (m *debugPanelModel) Stop() {
	if m.cancel != nil {
		m.cancel()
		m.cancel = nil
	}
}

// streamPodLogs polls for a pod matching selector in namespace and streams
// its logs into buf. Reconnects on pod restart or stream error until ctx
// is cancelled. Every state transition writes a diagnostic line into buf
// so the operator can see what the streaming goroutine is doing without
// having to re-run and add println statements.
func streamPodLogs(ctx context.Context, client kubernetes.Interface, namespace, selector string, buf *LogBuffer) {
	var announcedWaiting, announcedStreaming bool
	for {
		if ctx.Err() != nil {
			return
		}

		pods, err := client.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
			LabelSelector: selector,
		})
		if err != nil {
			buf.Write(fmt.Sprintf("[debug] list pods error: %v", err))
			select {
			case <-ctx.Done():
				return
			case <-time.After(3 * time.Second):
				continue
			}
		}
		if len(pods.Items) == 0 {
			if !announcedWaiting {
				buf.Write(fmt.Sprintf("[debug] no pods matching %q in %s yet", selector, namespace))
				announcedWaiting = true
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(3 * time.Second):
				continue
			}
		}

		pod := pods.Items[0]
		if pod.Status.Phase != corev1.PodRunning {
			buf.Write(fmt.Sprintf("[debug] pod %s in phase %s, waiting...", pod.Name, pod.Status.Phase))
			select {
			case <-ctx.Done():
				return
			case <-time.After(3 * time.Second):
				continue
			}
		}

		if !announcedStreaming {
			buf.Write(fmt.Sprintf("[debug] streaming logs from %s", pod.Name))
			announcedStreaming = true
		}

		// Stream logs — Follow + SinceSeconds to catch anything we missed
		// since the last reconnect attempt.
		sinceSeconds := int64(300)
		req := client.CoreV1().Pods(namespace).GetLogs(pod.Name, &corev1.PodLogOptions{
			Follow:       true,
			SinceSeconds: &sinceSeconds,
		})

		stream, err := req.Stream(ctx)
		if err != nil {
			buf.Write(fmt.Sprintf("[debug] log stream error: %v", err))
			select {
			case <-ctx.Done():
				return
			case <-time.After(2 * time.Second):
				continue
			}
		}

		scanner := bufio.NewScanner(stream)
		scanner.Buffer(make([]byte, 64*1024), 1024*1024)
		lineCount := 0
		for scanner.Scan() {
			line := scanner.Text()
			_ = strings.TrimSpace(line)
			buf.Write(line)
			lineCount++
		}
		_ = stream.Close()

		buf.Write(fmt.Sprintf("[debug] stream ended after %d lines, reconnecting...", lineCount))
		announcedStreaming = false

		// Stream ended — loop to reconnect.
		select {
		case <-ctx.Done():
			return
		case <-time.After(1 * time.Second):
		}
	}
}

// View renders the active tab's content. Caller is responsible for adding
// surrounding chrome (title bar, help bar, etc.) — this returns only the
// tab bar + body.
func (m debugPanelModel) View() string {
	var b strings.Builder

	// Tab bar
	b.WriteString(m.renderTabBar())
	b.WriteString("\n\n")

	// Body
	switch m.tab {
	case debugTabController:
		b.WriteString(m.renderLogs(m.controllerLogs))
	case debugTabProvider:
		b.WriteString(m.renderLogs(m.providerLogs))
	case debugTabCRStatus:
		b.WriteString(m.renderCRStatus())
	}

	return b.String()
}

func (m debugPanelModel) renderTabBar() string {
	dim := lipgloss.NewStyle().Foreground(lipgloss.Color("#666666"))
	active := lipgloss.NewStyle().Foreground(lipgloss.Color("#22c55e")).Bold(true).Underline(true)

	var parts []string
	for i, name := range debugTabNames {
		label := fmt.Sprintf("%d:%s", i+1, name)
		if debugTab(i) == m.tab {
			parts = append(parts, active.Render(label))
		} else {
			parts = append(parts, dim.Render(label))
		}
	}
	return strings.Join(parts, "   ")
}

func (m debugPanelModel) renderLogs(buf *LogBuffer) string {
	lines := buf.Lines()
	if len(lines) == 0 {
		return dimStyle.Render("  (no output yet — waiting for KIND cluster and controller pod)")
	}

	// Reserve space for tab bar (2 lines) and help bar (2 lines).
	viewportHeight := m.height - 8
	if viewportHeight < 5 {
		viewportHeight = 5
	}

	// Clamp scroll offset so the viewport never starts past the end.
	maxOffset := len(lines) - viewportHeight
	if maxOffset < 0 {
		maxOffset = 0
	}
	if m.scrollOffset > maxOffset {
		m.scrollOffset = maxOffset
	}

	// If no scroll applied, show the tail (latest logs).
	start := len(lines) - viewportHeight - m.scrollOffset
	if start < 0 {
		start = 0
	}
	end := start + viewportHeight
	if end > len(lines) {
		end = len(lines)
	}

	return strings.Join(lines[start:end], "\n")
}

func (m debugPanelModel) renderCRStatus() string {
	if m.lastStatus == nil {
		return dimStyle.Render("  (no CR status received yet — bootstrap controller has not published a snapshot)")
	}

	s := m.lastStatus
	var b strings.Builder

	fmt.Fprintf(&b, "Phase:                %s\n", s.Phase)
	if s.Endpoint != "" {
		fmt.Fprintf(&b, "Control Plane:        %s\n", s.Endpoint)
	}
	if s.ConsoleURL != "" {
		fmt.Fprintf(&b, "Console URL:          %s\n", s.ConsoleURL)
	}
	if s.FailureReason != "" {
		fmt.Fprintf(&b, "Failure Reason:       %s\n", s.FailureReason)
	}
	if s.FailureMessage != "" {
		fmt.Fprintf(&b, "Failure Message:      %s\n", s.FailureMessage)
	}

	b.WriteString("\n")
	b.WriteString(sectionStyle.Render("Machines"))
	b.WriteString("\n")
	if len(s.Machines) == 0 {
		b.WriteString(dimStyle.Render("  (none yet)\n"))
	} else {
		for _, mach := range s.Machines {
			ready := "no"
			if mach.Ready {
				ready = "yes"
			}
			fmt.Fprintf(&b, "  %-30s  %-13s  %-10s  %-15s  ready=%s  talos=%v\n",
				mach.Name, mach.Role, mach.Phase, mach.IPAddress, ready, mach.TalosConfigured)
		}
	}

	b.WriteString("\n")
	b.WriteString(sectionStyle.Render("Addons Installed"))
	b.WriteString("\n")
	if len(s.AddonsInstalled) == 0 {
		b.WriteString(dimStyle.Render("  (none yet)\n"))
	} else {
		for name, installed := range s.AddonsInstalled {
			mark := "✗"
			if installed {
				mark = "✓"
			}
			fmt.Fprintf(&b, "  %s  %s\n", mark, name)
		}
	}

	return b.String()
}
