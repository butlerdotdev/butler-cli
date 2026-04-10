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

	tea "github.com/charmbracelet/bubbletea"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/butlerdotdev/butler/internal/common/client"
	"github.com/butlerdotdev/butler/internal/common/output"
	clusterhelpers "github.com/butlerdotdev/butler/internal/ctl/cluster"
	"github.com/butlerdotdev/butler/internal/tui/components"
	"github.com/butlerdotdev/butler/internal/tui/styles"
)

// Tab indices for the cluster detail view.
const (
	tabOverview    = 0
	tabNodes       = 1
	tabAddons      = 2
	tabConditions  = 3
	tabEvents      = 4
	tabCount       = 5
)

var tabNames = []string{"Overview", "Nodes", "Addons", "Conditions", "Events"}

// clusterDetailMsg carries fetched detail data.
type clusterDetailMsg struct {
	info       clusterhelpers.TenantClusterInfo
	raw        *unstructured.Unstructured
	machines   []machineInfo
	addons     []addonInfo
	conditions []conditionInfo
	events     []eventInfo
	err        error
}

type machineInfo struct {
	Name    string
	Phase   string
	Address string
	Role    string
}

type addonInfo struct {
	Name    string
	Phase   string
	Version string
}

type conditionInfo struct {
	Type    string
	Status  string
	Reason  string
	Message string
}

type eventInfo struct {
	Type    string
	Reason  string
	Message string
	Age     string
}

// ClusterDetailView shows detailed info about a single cluster.
type ClusterDetailView struct {
	client    *client.Client
	name      string
	namespace string
	activeTab int
	loading   bool
	err       error

	info       clusterhelpers.TenantClusterInfo
	machines   []machineInfo
	addons     []addonInfo
	conditions []conditionInfo
	events     []eventInfo

	nodeTable      components.Table
	addonTable     components.Table
	conditionTable components.Table
	eventTable     components.Table

	width  int
	height int
}

// NewClusterDetailView creates a detail view for a named cluster.
func NewClusterDetailView(c *client.Client, name, namespace string) ClusterDetailView {
	return ClusterDetailView{
		client:         c,
		name:           name,
		namespace:      namespace,
		loading:        true,
		nodeTable:      components.NewTable([]string{"NAME", "PHASE", "ADDRESS", "ROLE"}),
		addonTable:     components.NewTable([]string{"NAME", "PHASE", "VERSION"}),
		conditionTable: components.NewTable([]string{"TYPE", "STATUS", "REASON", "MESSAGE"}),
		eventTable:     components.NewTable([]string{"TYPE", "REASON", "MESSAGE", "AGE"}),
	}
}

// Name returns the cluster name for display in breadcrumbs.
func (v *ClusterDetailView) Name() string {
	return v.name
}

// IsFiltering returns true if any active sub-table is in filter mode.
func (v *ClusterDetailView) IsFiltering() bool {
	return v.activeTableFiltering()
}

// Init starts fetching cluster detail.
func (v ClusterDetailView) Init() tea.Cmd {
	return v.fetchDetail()
}

// Update handles messages.
func (v ClusterDetailView) Update(msg tea.Msg) (ClusterDetailView, tea.Cmd) {
	switch msg := msg.(type) {
	case clusterDetailMsg:
		v.loading = false
		v.err = msg.err
		if msg.err == nil {
			v.info = msg.info
			v.machines = msg.machines
			v.addons = msg.addons
			v.conditions = msg.conditions
			v.events = msg.events
			v.rebuildTables()
		}
		return v, nil

	case tea.WindowSizeMsg:
		v.width = msg.Width
		v.height = msg.Height
		tabH := msg.Height - 6
		v.nodeTable.SetSize(msg.Width, tabH)
		v.addonTable.SetSize(msg.Width, tabH)
		v.conditionTable.SetSize(msg.Width, tabH)
		v.eventTable.SetSize(msg.Width, tabH)
		return v, nil

	case tea.KeyMsg:
		// Forward to active table if filtering
		if v.activeTableFiltering() {
			return v.updateActiveTable(msg)
		}

		switch msg.String() {
		case "tab":
			v.activeTab = (v.activeTab + 1) % tabCount
			return v, nil
		case "shift+tab":
			v.activeTab = (v.activeTab - 1 + tabCount) % tabCount
			return v, nil
		case "r":
			v.loading = true
			return v, v.fetchDetail()
		default:
			return v.updateActiveTable(msg)
		}
	}
	return v, nil
}

func (v *ClusterDetailView) activeTableFiltering() bool {
	switch v.activeTab {
	case tabNodes:
		return v.nodeTable.Filtering()
	case tabAddons:
		return v.addonTable.Filtering()
	case tabConditions:
		return v.conditionTable.Filtering()
	case tabEvents:
		return v.eventTable.Filtering()
	}
	return false
}

func (v ClusterDetailView) updateActiveTable(msg tea.KeyMsg) (ClusterDetailView, tea.Cmd) {
	var cmd tea.Cmd
	switch v.activeTab {
	case tabNodes:
		cmd = v.nodeTable.Update(msg)
	case tabAddons:
		cmd = v.addonTable.Update(msg)
	case tabConditions:
		cmd = v.conditionTable.Update(msg)
	case tabEvents:
		cmd = v.eventTable.Update(msg)
	}
	return v, cmd
}

// View renders the detail view with tabs.
func (v ClusterDetailView) View() string {
	if v.loading {
		return "  Loading cluster detail..."
	}
	if v.err != nil {
		return fmt.Sprintf("  Error: %v", v.err)
	}

	var b strings.Builder

	// Tab bar
	for i, name := range tabNames {
		if i == v.activeTab {
			b.WriteString(styles.ActiveTabStyle.Render(name))
		} else {
			b.WriteString(styles.TabStyle.Render(name))
		}
	}
	b.WriteString("\n\n")

	// Tab content
	switch v.activeTab {
	case tabOverview:
		b.WriteString(v.viewOverview())
	case tabNodes:
		b.WriteString(v.nodeTable.View())
	case tabAddons:
		b.WriteString(v.addonTable.View())
	case tabConditions:
		b.WriteString(v.conditionTable.View())
	case tabEvents:
		b.WriteString(v.eventTable.View())
	}

	return b.String()
}

func (v *ClusterDetailView) viewOverview() string {
	detail := components.DetailPane{
		Sections: []components.DetailSection{
			{
				Fields: []components.DetailField{
					{Label: "Name", Value: v.info.Name},
					{Label: "Namespace", Value: v.info.Namespace},
					{Label: "Phase", Value: v.info.Phase},
					{Label: "K8s Version", Value: v.info.KubernetesVersion},
					{Label: "Workers", Value: output.FormatWorkers(v.info.WorkersReady, v.info.WorkersDesired)},
					{Label: "Endpoint", Value: valueOr(v.info.Endpoint, "-")},
					{Label: "Provider", Value: valueOr(v.info.ProviderConfig, "-")},
					{Label: "Tenant NS", Value: valueOr(v.info.TenantNamespace, "-")},
					{Label: "Created", Value: v.info.CreationTime},
				},
			},
		},
	}
	return detail.View()
}

func (v *ClusterDetailView) rebuildTables() {
	// Nodes
	nodeRows := make([][]string, len(v.machines))
	for i, m := range v.machines {
		nodeRows[i] = []string{m.Name, m.Phase, m.Address, m.Role}
	}
	v.nodeTable.SetRows(nodeRows)

	// Addons
	addonRows := make([][]string, len(v.addons))
	for i, a := range v.addons {
		addonRows[i] = []string{a.Name, a.Phase, a.Version}
	}
	v.addonTable.SetRows(addonRows)

	// Conditions
	condRows := make([][]string, len(v.conditions))
	for i, c := range v.conditions {
		condRows[i] = []string{c.Type, c.Status, c.Reason, truncate(c.Message, 60)}
	}
	v.conditionTable.SetRows(condRows)

	// Events
	eventRows := make([][]string, len(v.events))
	for i, e := range v.events {
		eventRows[i] = []string{e.Type, e.Reason, truncate(e.Message, 60), e.Age}
	}
	v.eventTable.SetRows(eventRows)
}

func (v ClusterDetailView) fetchDetail() tea.Cmd {
	c := v.client
	name := v.name
	ns := v.namespace
	return func() tea.Msg {
		ctx := context.Background()

		tc, err := c.GetTenantCluster(ctx, ns, name)
		if err != nil {
			return clusterDetailMsg{err: fmt.Errorf("getting TenantCluster: %w", err)}
		}

		info := clusterhelpers.ExtractTenantClusterInfo(tc)
		clusterhelpers.EnrichWithMachineDeploymentStatus(ctx, c, &info)
		clusterhelpers.EnrichWithControlPlaneEndpoint(ctx, c, &info)

		// Fetch CAPI Machines
		machines := fetchMachines(ctx, c, info.TenantNamespace, name)

		// Fetch TenantAddons
		addons := fetchAddons(ctx, c, ns, name)

		// Fetch conditions from the TC status
		conditions := extractConditions(tc)

		// Fetch K8s events
		events := fetchEvents(ctx, c, ns, name)

		return clusterDetailMsg{
			info:       info,
			raw:        tc,
			machines:   machines,
			addons:     addons,
			conditions: conditions,
			events:     events,
		}
	}
}

func fetchMachines(ctx context.Context, c *client.Client, ns, clusterName string) []machineInfo {
	if ns == "" {
		return nil
	}
	list, err := c.Dynamic.Resource(client.MachineGVR).Namespace(ns).List(ctx, metav1.ListOptions{
		LabelSelector: fmt.Sprintf("cluster.x-k8s.io/cluster-name=%s", clusterName),
	})
	if err != nil {
		return nil
	}
	machines := make([]machineInfo, len(list.Items))
	for i, m := range list.Items {
		addr := ""
		addresses, found, _ := unstructured.NestedSlice(m.Object, "status", "addresses")
		if found && len(addresses) > 0 {
			if a, ok := addresses[0].(map[string]interface{}); ok {
				addr = client.GetNestedString(a, "address")
			}
		}
		role := "worker"
		labels := m.GetLabels()
		if _, ok := labels["cluster.x-k8s.io/control-plane"]; ok {
			role = "control-plane"
		}
		machines[i] = machineInfo{
			Name:    m.GetName(),
			Phase:   client.GetNestedString(m.Object, "status", "phase"),
			Address: addr,
			Role:    role,
		}
	}
	return machines
}

func fetchAddons(ctx context.Context, c *client.Client, ns, clusterName string) []addonInfo {
	list, err := c.Dynamic.Resource(client.TenantAddonGVR).Namespace(ns).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil
	}
	var addons []addonInfo
	for _, a := range list.Items {
		ref := client.GetNestedString(a.Object, "spec", "clusterRef", "name")
		if ref != clusterName {
			continue
		}
		addons = append(addons, addonInfo{
			Name:    a.GetName(),
			Phase:   client.GetNestedString(a.Object, "status", "phase"),
			Version: client.GetNestedString(a.Object, "spec", "version"),
		})
	}
	return addons
}

func extractConditions(tc *unstructured.Unstructured) []conditionInfo {
	conditions, found, _ := unstructured.NestedSlice(tc.Object, "status", "conditions")
	if !found {
		return nil
	}
	result := make([]conditionInfo, 0, len(conditions))
	for _, c := range conditions {
		cond, ok := c.(map[string]interface{})
		if !ok {
			continue
		}
		result = append(result, conditionInfo{
			Type:    client.GetNestedString(cond, "type"),
			Status:  client.GetNestedString(cond, "status"),
			Reason:  client.GetNestedString(cond, "reason"),
			Message: client.GetNestedString(cond, "message"),
		})
	}
	return result
}

func fetchEvents(ctx context.Context, c *client.Client, ns, name string) []eventInfo {
	events, err := c.Clientset.CoreV1().Events(ns).List(ctx, metav1.ListOptions{
		FieldSelector: fmt.Sprintf("involvedObject.name=%s", name),
	})
	if err != nil {
		return nil
	}
	result := make([]eventInfo, 0, len(events.Items))
	for _, e := range events.Items {
		age := ""
		if !e.LastTimestamp.IsZero() {
			age = output.FormatAge(e.LastTimestamp.Time)
		} else if !e.EventTime.IsZero() {
			age = output.FormatAge(e.EventTime.Time)
		}
		result = append(result, eventInfo{
			Type:    e.Type,
			Reason:  e.Reason,
			Message: e.Message,
			Age:     age,
		})
	}
	return result
}

func valueOr(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
