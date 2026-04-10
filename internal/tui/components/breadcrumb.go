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
	"strings"

	"github.com/butlerdotdev/butler/internal/tui/styles"
)

// Breadcrumb renders a navigation path like "Clusters > tempo > Addons".
type Breadcrumb struct {
	Parts []string
}

// View renders the breadcrumb bar.
func (b Breadcrumb) View() string {
	if len(b.Parts) == 0 {
		return ""
	}

	styled := make([]string, len(b.Parts))
	for i, p := range b.Parts {
		if i == len(b.Parts)-1 {
			styled[i] = styles.ActiveTabStyle.Render(p)
		} else {
			styled[i] = styles.TabStyle.Render(p)
		}
	}
	sep := styles.HelpStyle.Render(" > ")
	return strings.Join(styled, sep) + "\n"
}
