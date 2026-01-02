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

// Package cmd implements the butleradm CLI commands.
package cmd

import (
	"github.com/butlerdotdev/butler/internal/adm/bootstrap"
	"github.com/butlerdotdev/butler/internal/common/log"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var (
	cfgFile string
	verbose bool
)

// Execute runs the butleradm CLI
func Execute(logger *log.Logger) error {
	rootCmd := NewRootCmd(logger)
	return rootCmd.Execute()
}

// NewRootCmd creates the root command for butleradm
func NewRootCmd(logger *log.Logger) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "butleradm",
		Short: "Butler Platform Administration",
		Long: `butleradm is the administration CLI for the Butler Kubernetes-as-a-Service platform.

It is designed for Platform Operators who manage the Butler infrastructure:
  • Bootstrap new management clusters
  • Upgrade Butler platform components
  • Backup and restore cluster state
  • Monitor platform health and status

Butler follows CNCF best practices with a Kubernetes-native, controller-based architecture.
All operations create Custom Resources that controllers reconcile to desired state.`,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			if verbose {
				logger.SetVerbose(true)
			}
			return initConfig(logger)
		},
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	// Global flags
	cmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default: ./bootstrap.yaml or ~/.butler/config.yaml)")
	cmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "enable verbose output")

	// Bind to viper
	viper.BindPFlag("config", cmd.PersistentFlags().Lookup("config"))

	// Register subcommands
	cmd.AddCommand(bootstrap.NewBootstrapCmd(logger))
	cmd.AddCommand(NewVersionCmd())
	// TODO: Add upgrade, backup, restore, status commands

	return cmd
}

// initConfig reads in config file and ENV variables
func initConfig(logger *log.Logger) error {
	if cfgFile != "" {
		viper.SetConfigFile(cfgFile)
	} else {
		// Search for config in current directory and home
		viper.SetConfigName("bootstrap")
		viper.SetConfigType("yaml")
		viper.AddConfigPath(".")
		viper.AddConfigPath("$HOME/.butler")
	}

	// Environment variables
	viper.AutomaticEnv()
	viper.SetEnvPrefix("BUTLER")

	// Read config if available (not required for all commands)
	if err := viper.ReadInConfig(); err == nil {
		logger.Debug("using config file", "path", viper.ConfigFileUsed())
	}

	return nil
}

// NewVersionCmd creates the version command
func NewVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Run: func(cmd *cobra.Command, args []string) {
			cmd.Println("butleradm version v0.1.0-dev")
			cmd.Println("Built with controller-based architecture")
			cmd.Println("https://github.com/butlerdotdev/butler")
		},
	}
}
