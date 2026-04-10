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
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"github.com/butlerdotdev/butler/internal/common/auth"
	"github.com/butlerdotdev/butler/internal/common/client"
)

// NewTUICmd creates the "tui" cobra command that launches the interactive
// terminal dashboard. The admin parameter controls whether admin-only
// views (teams, users, health) are shown.
func NewTUICmd(admin bool) *cobra.Command {
	var kubeconfig string

	cmd := &cobra.Command{
		Use:   "tui",
		Short: "Launch the interactive terminal dashboard",
		Long: `Launch a full-screen terminal dashboard for Butler.

The dashboard provides an interactive view of clusters, addons, teams,
providers, network pools, users, and platform health.

Navigation:
  1-7    Switch views
  j/k    Move cursor
  Enter  Drill into detail
  Esc    Go back
  /      Filter
  r      Refresh
  ?      Help
  q      Quit

Examples:
  butlerctl tui
  butlerctl tui --kubeconfig ~/.butler/butler-beta-kubeconfig
  butlerctl tui --context butler-beta`,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			kubeContext, _ := cmd.Flags().GetString("context")

			c, err := client.New(kubeconfig, kubeContext)
			if err != nil {
				return fmt.Errorf("connecting to cluster: %w", err)
			}

			// Derive context display name
			contextName := kubeContext
			if contextName == "" {
				contextName = inferContextName(kubeconfig)
			}

			// Determine admin status from Butler credentials if available,
			// otherwise fall back to binary mode (butleradm = admin)
			isAdmin := admin
			creds, err := auth.LoadCredentials()
			if err == nil {
				if sc := creds.ActiveCredential(); sc != nil {
					isAdmin = sc.User.IsPlatformAdmin
				}
			}

			app := NewApp(c, contextName, isAdmin)

			p := tea.NewProgram(app, tea.WithAltScreen())
			_, err = p.Run()
			return err
		},
	}

	cmd.Flags().StringVar(&kubeconfig, "kubeconfig", "", "path to kubeconfig file")

	return cmd
}

// inferContextName tries to determine a display name for the status bar
// from the kubeconfig path or environment.
func inferContextName(kubeconfig string) string {
	if kubeconfig != "" {
		return kubeconfig
	}
	return "default context"
}
