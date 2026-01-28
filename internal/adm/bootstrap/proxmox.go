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

package bootstrap

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/butlerdotdev/butler/internal/adm/bootstrap/orchestrator"
	"github.com/butlerdotdev/butler/internal/common/log"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// NewProxmoxCmd creates the proxmox bootstrap subcommand
func NewProxmoxCmd(logger *log.Logger) *cobra.Command {
	var (
		configFile  string
		dryRun      bool
		skipCleanup bool
		localDev    bool
		repoRoot    string
	)

	cmd := &cobra.Command{
		Use:   "proxmox",
		Short: "Bootstrap a Butler management cluster on Proxmox VE",
		Long: `Bootstrap creates a new Butler management cluster on Proxmox VE.

This command will:
1. Create a local KIND cluster for orchestration
2. Deploy Butler CRDs and controllers
3. Create VMs on your Proxmox cluster
4. Install Talos Linux and bootstrap Kubernetes
5. Install Butler platform components
6. Clean up the KIND cluster

Prerequisites:
  • Docker running locally
  • Proxmox VE access (API Token with appropriate permissions)
  • Talos image uploaded to Proxmox VE

Example:
  butleradm bootstrap proxmox --config bootstrap-proxmox.yaml
  
Local Development:
  butleradm bootstrap proxmox --config bootstrap-proxmox.yaml --local
  butleradm bootstrap proxmox --config bootstrap-proxmox.yaml --local --repo-root ~/code/github.com/butlerdotdev`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Handle interrupts gracefully
			ctx, cancel := context.WithCancel(cmd.Context())
			defer cancel()

			sigCh := make(chan os.Signal, 1)
			signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
			go func() {
				<-sigCh
				logger.Info("Interrupt received, shutting down...")
				cancel()
			}()

			// Load configuration
			if configFile != "" {
				viper.SetConfigFile(configFile)
				if err := viper.ReadInConfig(); err != nil {
					return fmt.Errorf("reading config file: %w", err)
				}
			}

			// Parse Config
			cfg, err := orchestrator.LoadConfig()
			if err != nil {
				return fmt.Errorf("parsing config: %w", err)
			}

			// Validate provider
			if cfg.Provider != "proxmox" {
				return fmt.Errorf("provider must be 'proxmox', got %q", cfg.Provider)
			}

			// Validate required Proxmox configs
			if cfg.ProviderConfig.Proxmox == nil {
				return fmt.Errorf("providerConfig.proxmox is required")
			}
			if cfg.ProviderConfig.Proxmox.Endpoint == "" {
				return fmt.Errorf("providerConfig.proxmox.endpoint is required")
			}
			if len(cfg.ProviderConfig.Proxmox.Nodes) == 0 {
				return fmt.Errorf("providerConfig.proxmox.nodes is required")
			}

			if localDev && repoRoot == "" {
				// Try to find repo root automatically
				home, _ := os.UserHomeDir()
				repoRoot = home + "/code/github.com/butlerdotdev"
			}

			// Create orchestrator
			orch := orchestrator.New(logger, orchestrator.Options{
				DryRun:      dryRun,
				SkipCleanup: skipCleanup,
				Timeout:     30 * time.Minute,
				LocalDev:    localDev,
				RepoRoot:    repoRoot,
			})

			// Run bootstrap
			if err := orch.Run(ctx, cfg); err != nil {
				return err
			}

			return nil
		},
	}

	cmd.Flags().StringP("config", "c", "", "Path to bootstrap configuration file (required)")
	cmd.MarkFlagRequired("config")

	return cmd
}
