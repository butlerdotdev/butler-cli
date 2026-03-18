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

// Package tui provides an interactive terminal UI for the bootstrap process.
package tui

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/butlerdotdev/butler/internal/adm/bootstrap/orchestrator"
	"github.com/butlerdotdev/butler/internal/common/log"
)

// RunConfig holds the configuration needed to run the TUI.
type RunConfig struct {
	Ctx              context.Context
	Cancel           context.CancelFunc
	Cfg              *orchestrator.Config
	OrcOptions       orchestrator.Options
	LoggerName       string
	LogLevel         slog.Level
	SkipPreBootstrap bool // Skip pre-bootstrap view (used when wizard already confirmed)
}

// Run starts the interactive TUI for the bootstrap process.
func Run(rc RunConfig) error {
	eventCh := make(chan orchestrator.Event, 100)
	logBuf := NewLogBuffer(1000)
	bufWriter := &logBufferWriter{buf: logBuf}
	orchLogger := log.NewWithWriter(rc.LoggerName, rc.LogLevel, bufWriter)

	orch := orchestrator.New(orchLogger, rc.OrcOptions)
	orch.SetEventSink(orchestrator.NewChannelSink(eventCh, func(msg string, args ...any) {
		orchLogger.Debug(msg, args...)
	}))

	var orchErr error
	var orchOnce sync.Once
	orchDone := make(chan struct{})

	startFn := func() {
		go func() {
			defer orchOnce.Do(func() { close(orchDone) })
			orchErr = orch.Run(rc.Ctx, rc.Cfg)
			// Ensure the TUI receives a terminal event for early failures
			// (before watchBootstrap which emits its own EventFailed).
			if orchErr != nil {
				select {
				case eventCh <- orchestrator.Event{
					Type:    orchestrator.EventFailed,
					Message: orchErr.Error(),
					Error:   orchErr,
				}:
				default:
				}
			}
			close(eventCh)
		}()
	}

	model := NewModel(ModelConfig{
		Cfg:              rc.Cfg,
		EventCh:          eventCh,
		LogBuf:           logBuf,
		StartFn:          startFn,
		SkipCleanup:      rc.OrcOptions.SkipCleanup,
		SkipPreBootstrap: rc.SkipPreBootstrap,
	})

	p := tea.NewProgram(model, tea.WithAltScreen())

	finalModel, err := p.Run()
	if err != nil {
		return fmt.Errorf("TUI error: %w", err)
	}

	if fm, ok := finalModel.(bootstrapModel); ok && fm.activeView == viewDuringBootstrap {
		// User quit while bootstrap was in progress — cancel and wait for cleanup.
		rc.Cancel()
		<-orchDone
	}

	if orchErr != nil {
		return orchErr
	}

	return nil
}

// logBufferWriter adapts a LogBuffer to io.Writer by splitting on newlines.
type logBufferWriter struct {
	buf *LogBuffer
}

func (w *logBufferWriter) Write(p []byte) (n int, err error) {
	s := string(p)
	for _, line := range strings.Split(s, "\n") {
		if line != "" {
			w.buf.Write(line)
		}
	}
	return len(p), nil
}
