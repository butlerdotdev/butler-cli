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

package output

import (
	"bytes"
	"regexp"
	"strings"

	"github.com/spf13/cobra"
)

// HelpTemplate is a custom Cobra help template with color support
const HelpTemplate = `{{with .Long}}{{. | colorizeDescription}}

{{end}}{{if or .Runnable .HasSubCommands}}{{sectionTitle "Usage:"}}
{{if .Runnable}}  {{colorizeCommand .UseLine}}{{end}}{{if .HasAvailableSubCommands}}
  {{colorizeCommand .CommandPath}} [command]{{end}}{{if gt (len .Aliases) 0}}

{{sectionTitle "Aliases:"}}
  {{.NameAndAliases}}{{end}}{{if .HasExample}}

{{sectionTitle "Examples:"}}
{{.Example | colorizeExamples}}{{end}}{{if .HasAvailableSubCommands}}{{$cmds := .Commands}}{{if eq (len .Groups) 0}}

{{sectionTitle "Available Commands:"}}{{range $cmds}}{{if (or .IsAvailableCommand (eq .Name "help"))}}
  {{colorizeSubcommand .Name .NamePadding}}{{.Short}}{{end}}{{end}}{{else}}{{range $group := .Groups}}

{{.Title}}{{range $cmds}}{{if (and (eq .GroupID $group.ID) (or .IsAvailableCommand (eq .Name "help")))}}
  {{colorizeSubcommand .Name .NamePadding}}{{.Short}}{{end}}{{end}}{{end}}{{if not .AllChildCommandsHaveGroup}}

{{sectionTitle "Additional Commands:"}}{{range $cmds}}{{if (and (eq .GroupID "") (or .IsAvailableCommand (eq .Name "help")))}}
  {{colorizeSubcommand .Name .NamePadding}}{{.Short}}{{end}}{{end}}{{end}}{{end}}{{end}}{{if .HasAvailableLocalFlags}}

{{sectionTitle "Flags:"}}
{{.LocalFlags.FlagUsages | colorizeFlags}}{{end}}{{if .HasAvailableInheritedFlags}}

{{sectionTitle "Global Flags:"}}
{{.InheritedFlags.FlagUsages | colorizeFlags}}{{end}}{{if .HasHelpSubCommands}}

{{sectionTitle "Additional help topics:"}}{{range .Commands}}{{if .IsAdditionalHelpTopicCommand}}
  {{colorizeSubcommand .Name .NamePadding}}{{.Short}}{{end}}{{end}}{{end}}{{if .HasAvailableSubCommands}}

Use "{{colorizeCommand (print .CommandPath " [command] --help")}}" for more information about a command.{{end}}
{{end}}`

// UsageTemplate is kept for compatibility but HelpTemplate handles everything
const UsageTemplate = HelpTemplate

// sectionTitle formats a section title (e.g., "Usage:", "Flags:")
func sectionTitle(s string) string {
	return Section(s)
}

// colorizeCommand formats a command line
func colorizeCommand(s string) string {
	if !ColorEnabled() {
		return s
	}
	// Split and colorize the binary name
	parts := strings.SplitN(s, " ", 2)
	if len(parts) == 0 {
		return s
	}
	result := Binary(parts[0])
	if len(parts) > 1 {
		result += " " + parts[1]
	}
	return result
}

// colorizeSubcommand formats a subcommand name with padding
func colorizeSubcommand(name string, padding int) string {
	if !ColorEnabled() {
		return name + strings.Repeat(" ", padding-len(name)+2)
	}
	colored := Command(name)
	// Account for ANSI codes in padding
	visiblePadding := padding - len(name) + 2
	if visiblePadding < 2 {
		visiblePadding = 2
	}
	return colored + strings.Repeat(" ", visiblePadding)
}

// colorizeFlags formats flag usage text
func colorizeFlags(s string) string {
	if !ColorEnabled() {
		return s
	}

	var result bytes.Buffer
	lines := strings.Split(s, "\n")

	// Regex to match flags like: -n, --namespace string
	flagPattern := regexp.MustCompile(`^(\s*)(-\w(?:,\s+)?)?(-{1,2}[\w-]+)(\s+\S+)?(.*)$`)

	for _, line := range lines {
		if line == "" {
			result.WriteString("\n")
			continue
		}

		matches := flagPattern.FindStringSubmatch(line)
		if matches != nil {
			indent := matches[1]
			shortFlag := matches[2] // -n, or empty
			longFlag := matches[3]  // --namespace
			argType := matches[4]   // string, int, etc.
			desc := matches[5]      // description

			result.WriteString(indent)
			if shortFlag != "" {
				result.WriteString(Flag(strings.TrimSuffix(shortFlag, " ")))
				if strings.HasSuffix(shortFlag, " ") {
					result.WriteString(" ")
				}
			}
			result.WriteString(Flag(longFlag))
			if argType != "" {
				result.WriteString(Dim(argType))
			}
			result.WriteString(desc)
			result.WriteString("\n")
		} else {
			result.WriteString(line + "\n")
		}
	}

	return strings.TrimSuffix(result.String(), "\n")
}

// colorizeExamples formats example blocks
func colorizeExamples(s string) string {
	if !ColorEnabled() {
		return s
	}

	var result bytes.Buffer
	lines := strings.Split(s, "\n")

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		indent := line[:len(line)-len(strings.TrimLeft(line, " \t"))]

		if strings.HasPrefix(trimmed, "#") {
			// Comment line - dim
			result.WriteString(indent + Dim(trimmed) + "\n")
		} else if strings.HasPrefix(trimmed, "butlerctl") || strings.HasPrefix(trimmed, "butleradm") {
			// Command line - colorize binary
			parts := strings.SplitN(trimmed, " ", 2)
			result.WriteString(indent + Binary(parts[0]))
			if len(parts) > 1 {
				result.WriteString(" " + parts[1])
			}
			result.WriteString("\n")
		} else {
			result.WriteString(line + "\n")
		}
	}

	return strings.TrimSuffix(result.String(), "\n")
}

// colorizeDescription colorizes the long description
func colorizeDescription(s string) string {
	if !ColorEnabled() {
		return s
	}

	// Colorize warning markers
	s = strings.ReplaceAll(s, "⚠️", Warning("⚠️"))
	s = strings.ReplaceAll(s, "WARNING:", Warning("WARNING:"))

	// Colorize bullet points with commands
	lines := strings.Split(s, "\n")
	var result bytes.Buffer

	for i, line := range lines {
		// Check if line contains a bullet with text that looks like a component
		if strings.HasPrefix(strings.TrimSpace(line), "•") {
			// Keep as-is but could colorize specific items
			result.WriteString(line)
		} else {
			result.WriteString(line)
		}
		if i < len(lines)-1 {
			result.WriteString("\n")
		}
	}

	return result.String()
}

// ConfigureHelp sets up colorized help for a Cobra command and all subcommands
func ConfigureHelp(cmd *cobra.Command) {
	// Set custom templates
	cmd.SetHelpTemplate(HelpTemplate)

	// Add template functions
	cobra.AddTemplateFunc("sectionTitle", sectionTitle)
	cobra.AddTemplateFunc("colorizeCommand", colorizeCommand)
	cobra.AddTemplateFunc("colorizeSubcommand", colorizeSubcommand)
	cobra.AddTemplateFunc("colorizeFlags", colorizeFlags)
	cobra.AddTemplateFunc("colorizeExamples", colorizeExamples)
	cobra.AddTemplateFunc("colorizeDescription", colorizeDescription)
}
