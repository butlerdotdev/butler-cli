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
	"github.com/butlerdotdev/butler/internal/adm/bootstrap/tui"
	"github.com/butlerdotdev/butler/internal/adm/bootstrap/wizard"
	"github.com/butlerdotdev/butler/internal/common/log"
	"github.com/butlerdotdev/butler/internal/common/output"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// NewNutanixCmd creates the nutanix bootstrap subcommand
func NewNutanixCmd(logger *log.Logger) *cobra.Command {
	var (
		configFile    string
		interactive   bool
		dryRun        bool
		skipCleanup   bool
		localDev      bool
		repoRoot      string
		prismEndpoint string
		prismUsername  string
		prismPassword string
		noTUI         bool
	)

	cmd := &cobra.Command{
		Use:   "nutanix",
		Short: "Bootstrap management cluster on Nutanix AHV",
		Long: `Bootstrap a Butler management cluster on Nutanix AHV.

Nutanix AHV is an enterprise hypervisor built into the Nutanix platform.
Butler provisions Talos Linux VMs running Kubernetes with:
  • Cilium CNI (kube-proxy replacement)
  • kube-vip for control plane HA
  • Longhorn distributed storage
  • MetalLB for LoadBalancer services
  • FluxCD for GitOps

Prerequisites:
  • Docker running locally
  • Nutanix Prism Central access (endpoint, username, password)
  • Talos image uploaded to Prism Central
  • Network subnet configured for VMs

Example:
  butleradm bootstrap nutanix --config bootstrap-nutanix.yaml
  
Local Development:
  butleradm bootstrap nutanix --config bootstrap-nutanix.yaml --local
  butleradm bootstrap nutanix --config bootstrap-nutanix.yaml --local --repo-root ~/code/github.com/butlerdotdev`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if !interactive && configFile == "" {
				return fmt.Errorf("must provide --config or use --interactive (-i)")
			}

			ctx, cancel := context.WithCancel(cmd.Context())
			defer cancel()

			var cfg *orchestrator.Config
			skipPreBootstrap := false

			if interactive {
				var err error
				cfg, err = wizard.Run("nutanix")
				if err != nil {
					return err
				}
				skipPreBootstrap = true
			} else {
				viper.SetConfigFile(configFile)
				if err := viper.ReadInConfig(); err != nil {
					return fmt.Errorf("reading config file: %w", err)
				}

				var err error
				cfg, err = orchestrator.LoadConfig()
				if err != nil {
					return fmt.Errorf("parsing config: %w", err)
				}

				if cfg.Provider != "nutanix" {
					return fmt.Errorf("provider must be 'nutanix', got %q", cfg.Provider)
				}
			}

			// Apply CLI flag overrides
			if prismEndpoint != "" || prismUsername != "" || prismPassword != "" {
				if cfg.ProviderConfig.Nutanix == nil {
					cfg.ProviderConfig.Nutanix = &orchestrator.NutanixProviderConfig{}
				}
				if prismEndpoint != "" {
					cfg.ProviderConfig.Nutanix.Endpoint = prismEndpoint
				}
				if prismUsername != "" {
					cfg.ProviderConfig.Nutanix.Username = prismUsername
				}
				if prismPassword != "" {
					cfg.ProviderConfig.Nutanix.Password = prismPassword
				}
			}

			// Validate required Nutanix config
			if cfg.ProviderConfig.Nutanix == nil {
				return fmt.Errorf("providerConfig.nutanix is required")
			}
			if cfg.ProviderConfig.Nutanix.Endpoint == "" {
				return fmt.Errorf("providerConfig.nutanix.endpoint is required")
			}
			if cfg.ProviderConfig.Nutanix.Username == "" {
				return fmt.Errorf("providerConfig.nutanix.username is required")
			}
			if cfg.ProviderConfig.Nutanix.Password == "" {
				return fmt.Errorf("providerConfig.nutanix.password is required")
			}
			if cfg.ProviderConfig.Nutanix.ClusterUUID == "" {
				return fmt.Errorf("providerConfig.nutanix.clusterUUID is required")
			}
			if cfg.ProviderConfig.Nutanix.SubnetUUID == "" {
				return fmt.Errorf("providerConfig.nutanix.subnetUUID is required")
			}

			// Determine repo root for local dev
			if localDev && repoRoot == "" {
				home, _ := os.UserHomeDir()
				repoRoot = home + "/code/github.com/butlerdotdev"
			}

			orchOptions := orchestrator.Options{
				DryRun:      dryRun,
				SkipCleanup: skipCleanup,
				Timeout:     30 * time.Minute,
				LocalDev:    localDev,
				RepoRoot:    repoRoot,
			}

			// Use TUI when stdout is a terminal and not explicitly disabled
			if output.IsTTY() && !noTUI && !dryRun {
				return tui.Run(tui.RunConfig{
					Ctx:              ctx,
					Cancel:           cancel,
					Cfg:              cfg,
					OrcOptions:       orchOptions,
					LoggerName:       logger.Name(),
					LogLevel:         logger.Level(),
					SkipPreBootstrap: skipPreBootstrap,
				})
			}

			// Non-interactive mode: handle signals directly
			sigCh := make(chan os.Signal, 1)
			signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
			go func() {
				<-sigCh
				logger.Warn("received interrupt, cleaning up...")
				cancel()
			}()

			orch := orchestrator.New(logger, orchOptions)
			if err := orch.Run(ctx, cfg); err != nil {
				return err
			}

			return nil
		},
	}

	cmd.Flags().StringVarP(&configFile, "config", "c", "", "path to bootstrap config file")
	cmd.Flags().BoolVarP(&interactive, "interactive", "i", false, "configure bootstrap interactively via wizard")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "show what would be created without executing")
	cmd.Flags().BoolVar(&skipCleanup, "skip-cleanup", false, "don't delete KIND cluster on failure (for debugging)")
	cmd.Flags().BoolVar(&localDev, "local", false, "local development mode - build and load images from source")
	cmd.Flags().StringVar(&repoRoot, "repo-root", "", "path to butlerdotdev repos (default: ~/code/github.com/butlerdotdev)")
	cmd.Flags().StringVar(&prismEndpoint, "prism-endpoint", "", "Nutanix Prism Central endpoint (overrides config file)")
	cmd.Flags().StringVar(&prismUsername, "prism-username", "", "Nutanix Prism Central username (overrides config file)")
	cmd.Flags().StringVar(&prismPassword, "prism-password", "", "Nutanix Prism Central password (overrides config file)")
	cmd.Flags().BoolVar(&noTUI, "no-tui", false, "disable interactive TUI (use line-by-line output)")
	cmd.MarkFlagsMutuallyExclusive("config", "interactive")

	return cmd
}
