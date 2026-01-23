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

// Package cmd implements the butlerctl CLI commands.
package cmd

import (
	"github.com/butlerdotdev/butler/internal/common/log"
	"github.com/butlerdotdev/butler/internal/common/output"
	"github.com/butlerdotdev/butler/internal/ctl/cluster"
	"github.com/spf13/cobra"
)

var (
	verbose bool
)

// Execute runs the butlerctl CLI
func Execute(logger *log.Logger) error {
	rootCmd := NewRootCmd(logger)
	return rootCmd.Execute()
}

// NewRootCmd creates the root command for butlerctl
func NewRootCmd(logger *log.Logger) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "butlerctl",
		Short: "Butler Kubernetes Cluster Management",
		Long: `butlerctl is the cluster management CLI for the Butler platform.

It is designed for developers and platform users who work with tenant clusters:

  • Create and destroy tenant clusters
  • Scale worker nodes up and down
  • Get kubeconfig for cluster access
  • Export cluster configs for GitOps

Butler provides Kubernetes-as-a-Service with hosted control planes (Steward)
and infrastructure-agnostic worker provisioning.

Examples:
  # List all clusters
  butlerctl cluster list

  # Create a new cluster
  butlerctl cluster create my-cluster --lb-pool 10.127.14.40

  # Get kubeconfig
  butlerctl cluster kubeconfig my-cluster --merge

  # Scale workers
  butlerctl cluster scale my-cluster --workers 3

  # Destroy a cluster
  butlerctl cluster destroy my-cluster`,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			if verbose {
				logger.SetVerbose(true)
			}
			return nil
		},
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	// Configure colorized help
	output.ConfigureHelp(cmd)

	// Global flags
	cmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "enable verbose output")

	// Register subcommands
	cmd.AddCommand(cluster.NewClusterCmd(logger))
	cmd.AddCommand(NewVersionCmd())

	return cmd
}

// NewVersionCmd creates the version command
func NewVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Run: func(cmd *cobra.Command, args []string) {
			cmd.Println(output.Binary("butlerctl") + " version v0.1.0-dev")
			cmd.Println("Butler Kubernetes-as-a-Service Platform")
			cmd.Println(output.Dim("https://github.com/butlerdotdev/butler"))
		},
	}
}
