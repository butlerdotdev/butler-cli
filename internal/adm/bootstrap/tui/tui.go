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

// Run starts the interactive TUI for the bootstrap process. It creates
// the event channel, log buffer, orchestrator, and bubbletea program,
// then blocks until the bootstrap completes or the user quits.
//
// The cancel function is called when the user aborts, which propagates
// to the orchestrator's context for KIND cleanup.
func Run(rc RunConfig) error {
	// Event channel for orchestrator -> TUI communication
	eventCh := make(chan orchestrator.Event, 100)

	// Log buffer for capturing orchestrator output
	logBuf := NewLogBuffer(1000)

	// Create a writer that feeds the log buffer. The prettyHandler writes
	// formatted lines including a trailing newline, so we strip it and
	// write each line to the buffer.
	bufWriter := &logBufferWriter{buf: logBuf}

	// Create a real log.Logger that writes to the buffer instead of stderr.
	orchLogger := log.NewWithWriter(rc.LoggerName, rc.LogLevel, bufWriter)

	orch := orchestrator.New(orchLogger, rc.OrcOptions)
	orch.SetEventSink(orchestrator.NewChannelSink(eventCh, func(msg string, args ...any) {
		orchLogger.Debug(msg, args...)
	}))

	// Track orchestrator completion
	var orchErr error
	var orchOnce sync.Once
	orchDone := make(chan struct{})

	startFn := func() {
		go func() {
			defer orchOnce.Do(func() { close(orchDone) })
			orchErr = orch.Run(rc.Ctx, rc.Cfg)
			close(eventCh)
		}()
	}

	// Create the root model
	model := NewModel(ModelConfig{
		Cfg:              rc.Cfg,
		EventCh:          eventCh,
		LogBuf:           logBuf,
		StartFn:          startFn,
		SkipCleanup:      rc.OrcOptions.SkipCleanup,
		SkipPreBootstrap: rc.SkipPreBootstrap,
	})

	// Run the bubbletea program
	p := tea.NewProgram(model, tea.WithAltScreen())

	finalModel, err := p.Run()
	if err != nil {
		return fmt.Errorf("TUI error: %w", err)
	}

	// If the user quit mid-bootstrap, cancel the context and wait for cleanup
	if fm, ok := finalModel.(bootstrapModel); ok && fm.quitting && fm.activeView == viewDuringBootstrap {
		rc.Cancel()
		<-orchDone
	}

	// Wait for orchestrator to finish if it was started
	select {
	case <-orchDone:
	default:
		// Orchestrator was never started (user quit from pre-bootstrap)
	}

	// Return the orchestrator error if it failed
	if orchErr != nil {
		return orchErr
	}

	return nil
}

// logBufferWriter adapts a LogBuffer to io.Writer. The prettyHandler writes
// complete lines ending with \n. This writer splits on newlines and feeds
// each non-empty line to the buffer.
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
