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
	"strings"

	"github.com/butlerdotdev/butler/internal/tui/styles"
)

// HelpView displays key bindings and usage instructions.
type HelpView struct {
	Width  int
	Height int
	Admin  bool
}

// View renders the help overlay.
func (v HelpView) View() string {
	var b strings.Builder

	b.WriteString(styles.SectionStyle.Render("Global"))
	b.WriteString("\n")
	for _, bind := range []struct{ key, desc string }{
		{"q / Ctrl+C", "Quit"},
		{"?", "Toggle help"},
		{"r", "Refresh current view"},
		{"1-8", "Switch views"},
	} {
		b.WriteString(renderBinding(bind.key, bind.desc))
	}

	b.WriteString("\n")
	b.WriteString(styles.SectionStyle.Render("List Navigation"))
	b.WriteString("\n")
	for _, bind := range []struct{ key, desc string }{
		{"j / Down", "Move cursor down"},
		{"k / Up", "Move cursor up"},
		{"g", "Go to top"},
		{"G", "Go to bottom"},
		{"/", "Filter rows"},
		{"Enter", "Select / drill into detail"},
		{"Esc", "Clear filter / go back"},
	} {
		b.WriteString(renderBinding(bind.key, bind.desc))
	}

	b.WriteString("\n")
	b.WriteString(styles.SectionStyle.Render("Cluster Detail"))
	b.WriteString("\n")
	for _, bind := range []struct{ key, desc string }{
		{"Tab", "Next tab"},
		{"Shift+Tab", "Previous tab"},
		{"Esc / Backspace", "Back to list"},
		{"s", "Scale workers"},
		{"u", "Upgrade Kubernetes version"},
		{"d", "Delete cluster"},
	} {
		b.WriteString(renderBinding(bind.key, bind.desc))
	}

	b.WriteString("\n")
	b.WriteString(styles.SectionStyle.Render("Addons Tab (in Cluster Detail)"))
	b.WriteString("\n")
	for _, bind := range []struct{ key, desc string }{
		{"a", "Install addon"},
		{"x", "Uninstall selected addon"},
	} {
		b.WriteString(renderBinding(bind.key, bind.desc))
	}

	b.WriteString("\n")
	b.WriteString(styles.SectionStyle.Render("Teams"))
	b.WriteString("\n")
	bindings := []struct{ key, desc string }{
		{"a", "Add team member"},
	}
	if v.Admin {
		bindings = append(bindings,
			struct{ key, desc string }{"c", "Create team (admin)"},
			struct{ key, desc string }{"d", "Delete team (admin)"},
		)
	}
	for _, bind := range bindings {
		b.WriteString(renderBinding(bind.key, bind.desc))
	}

	if v.Admin {
		b.WriteString("\n")
		b.WriteString(styles.SectionStyle.Render("Users (admin)"))
		b.WriteString("\n")
		for _, bind := range []struct{ key, desc string }{
			{"c", "Create user"},
			{"d", "Delete user"},
			{"x", "Disable / enable user"},
		} {
			b.WriteString(renderBinding(bind.key, bind.desc))
		}

		b.WriteString("\n")
		b.WriteString(styles.SectionStyle.Render("Providers (admin)"))
		b.WriteString("\n")
		for _, bind := range []struct{ key, desc string }{
			{"d", "Delete provider"},
		} {
			b.WriteString(renderBinding(bind.key, bind.desc))
		}

		b.WriteString("\n")
		b.WriteString(styles.SectionStyle.Render("Networks (admin)"))
		b.WriteString("\n")
		for _, bind := range []struct{ key, desc string }{
			{"d", "Delete network pool"},
		} {
			b.WriteString(renderBinding(bind.key, bind.desc))
		}
	}

	b.WriteString("\n")
	b.WriteString(styles.SectionStyle.Render("Action Prompts"))
	b.WriteString("\n")
	for _, bind := range []struct{ key, desc string }{
		{"Enter", "Confirm input"},
		{"Esc", "Cancel action"},
		{"y / n", "Confirm or deny (delete/uninstall)"},
	} {
		b.WriteString(renderBinding(bind.key, bind.desc))
	}

	b.WriteString("\n")
	b.WriteString(styles.SectionStyle.Render("Views"))
	b.WriteString("\n")
	for _, bind := range []struct{ key, desc string }{
		{"1", "Clusters"},
		{"2", "Addon Catalog"},
		{"3", "Teams"},
		{"4", "Providers"},
		{"5", "Networks"},
		{"6", "Users"},
		{"7", "Health"},
		{"8", "Help"},
	} {
		b.WriteString(renderBinding(bind.key, bind.desc))
	}

	return b.String()
}

func renderBinding(key, desc string) string {
	k := styles.ActiveTabStyle.Render(padRight(key, 18))
	d := styles.HelpStyle.Render(desc)
	return fmt.Sprintf("  %s %s\n", k, d)
}
