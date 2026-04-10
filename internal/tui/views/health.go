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
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/butlerdotdev/butler/internal/common/client"
	"github.com/butlerdotdev/butler/internal/tui/styles"
)

const autoRefreshInterval = 30 * time.Second

// healthCheck represents a single health check result.
type healthCheck struct {
	Name   string
	Status string // pass, fail, warn
	Detail string
}

type healthResultMsg struct {
	checks []healthCheck
	err    error
}

type autoRefreshMsg struct{}

// HealthView displays platform health checks.
type HealthView struct {
	client  *client.Client
	checks  []healthCheck
	loading bool
	err     error
	width   int
	height  int
}

// NewHealthView creates the health dashboard view.
func NewHealthView(c *client.Client) HealthView {
	return HealthView{
		client:  c,
		loading: true,
	}
}

// Init runs health checks and starts auto-refresh.
func (v HealthView) Init() tea.Cmd {
	return tea.Batch(v.runChecks(), v.scheduleRefresh())
}

// Update handles messages.
func (v HealthView) Update(msg tea.Msg) (HealthView, tea.Cmd) {
	switch msg := msg.(type) {
	case healthResultMsg:
		v.loading = false
		v.err = msg.err
		v.checks = msg.checks
		return v, nil

	case autoRefreshMsg:
		v.loading = true
		return v, tea.Batch(v.runChecks(), v.scheduleRefresh())

	case tea.WindowSizeMsg:
		v.width = msg.Width
		v.height = msg.Height
		return v, nil

	case tea.KeyMsg:
		if msg.String() == "r" {
			v.loading = true
			return v, v.runChecks()
		}
	}
	return v, nil
}

// View renders the health dashboard.
func (v HealthView) View() string {
	var b strings.Builder

	b.WriteString(styles.SectionStyle.Render("Platform Health Checks"))
	b.WriteString("\n\n")

	if v.loading && len(v.checks) == 0 {
		b.WriteString("  Running health checks...")
		return b.String()
	}
	if v.err != nil {
		b.WriteString(fmt.Sprintf("  Error: %v", v.err))
		return b.String()
	}

	for _, c := range v.checks {
		var icon string
		switch c.Status {
		case "pass":
			icon = styles.BadgeReady.Render("[PASS]")
		case "fail":
			icon = styles.BadgeFailed.Render("[FAIL]")
		case "warn":
			icon = styles.BadgeProvisioning.Render("[WARN]")
		}
		name := padRight(c.Name, 32)
		b.WriteString(fmt.Sprintf("  %s %s %s\n", icon, name, styles.HelpStyle.Render(c.Detail)))
	}

	if v.loading {
		b.WriteString("\n  Refreshing...")
	}

	return b.String()
}

func (v HealthView) scheduleRefresh() tea.Cmd {
	return tea.Tick(autoRefreshInterval, func(time.Time) tea.Msg {
		return autoRefreshMsg{}
	})
}

func (v HealthView) runChecks() tea.Cmd {
	c := v.client
	return func() tea.Msg {
		ctx := context.Background()
		var checks []healthCheck

		// 1. K8s API connectivity
		serverVersion, err := c.Clientset.Discovery().ServerVersion()
		if err != nil {
			checks = append(checks, healthCheck{"K8s API connectivity", "fail", fmt.Sprintf("unreachable: %v", err)})
			return healthResultMsg{checks: checks}
		}
		checks = append(checks, healthCheck{"K8s API connectivity", "pass", fmt.Sprintf("server %s", serverVersion.GitVersion)})

		// 2. butler-system namespace
		_, err = c.Clientset.CoreV1().Namespaces().Get(ctx, "butler-system", metav1.GetOptions{})
		if err != nil {
			checks = append(checks, healthCheck{"butler-system namespace", "fail", "not found"})
		} else {
			checks = append(checks, healthCheck{"butler-system namespace", "pass", "exists"})
		}

		// 3. ButlerConfig singleton
		bc, err := c.Dynamic.Resource(client.ButlerConfigGVR).Get(ctx, "butler", metav1.GetOptions{})
		if err != nil {
			checks = append(checks, healthCheck{"ButlerConfig singleton", "fail", "not found"})
		} else {
			phase := client.GetNestedString(bc.Object, "status", "phase")
			if phase == "" {
				phase = "no phase"
			}
			checks = append(checks, healthCheck{"ButlerConfig singleton", "pass", fmt.Sprintf("phase: %s", phase)})
		}

		// 4. Butler CRDs
		_, err = c.Dynamic.Resource(client.TenantClusterGVR).Namespace("butler-system").List(ctx, metav1.ListOptions{Limit: 1})
		if err != nil {
			errStr := err.Error()
			if strings.Contains(errStr, "could not find the requested resource") || strings.Contains(errStr, "no matches for kind") {
				checks = append(checks, healthCheck{"Butler CRDs installed", "fail", "TenantCluster CRD not registered"})
			} else {
				checks = append(checks, healthCheck{"Butler CRDs installed", "warn", fmt.Sprintf("could not verify: %v", err)})
			}
		} else {
			checks = append(checks, healthCheck{"Butler CRDs installed", "pass", "TenantCluster CRD found"})
		}

		// 5-7. Core deployments
		for _, dep := range []struct{ ns, name, display string }{
			{"butler-system", "butler-controller", "butler-controller"},
			{"butler-system", "butler-console-server", "butler-server"},
			{"steward-system", "steward", "steward"},
		} {
			d, err := c.Clientset.AppsV1().Deployments(dep.ns).Get(ctx, dep.name, metav1.GetOptions{})
			if err != nil {
				checks = append(checks, healthCheck{dep.display, "fail", "not found"})
				continue
			}
			desired := int32(1)
			if d.Spec.Replicas != nil {
				desired = *d.Spec.Replicas
			}
			ready := d.Status.ReadyReplicas
			detail := fmt.Sprintf("%d/%d ready", ready, desired)
			switch {
			case ready >= desired && desired > 0:
				checks = append(checks, healthCheck{dep.display, "pass", detail})
			case ready > 0:
				checks = append(checks, healthCheck{dep.display, "warn", detail})
			default:
				checks = append(checks, healthCheck{dep.display, "fail", detail})
			}
		}

		// 8. TenantCluster summary
		tcList, err := c.Dynamic.Resource(client.TenantClusterGVR).List(ctx, metav1.ListOptions{})
		if err == nil {
			phases := map[string]int{}
			for _, tc := range tcList.Items {
				phase := client.GetNestedString(tc.Object, "status", "phase")
				if phase == "" {
					phase = "Unknown"
				}
				phases[phase]++
			}
			total := len(tcList.Items)
			detail := fmt.Sprintf("total: %d", total)
			if r := phases["Ready"]; r > 0 {
				detail += fmt.Sprintf(", ready: %d", r)
			}
			if f := phases["Failed"]; f > 0 {
				detail += fmt.Sprintf(", failed: %d", f)
			}
			status := "pass"
			if phases["Failed"] > 0 {
				status = "warn"
			}
			checks = append(checks, healthCheck{"Tenant clusters", status, detail})
		}

		return healthResultMsg{checks: checks}
	}
}
