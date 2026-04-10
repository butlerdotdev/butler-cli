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

// Package tui implements the Butler interactive terminal dashboard.
package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/butlerdotdev/butler/internal/common/auth"
	"github.com/butlerdotdev/butler/internal/common/client"
	"github.com/butlerdotdev/butler/internal/tui/components"
	"github.com/butlerdotdev/butler/internal/tui/styles"
	"github.com/butlerdotdev/butler/internal/tui/views"
)

// ViewType identifies which view is currently displayed.
type ViewType int

const (
	ViewClusters ViewType = iota
	ViewClusterDetail
	ViewAddons
	ViewTeams
	ViewProviders
	ViewNetworks
	ViewUsers
	ViewHealth
	ViewHelp
)

var viewNames = map[ViewType]string{
	ViewClusters:      "Clusters",
	ViewClusterDetail: "Cluster Detail",
	ViewAddons:        "Addon Catalog",
	ViewTeams:         "Teams",
	ViewProviders:     "Providers",
	ViewNetworks:      "Networks",
	ViewUsers:         "Users",
	ViewHealth:        "Health",
	ViewHelp:          "Help",
}

// App is the root Bubbletea model for the Butler dashboard.
type App struct {
	client    *client.Client
	view      ViewType
	prevView  ViewType
	keys      KeyMap
	width     int
	height    int
	context        string // kubeconfig context name for status bar
	isAdmin        bool   // controls whether admin-only views are shown
	unauthenticated bool  // true when using raw kubeconfig without Butler auth

	// Views
	clusterList   views.ClusterListView
	clusterDetail views.ClusterDetailView
	addonCatalog  views.AddonCatalogView
	teamList      views.TeamListView
	providerList  views.ProviderListView
	networkList   views.NetworkListView
	userList      views.UserListView
	healthView    views.HealthView
	helpView      views.HelpView

	// Track which views have been initialized
	initialized map[ViewType]bool
}

// NewApp creates a new dashboard application.
func NewApp(c *client.Client, contextName string, admin bool) *App {
	// Check if Butler credentials are active
	unauth := false
	if admin {
		creds, err := auth.LoadCredentials()
		if err != nil || creds.ActiveCredential() == nil {
			unauth = true
		}
	}

	return &App{
		client:          c,
		view:            ViewClusters,
		keys:            DefaultKeyMap(),
		context:         contextName,
		isAdmin:         admin,
		unauthenticated: unauth,
		clusterList:     views.NewClusterListView(c),
		helpView:        views.HelpView{Admin: admin},
		initialized:     map[ViewType]bool{},
	}
}

// Init initializes the first view.
func (a App) Init() tea.Cmd {
	a.initialized[ViewClusters] = true
	return a.clusterList.Init()
}

// Update handles incoming messages and dispatches to the active view.
func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		a.width = msg.Width
		a.height = msg.Height
		a.helpView.Width = msg.Width
		a.helpView.Height = msg.Height
		// Forward to all initialized views
		return a, a.forwardToAll(msg)

	case tea.KeyMsg:
		// If the active view is filtering or in an action mode, let it
		// handle everything — do not intercept for global navigation.
		if a.isActiveFiltering() || a.isActiveInActionMode() {
			return a.forwardToActive(msg)
		}

		// Global navigation
		switch {
		case key.Matches(msg, a.keys.Quit):
			return a, tea.Quit

		case key.Matches(msg, a.keys.Help):
			if a.view == ViewHelp {
				a.view = a.prevView
			} else {
				a.prevView = a.view
				a.view = ViewHelp
			}
			return a, nil

		case key.Matches(msg, a.keys.Back):
			if a.view == ViewClusterDetail {
				a.view = ViewClusters
				return a, nil
			}
			if a.view == ViewHelp {
				a.view = a.prevView
				return a, nil
			}

		case key.Matches(msg, a.keys.Enter):
			if a.view == ViewClusters {
				return a.drillIntoCluster()
			}

		case key.Matches(msg, a.keys.ViewOne):
			return a.switchView(ViewClusters)
		case key.Matches(msg, a.keys.ViewTwo):
			return a.switchView(ViewAddons)
		case key.Matches(msg, a.keys.ViewThree):
			return a.switchView(ViewTeams)
		case key.Matches(msg, a.keys.ViewFour):
			return a.switchView(ViewProviders)
		case key.Matches(msg, a.keys.ViewFive):
			return a.switchView(ViewNetworks)
		case key.Matches(msg, a.keys.ViewSix):
			return a.switchView(ViewUsers)
		case key.Matches(msg, a.keys.ViewSeven):
			return a.switchView(ViewHealth)
		case key.Matches(msg, a.keys.ViewEight):
			if a.view == ViewHelp {
				a.view = a.prevView
			} else {
				a.prevView = a.view
				a.view = ViewHelp
			}
			return a, nil
		}

		// Forward to active view
		return a.forwardToActive(msg)
	}

	// Forward data messages to all views (they'll ignore irrelevant ones)
	return a.forwardToActive(msg)
}

// View renders the full dashboard layout.
func (a App) View() string {
	var b strings.Builder

	// Header
	header := styles.HeaderStyle.Render(" Butler Dashboard ")
	tabs := a.renderTabs()
	b.WriteString(header + "  " + tabs)
	b.WriteString("\n")

	// Breadcrumb (only for detail views)
	if a.view == ViewClusterDetail {
		bc := components.Breadcrumb{Parts: []string{"Clusters", a.clusterDetail.Name()}}
		b.WriteString(bc.View())
	}
	b.WriteString("\n")

	// Main content
	extraLines := 2 // key legend + status bar
	if a.unauthenticated {
		extraLines++ // auth warning banner
	}
	contentHeight := a.height - 3 - extraLines // header + breadcrumb + newline + bottom
	content := a.renderActiveView()
	b.WriteString(content)

	// Pad to fill remaining space
	contentLines := strings.Count(content, "\n") + 1
	for i := contentLines; i < contentHeight; i++ {
		b.WriteString("\n")
	}

	// Status bar
	// Auth warning banner
	if a.unauthenticated {
		warn := styles.WarningBannerStyle.Render(" Warning: unauthenticated. Run 'butleradm login' for scoped access. ")
		b.WriteString(warn)
		b.WriteString("\n")
	}

	// Key legend
	legend := a.renderKeyLegend()
	b.WriteString(legend)
	b.WriteString("\n")

	// Status bar
	sb := components.StatusBar{
		ViewName: viewNames[a.view],
		Context:  a.context,
		Width:    a.width,
	}
	b.WriteString(sb.View())

	return b.String()
}

func (a App) renderKeyLegend() string {
	dimStyle := styles.DimStyle
	keyStyle := styles.KeyLegendStyle

	switch a.view {
	case ViewClusterDetail:
		return a.clusterDetail.KeyLegend()
	case ViewTeams:
		return a.teamList.KeyLegend()
	case ViewUsers:
		return a.userList.KeyLegend()
	case ViewProviders:
		return a.providerList.KeyLegend()
	case ViewNetworks:
		return a.networkList.KeyLegend()
	default:
		return dimStyle.Render("  ") +
			keyStyle.Render("j/k") + dimStyle.Render(":navigate  ") +
			keyStyle.Render("enter") + dimStyle.Render(":select  ") +
			keyStyle.Render("/") + dimStyle.Render(":filter  ") +
			keyStyle.Render("r") + dimStyle.Render(":refresh  ") +
			keyStyle.Render("?") + dimStyle.Render(":help  ") +
			keyStyle.Render("q") + dimStyle.Render(":quit")
	}
}

func (a App) renderTabs() string {
	tabs := []struct {
		key  string
		name string
		view ViewType
	}{
		{"1", "Clusters", ViewClusters},
		{"2", "Addons", ViewAddons},
		{"3", "Teams", ViewTeams},
		{"4", "Providers", ViewProviders},
		{"5", "Networks", ViewNetworks},
		{"6", "Users", ViewUsers},
		{"7", "Health", ViewHealth},
	}

	parts := make([]string, len(tabs))
	for i, t := range tabs {
		label := fmt.Sprintf("%s:%s", t.key, t.name)
		if a.view == t.view || (a.view == ViewClusterDetail && t.view == ViewClusters) {
			parts[i] = styles.ActiveTabStyle.Render(label)
		} else {
			parts[i] = styles.TabStyle.Render(label)
		}
	}
	return strings.Join(parts, "")
}

func (a App) renderActiveView() string {
	switch a.view {
	case ViewClusters:
		return a.clusterList.View()
	case ViewClusterDetail:
		return a.clusterDetail.View()
	case ViewAddons:
		return a.addonCatalog.View()
	case ViewTeams:
		return a.teamList.View()
	case ViewProviders:
		return a.providerList.View()
	case ViewNetworks:
		return a.networkList.View()
	case ViewUsers:
		return a.userList.View()
	case ViewHealth:
		return a.healthView.View()
	case ViewHelp:
		return a.helpView.View()
	default:
		return ""
	}
}

func (a *App) switchView(v ViewType) (tea.Model, tea.Cmd) {
	a.view = v
	if !a.initialized[v] {
		a.initialized[v] = true
		cmd := a.initView(v)
		// Send a WindowSizeMsg to initialize dimensions
		sizeCmd := func() tea.Msg {
			return tea.WindowSizeMsg{Width: a.width, Height: a.height}
		}
		return a, tea.Batch(cmd, sizeCmd)
	}
	return a, nil
}

func (a *App) initView(v ViewType) tea.Cmd {
	switch v {
	case ViewClusters:
		a.clusterList = views.NewClusterListView(a.client)
		return a.clusterList.Init()
	case ViewAddons:
		a.addonCatalog = views.NewAddonCatalogView(a.client)
		return a.addonCatalog.Init()
	case ViewTeams:
		a.teamList = views.NewTeamListView(a.client, a.isAdmin)
		return a.teamList.Init()
	case ViewProviders:
		a.providerList = views.NewProviderListView(a.client, a.isAdmin)
		return a.providerList.Init()
	case ViewNetworks:
		a.networkList = views.NewNetworkListView(a.client, a.isAdmin)
		return a.networkList.Init()
	case ViewUsers:
		a.userList = views.NewUserListView(a.client, a.isAdmin)
		return a.userList.Init()
	case ViewHealth:
		a.healthView = views.NewHealthView(a.client)
		return a.healthView.Init()
	}
	return nil
}

func (a *App) drillIntoCluster() (tea.Model, tea.Cmd) {
	name, ns := a.clusterList.SelectedCluster()
	if name == "" {
		return a, nil
	}
	a.clusterDetail = views.NewClusterDetailView(a.client, name, ns)
	a.view = ViewClusterDetail
	a.initialized[ViewClusterDetail] = true
	cmd := a.clusterDetail.Init()
	sizeCmd := func() tea.Msg {
		return tea.WindowSizeMsg{Width: a.width, Height: a.height}
	}
	return a, tea.Batch(cmd, sizeCmd)
}

func (a *App) isActiveFiltering() bool {
	switch a.view {
	case ViewClusters:
		return a.clusterList.IsFiltering()
	case ViewClusterDetail:
		return a.clusterDetail.IsFiltering()
	case ViewTeams:
		return a.teamList.IsFiltering()
	case ViewProviders:
		return a.providerList.IsFiltering()
	case ViewNetworks:
		return a.networkList.IsFiltering()
	case ViewUsers:
		return a.userList.IsFiltering()
	case ViewAddons:
		return a.addonCatalog.IsFiltering()
	}
	return false
}

func (a *App) isActiveInActionMode() bool {
	switch a.view {
	case ViewClusterDetail:
		return a.clusterDetail.IsInActionMode()
	case ViewTeams:
		return a.teamList.IsInActionMode()
	case ViewUsers:
		return a.userList.IsInActionMode()
	case ViewProviders:
		return a.providerList.IsInActionMode()
	case ViewNetworks:
		return a.networkList.IsInActionMode()
	}
	return false
}

func (a *App) forwardToActive(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch a.view {
	case ViewClusters:
		a.clusterList, cmd = a.clusterList.Update(msg)
	case ViewClusterDetail:
		a.clusterDetail, cmd = a.clusterDetail.Update(msg)
		// Navigate back to cluster list if the cluster was deleted
		if a.clusterDetail.Deleted {
			a.view = ViewClusters
			a.clusterList = views.NewClusterListView(a.client)
			a.initialized[ViewClusters] = true
			initCmd := a.clusterList.Init()
			sizeCmd := func() tea.Msg {
				return tea.WindowSizeMsg{Width: a.width, Height: a.height}
			}
			return a, tea.Batch(initCmd, sizeCmd)
		}
	case ViewAddons:
		a.addonCatalog, cmd = a.addonCatalog.Update(msg)
	case ViewTeams:
		a.teamList, cmd = a.teamList.Update(msg)
	case ViewProviders:
		a.providerList, cmd = a.providerList.Update(msg)
	case ViewNetworks:
		a.networkList, cmd = a.networkList.Update(msg)
	case ViewUsers:
		a.userList, cmd = a.userList.Update(msg)
	case ViewHealth:
		a.healthView, cmd = a.healthView.Update(msg)
	}
	return a, cmd
}

func (a *App) forwardToAll(msg tea.Msg) tea.Cmd {
	var cmds []tea.Cmd
	var cmd tea.Cmd

	if a.initialized[ViewClusters] {
		a.clusterList, cmd = a.clusterList.Update(msg)
		cmds = append(cmds, cmd)
	}
	if a.initialized[ViewClusterDetail] {
		a.clusterDetail, cmd = a.clusterDetail.Update(msg)
		cmds = append(cmds, cmd)
	}
	if a.initialized[ViewAddons] {
		a.addonCatalog, cmd = a.addonCatalog.Update(msg)
		cmds = append(cmds, cmd)
	}
	if a.initialized[ViewTeams] {
		a.teamList, cmd = a.teamList.Update(msg)
		cmds = append(cmds, cmd)
	}
	if a.initialized[ViewProviders] {
		a.providerList, cmd = a.providerList.Update(msg)
		cmds = append(cmds, cmd)
	}
	if a.initialized[ViewNetworks] {
		a.networkList, cmd = a.networkList.Update(msg)
		cmds = append(cmds, cmd)
	}
	if a.initialized[ViewUsers] {
		a.userList, cmd = a.userList.Update(msg)
		cmds = append(cmds, cmd)
	}
	if a.initialized[ViewHealth] {
		a.healthView, cmd = a.healthView.Update(msg)
		cmds = append(cmds, cmd)
	}

	return tea.Batch(cmds...)
}
