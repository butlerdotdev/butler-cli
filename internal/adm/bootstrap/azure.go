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

// NewAzureCmd creates the azure bootstrap subcommand
func NewAzureCmd(logger *log.Logger) *cobra.Command {
	var (
		configFile     string
		interactive    bool
		dryRun         bool
		skipCleanup    bool
		localDev       bool
		repoRoot       string
		clientID       string
		clientSecret   string
		tenantID       string
		subscriptionID string
		noTUI          bool
	)

	cmd := &cobra.Command{
		Use:   "azure",
		Short: "Bootstrap management cluster on Microsoft Azure",
		Long: `Bootstrap a Butler management cluster on Microsoft Azure.

Azure Virtual Machines provide scalable compute for running Kubernetes
clusters. Butler provisions Talos Linux VMs with:
  - Cilium CNI (kube-proxy replacement)
  - Longhorn distributed storage
  - FluxCD for GitOps

Cloud providers do NOT use kube-vip or MetalLB. The first control plane
node's public IP serves as the API endpoint.

Prerequisites:
  - Docker running locally
  - Azure service principal with Compute/Network Contributor permissions
  - Resource group, VNet, subnet, and NSG configured
  - NSG allowing ports: 6443, 50000, 50001, 4240 (TCP),
    8472 (UDP), ICMP

Example:
  butleradm bootstrap azure --config bootstrap-azure.yaml

Local Development:
  butleradm bootstrap azure --config bootstrap-azure.yaml --local`,
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
				cfg, err = wizard.Run("azure")
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

				if cfg.Provider != "azure" {
					return fmt.Errorf("provider must be 'azure', got %q", cfg.Provider)
				}
			}

			// Apply CLI flag overrides
			if clientID != "" || clientSecret != "" || tenantID != "" || subscriptionID != "" {
				if cfg.ProviderConfig.Azure == nil {
					cfg.ProviderConfig.Azure = &orchestrator.AzureProviderConfig{}
				}
				if clientID != "" {
					cfg.ProviderConfig.Azure.ClientID = clientID
				}
				if clientSecret != "" {
					cfg.ProviderConfig.Azure.ClientSecret = clientSecret
				}
				if tenantID != "" {
					cfg.ProviderConfig.Azure.TenantID = tenantID
				}
				if subscriptionID != "" {
					cfg.ProviderConfig.Azure.SubscriptionID = subscriptionID
				}
			}

			if cfg.ProviderConfig.Azure == nil {
				return fmt.Errorf("providerConfig.azure is required")
			}
			if cfg.ProviderConfig.Azure.ClientID == "" {
				return fmt.Errorf("providerConfig.azure.clientID is required")
			}
			if cfg.ProviderConfig.Azure.ClientSecret == "" {
				return fmt.Errorf("providerConfig.azure.clientSecret is required")
			}
			if cfg.ProviderConfig.Azure.TenantID == "" {
				return fmt.Errorf("providerConfig.azure.tenantID is required")
			}
			if cfg.ProviderConfig.Azure.SubscriptionID == "" {
				return fmt.Errorf("providerConfig.azure.subscriptionID is required")
			}
			if cfg.ProviderConfig.Azure.ResourceGroup == "" {
				return fmt.Errorf("providerConfig.azure.resourceGroup is required")
			}
			if cfg.ProviderConfig.Azure.Location == "" {
				return fmt.Errorf("providerConfig.azure.location is required")
			}

			if localDev && repoRoot == "" {
				home, _ := os.UserHomeDir()
				repoRoot = home + "/code/github.com/butlerdotdev"
			}

			orchOptions := orchestrator.Options{
				DryRun:      dryRun,
				SkipCleanup: skipCleanup,
				Timeout:     60 * time.Minute,
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
	cmd.Flags().StringVar(&clientID, "client-id", "", "Azure service principal app ID (overrides config file)")
	cmd.Flags().StringVar(&clientSecret, "client-secret", "", "Azure service principal password (overrides config file)")
	cmd.Flags().StringVar(&tenantID, "tenant-id", "", "Azure tenant ID (overrides config file)")
	cmd.Flags().StringVar(&subscriptionID, "subscription-id", "", "Azure subscription ID (overrides config file)")
	cmd.Flags().BoolVar(&noTUI, "no-tui", false, "disable interactive TUI (use line-by-line output)")
	cmd.MarkFlagsMutuallyExclusive("config", "interactive")

	return cmd
}
