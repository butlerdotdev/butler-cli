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
	"fmt"
	"sort"

	tea "github.com/charmbracelet/bubbletea"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/butlerdotdev/butler/internal/common/client"
	"github.com/butlerdotdev/butler/internal/common/output"
	"github.com/butlerdotdev/butler/internal/ctl/cluster"
	"github.com/butlerdotdev/butler/internal/tui/components"
)

// clusterListMsg is sent when cluster data has been fetched.
type clusterListMsg struct {
	clusters []cluster.TenantClusterInfo
	err      error
}

// ClusterListView displays a table of TenantClusters.
type ClusterListView struct {
	client   *client.Client
	table    components.Table
	clusters []cluster.TenantClusterInfo
	loading  bool
	err      error
	width    int
	height   int
}

// NewClusterListView creates the cluster list view.
func NewClusterListView(c *client.Client) ClusterListView {
	return ClusterListView{
		client:  c,
		table:   components.NewTable([]string{"NAME", "NAMESPACE", "PHASE", "K8S VERSION", "WORKERS", "PROVIDER", "AGE"}),
		loading: true,
	}
}

// Init fetches cluster data on startup.
func (v ClusterListView) Init() tea.Cmd {
	return v.fetchClusters()
}

// Update handles messages and key events.
func (v ClusterListView) Update(msg tea.Msg) (ClusterListView, tea.Cmd) {
	switch msg := msg.(type) {
	case clusterListMsg:
		v.loading = false
		v.err = msg.err
		v.clusters = msg.clusters
		v.rebuildRows()
		return v, nil

	case tea.WindowSizeMsg:
		v.width = msg.Width
		v.height = msg.Height
		v.table.SetSize(msg.Width, msg.Height-4)
		return v, nil

	case tea.KeyMsg:
		if v.table.Filtering() {
			cmd := v.table.Update(msg)
			return v, cmd
		}
		switch msg.String() {
		case "r":
			v.loading = true
			return v, v.fetchClusters()
		default:
			cmd := v.table.Update(msg)
			return v, cmd
		}
	}
	return v, nil
}

// IsFiltering returns true if the table is in filter mode.
func (v *ClusterListView) IsFiltering() bool {
	return v.table.Filtering()
}

// SelectedCluster returns the name and namespace of the currently selected cluster.
func (v *ClusterListView) SelectedCluster() (name, namespace string) {
	row := v.table.SelectedRow()
	if row == nil || len(row) < 2 {
		return "", ""
	}
	return row[0], row[1]
}

// View renders the cluster list.
func (v ClusterListView) View() string {
	if v.loading {
		return "  Loading clusters..."
	}
	if v.err != nil {
		return fmt.Sprintf("  Error: %v", v.err)
	}
	return v.table.View()
}

func (v ClusterListView) fetchClusters() tea.Cmd {
	c := v.client
	return func() tea.Msg {
		ctx, cancel := apiContext()
		defer cancel()
		list, err := c.Dynamic.Resource(client.TenantClusterGVR).List(ctx, metav1.ListOptions{})
		if err != nil {
			return clusterListMsg{err: fmt.Errorf("listing TenantClusters: %w", err)}
		}

		items := list.Items
		sort.Slice(items, func(i, j int) bool {
			if items[i].GetNamespace() != items[j].GetNamespace() {
				return items[i].GetNamespace() < items[j].GetNamespace()
			}
			return items[i].GetName() < items[j].GetName()
		})

		infos := make([]cluster.TenantClusterInfo, len(items))
		for i := range items {
			infos[i] = cluster.ExtractTenantClusterInfo(&items[i])
			cluster.EnrichWithMachineDeploymentStatus(ctx, c, &infos[i])
			cluster.EnrichWithControlPlaneEndpoint(ctx, c, &infos[i])
		}
		return clusterListMsg{clusters: infos}
	}
}

func (v *ClusterListView) rebuildRows() {
	rows := make([][]string, len(v.clusters))
	for i, tc := range v.clusters {
		workers := output.FormatWorkers(tc.WorkersReady, tc.WorkersDesired)
		age := formatAge(tc.CreationTime)
		provider := tc.ProviderConfig
		if provider == "" {
			provider = "-"
		}
		rows[i] = []string{
			tc.Name,
			tc.Namespace,
			tc.Phase,
			tc.KubernetesVersion,
			workers,
			provider,
			age,
		}
	}
	v.table.SetRows(rows)
}
