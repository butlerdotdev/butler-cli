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

// Package output provides shared output formatting utilities for Butler CLIs.
package output

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"golang.org/x/term"
	"sigs.k8s.io/yaml"
)

// Format represents the output format
type Format string

const (
	FormatTable Format = "table"
	FormatWide  Format = "wide"
	FormatJSON  Format = "json"
	FormatYAML  Format = "yaml"
)

// ParseFormat parses a string into an output Format
func ParseFormat(s string) (Format, error) {
	switch strings.ToLower(s) {
	case "table", "":
		return FormatTable, nil
	case "wide":
		return FormatWide, nil
	case "json":
		return FormatJSON, nil
	case "yaml":
		return FormatYAML, nil
	default:
		return "", fmt.Errorf("unknown output format %q (valid: table, wide, json, yaml)", s)
	}
}

// Styles for colorized output
var (
	// Phase colors
	PhaseReady        = lipgloss.NewStyle().Foreground(lipgloss.Color("2")).Bold(true) // Green
	PhaseProvisioning = lipgloss.NewStyle().Foreground(lipgloss.Color("3"))            // Yellow
	PhaseFailed       = lipgloss.NewStyle().Foreground(lipgloss.Color("1")).Bold(true) // Red
	PhasePending      = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))            // Gray
	PhaseDeleting     = lipgloss.NewStyle().Foreground(lipgloss.Color("5"))            // Magenta

	// Status indicators
	StatusOK      = lipgloss.NewStyle().Foreground(lipgloss.Color("2")).SetString("✓")
	StatusWarning = lipgloss.NewStyle().Foreground(lipgloss.Color("3")).SetString("!")
	StatusError   = lipgloss.NewStyle().Foreground(lipgloss.Color("1")).SetString("✗")
	StatusPending = lipgloss.NewStyle().Foreground(lipgloss.Color("8")).SetString("○")

	// Header style
	HeaderStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("4"))

	// Help text styles
	HelpCommand     = lipgloss.NewStyle().Foreground(lipgloss.Color("6"))            // Cyan
	HelpFlag        = lipgloss.NewStyle().Foreground(lipgloss.Color("3"))            // Yellow
	HelpFlagDesc    = lipgloss.NewStyle().Foreground(lipgloss.Color("7"))            // White/Light gray
	HelpSection     = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("4")) // Bold Blue
	HelpExample     = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))            // Dim/Gray
	HelpExampleCmd  = lipgloss.NewStyle().Foreground(lipgloss.Color("7"))            // Normal
	HelpBinary      = lipgloss.NewStyle().Foreground(lipgloss.Color("6")).Bold(true) // Bold Cyan
	HelpDescription = lipgloss.NewStyle().Foreground(lipgloss.Color("7"))            // Normal
	HelpWarning     = lipgloss.NewStyle().Foreground(lipgloss.Color("3")).Bold(true) // Bold Yellow
	HelpDanger      = lipgloss.NewStyle().Foreground(lipgloss.Color("1")).Bold(true) // Bold Red
)

// ColorEnabled returns true if colors should be used
// Respects NO_COLOR env var (https://no-color.org/)
func ColorEnabled() bool {
	// NO_COLOR takes precedence
	if _, exists := os.LookupEnv("NO_COLOR"); exists {
		return false
	}
	// Also check BUTLER_NO_COLOR for convenience
	if _, exists := os.LookupEnv("BUTLER_NO_COLOR"); exists {
		return false
	}
	// Only colorize if stdout is a TTY
	return IsTTY()
}

// IsTTY returns true if stdout is a terminal
func IsTTY() bool {
	return term.IsTerminal(int(os.Stdout.Fd()))
}

// ColorizePhase returns a colorized phase string if TTY, plain otherwise
func ColorizePhase(phase string) string {
	if !ColorEnabled() {
		return phase
	}

	switch strings.ToLower(phase) {
	case "ready":
		return PhaseReady.Render(phase)
	case "provisioning", "installing", "updating":
		return PhaseProvisioning.Render(phase)
	case "failed":
		return PhaseFailed.Render(phase)
	case "pending":
		return PhasePending.Render(phase)
	case "deleting":
		return PhaseDeleting.Render(phase)
	default:
		return phase
	}
}

// Semantic color functions for consistent CLI output

// Command returns a colorized command name (cyan)
func Command(s string) string {
	if !ColorEnabled() {
		return s
	}
	return HelpCommand.Render(s)
}

// Flag returns a colorized flag name (yellow)
func Flag(s string) string {
	if !ColorEnabled() {
		return s
	}
	return HelpFlag.Render(s)
}

// Section returns a colorized section header (bold blue)
func Section(s string) string {
	if !ColorEnabled() {
		return s
	}
	return HelpSection.Render(s)
}

// Example returns a colorized example comment (dim)
func Example(s string) string {
	if !ColorEnabled() {
		return s
	}
	return HelpExample.Render(s)
}

// Binary returns the colorized binary name (bold cyan)
func Binary(s string) string {
	if !ColorEnabled() {
		return s
	}
	return HelpBinary.Render(s)
}

// Warning returns colorized warning text (bold yellow)
func Warning(s string) string {
	if !ColorEnabled() {
		return s
	}
	return HelpWarning.Render(s)
}

// Danger returns colorized danger text (bold red)
func Danger(s string) string {
	if !ColorEnabled() {
		return s
	}
	return HelpDanger.Render(s)
}

// Success returns colorized success text (green)
func Success(s string) string {
	if !ColorEnabled() {
		return s
	}
	return PhaseReady.Render(s)
}

// Dim returns dimmed text (gray)
func Dim(s string) string {
	if !ColorEnabled() {
		return s
	}
	return HelpExample.Render(s)
}

// Bold returns bold text
func Bold(s string) string {
	if !ColorEnabled() {
		return s
	}
	return lipgloss.NewStyle().Bold(true).Render(s)
}

// StatusIcon returns an appropriate status icon for a phase
func StatusIcon(phase string) string {
	if !IsTTY() {
		return ""
	}

	switch strings.ToLower(phase) {
	case "ready":
		return StatusOK.String() + " "
	case "failed":
		return StatusError.String() + " "
	case "provisioning", "installing", "updating", "deleting":
		return StatusWarning.String() + " "
	default:
		return StatusPending.String() + " "
	}
}

// FormatAge formats a time as a human-readable age string
func FormatAge(t time.Time) string {
	if t.IsZero() {
		return "<unknown>"
	}

	d := time.Since(t)
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	case d < 7*24*time.Hour:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	default:
		return fmt.Sprintf("%dw", int(d.Hours()/(24*7)))
	}
}

// FormatWorkers formats worker counts as ready/desired
func FormatWorkers(ready, desired int64) string {
	if desired == 0 {
		return "-"
	}
	return fmt.Sprintf("%d/%d", ready, desired)
}

// Table provides a simple table writer with header support
// Note: When using colors, we use fixed-width columns instead of tabwriter
// because tabwriter counts ANSI escape codes as visible characters
type Table struct {
	writer    io.Writer
	headers   []string
	rows      [][]string
	colWidths []int
	useColors bool
}

// NewTable creates a new table writer
func NewTable(output io.Writer, headers ...string) *Table {
	t := &Table{
		writer:    output,
		headers:   headers,
		rows:      make([][]string, 0),
		colWidths: make([]int, len(headers)),
		useColors: IsTTY(),
	}

	// Initialize column widths from headers
	for i, h := range headers {
		t.colWidths[i] = len(h)
	}

	return t
}

// AddRow adds a row to the table
func (t *Table) AddRow(columns ...string) {
	// Store the raw (uncolored) row for width calculation
	// But we need to strip ANSI codes for width calculation
	for i, col := range columns {
		if i < len(t.colWidths) {
			// Strip ANSI codes to get true visible width
			visibleLen := visibleLength(col)
			if visibleLen > t.colWidths[i] {
				t.colWidths[i] = visibleLen
			}
		}
	}
	t.rows = append(t.rows, columns)
}

// Flush writes the table to output
func (t *Table) Flush() error {
	// Print headers
	if len(t.headers) > 0 {
		for i, h := range t.headers {
			if t.useColors {
				h = HeaderStyle.Render(h)
			}
			// Pad based on visible width
			fmt.Fprint(t.writer, h)
			if i < len(t.headers)-1 {
				padding := t.colWidths[i] - len(t.headers[i]) + 2
				fmt.Fprint(t.writer, strings.Repeat(" ", padding))
			}
		}
		fmt.Fprintln(t.writer)
	}

	// Print rows
	for _, row := range t.rows {
		for i, col := range row {
			fmt.Fprint(t.writer, col)
			if i < len(row)-1 && i < len(t.colWidths) {
				// Calculate padding based on visible length vs column width
				visLen := visibleLength(col)
				padding := t.colWidths[i] - visLen + 2
				if padding < 2 {
					padding = 2
				}
				fmt.Fprint(t.writer, strings.Repeat(" ", padding))
			}
		}
		fmt.Fprintln(t.writer)
	}

	return nil
}

// visibleLength returns the visible length of a string, excluding ANSI escape codes
func visibleLength(s string) int {
	// Simple ANSI stripper - handles common escape sequences
	inEscape := false
	visibleLen := 0
	for _, r := range s {
		if r == '\x1b' {
			inEscape = true
			continue
		}
		if inEscape {
			if r == 'm' {
				inEscape = false
			}
			continue
		}
		visibleLen++
	}
	return visibleLen
}

// PrintJSON prints data as JSON
func PrintJSON(output io.Writer, data interface{}) error {
	encoder := json.NewEncoder(output)
	encoder.SetIndent("", "  ")
	return encoder.Encode(data)
}

// PrintYAML prints data as YAML
func PrintYAML(output io.Writer, data interface{}) error {
	yamlData, err := yaml.Marshal(data)
	if err != nil {
		return err
	}
	_, err = output.Write(yamlData)
	return err
}

// Printer handles multi-format output
type Printer struct {
	Format Format
	Output io.Writer
}

// NewPrinter creates a new printer with the specified format
func NewPrinter(format Format, output io.Writer) *Printer {
	if output == nil {
		output = os.Stdout
	}
	return &Printer{
		Format: format,
		Output: output,
	}
}

// Print outputs data in the configured format
// For table/wide formats, tableFunc is called to render the table
// For json/yaml, the data is marshaled directly
func (p *Printer) Print(data interface{}, tableFunc func(io.Writer) error) error {
	switch p.Format {
	case FormatJSON:
		return PrintJSON(p.Output, data)
	case FormatYAML:
		return PrintYAML(p.Output, data)
	case FormatTable, FormatWide:
		if tableFunc != nil {
			return tableFunc(p.Output)
		}
		return fmt.Errorf("table output not supported for this data")
	default:
		return fmt.Errorf("unknown format: %s", p.Format)
	}
}
