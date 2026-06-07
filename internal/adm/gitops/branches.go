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

package gitops

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"os"

	"github.com/butlerdotdev/butler/internal/common/log"
	"github.com/butlerdotdev/butler/internal/common/output"
	"github.com/butlerdotdev/butler/internal/common/serverhttp"
	"github.com/spf13/cobra"
)

// branch mirrors butler-server's gitops.Branch
// (GET /api/gitops/repos/branches). The HTTP contract is the boundary.
type branch struct {
	Name      string `json:"name"`
	Protected bool   `json:"protected"`
	Default   bool   `json:"default"`
}

// newBranchesCmd creates the `gitops branches` parent command.
func newBranchesCmd(logger *log.Logger) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "branches",
		Args:  cobra.NoArgs,
		RunE:  output.ShowHelp,
		Short: "List branches for a repository",
		Long: `List branches for a repository on the configured Git provider.

Examples:
  butleradm gitops branches list --repo owner/repo`,
	}

	cmd.AddCommand(newBranchesListCmd(logger))

	return cmd
}

// newBranchesListCmd creates `gitops branches list`.
func newBranchesListCmd(_ *log.Logger) *cobra.Command {
	var (
		repo         string
		outputFormat string
	)

	cmd := &cobra.Command{
		Use:   "list --repo OWNER/REPO",
		Args:  cobra.NoArgs,
		Short: "List branches for a repository",
		Long: `List branches for a repository accessible to the Git provider credential.

The --repo flag is the full repository name, for example
butlerdotdev/cluster-config.

Exit codes:
  0  branches listed
  1  client-side error or server error

Examples:
  butleradm gitops branches list --repo butlerdotdev/cluster-config
  butleradm gitops branches list --repo butlerdotdev/cluster-config -o json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runBranchesList(cmd.Context(), os.Stdout, repo, outputFormat)
		},
	}

	cmd.Flags().StringVar(&repo, "repo", "", "full repository name, e.g. owner/repo (required)")
	cmd.Flags().StringVarP(&outputFormat, "output", "o", "", "output format (json, yaml)")
	_ = cmd.MarkFlagRequired("repo")

	return cmd
}

func runBranchesList(ctx context.Context, out io.Writer, repo, outputFormat string) error {
	if repo == "" {
		return fmt.Errorf("--repo is required (full repository name, e.g. owner/repo)")
	}

	sh, err := serverhttp.New()
	if err != nil {
		return err
	}

	path := "/api/gitops/repos/branches?repo=" + url.QueryEscape(repo)
	var branches []branch
	if err := sh.Get(ctx, path, &branches); err != nil {
		return translateServerError(err)
	}

	switch outputFormat {
	case "json":
		return output.PrintJSON(out, branches)
	case "yaml":
		return output.PrintYAML(out, branches)
	case "", "table":
		return printBranchesTable(out, branches)
	default:
		return fmt.Errorf("unsupported output format %q (use json or yaml)", outputFormat)
	}
}

func printBranchesTable(out io.Writer, branches []branch) error {
	if len(branches) == 0 {
		fmt.Fprintln(out, "No branches found.")
		return nil
	}

	t := output.NewTable(out, "NAME", "DEFAULT", "PROTECTED")
	for _, b := range branches {
		t.AddRow(b.Name, fmt.Sprintf("%t", b.Default), fmt.Sprintf("%t", b.Protected))
	}
	return t.Flush()
}
