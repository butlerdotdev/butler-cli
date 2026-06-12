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
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"github.com/butlerdotdev/butler/internal/adm/bootstrap/orchestrator"
	"github.com/butlerdotdev/butler/internal/common/auth"
	"github.com/butlerdotdev/butler/internal/common/client"
	"github.com/butlerdotdev/butler/internal/common/log"
	"github.com/butlerdotdev/butler/internal/tui/bootstrap"
	"github.com/butlerdotdev/butler/internal/tui/wizard"
)

// NewTUICmd creates the "tui" cobra command that launches the interactive
// terminal dashboard. The admin parameter controls whether admin-only
// views (bootstrap, teams, users, health) are shown.
func NewTUICmd(admin bool) *cobra.Command {
	var kubeconfig string
	var skipCleanup bool
	var fromSource bool

	cmd := &cobra.Command{
		Use:   "tui",
		Short: "Launch the interactive terminal dashboard",
		Long: `Launch a full-screen terminal dashboard for Butler.

The dashboard provides an interactive view of clusters, addons, teams,
providers, network pools, users, and platform health. Platform admins
additionally get tab 0 which launches the bootstrap wizard for creating
a new Butler management cluster.

Navigation:
  0      Bootstrap (admin only)
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
  butlerctl tui --context butler-beta
  butleradm tui    # starts on bootstrap tab if no kubeconfig is set
  butleradm tui --skip-cleanup   # preserve KIND cluster on bootstrap failure`,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			kubeContext, _ := cmd.Flags().GetString("context")
			return runDashboardLoop(kubeconfig, kubeContext, admin, skipCleanup, fromSource)
		},
	}

	cmd.Flags().StringVar(&kubeconfig, "kubeconfig", "", "path to kubeconfig file")
	cmd.Flags().BoolVar(&skipCleanup, "skip-cleanup", false, "don't delete KIND cluster on bootstrap failure (for debugging)")
	cmd.Flags().BoolVar(&fromSource, "from-source", false, "for a local bootstrap, build component images from the sibling butlerdotdev repos instead of pulling published images (for development)")

	return cmd
}

// runDashboardLoop is the dashboard entry point. It launches the dashboard,
// and if the user presses Enter on the bootstrap launcher (tab 0), it quits
// the dashboard cleanly, runs the wizard + bootstrap TUI as a standalone
// sub-flow, then relaunches the dashboard with the freshly-bootstrapped
// cluster's kubeconfig. This nested-TUI pattern works around Bubbletea's
// exclusive grab on stdin — huh forms and bootstrap's own tea.Program can't
// literally nest inside the dashboard tea.Program.
func runDashboardLoop(kubeconfig, kubeContext string, admin bool, skipCleanup bool, fromSource bool) error {
	for {
		// 1. Try to build a Kubernetes client. A missing or invalid
		//    kubeconfig is not a fatal error in admin mode — the app
		//    will drop into the bootstrap tab and let the user create
		//    a new cluster.
		c, clientErr := client.New(kubeconfig, kubeContext)
		if clientErr != nil && !admin {
			return fmt.Errorf("connecting to cluster: %w", clientErr)
		}
		if clientErr != nil {
			c = nil
		}

		contextName := kubeContext
		if contextName == "" {
			contextName = inferContextName(kubeconfig)
		}

		// 2. Resolve admin status from Butler credentials if available,
		//    otherwise fall back to the binary mode (butleradm = admin).
		isAdmin := admin
		if creds, err := auth.LoadCredentials(); err == nil {
			if sc := creds.ActiveCredential(); sc != nil {
				isAdmin = sc.User.IsPlatformAdmin
			}
		}

		// 3. Run the dashboard.
		app := NewApp(c, contextName, isAdmin)
		p := tea.NewProgram(app, tea.WithAltScreen())
		if _, err := p.Run(); err != nil {
			return err
		}

		// 4. Dashboard quit normally unless the user asked to bootstrap.
		if !app.BootstrapRequested {
			return nil
		}

		// 5. Bootstrap requested. Run the wizard to collect config, then
		//    hand off to the bootstrap TUI for live progress. If the
		//    wizard or bootstrap fails, fall back to the dashboard so the
		//    user isn't kicked out of the app.
		cfg, err := wizard.Run()
		if err != nil {
			fmt.Fprintf(os.Stderr, "\nwizard error: %v\n", err)
			fmt.Fprintf(os.Stderr, "Press Enter to return to the dashboard...\n")
			fmt.Scanln()
			continue
		}

		if err := runBootstrap(cfg, skipCleanup, fromSource); err != nil {
			fmt.Fprintf(os.Stderr, "\nbootstrap error: %v\n", err)
			fmt.Fprintf(os.Stderr, "Press Enter to return to the dashboard...\n")
			fmt.Scanln()
			continue
		}

		// 6. Bootstrap succeeded — the orchestrator wrote the new kubeconfig
		//    to ~/.butler/<name>-kubeconfig. Relaunch the dashboard pointing
		//    at it.
		home, _ := os.UserHomeDir()
		kubeconfig = filepath.Join(home, ".butler", cfg.Cluster.Name+"-kubeconfig")
		kubeContext = ""
	}
}

// runBootstrap hands a wizard-assembled Config to the bootstrap TUI.
// Signal handling and orchestrator cleanup are delegated to bootstrap.Run.
func runBootstrap(cfg *orchestrator.Config, skipCleanup bool, fromSource bool) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	logger := log.New("butleradm")

	orcOpts := orchestrator.Options{
		Timeout:     30 * time.Minute,
		SkipCleanup: skipCleanup,
	}
	// The local provider pulls published component images from ghcr by default, so
	// the wizard flow needs no source checkout. With --from-source it builds the
	// controller, bootstrap, and steward images from the sibling butlerdotdev repos
	// instead (for iterating on those components before they are published).
	if cfg.Provider == "local" && fromSource {
		orcOpts.LocalDev = true
		home, _ := os.UserHomeDir()
		orcOpts.RepoRoot = filepath.Join(home, "code", "github.com", "butlerdotdev")
	}

	return bootstrap.Run(bootstrap.RunConfig{
		Ctx:        ctx,
		Cancel:     cancel,
		Cfg:        cfg,
		OrcOptions: orcOpts,
		LoggerName: logger.Name(),
		LogLevel:   logger.Level(),
	})
}

// inferContextName tries to determine a display name for the status bar
// from the kubeconfig path or environment.
func inferContextName(kubeconfig string) string {
	if kubeconfig != "" {
		return kubeconfig
	}
	return "default context"
}
