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
	"os"
	"time"

	"github.com/butlerdotdev/butler/internal/adm/bootstrap/orchestrator"
	"github.com/butlerdotdev/butler/internal/common/log"
	"github.com/spf13/cobra"
)

// NewLocalCmd creates the local bootstrap subcommand. It stands up the entire Butler
// platform on a local KIND cluster, with tenant worker nodes running as containers via
// the Cluster API Docker provider (CAPD). No hypervisor or cloud account is required.
func NewLocalCmd(logger *log.Logger) *cobra.Command {
	var (
		clusterName string
		repoRoot    string
		skipCleanup bool
	)

	cmd := &cobra.Command{
		Use:   "local",
		Short: "Bootstrap a Butler management cluster locally on KIND (no hypervisor)",
		Long: `Bootstrap a Butler management cluster on a local KIND cluster.

This runs the entire Butler platform on your laptop using Docker and KIND. Tenant
worker nodes run as containers via the Cluster API Docker provider (CAPD), so no
hypervisor or cloud account is required. The KIND cluster is kept after bootstrap so
you can create tenant clusters against it.

Prerequisites:
  • Docker running locally
  • kind and kubectl installed
  • The butlerdotdev repos checked out as siblings (for building images from source)

Example:
  butleradm bootstrap local`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := setupSignalHandler(cmd, logger)
			defer cancel()

			if repoRoot == "" {
				home, _ := os.UserHomeDir()
				repoRoot = home + "/code/github.com/butlerdotdev"
			}

			cfg := localConfig(clusterName)

			orch := orchestrator.New(logger, orchestrator.Options{
				SkipCleanup: skipCleanup,
				Timeout:     30 * time.Minute,
				LocalDev:    true,
				RepoRoot:    repoRoot,
			})
			return orch.Run(ctx, cfg)
		},
	}

	cmd.Flags().StringVar(&clusterName, "name", "butler-local", "management cluster name")
	cmd.Flags().StringVar(&repoRoot, "repo-root", "", "path to butlerdotdev repos (default: ~/code/github.com/butlerdotdev)")
	cmd.Flags().BoolVar(&skipCleanup, "skip-cleanup", false, "no-op for local (the KIND cluster is always kept); accepted for parity")

	return cmd
}

// localConfig builds the bootstrap Config for the local provider with laptop defaults:
// single control-plane node, Cilium CNI, MetalLB (pool derived from the kind network),
// CAPI/CAPD for tenant workers, and the locally built butler-controller image. Longhorn
// and kube-vip are intentionally omitted (kind's default StorageClass is used; no HA VIP).
func localConfig(name string) *orchestrator.Config {
	if name == "" {
		name = "butler-local"
	}
	return &orchestrator.Config{
		Provider: "local",
		Cluster: orchestrator.ClusterConfig{
			Name:     name,
			Topology: "single-node",
			// CPU/MemoryMB/DiskGB are schema-required VM specs. They are unused for
			// the local provider (the KIND node is the control plane, not a VM), but
			// must satisfy the ClusterBootstrap CRD minimums.
			ControlPlane: orchestrator.NodePoolConfig{
				Replicas: 1,
				CPU:      2,
				MemoryMB: 2048,
				DiskGB:   20,
			},
		},
		Network: orchestrator.NetworkConfig{
			PodCIDR:     "10.244.0.0/16",
			ServiceCIDR: "10.96.0.0/12",
			// LoadBalancerPool is derived from the kind docker network at runtime.
		},
		// Talos.Version is unused for local (no Talos nodes) but the CRD requires a
		// valid vX.Y.Z value.
		Talos: orchestrator.TalosConfig{Version: "v1.9.3"},
		Addons: orchestrator.AddonsConfig{
			// The management cluster keeps KIND's default CNI (kindnet); installing
			// Cilium here would conflict. Tenant clusters get Cilium separately.
			CNI:          orchestrator.CNIConfig{Type: "none"},
			Storage:      orchestrator.StorageConfig{Type: "none"},
			LoadBalancer: orchestrator.LoadBalancerConfig{Type: "metallb"},
			CAPI:         orchestrator.CAPIConfig{Version: "v1.9.4"},
			// ButlerController image is left at its default (ghcr...butler-controller:latest),
			// which matches the image buildAndLoadImages builds and loads for local.
			// GetButlerControllerImage appends the version, so setting an image with a tag
			// here would produce a doubled :latest:latest reference.
		},
	}
}
