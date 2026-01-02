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
	"github.com/butlerdotdev/butler/internal/common/log"
	"github.com/spf13/cobra"
)

// NewBootstrapCmd creates the bootstrap parent command
func NewBootstrapCmd(logger *log.Logger) *cobra.Command {
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

Example:
  butleradm bootstrap harvester --config bootstrap.yaml`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.Help()
			return nil
		},
	}

	// Register provider subcommands
	cmd.AddCommand(NewHarvesterCmd(logger))
	// TODO: Add nutanix, proxmox commands

	return cmd
}
