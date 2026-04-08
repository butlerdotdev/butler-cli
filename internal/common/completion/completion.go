// Copyright 2026 The Butler Authors.
// SPDX-License-Identifier: Apache-2.0

// Package completion provides shell completion commands for Butler CLIs.
package completion

import (
	"os"

	"github.com/spf13/cobra"
)

// NewCompletionCmd creates the completion parent command with subcommands
// for bash, zsh, fish, and powershell. The binaryName is used in help text
// to show the correct eval/source invocation for the active CLI.
func NewCompletionCmd(binaryName string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "completion [shell]",
		Short: "Generate shell completion scripts",
		Long: `Generate shell completion scripts for ` + binaryName + `.

The generated script should be saved to the appropriate location for your shell,
or sourced directly in your shell profile.`,
	}

	cmd.AddCommand(newBashCmd(binaryName))
	cmd.AddCommand(newZshCmd(binaryName))
	cmd.AddCommand(newFishCmd(binaryName))
	cmd.AddCommand(newPowershellCmd(binaryName))

	return cmd
}

func newBashCmd(binaryName string) *cobra.Command {
	return &cobra.Command{
		Use:   "bash",
		Short: "Generate bash completion script",
		Long: `Generate bash completion script for ` + binaryName + `.

To load completions in your current shell session:

  source <(` + binaryName + ` completion bash)

To load completions for every new session, add the above line to your ~/.bashrc
or execute:

  ` + binaryName + ` completion bash > /etc/bash_completion.d/` + binaryName,
		Args:              cobra.NoArgs,
		DisableFlagParsing: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Root().GenBashCompletionV2(os.Stdout, true)
		},
	}
}

func newZshCmd(binaryName string) *cobra.Command {
	return &cobra.Command{
		Use:   "zsh",
		Short: "Generate zsh completion script",
		Long: `Generate zsh completion script for ` + binaryName + `.

To load completions in your current shell session:

  source <(` + binaryName + ` completion zsh)

To load completions for every new session, add the output to your completions
directory:

  ` + binaryName + ` completion zsh > "${fpath[1]}/_` + binaryName + `"

You may need to start a new shell for the completions to take effect.`,
		Args:              cobra.NoArgs,
		DisableFlagParsing: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Root().GenZshCompletion(os.Stdout)
		},
	}
}

func newFishCmd(binaryName string) *cobra.Command {
	return &cobra.Command{
		Use:   "fish",
		Short: "Generate fish completion script",
		Long: `Generate fish completion script for ` + binaryName + `.

To load completions in your current shell session:

  ` + binaryName + ` completion fish | source

To load completions for every new session:

  ` + binaryName + ` completion fish > ~/.config/fish/completions/` + binaryName + `.fish`,
		Args:              cobra.NoArgs,
		DisableFlagParsing: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Root().GenFishCompletion(os.Stdout, true)
		},
	}
}

func newPowershellCmd(binaryName string) *cobra.Command {
	return &cobra.Command{
		Use:   "powershell",
		Short: "Generate PowerShell completion script",
		Long: `Generate PowerShell completion script for ` + binaryName + `.

To load completions in your current shell session:

  ` + binaryName + ` completion powershell | Out-String | Invoke-Expression

To load completions for every new session, add the output to your PowerShell
profile.`,
		Args:              cobra.NoArgs,
		DisableFlagParsing: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Root().GenPowerShellCompletion(os.Stdout)
		},
	}
}
