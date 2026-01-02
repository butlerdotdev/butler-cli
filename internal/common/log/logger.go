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

// Package log provides structured logging for Butler CLIs.
package log

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"

	"github.com/charmbracelet/lipgloss"
)

// Colors and styles for terminal output
var (
	// Level colors
	debugStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("8")) // Gray
	infoStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("6")) // Cyan
	warnStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("3")) // Yellow
	errorStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("1")) // Red

	// Component styles
	timestampStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	nameStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("5")).Bold(true)
	keyStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("4"))
)

// Logger wraps slog.Logger with Butler-specific functionality
type Logger struct {
	*slog.Logger
	name  string
	level slog.Level
}

// New creates a new Logger with the given name
func New(name string) *Logger {
	return NewWithLevel(name, slog.LevelInfo)
}

// NewWithLevel creates a new Logger with the given name and level
func NewWithLevel(name string, level slog.Level) *Logger {
	handler := &prettyHandler{
		name:   name,
		level:  level,
		output: os.Stderr,
	}

	return &Logger{
		Logger: slog.New(handler),
		name:   name,
		level:  level,
	}
}

// SetVerbose enables debug logging
func (l *Logger) SetVerbose(verbose bool) {
	if verbose {
		l.level = slog.LevelDebug
	}
}

// WithComponent returns a new logger with a component name suffix
func (l *Logger) WithComponent(component string) *Logger {
	newName := l.name + "/" + component
	return NewWithLevel(newName, l.level)
}

// Phase logs a phase transition (used for bootstrap phases)
func (l *Logger) Phase(phase string) {
	style := lipgloss.NewStyle().
		Foreground(lipgloss.Color("2")).
		Bold(true)
	l.Info(style.Render("▶ " + phase))
}

// Success logs a success message
func (l *Logger) Success(msg string, args ...any) {
	style := lipgloss.NewStyle().
		Foreground(lipgloss.Color("2"))
	l.Info(style.Render("✓ "+msg), args...)
}

// Waiting logs a waiting/polling message
func (l *Logger) Waiting(msg string, args ...any) {
	style := lipgloss.NewStyle().
		Foreground(lipgloss.Color("3"))
	l.Info(style.Render("⏳ "+msg), args...)
}

// prettyHandler is a custom slog handler for pretty terminal output
type prettyHandler struct {
	name   string
	level  slog.Level
	output io.Writer
	attrs  []slog.Attr
	groups []string
}

func (h *prettyHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.level
}

func (h *prettyHandler) Handle(_ context.Context, r slog.Record) error {
	// Format timestamp
	ts := timestampStyle.Render(r.Time.Format("15:04:05"))

	// Format level
	var levelStr string
	switch r.Level {
	case slog.LevelDebug:
		levelStr = debugStyle.Render("DBG")
	case slog.LevelInfo:
		levelStr = infoStyle.Render("INF")
	case slog.LevelWarn:
		levelStr = warnStyle.Render("WRN")
	case slog.LevelError:
		levelStr = errorStyle.Render("ERR")
	}

	// Format name
	name := nameStyle.Render("[" + h.name + "]")

	// Format message
	msg := r.Message

	// Format attributes
	var attrs string
	r.Attrs(func(a slog.Attr) bool {
		key := keyStyle.Render(a.Key + "=")
		attrs += " " + key + fmt.Sprintf("%v", a.Value.Any())
		return true
	})

	// Write output
	line := ts + " " + levelStr + " " + name + " " + msg + attrs + "\n"
	_, err := h.output.Write([]byte(line))
	return err
}

func (h *prettyHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	newHandler := *h
	newHandler.attrs = append(newHandler.attrs, attrs...)
	return &newHandler
}

func (h *prettyHandler) WithGroup(name string) slog.Handler {
	newHandler := *h
	newHandler.groups = append(newHandler.groups, name)
	return &newHandler
}
