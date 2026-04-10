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

package components

import (
	"fmt"
	"strings"

	"github.com/butlerdotdev/butler/internal/tui/styles"
)

// DetailField is a single label/value pair in a detail pane.
type DetailField struct {
	Label string
	Value string
}

// DetailSection groups fields under an optional heading.
type DetailSection struct {
	Title  string
	Fields []DetailField
}

// DetailPane renders key-value detail sections.
type DetailPane struct {
	Sections []DetailSection
}

// View renders the detail pane.
func (d DetailPane) View() string {
	var b strings.Builder

	for _, sec := range d.Sections {
		if sec.Title != "" {
			b.WriteString(styles.SectionStyle.Render(sec.Title))
			b.WriteString("\n")
		}
		for _, f := range sec.Fields {
			label := styles.LabelStyle.Render(f.Label + ":")
			value := colorizeValue(f.Value)
			b.WriteString(fmt.Sprintf("%s %s\n", label, value))
		}
	}

	return b.String()
}

// colorizeValue applies phase-like coloring to known values.
func colorizeValue(v string) string {
	switch v {
	case "Ready":
		return styles.BadgeReady.Render(v)
	case "Provisioning", "Installing", "Updating":
		return styles.BadgeProvisioning.Render(v)
	case "Failed":
		return styles.BadgeFailed.Render(v)
	case "true", "True", "Yes":
		return styles.BadgeReady.Render(v)
	case "false", "False", "No":
		return styles.HelpStyle.Render(v)
	default:
		return styles.ValueStyle.Render(v)
	}
}
