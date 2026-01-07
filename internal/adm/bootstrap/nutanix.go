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

// NewNutanixCmd creates the nutanix bootstrap subcommand
func NewNutanixCmd(logger *log.Logger) *cobra.Command {
	var (
		configFile  string
		dryRun      bool
		skipCleanup bool
		localDev    bool
		repoRoot    string
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
			if cfg.Provider != "nutanix" {
				return fmt.Errorf("provider must be 'nutanix', got %q", cfg.Provider)
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
