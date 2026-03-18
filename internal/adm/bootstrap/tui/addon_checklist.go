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
)

// addonDisplayOrder defines the display order and friendly names for addons.
var addonDisplayOrder = []struct {
	key  string
	name string
}{
	{"kube-vip", "kube-vip"},
	{"cilium", "cilium"},
	{"cloud-controller-manager", "ccm"},
	{"cert-manager", "cert-manager"},
	{"longhorn", "longhorn"},
	{"metallb", "metallb"},
	{"traefik", "traefik"},
	{"gateway-api", "gateway-api"},
	{"steward", "steward"},
	{"capi", "capi"},
	{"flux", "flux"},
	{"butler-crds", "butler-crds"},
	{"provider-config", "provider"},
	{"butler-addons", "addons"},
	{"butler-controller", "controller"},
	{"butler", "butler"},
	{"butler-config-exposure", "exposure"},
	{"butler-console", "console"},
}

// addonChecklistModel displays addon installation progress as a compact grid.
type addonChecklistModel struct {
	installed map[string]bool
	width     int
}

func newAddonChecklistModel() addonChecklistModel {
	return addonChecklistModel{
		installed: make(map[string]bool),
		width:     80,
	}
}

// SetAddons updates the installed status map.
func (m *addonChecklistModel) SetAddons(installed map[string]bool) {
	m.installed = installed
}

// SetWidth sets the available render width.
func (m *addonChecklistModel) SetWidth(w int) {
	m.width = w
}

func (m addonChecklistModel) View() string {
	if len(m.installed) == 0 {
		return phasePending.Render("  Waiting for addon installation...")
	}

	// Render addons as a compact grid
	colWidth := 20
	cols := m.width / colWidth
	if cols < 1 {
		cols = 1
	}

	var b strings.Builder
	col := 0
	for _, addon := range addonDisplayOrder {
		installed, exists := m.installed[addon.key]
		if !exists {
			continue
		}

		var icon, name string
		if installed {
			icon = addonInstalled.Render(iconCompleted)
			name = addonInstalled.Render(addon.name)
		} else {
			icon = addonPending.Render(iconPending)
			name = addonPending.Render(addon.name)
		}

		entry := fmt.Sprintf("%s %s", icon, name)
		// Pad to column width
		padded := fmt.Sprintf("  %-*s", colWidth-2, entry)
		b.WriteString(padded)

		col++
		if col >= cols {
			b.WriteString("\n")
			col = 0
		}
	}
	if col > 0 {
		b.WriteString("\n")
	}

	return b.String()
}

// summary returns counts like "12/18 installed".
func (m addonChecklistModel) summary() string {
	if len(m.installed) == 0 {
		return ""
	}
	total := 0
	done := 0
	for _, v := range m.installed {
		total++
		if v {
			done++
		}
	}
	return fmt.Sprintf("%d/%d", done, total)
}
