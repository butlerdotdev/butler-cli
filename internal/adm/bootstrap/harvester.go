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

// NewHarvesterCmd creates the harvester bootstrap subcommand
func NewHarvesterCmd(logger *log.Logger) *cobra.Command {
	var (
		configFile  string
		dryRun      bool
		skipCleanup bool
		localDev    bool
		repoRoot    string
	)

	cmd := &cobra.Command{
		Use:   "harvester",
		Short: "Bootstrap management cluster on Harvester HCI",
		Long: `Bootstrap a Butler management cluster on Harvester HCI.

Harvester is a modern open-source hyperconverged infrastructure (HCI) platform
built on Kubernetes. Butler provisions Talos Linux VMs running Kubernetes with:
  • Cilium CNI (kube-proxy replacement)
  • kube-vip for control plane HA
  • Longhorn distributed storage
  • MetalLB for LoadBalancer services
  • FluxCD for GitOps

Prerequisites:
  • Docker running locally
  • Harvester kubeconfig at ~/.butler/harvester-kubeconfig (or specified in config)
  • Talos image with qemu-guest-agent extension in Harvester

Example:
  butleradm bootstrap harvester --config bootstrap.yaml
  
Local Development:
  butleradm bootstrap harvester --config bootstrap.yaml --local
  butleradm bootstrap harvester --config bootstrap.yaml --local --repo-root ~/code/github.com/butlerdotdev`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Handle interrupts gracefully
			ctx, cancel := context.WithCancel(cmd.Context())
			defer cancel()

			sigCh := make(chan os.Signal, 1)
			signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
			go func() {
				<-sigCh
				logger.Warn("received interrupt, cleaning up...")
				cancel()
			}()

			// Load config
			if configFile != "" {
				viper.SetConfigFile(configFile)
				if err := viper.ReadInConfig(); err != nil {
					return fmt.Errorf("reading config file: %w", err)
				}
			}

			// Parse config
			cfg, err := orchestrator.LoadConfig()
			if err != nil {
				return fmt.Errorf("parsing config: %w", err)
			}

			// Validate provider
			if cfg.Provider != "harvester" {
				return fmt.Errorf("provider must be 'harvester', got %q", cfg.Provider)
			}

			// Determine repo root for local dev
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

	cmd.Flags().StringVarP(&configFile, "config", "c", "", "path to bootstrap config file (required)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "show what would be created without executing")
	cmd.Flags().BoolVar(&skipCleanup, "skip-cleanup", false, "don't delete KIND cluster on failure (for debugging)")
	cmd.Flags().BoolVar(&localDev, "local", false, "local development mode - build and load images from source")
	cmd.Flags().StringVar(&repoRoot, "repo-root", "", "path to butlerdotdev repos (default: ~/code/github.com/butlerdotdev)")

	cmd.MarkFlagRequired("config")

	return cmd
}
