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

// Package context implements the butleradm context commands for switching
// team context.
package context

import (
	"fmt"
	"os"
	"time"

	"github.com/butlerdotdev/butler/internal/common/auth"
	"github.com/butlerdotdev/butler/internal/common/log"
	"github.com/butlerdotdev/butler/internal/common/output"
	"github.com/spf13/cobra"
)

// NewContextCmd creates the context parent command for butleradm.
func NewContextCmd(logger *log.Logger) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "context",
		Short: "Manage team context for CLI operations",
		Long: `View and switch the active team context used by Butler CLI commands.

The active team determines the default namespace (team-{name}) for
cluster operations. Use "context list" to see available teams and
"context use" to switch.

Examples:
  # List teams for the current server
  butleradm context list

  # Switch active team
  butleradm context use payments`,
	}

	cmd.AddCommand(newListCmd(logger))
	cmd.AddCommand(newUseCmd(logger))

	return cmd
}

func newListCmd(logger *log.Logger) *cobra.Command {
	var serverURL string

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List available teams and current context",
		Long: `List all teams the authenticated user belongs to, along with their
role and the token expiry time.

The currently active team is marked with an asterisk (*).

Examples:
  # List teams for the active server
  butleradm context list

  # List teams for a specific server
  butleradm context list --server https://butler.example.com`,
		Aliases: []string{"ls"},
		RunE: func(cmd *cobra.Command, args []string) error {
			return runList(logger, serverURL)
		},
	}

	cmd.Flags().StringVar(&serverURL, "server", "", "Butler server URL (defaults to active server)")

	return cmd
}

func runList(logger *log.Logger, serverURL string) error {
	creds, err := auth.LoadCredentials()
	if err != nil {
		return fmt.Errorf("loading credential file: %w", err)
	}

	// Resolve which server to display
	server := serverURL
	if server == "" {
		server = creds.ActiveServer
	}
	if server == "" {
		return fmt.Errorf("no active server; run 'butleradm login --server URL' first")
	}

	sc, ok := creds.Servers[server]
	if !ok {
		return fmt.Errorf("no credentials for server %s; run 'butleradm login --server %s' first", server, server)
	}

	fmt.Printf("Server:  %s\n", server)
	fmt.Printf("User:    %s (%s)\n", sc.User.Name, sc.User.Email)

	// Token status
	if sc.IsExpired() {
		if sc.CanRefresh() {
			fmt.Printf("Token:   expired (refresh available until %s)\n", sc.RefreshExpiresAt.Local().Format(time.RFC3339))
		} else {
			fmt.Printf("Token:   %s\n", output.Danger("expired"))
		}
	} else {
		fmt.Printf("Token:   valid until %s\n", sc.ExpiresAt.Local().Format(time.RFC3339))
	}

	fmt.Println()

	if len(sc.User.Teams) == 0 {
		fmt.Println("No team memberships.")
		return nil
	}

	table := output.NewTable(os.Stdout, "", "TEAM", "ROLE", "NAMESPACE")

	for _, tm := range sc.User.Teams {
		marker := " "
		if tm.Name == sc.ActiveTeam {
			marker = "*"
		}
		table.AddRow(marker, tm.Name, tm.Role, "team-"+tm.Name)
	}

	return table.Flush()
}

func newUseCmd(logger *log.Logger) *cobra.Command {
	var serverURL string

	cmd := &cobra.Command{
		Use:   "use TEAM",
		Short: "Switch the active team context",
		Long: `Set the active team for subsequent CLI operations. This updates
the credential file so that commands default to the team's namespace
(team-{name}).

Examples:
  # Switch to the payments team
  butleradm context use payments

  # Switch team on a specific server
  butleradm context use payments --server https://butler.example.com`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runUse(logger, args[0], serverURL)
		},
	}

	cmd.Flags().StringVar(&serverURL, "server", "", "Butler server URL (defaults to active server)")

	return cmd
}

func runUse(logger *log.Logger, team, serverURL string) error {
	creds, err := auth.LoadCredentials()
	if err != nil {
		return fmt.Errorf("loading credential file: %w", err)
	}

	server := serverURL
	if server == "" {
		server = creds.ActiveServer
	}
	if server == "" {
		return fmt.Errorf("no active server; run 'butleradm login --server URL' first")
	}

	sc, ok := creds.Servers[server]
	if !ok {
		return fmt.Errorf("no credentials for server %s", server)
	}

	// Validate that the user belongs to the requested team
	found := false
	for _, tm := range sc.User.Teams {
		if tm.Name == team {
			found = true
			break
		}
	}
	if !found {
		fmt.Printf("Available teams:\n")
		for _, tm := range sc.User.Teams {
			fmt.Printf("  %s (%s)\n", tm.Name, tm.Role)
		}
		return fmt.Errorf("you are not a member of team %q", team)
	}

	sc.ActiveTeam = team

	if err := creds.Save(); err != nil {
		return fmt.Errorf("saving credential file: %w", err)
	}

	fmt.Printf("Switched to team %s (namespace: team-%s)\n", output.Bold(team), team)

	return nil
}
