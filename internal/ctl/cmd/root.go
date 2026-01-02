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
	"github.com/butlerdotdev/butler/internal/ctl/cluster"
	"github.com/spf13/cobra"
)

var verbose bool

// Execute runs the butlerctl CLI
func Execute(logger *log.Logger) error {
	rootCmd := NewRootCmd(logger)
	return rootCmd.Execute()
}

// NewRootCmd creates the root command for butlerctl
func NewRootCmd(logger *log.Logger) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "butlerctl",
		Short: "Butler Platform User CLI",
		Long: `butlerctl is the user CLI for the Butler Kubernetes-as-a-Service platform.

It is designed for Platform Users and Developers who consume the Butler platform:
  • Create and manage tenant Kubernetes clusters
  • Enable and configure platform addons
  • Manage cluster access and permissions
  • Download kubeconfig for cluster access

All operations create Custom Resources that Butler controllers reconcile.
This enables consistent behavior whether using CLI, Console, or direct API.

Examples:
  # Create a new tenant cluster
  butlerctl cluster create my-app --workers 3

  # Get kubeconfig for a cluster
  butlerctl cluster kubeconfig my-app

  # Enable monitoring addon
  butlerctl addon enable prometheus --cluster my-app

  # Grant access to a team member
  butlerctl access grant --cluster my-app --user alice@example.com --role admin`,
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			if verbose {
				logger.SetVerbose(true)
			}
		},
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	// Global flags
	cmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "enable verbose output")

	// Register subcommands
	cmd.AddCommand(cluster.NewClusterCmd(logger))
	// TODO: Add addon, access commands
	cmd.AddCommand(NewVersionCmd())

	return cmd
}

// NewVersionCmd creates the version command
func NewVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Run: func(cmd *cobra.Command, args []string) {
			cmd.Println("butlerctl version v0.1.0-dev")
			cmd.Println("Butler Kubernetes-as-a-Service Platform")
			cmd.Println("https://github.com/butlerdotdev/butler")
		},
	}
}
