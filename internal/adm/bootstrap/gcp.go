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

// NewGCPCmd creates the gcp bootstrap subcommand
func NewGCPCmd(logger *log.Logger) *cobra.Command {
	var (
		configFile  string
		dryRun      bool
		skipCleanup bool
		localDev    bool
		repoRoot    string
	)

	cmd := &cobra.Command{
		Use:   "gcp",
		Short: "Bootstrap management cluster on Google Cloud Platform",
		Long: `Bootstrap a Butler management cluster on Google Cloud Platform.

GCP Compute Engine provides scalable virtual machines for running
Kubernetes clusters. Butler provisions Talos Linux VMs with:
  - Cilium CNI (kube-proxy replacement)
  - Longhorn distributed storage
  - FluxCD for GitOps

Cloud providers use a cloud-native load balancer for the control plane
endpoint instead of kube-vip. MetalLB is not installed on cloud.

Prerequisites:
  - Docker running locally
  - GCP service account key JSON file with Compute Engine permissions
  - VPC network and subnet configured
  - Firewall rules allowing inter-node communication

Example:
  butleradm bootstrap gcp --config bootstrap-gcp.yaml

Local Development:
  butleradm bootstrap gcp --config bootstrap-gcp.yaml --local
  butleradm bootstrap gcp --config bootstrap-gcp.yaml --local --repo-root ~/code/github.com/butlerdotdev`,
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
			if cfg.Provider != "gcp" {
				return fmt.Errorf("provider must be 'gcp', got %q", cfg.Provider)
			}

			// Validate required GCP config
			if cfg.ProviderConfig.GCP == nil {
				return fmt.Errorf("providerConfig.gcp is required")
			}
			if cfg.ProviderConfig.GCP.ServiceAccountKeyPath == "" {
				return fmt.Errorf("providerConfig.gcp.serviceAccountKeyPath is required")
			}
			if cfg.ProviderConfig.GCP.ProjectID == "" {
				return fmt.Errorf("providerConfig.gcp.projectID is required")
			}
			if cfg.ProviderConfig.GCP.Region == "" {
				return fmt.Errorf("providerConfig.gcp.region is required")
			}
			if cfg.ProviderConfig.GCP.Network == "" {
				return fmt.Errorf("providerConfig.gcp.network is required")
			}

			// Verify service account key file exists
			if _, err := os.Stat(cfg.ProviderConfig.GCP.ServiceAccountKeyPath); os.IsNotExist(err) {
				return fmt.Errorf("service account key file not found: %s", cfg.ProviderConfig.GCP.ServiceAccountKeyPath)
			}

			// Determine repo root for local dev
			if localDev && repoRoot == "" {
				home, _ := os.UserHomeDir()
				repoRoot = home + "/code/github.com/butlerdotdev"
			}

			// Create orchestrator
			orch := orchestrator.New(logger, orchestrator.Options{
				DryRun:      dryRun,
				SkipCleanup: skipCleanup,
				Timeout:     60 * time.Minute,
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
