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

package wizard

import (
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
)

var (
	brandGreen = lipgloss.Color("#22c55e")
	brandBlue  = lipgloss.Color("#3b82f6")
	brandRed   = lipgloss.Color("#ef4444")
	brandGray  = lipgloss.Color("#a3a3a3")
	brandText  = lipgloss.Color("#fafafa")
	brandBg    = lipgloss.Color("#0a0a0a")
	brandBgAlt = lipgloss.Color("#171717")
)

// butlerTheme returns a huh theme with Butler brand colors.
func butlerTheme() *huh.Theme {
	t := huh.ThemeCharm()

	// Focused
	t.Focused.Title = t.Focused.Title.Foreground(brandBlue).Bold(true)
	t.Focused.Description = t.Focused.Description.Foreground(brandGray)
	t.Focused.Base = t.Focused.Base.BorderForeground(brandGreen)
	t.Focused.TextInput.Cursor = t.Focused.TextInput.Cursor.Foreground(brandGreen)
	t.Focused.TextInput.Prompt = t.Focused.TextInput.Prompt.Foreground(brandGreen)
	t.Focused.SelectSelector = t.Focused.SelectSelector.Foreground(brandGreen)
	t.Focused.SelectedOption = t.Focused.SelectedOption.Foreground(brandGreen)
	t.Focused.FocusedButton = t.Focused.FocusedButton.Background(brandGreen).Foreground(brandBg)
	t.Focused.BlurredButton = t.Focused.BlurredButton.Foreground(brandGray)

	// Blurred
	t.Blurred.Title = t.Blurred.Title.Foreground(brandGray)
	t.Blurred.TextInput.Prompt = t.Blurred.TextInput.Prompt.Foreground(brandGray)

	return t
}

// wizardKeyMap adds Esc to the Prev binding on every field type.
func wizardKeyMap() *huh.KeyMap {
	km := huh.NewDefaultKeyMap()
	prevBinding := key.NewBinding(
		key.WithKeys("shift+tab", "esc"),
		key.WithHelp("esc", "back"),
	)
	km.Input.Prev = prevBinding
	km.Text.Prev = prevBinding
	km.Select.Prev = prevBinding
	km.MultiSelect.Prev = prevBinding
	km.Confirm.Prev = prevBinding
	km.Note.Prev = prevBinding
	km.FilePicker.Prev = prevBinding
	return km
}

