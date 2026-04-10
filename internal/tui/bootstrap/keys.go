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

package bootstrap

import "github.com/charmbracelet/bubbles/key"

type keyMap struct {
	Quit       key.Binding
	ForceQuit  key.Binding
	TabNext    key.Binding
	TabPrev    key.Binding
	ToggleLogs key.Binding
	ScrollUp   key.Binding
	ScrollDown key.Binding
	Filter     key.Binding
	Confirm    key.Binding
	Cancel     key.Binding
	Yes        key.Binding
	No         key.Binding
}

var keys = keyMap{
	Quit: key.NewBinding(
		key.WithKeys("q"),
		key.WithHelp("q", "quit"),
	),
	ForceQuit: key.NewBinding(
		key.WithKeys("ctrl+c"),
		key.WithHelp("ctrl+c", "force quit"),
	),
	TabNext: key.NewBinding(
		key.WithKeys("tab"),
		key.WithHelp("tab", "next panel"),
	),
	TabPrev: key.NewBinding(
		key.WithKeys("shift+tab"),
		key.WithHelp("shift+tab", "prev panel"),
	),
	ToggleLogs: key.NewBinding(
		key.WithKeys("l"),
		key.WithHelp("l", "toggle logs"),
	),
	ScrollUp: key.NewBinding(
		key.WithKeys("k", "up"),
		key.WithHelp("k/up", "scroll up"),
	),
	ScrollDown: key.NewBinding(
		key.WithKeys("j", "down"),
		key.WithHelp("j/down", "scroll down"),
	),
	Filter: key.NewBinding(
		key.WithKeys("/"),
		key.WithHelp("/", "filter"),
	),
	Confirm: key.NewBinding(
		key.WithKeys("enter"),
		key.WithHelp("enter", "confirm"),
	),
	Cancel: key.NewBinding(
		key.WithKeys("esc"),
		key.WithHelp("esc", "cancel"),
	),
	Yes: key.NewBinding(
		key.WithKeys("y"),
		key.WithHelp("y", "yes"),
	),
	No: key.NewBinding(
		key.WithKeys("n"),
		key.WithHelp("n", "no"),
	),
}
