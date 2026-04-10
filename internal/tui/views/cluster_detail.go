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
	"encoding/json"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"

	"github.com/butlerdotdev/butler/internal/common/client"
	"github.com/butlerdotdev/butler/internal/common/output"
	clusterhelpers "github.com/butlerdotdev/butler/internal/ctl/cluster"
	"github.com/butlerdotdev/butler/internal/tui/components"
	"github.com/butlerdotdev/butler/internal/tui/styles"
)

// Tab indices for the cluster detail view.
const (
	tabOverview = iota
	tabNodes
	tabAddons
	tabConditions
	tabEvents
)

var tabNames = []string{"Overview", "Nodes", "Addons", "Conditions", "Events"}

// tabCount derives from tabNames so adding a tab doesn't require updating a constant.
var tabCount = len(tabNames)

// actionMode tracks the current interaction state for inline actions.
type actionMode int

const (
	modeNormal  actionMode = iota
	modeInput              // text input active (scale, upgrade, install addon)
	modeConfirm            // y/n confirmation (delete, uninstall)
	modeResult             // success/error message displayed
)

// pendingAction identifies which action is being executed.
const (
	actionNone             = ""
	actionScale            = "scale"
	actionUpgrade          = "upgrade"
	actionDelete           = "delete"
	actionInstallAddon     = "install-addon"
	actionUninstallAddon   = "uninstall-addon"
)

// clusterDetailMsg carries fetched detail data.
type clusterDetailMsg struct {
	info       clusterhelpers.TenantClusterInfo
	machines   []machineInfo
	addons     []addonInfo
	conditions []conditionInfo
	events     []eventInfo
	err        error
}

// clusterActionResultMsg carries the result of an async action.
type clusterActionResultMsg struct {
	err     error
	deleted bool // true when the cluster was deleted
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

	// Action state
	mode          actionMode
	pendingAction string
	input         textinput.Model
	confirmMsg    string
	resultMsg     string
	resultIsError bool
	Deleted       bool // set when the cluster was deleted; app navigates back

	width  int
	height int
}

// NewClusterDetailView creates a detail view for a named cluster.
func NewClusterDetailView(c *client.Client, name, namespace string) ClusterDetailView {
	ti := textinput.New()
	ti.CharLimit = 64
	return ClusterDetailView{
		client:         c,
		name:           name,
		namespace:      namespace,
		loading:        true,
		input:          ti,
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

// IsInActionMode returns true when an action prompt is active and should
// consume all key input (preventing global navigation).
func (v *ClusterDetailView) IsInActionMode() bool {
	return v.mode != modeNormal
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

	case clusterActionResultMsg:
		v.loading = false
		if msg.err != nil {
			v.mode = modeResult
			v.resultMsg = msg.err.Error()
			v.resultIsError = true
		} else if msg.deleted {
			v.Deleted = true
			return v, nil
		} else {
			v.mode = modeResult
			v.resultMsg = v.actionSuccessMessage()
			v.resultIsError = false
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
		// Route based on current action mode
		switch v.mode {
		case modeInput:
			return v.updateInputMode(msg)
		case modeConfirm:
			return v.updateConfirmMode(msg)
		case modeResult:
			return v.updateResultMode(msg)
		default:
			return v.updateNormalMode(msg)
		}
	}
	return v, nil
}

// updateNormalMode handles keys when no action is active.
func (v ClusterDetailView) updateNormalMode(msg tea.KeyMsg) (ClusterDetailView, tea.Cmd) {
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
	case "s":
		v.startInputAction(actionScale, "New worker count: ")
		return v, nil
	case "u":
		v.startInputAction(actionUpgrade, "New Kubernetes version: ")
		return v, nil
	case "d":
		v.mode = modeConfirm
		v.pendingAction = actionDelete
		v.confirmMsg = fmt.Sprintf("Delete cluster %s? (y/n)", v.name)
		return v, nil
	case "a":
		if v.activeTab == tabAddons {
			v.startInputAction(actionInstallAddon, "Addon name to install: ")
			return v, nil
		}
	case "x":
		if v.activeTab == tabAddons {
			row := v.addonTable.SelectedRow()
			if row != nil && len(row) > 0 {
				v.mode = modeConfirm
				v.pendingAction = actionUninstallAddon
				v.confirmMsg = fmt.Sprintf("Uninstall addon %s? (y/n)", row[0])
				return v, nil
			}
		}
	}

	return v.updateActiveTable(msg)
}

// startInputAction sets up the text input for an action.
func (v *ClusterDetailView) startInputAction(action, placeholder string) {
	v.mode = modeInput
	v.pendingAction = action
	v.input.Reset()
	v.input.Placeholder = placeholder
	v.input.Focus()
}

// updateInputMode handles keys when a text input is active.
func (v ClusterDetailView) updateInputMode(msg tea.KeyMsg) (ClusterDetailView, tea.Cmd) {
	switch msg.String() {
	case "esc":
		v.mode = modeNormal
		v.pendingAction = actionNone
		v.input.Blur()
		return v, nil
	case "enter":
		value := strings.TrimSpace(v.input.Value())
		if value == "" {
			// Empty input, cancel
			v.mode = modeNormal
			v.pendingAction = actionNone
			v.input.Blur()
			return v, nil
		}
		v.input.Blur()
		v.loading = true
		cmd := v.executeAction(value)
		return v, cmd
	default:
		var cmd tea.Cmd
		v.input, cmd = v.input.Update(msg)
		return v, cmd
	}
}

// updateConfirmMode handles keys when a y/n prompt is active.
func (v ClusterDetailView) updateConfirmMode(msg tea.KeyMsg) (ClusterDetailView, tea.Cmd) {
	switch msg.String() {
	case "y", "Y":
		v.loading = true
		cmd := v.executeAction("")
		return v, cmd
	case "n", "N", "esc":
		v.mode = modeNormal
		v.pendingAction = actionNone
		return v, nil
	}
	return v, nil
}

// updateResultMode handles keys when showing a result message.
func (v ClusterDetailView) updateResultMode(msg tea.KeyMsg) (ClusterDetailView, tea.Cmd) {
	// Any key clears the result and refreshes the view
	v.mode = modeNormal
	v.pendingAction = actionNone
	v.resultMsg = ""
	v.loading = true
	return v, v.fetchDetail()
}

// executeAction runs the pending action asynchronously.
//
// The returned closure MUST NOT capture the receiver pointer v — the App
// model is replaced by value on each Update, so by the time the Bubbletea
// runtime dispatches this Cmd, v would point at a stale heap-escaped
// snapshot. Capture all inputs as locals and call package-level do* helpers.
func (v *ClusterDetailView) executeAction(inputValue string) tea.Cmd {
	c := v.client
	name := v.name
	ns := v.namespace
	action := v.pendingAction

	switch action {
	case actionScale:
		return func() tea.Msg {
			return doScale(c, ns, name, inputValue)
		}
	case actionUpgrade:
		return func() tea.Msg {
			return doUpgrade(c, ns, name, inputValue)
		}
	case actionDelete:
		return func() tea.Msg {
			return doDelete(c, ns, name)
		}
	case actionInstallAddon:
		return func() tea.Msg {
			return doInstallAddon(c, ns, name, inputValue)
		}
	case actionUninstallAddon:
		addonName := ""
		row := v.addonTable.SelectedRow()
		if row != nil && len(row) > 0 {
			addonName = row[0]
		}
		return func() tea.Msg {
			return doUninstallAddon(c, ns, addonName)
		}
	}

	return nil
}

// doScale patches spec.workers.replicas.
func doScale(c *client.Client, ns, name, countStr string) clusterActionResultMsg {
	ctx, cancel := apiContext()
	defer cancel()

	var count int
	if _, err := fmt.Sscanf(countStr, "%d", &count); err != nil || count < 1 {
		return clusterActionResultMsg{err: fmt.Errorf("invalid worker count: %s (must be a positive integer)", countStr)}
	}

	patch := map[string]interface{}{
		"spec": map[string]interface{}{
			"workers": map[string]interface{}{
				"replicas": int64(count),
			},
		},
	}
	patchBytes, err := json.Marshal(patch)
	if err != nil {
		return clusterActionResultMsg{err: fmt.Errorf("marshaling patch: %w", err)}
	}

	_, err = c.Dynamic.Resource(client.TenantClusterGVR).Namespace(ns).Patch(
		ctx, name, types.MergePatchType, patchBytes, metav1.PatchOptions{},
	)
	if err != nil {
		return clusterActionResultMsg{err: fmt.Errorf("scaling cluster: %w", err)}
	}

	return clusterActionResultMsg{}
}

// doUpgrade patches spec.kubernetesVersion.
func doUpgrade(c *client.Client, ns, name, version string) clusterActionResultMsg {
	ctx, cancel := apiContext()
	defer cancel()

	patch := map[string]interface{}{
		"spec": map[string]interface{}{
			"kubernetesVersion": version,
		},
	}
	patchBytes, err := json.Marshal(patch)
	if err != nil {
		return clusterActionResultMsg{err: fmt.Errorf("marshaling patch: %w", err)}
	}

	_, err = c.Dynamic.Resource(client.TenantClusterGVR).Namespace(ns).Patch(
		ctx, name, types.MergePatchType, patchBytes, metav1.PatchOptions{},
	)
	if err != nil {
		return clusterActionResultMsg{err: fmt.Errorf("upgrading cluster: %w", err)}
	}

	return clusterActionResultMsg{}
}

// doDelete deletes the TenantCluster.
func doDelete(c *client.Client, ns, name string) clusterActionResultMsg {
	ctx, cancel := apiContext()
	defer cancel()

	err := c.Dynamic.Resource(client.TenantClusterGVR).Namespace(ns).Delete(
		ctx, name, metav1.DeleteOptions{},
	)
	if err != nil {
		return clusterActionResultMsg{err: fmt.Errorf("deleting cluster: %w", err)}
	}

	return clusterActionResultMsg{deleted: true}
}

// doInstallAddon creates a TenantAddon resource.
func doInstallAddon(c *client.Client, ns, clusterName, addonName string) clusterActionResultMsg {
	ctx, cancel := apiContext()
	defer cancel()

	// Resolve version from AddonDefinition catalog
	version := ""
	ad, err := c.Dynamic.Resource(client.AddonDefinitionGVR).Get(ctx, addonName, metav1.GetOptions{})
	if err == nil {
		version = client.GetNestedString(ad.Object, "spec", "chart", "defaultVersion")
	}

	ta := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "butler.butlerlabs.dev/v1alpha1",
			"kind":       "TenantAddon",
			"metadata": map[string]interface{}{
				"name":      addonName,
				"namespace": ns,
			},
			"spec": map[string]interface{}{
				"clusterRef": map[string]interface{}{
					"name": clusterName,
				},
				"addon":   addonName,
				"version": version,
			},
		},
	}

	_, err = c.Dynamic.Resource(client.TenantAddonGVR).Namespace(ns).Create(ctx, ta, metav1.CreateOptions{})
	if err != nil {
		return clusterActionResultMsg{err: fmt.Errorf("installing addon %s: %w", addonName, err)}
	}

	return clusterActionResultMsg{}
}

// doUninstallAddon deletes a TenantAddon resource.
func doUninstallAddon(c *client.Client, ns, addonName string) clusterActionResultMsg {
	ctx, cancel := apiContext()
	defer cancel()

	err := c.Dynamic.Resource(client.TenantAddonGVR).Namespace(ns).Delete(ctx, addonName, metav1.DeleteOptions{})
	if err != nil {
		return clusterActionResultMsg{err: fmt.Errorf("uninstalling addon %s: %w", addonName, err)}
	}

	return clusterActionResultMsg{}
}

// actionSuccessMessage returns a human-readable success message for the current action.
func (v *ClusterDetailView) actionSuccessMessage() string {
	switch v.pendingAction {
	case actionScale:
		return fmt.Sprintf("Scale initiated for %s. Press any key to refresh.", v.name)
	case actionUpgrade:
		return fmt.Sprintf("Upgrade initiated for %s. Press any key to refresh.", v.name)
	case actionDelete:
		return fmt.Sprintf("Cluster %s deletion initiated.", v.name)
	case actionInstallAddon:
		return fmt.Sprintf("Addon install initiated. Press any key to refresh.")
	case actionUninstallAddon:
		return fmt.Sprintf("Addon uninstall initiated. Press any key to refresh.")
	}
	return "Action completed. Press any key to continue."
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

// KeyLegend returns action keys available in the current state, for the app
// to include in the bottom key legend bar.
func (v *ClusterDetailView) KeyLegend() string {
	dimStyle := styles.DimStyle
	keyStyle := styles.KeyLegendStyle

	base := dimStyle.Render("  ") +
		keyStyle.Render("tab") + dimStyle.Render(":switch tab  ") +
		keyStyle.Render("esc") + dimStyle.Render(":back  ") +
		keyStyle.Render("r") + dimStyle.Render(":refresh  ")

	actions := keyStyle.Render("s") + dimStyle.Render(":scale  ") +
		keyStyle.Render("u") + dimStyle.Render(":upgrade  ") +
		keyStyle.Render("d") + dimStyle.Render(":delete  ")

	if v.activeTab == tabAddons {
		actions += keyStyle.Render("a") + dimStyle.Render(":install addon  ") +
			keyStyle.Render("x") + dimStyle.Render(":uninstall  ")
	}

	tail := keyStyle.Render("/") + dimStyle.Render(":filter  ") +
		keyStyle.Render("?") + dimStyle.Render(":help  ") +
		keyStyle.Render("q") + dimStyle.Render(":quit")

	return base + actions + tail
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

	// Action prompt/result overlay at the bottom of content
	if prompt := v.renderActionPrompt(); prompt != "" {
		b.WriteString("\n")
		b.WriteString(prompt)
	}

	return b.String()
}

// renderActionPrompt renders the inline prompt for the current action mode.
func (v ClusterDetailView) renderActionPrompt() string {
	switch v.mode {
	case modeInput:
		return styles.ActionPromptStyle.Render(v.input.Placeholder) + v.input.View()
	case modeConfirm:
		return styles.ActionConfirmStyle.Render(v.confirmMsg)
	case modeResult:
		if v.resultIsError {
			return styles.ActionErrorStyle.Render("Error: " + v.resultMsg)
		}
		return styles.ActionSuccessStyle.Render(v.resultMsg)
	}
	return ""
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
		ctx, cancel := apiContext()
		defer cancel()

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
	// Scope strictly to events involving this TenantCluster — name alone
	// would leak events from any Pod/ConfigMap/etc. that happens to share
	// the name in this namespace.
	events, err := c.Clientset.CoreV1().Events(ns).List(ctx, metav1.ListOptions{
		FieldSelector: fmt.Sprintf("involvedObject.name=%s,involvedObject.kind=TenantCluster", name),
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
