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
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/butlerdotdev/butler/internal/adm/bootstrap/orchestrator"
)

// orchestratorEventMsg wraps an orchestrator event for the bubbletea update loop.
type orchestratorEventMsg orchestrator.Event

// bootstrapDoneMsg signals that the orchestrator goroutine has completed.
type bootstrapDoneMsg struct {
	err error
}

// tickMsg triggers periodic UI refresh (log viewport, elapsed time).
type tickMsg time.Time

// tickCmd returns a command that sends a tickMsg after the given interval.
func tickCmd(d time.Duration) tea.Cmd {
	return tea.Tick(d, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

// waitForEvent blocks until an event arrives and wraps it as orchestratorEventMsg.
// When the channel closes, returns bootstrapDoneMsg to trigger the post view.
func waitForEvent(ch <-chan orchestrator.Event) tea.Cmd {
	return func() tea.Msg {
		e, ok := <-ch
		if !ok {
			return bootstrapDoneMsg{}
		}
		return orchestratorEventMsg(e)
	}
}
