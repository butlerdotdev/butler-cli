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

// Package bootstrap implements the butleradm bootstrap command.
package bootstrap

import (
	"context"
	"fmt"
	"time"

	"github.com/butlerdotdev/butler/internal/adm/bootstrap/orchestrator"
	"github.com/butlerdotdev/butler/internal/adm/bootstrap/tui"
	"github.com/butlerdotdev/butler/internal/adm/bootstrap/wizard"
	"github.com/butlerdotdev/butler/internal/common/log"
	"github.com/spf13/cobra"
)

// NewBootstrapCmd creates the bootstrap parent command.
func NewBootstrapCmd(logger *log.Logger) *cobra.Command {
	var interactive bool

	cmd := &cobra.Command{
		Use:   "bootstrap",
		Short: "Bootstrap a Butler management cluster",
		Long: `Bootstrap creates a new Butler management cluster on the specified infrastructure provider.

The bootstrap process:
  1. Creates a temporary KIND cluster for orchestration
  2. Deploys Butler CRDs (MachineRequest, ProviderConfig, ClusterBootstrap)
  3. Deploys butler-bootstrap and butler-provider-<provider> controllers
  4. Creates a ClusterBootstrap CR from your config
  5. Watches the CR status until the cluster is ready
  6. Extracts kubeconfig to ~/.butler/<cluster>-kubeconfig
  7. Cleans up the temporary KIND cluster

The management cluster runs on your infrastructure and becomes self-managing.

Use --interactive to launch the guided wizard, or use a provider
subcommand with --config for file-based configuration.

Example:
  butleradm bootstrap --interactive
  butleradm bootstrap harvester --config bootstrap.yaml`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if interactive {
				return runInteractiveBootstrap(cmd, logger)
			}
			return cmd.Help()
		},
	}

	cmd.Flags().BoolVarP(&interactive, "interactive", "i", false,
		"launch interactive bootstrap wizard")

	// Register provider subcommands
	cmd.AddCommand(NewHarvesterCmd(logger))
	cmd.AddCommand(NewNutanixCmd(logger))
	cmd.AddCommand(NewGCPCmd(logger))
	cmd.AddCommand(NewAWSCmd(logger))
	cmd.AddCommand(NewAzureCmd(logger))

	return cmd
}

// runInteractiveBootstrap launches the wizard and then runs the TUI
// bootstrap flow with the wizard-produced config.
func runInteractiveBootstrap(cmd *cobra.Command, logger *log.Logger) error {
	cfg, err := wizard.Run()
	if err != nil {
		return err
	}

	if cfg == nil {
		return fmt.Errorf("wizard returned no configuration")
	}

	ctx, cancel := context.WithCancel(cmd.Context())
	defer cancel()

	orchOptions := orchestrator.Options{
		Timeout: 60 * time.Minute,
	}

	return tui.Run(tui.RunConfig{
		Ctx:              ctx,
		Cancel:           cancel,
		Cfg:              cfg,
		OrcOptions:       orchOptions,
		LoggerName:       logger.Name(),
		LogLevel:         logger.Level(),
		SkipPreBootstrap: true,
	})
}
