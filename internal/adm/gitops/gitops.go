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

// Package gitops implements butleradm gitops commands. The command group
// configures the platform Git provider and (in later sub-features) drives
// GitOps lifecycle operations against butler-server. Commands are thin
// HTTP clients over internal/common/serverhttp; the orchestration lives in
// butler-server, consistent with the ADR-016 amendment.
package gitops

import (
	"github.com/butlerdotdev/butler/internal/common/auth"
	"github.com/butlerdotdev/butler/internal/common/log"
	"github.com/butlerdotdev/butler/internal/common/output"
	"github.com/spf13/cobra"
)

// NewGitopsCmd creates the gitops parent command for butleradm.
func NewGitopsCmd(logger *log.Logger) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "gitops",
		Args:  cobra.NoArgs,
		RunE:  output.ShowHelp,
		Short: "Manage GitOps configuration and lifecycle",
		Long: `Manage the platform Git provider and GitOps lifecycle.

The config subcommand group manages the Git provider used for GitOps
operations. It is a prerequisite for enabling GitOps on clusters. The
repositories and branches subcommands list what the configured provider
credential can reach.

Examples:
  butleradm gitops config get
  butleradm gitops repositories list
  butleradm gitops branches list --repo owner/repo`,
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			auth.WarnIfUnauthenticated()
		},
	}

	cmd.AddCommand(newConfigCmd(logger))
	cmd.AddCommand(newRepositoriesCmd(logger))
	cmd.AddCommand(newBranchesCmd(logger))

	return cmd
}
