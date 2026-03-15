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

// NewAWSCmd creates the aws bootstrap subcommand
func NewAWSCmd(logger *log.Logger) *cobra.Command {
	var (
		configFile      string
		interactive     bool
		dryRun          bool
		skipCleanup     bool
		localDev        bool
		repoRoot        string
		accessKeyID     string
		secretAccessKey string
		noTUI           bool
	)

	cmd := &cobra.Command{
		Use:   "aws",
		Short: "Bootstrap management cluster on Amazon Web Services",
		Long: `Bootstrap a Butler management cluster on Amazon Web Services.

AWS EC2 provides scalable virtual machines for running Kubernetes clusters.
Butler provisions Talos Linux VMs with:
  - Cilium CNI (kube-proxy replacement)
  - Longhorn distributed storage
  - FluxCD for GitOps

Cloud providers do NOT use kube-vip or MetalLB. The first control plane
node's public IP serves as the API endpoint.

Prerequisites:
  - Docker running locally
  - AWS access key ID and secret access key with EC2 permissions
  - VPC with subnet and security group configured
  - Security group allowing ports: 6443, 50000, 50001, 4240 (TCP),
    8472 (UDP), ICMP

Example:
  butleradm bootstrap aws --config bootstrap-aws.yaml

Local Development:
  butleradm bootstrap aws --config bootstrap-aws.yaml --local`,
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
				cfg, err = wizard.Run("aws")
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

				if cfg.Provider != "aws" {
					return fmt.Errorf("provider must be 'aws', got %q", cfg.Provider)
				}
			}

			// Apply CLI flag overrides
			if accessKeyID != "" || secretAccessKey != "" {
				if cfg.ProviderConfig.AWS == nil {
					cfg.ProviderConfig.AWS = &orchestrator.AWSProviderConfig{}
				}
				if accessKeyID != "" {
					cfg.ProviderConfig.AWS.AccessKeyID = accessKeyID
				}
				if secretAccessKey != "" {
					cfg.ProviderConfig.AWS.SecretAccessKey = secretAccessKey
				}
			}

			if cfg.ProviderConfig.AWS == nil {
				return fmt.Errorf("providerConfig.aws is required")
			}
			if cfg.ProviderConfig.AWS.AccessKeyID == "" {
				return fmt.Errorf("providerConfig.aws.accessKeyID is required")
			}
			if cfg.ProviderConfig.AWS.SecretAccessKey == "" {
				return fmt.Errorf("providerConfig.aws.secretAccessKey is required")
			}
			if cfg.ProviderConfig.AWS.Region == "" {
				return fmt.Errorf("providerConfig.aws.region is required")
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
	cmd.Flags().StringVar(&accessKeyID, "access-key-id", "", "AWS access key ID (overrides config file)")
	cmd.Flags().StringVar(&secretAccessKey, "secret-access-key", "", "AWS secret access key (overrides config file)")
	cmd.Flags().BoolVar(&noTUI, "no-tui", false, "disable interactive TUI (use line-by-line output)")
	cmd.MarkFlagsMutuallyExclusive("config", "interactive")

	return cmd
}
