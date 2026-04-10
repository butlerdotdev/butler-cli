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

import "github.com/charmbracelet/bubbles/key"

// KeyMap defines all key bindings for the dashboard.
type KeyMap struct {
	Quit      key.Binding
	Help      key.Binding
	Refresh   key.Binding
	Enter     key.Binding
	Back      key.Binding
	Tab       key.Binding
	ShiftTab  key.Binding
	Up        key.Binding
	Down      key.Binding
	Filter    key.Binding
	ViewZero  key.Binding // bootstrap (admin only)
	ViewOne   key.Binding
	ViewTwo   key.Binding
	ViewThree key.Binding
	ViewFour  key.Binding
	ViewFive  key.Binding
	ViewSix   key.Binding
	ViewSeven key.Binding
	ViewEight key.Binding
}

// DefaultKeyMap returns the default key bindings.
func DefaultKeyMap() KeyMap {
	return KeyMap{
		Quit:      key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
		Help:      key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help")),
		Refresh:   key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "refresh")),
		Enter:     key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "select")),
		Back:      key.NewBinding(key.WithKeys("esc", "backspace"), key.WithHelp("esc", "back")),
		Tab:       key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "next tab")),
		ShiftTab:  key.NewBinding(key.WithKeys("shift+tab"), key.WithHelp("shift+tab", "prev tab")),
		Up:        key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("k/up", "up")),
		Down:      key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("j/down", "down")),
		Filter:    key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "filter")),
		ViewZero:  key.NewBinding(key.WithKeys("0"), key.WithHelp("0", "bootstrap")),
		ViewOne:   key.NewBinding(key.WithKeys("1"), key.WithHelp("1", "clusters")),
		ViewTwo:   key.NewBinding(key.WithKeys("2"), key.WithHelp("2", "addons")),
		ViewThree: key.NewBinding(key.WithKeys("3"), key.WithHelp("3", "teams")),
		ViewFour:  key.NewBinding(key.WithKeys("4"), key.WithHelp("4", "providers")),
		ViewFive:  key.NewBinding(key.WithKeys("5"), key.WithHelp("5", "networks")),
		ViewSix:   key.NewBinding(key.WithKeys("6"), key.WithHelp("6", "users")),
		ViewSeven: key.NewBinding(key.WithKeys("7"), key.WithHelp("7", "health")),
		ViewEight: key.NewBinding(key.WithKeys("8"), key.WithHelp("8", "help")),
	}
}
