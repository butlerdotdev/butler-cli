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
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/butlerdotdev/butler/internal/common/log"
	"github.com/butlerdotdev/butler/internal/common/output"
	"github.com/butlerdotdev/butler/internal/common/serverhttp"
	"github.com/spf13/cobra"
)

// The management gitops lifecycle verbs mirror the tenant verbs in
// butlerctl cluster gitops (Feature 2.2), but target the single well-known
// management cluster: no NAME argument, no namespace. The HTTP contract is the
// boundary, so the request/response types are mirrored here (the package
// convention; see config.go). butler-server resolves the management cluster.
//
// disableManageConfirm is the typed phrase that management disable requires.
// It is deliberately not the cluster name and there is no --yes shortcut, so a
// habitual --yes cannot tear down the platform's own Flux.
const disableManageConfirm = "uninstall-management-flux"

const (
	gitopsInventoryTimeout = 2 * time.Minute
	gitopsEnableTimeout    = 5 * time.Minute
	gitopsExportTimeout    = 5 * time.Minute
)

// mgmtGitOpsStatus mirrors butler-server's gitops.GitOpsStatusResponse
// (GET /api/management/gitops/status).
type mgmtGitOpsStatus struct {
	Enabled    bool   `json:"enabled"`
	Provider   string `json:"provider,omitempty"`
	Repository string `json:"repository,omitempty"`
	Branch     string `json:"branch,omitempty"`
	Path       string `json:"path,omitempty"`
	Status     string `json:"status,omitempty"`
	Version    string `json:"version,omitempty"`
}

type discoveredRelease struct {
	Name         string `json:"name"`
	Namespace    string `json:"namespace"`
	Chart        string `json:"chart"`
	ChartVersion string `json:"chartVersion"`
	Status       string `json:"status"`
}

type gitopsEngineStatus struct {
	Provider  string `json:"provider,omitempty"`
	Installed bool   `json:"installed"`
	Ready     bool   `json:"ready"`
	Version   string `json:"version,omitempty"`
}

type discoveryResult struct {
	Matched      []discoveredRelease `json:"matched"`
	Unmatched    []discoveredRelease `json:"unmatched"`
	GitOpsEngine *gitopsEngineStatus `json:"gitopsEngine,omitempty"`
}

type previewRequest struct {
	Env         string `json:"env,omitempty"`
	ClusterName string `json:"clusterName,omitempty"`
}

type previewSummary struct {
	FileCount  int `json:"fileCount"`
	Collisions int `json:"collisions"`
	Failures   int `json:"failures"`
}

type previewResult struct {
	ClusterName string            `json:"clusterName"`
	Files       map[string]string `json:"files"`
	Summary     previewSummary    `json:"summary"`
	Coverage    json.RawMessage   `json:"coverage,omitempty"`
}

type exportRequest struct {
	Env           string `json:"env,omitempty"`
	ClusterName   string `json:"clusterName,omitempty"`
	Repository    string `json:"repository"`
	Branch        string `json:"branch"`
	CreatePR      bool   `json:"createPR,omitempty"`
	PRTitle       string `json:"prTitle,omitempty"`
	PRBody        string `json:"prBody,omitempty"`
	CommitMessage string `json:"commitMessage,omitempty"`
}

type exportResult struct {
	Success    bool     `json:"success"`
	Message    string   `json:"message"`
	Mode       string   `json:"mode,omitempty"`
	Branch     string   `json:"branch,omitempty"`
	CommitSHA  string   `json:"commitSha,omitempty"`
	PRURL      string   `json:"prUrl,omitempty"`
	PRNumber   int      `json:"prNumber,omitempty"`
	FilesCount int      `json:"filesCount,omitempty"`
	Files      []string `json:"files,omitempty"`
}

type enableRequest struct {
	Provider   string `json:"provider,omitempty"`
	Repository string `json:"repository"`
	Branch     string `json:"branch,omitempty"`
	Path       string `json:"path,omitempty"`
	Private    bool   `json:"private,omitempty"`
}

type enableResult struct {
	Success       bool   `json:"success"`
	Message       string `json:"message"`
	RepositoryURL string `json:"repositoryUrl"`
	Provider      string `json:"provider"`
	Version       string `json:"version,omitempty"`
}

type disableResult struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

type enableOptions struct {
	repository string
	branch     string
	path       string
	provider   string
	private    bool
	yes        bool
}

type disableOptions struct {
	confirm string
}

type previewOptions struct {
	env         string
	clusterName string
}

type exportOptions struct {
	env           string
	clusterName   string
	repository    string
	branch        string
	createPR      bool
	prTitle       string
	prBody        string
	commitMessage string
}

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

// validateRepoFullName checks that a --repo value is in owner/repo form, which
// is what butler-server expects (it parses owner and repo from the last "/").
// A full URL is the common mistake, so it gets a specific hint. GitLab subgroup
// paths are allowed: only the last segment is the repo.
func validateRepoFullName(repo string) error {
	if strings.Contains(repo, "://") || strings.HasPrefix(repo, "git@") {
		return fmt.Errorf("--repo must be in owner/repo form (for example acme/clusters), not a URL: %q", repo)
	}
	idx := strings.LastIndex(repo, "/")
	if idx <= 0 || idx == len(repo)-1 {
		return fmt.Errorf("--repo must be in owner/repo form (for example acme/clusters): %q", repo)
	}
	return nil
}

// --- status ---

func newGitopsStatusCmd(_ *log.Logger) *cobra.Command {
	var outputFormat string
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show live GitOps status for the management cluster",
		Long: `Show the live GitOps status for the management cluster.

Queries butler-server for the management cluster's current GitOps state:
whether GitOps is enabled, the provider, repository, branch, path, and sync
status.

Exit codes:
  0  status retrieved
  1  client-side error or unexpected server error

Examples:
  butleradm gitops status
  butleradm gitops status -o json`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runMgmtStatus(cmd.Context(), os.Stdout, outputFormat)
		},
	}
	cmd.Flags().StringVarP(&outputFormat, "output", "o", "", "output format (json, yaml)")
	return cmd
}

func runMgmtStatus(ctx context.Context, out io.Writer, outputFormat string) error {
	sh, err := serverhttp.New()
	if err != nil {
		return err
	}
	var st mgmtGitOpsStatus
	if err := sh.Get(ctx, "/api/management/gitops/status", &st); err != nil {
		return translateServerError(err)
	}
	switch outputFormat {
	case "json":
		return output.PrintJSON(out, st)
	case "yaml":
		return output.PrintYAML(out, st)
	case "", "table":
		return printMgmtStatus(out, st)
	default:
		return fmt.Errorf("unsupported output format %q (use json or yaml)", outputFormat)
	}
}

func printMgmtStatus(out io.Writer, st mgmtGitOpsStatus) error {
	if !st.Enabled {
		fmt.Fprintln(out, "GitOps is not enabled on the management cluster.")
		fmt.Fprintln(out, "Enable it with: butleradm gitops enable --repo <owner/repo>")
		return nil
	}
	t := output.NewTable(out, "FIELD", "VALUE")
	t.AddRow("Enabled", "true")
	t.AddRow("Provider", st.Provider)
	t.AddRow("Repository", st.Repository)
	t.AddRow("Branch", st.Branch)
	if st.Path != "" {
		t.AddRow("Path", st.Path)
	}
	if st.Status != "" {
		t.AddRow("Status", st.Status)
	}
	if st.Version != "" {
		t.AddRow("Version", st.Version)
	}
	return t.Flush()
}

// --- discover ---

func newGitopsDiscoverCmd(_ *log.Logger) *cobra.Command {
	var outputFormat string
	cmd := &cobra.Command{
		Use:   "discover",
		Short: "Discover Helm releases and GitOps engine state on the management cluster",
		Long: `Discover the Helm releases running on the management cluster and the state of
its GitOps engine.

Releases are reported as matched (recognized as Butler addons) or unmatched.

Exit codes:
  0  discovery completed
  1  client-side error or server error

Examples:
  butleradm gitops discover
  butleradm gitops discover -o json`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runMgmtDiscover(cmd.Context(), os.Stdout, outputFormat)
		},
	}
	cmd.Flags().StringVarP(&outputFormat, "output", "o", "", "output format (json, yaml)")
	return cmd
}

func runMgmtDiscover(ctx context.Context, out io.Writer, outputFormat string) error {
	sh, err := serverhttp.NewWithTimeout(gitopsInventoryTimeout)
	if err != nil {
		return err
	}
	var res discoveryResult
	if err := sh.Get(ctx, "/api/management/gitops/discover", &res); err != nil {
		return translateServerError(err)
	}
	switch outputFormat {
	case "json":
		return output.PrintJSON(out, res)
	case "yaml":
		return output.PrintYAML(out, res)
	case "", "table":
		return printDiscovery(out, res)
	default:
		return fmt.Errorf("unsupported output format %q (use json or yaml)", outputFormat)
	}
}

func printDiscovery(out io.Writer, res discoveryResult) error {
	if res.GitOpsEngine != nil {
		e := res.GitOpsEngine
		fmt.Fprintf(out, "GitOps engine: provider=%s installed=%t ready=%t", orDefault(e.Provider, "none"), e.Installed, e.Ready)
		if e.Version != "" {
			fmt.Fprintf(out, " version=%s", e.Version)
		}
		fmt.Fprintln(out)
	}
	printReleaseSection(out, "Matched (Butler addons)", res.Matched)
	printReleaseSection(out, "Unmatched", res.Unmatched)
	return nil
}

func printReleaseSection(out io.Writer, title string, releases []discoveredRelease) {
	fmt.Fprintf(out, "\n%s: %d\n", title, len(releases))
	if len(releases) == 0 {
		return
	}
	t := output.NewTable(out, "NAME", "NAMESPACE", "CHART", "VERSION", "STATUS")
	for _, r := range releases {
		t.AddRow(r.Name, r.Namespace, r.Chart, r.ChartVersion, r.Status)
	}
	_ = t.Flush()
}

// --- preview ---

func newGitopsPreviewCmd(_ *log.Logger) *cobra.Command {
	opts := &previewOptions{}
	var outputFormat string
	cmd := &cobra.Command{
		Use:   "preview",
		Short: "Preview the GitOps layout for the management cluster",
		Long: `Render the GitOps layout that would be written for the management cluster
without writing anything. Non-mutating.

Exit codes:
  0  preview rendered
  1  client-side error or server error

Examples:
  butleradm gitops preview
  butleradm gitops preview -o json`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runMgmtPreview(cmd.Context(), os.Stdout, outputFormat, opts)
		},
	}
	cmd.Flags().StringVar(&opts.env, "env", "", "apps overlay name (defaults server-side to prd)")
	cmd.Flags().StringVar(&opts.clusterName, "cluster-name", "", "directory name emitted under clusters/ (defaults server-side)")
	cmd.Flags().StringVarP(&outputFormat, "output", "o", "", "output format (json, yaml)")
	return cmd
}

func runMgmtPreview(ctx context.Context, out io.Writer, outputFormat string, opts *previewOptions) error {
	sh, err := serverhttp.NewWithTimeout(gitopsInventoryTimeout)
	if err != nil {
		return err
	}
	req := previewRequest{Env: opts.env, ClusterName: opts.clusterName}
	var res previewResult
	if err := sh.Post(ctx, "/api/management/gitops/preview-cluster", req, &res); err != nil {
		return translateServerError(err)
	}
	switch outputFormat {
	case "json":
		return output.PrintJSON(out, res)
	case "yaml":
		return output.PrintYAML(out, res)
	case "", "table":
		return printPreview(out, res)
	default:
		return fmt.Errorf("unsupported output format %q (use json or yaml)", outputFormat)
	}
}

func printPreview(out io.Writer, res previewResult) error {
	fmt.Fprintf(out, "Cluster directory: %s\n", res.ClusterName)
	fmt.Fprintf(out, "Files: %d   Collisions: %d   Failures: %d\n", res.Summary.FileCount, res.Summary.Collisions, res.Summary.Failures)
	if len(res.Files) == 0 {
		return nil
	}
	paths := make([]string, 0, len(res.Files))
	for p := range res.Files {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	fmt.Fprintln(out, "\nFiles that would be written:")
	for _, p := range paths {
		fmt.Fprintf(out, "  %s\n", p)
	}
	return nil
}

// --- export ---

func newGitopsExportCmd(_ *log.Logger) *cobra.Command {
	opts := &exportOptions{}
	var outputFormat string
	cmd := &cobra.Command{
		Use:   "export",
		Short: "Export the management cluster's inventory to a GitOps repository",
		Long: `Export the management cluster's full inventory to a GitOps repository.

butler-server discovers the cluster's Helm and native inventory, renders the
GitOps layout, and writes it to the target repository (direct push, or a pull
request with --create-pr). Run 'gitops preview' first to see what would be
written.

The --repo value is the repository in owner/repo form (for example
acme/clusters), not a URL.

Exit codes:
  0  export completed
  1  client-side error or server error

Examples:
  butleradm gitops export --repo acme/clusters
  butleradm gitops export --repo acme/clusters --create-pr --pr-title "GitOps export"`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runMgmtExport(cmd.Context(), os.Stdout, outputFormat, opts)
		},
	}
	cmd.Flags().StringVar(&opts.repository, "repo", "", "target GitOps repository in owner/repo form, e.g. acme/clusters (required)")
	cmd.Flags().StringVar(&opts.branch, "branch", "main", "repository branch")
	cmd.Flags().BoolVar(&opts.createPR, "create-pr", false, "open a pull request instead of pushing directly")
	cmd.Flags().StringVar(&opts.prTitle, "pr-title", "", "pull request title (with --create-pr)")
	cmd.Flags().StringVar(&opts.prBody, "pr-body", "", "pull request body (with --create-pr)")
	cmd.Flags().StringVar(&opts.commitMessage, "commit-message", "", "commit message for the export")
	cmd.Flags().StringVar(&opts.env, "env", "", "apps overlay name (defaults server-side to prd)")
	cmd.Flags().StringVar(&opts.clusterName, "cluster-name", "", "directory name emitted under clusters/ (defaults server-side)")
	cmd.Flags().StringVarP(&outputFormat, "output", "o", "", "output format (json, yaml)")
	_ = cmd.MarkFlagRequired("repo")
	return cmd
}

func buildExportRequest(opts *exportOptions) exportRequest {
	return exportRequest{
		Env:           opts.env,
		ClusterName:   opts.clusterName,
		Repository:    opts.repository,
		Branch:        opts.branch,
		CreatePR:      opts.createPR,
		PRTitle:       opts.prTitle,
		PRBody:        opts.prBody,
		CommitMessage: opts.commitMessage,
	}
}

func runMgmtExport(ctx context.Context, out io.Writer, outputFormat string, opts *exportOptions) error {
	if err := validateRepoFullName(opts.repository); err != nil {
		return err
	}
	sh, err := serverhttp.NewWithTimeout(gitopsExportTimeout)
	if err != nil {
		return err
	}
	var res exportResult
	if err := sh.Post(ctx, "/api/management/gitops/export-cluster", buildExportRequest(opts), &res); err != nil {
		return translateServerError(err)
	}
	switch outputFormat {
	case "json":
		return output.PrintJSON(out, res)
	case "yaml":
		return output.PrintYAML(out, res)
	case "", "table":
		return printExportResult(out, res)
	default:
		return fmt.Errorf("unsupported output format %q (use json or yaml)", outputFormat)
	}
}

func printExportResult(out io.Writer, res exportResult) error {
	fmt.Fprintf(out, "Export complete (%d files, mode=%s).\n", res.FilesCount, orDefault(res.Mode, "unknown"))
	if res.PRURL != "" {
		fmt.Fprintf(out, "Pull request: %s\n", res.PRURL)
	}
	if res.Branch != "" {
		fmt.Fprintf(out, "Branch: %s\n", res.Branch)
	}
	if res.CommitSHA != "" {
		fmt.Fprintf(out, "Commit: %s\n", res.CommitSHA)
	}
	return nil
}

// --- enable ---

func newGitopsEnableCmd(_ *log.Logger) *cobra.Command {
	opts := &enableOptions{}
	cmd := &cobra.Command{
		Use:   "enable",
		Short: "Enable (bootstrap) GitOps on the management cluster",
		Long: `Enable GitOps on the management cluster: butler-server bootstraps the GitOps
engine (Flux) against the given repository.

The management cluster typically already runs Flux (bootstrapped at platform
setup). Running enable re-bootstraps it against the given repository, which can
re-point or conflict with the platform's existing GitOps. It therefore requires
confirmation; pass --yes for non-interactive use.

The --repo value is the repository in owner/repo form (for example
acme/clusters), not a URL.

Exit codes:
  0  GitOps enabled
  1  client-side error, confirmation declined, or server error

Examples:
  butleradm gitops enable --repo acme/clusters
  butleradm gitops enable --repo acme/clusters --yes`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runMgmtEnable(cmd.Context(), os.Stdin, os.Stdout, opts)
		},
	}
	cmd.Flags().StringVar(&opts.repository, "repo", "", "GitOps repository in owner/repo form, e.g. acme/clusters (required)")
	cmd.Flags().StringVar(&opts.branch, "branch", "main", "repository branch")
	cmd.Flags().StringVar(&opts.path, "path", "", "path within the repository (defaults server-side to clusters/management)")
	cmd.Flags().StringVar(&opts.provider, "provider", "github", "git provider (github, gitlab)")
	cmd.Flags().BoolVar(&opts.private, "private", false, "treat the repository as private")
	cmd.Flags().BoolVar(&opts.yes, "yes", false, "skip the interactive confirmation prompt")
	_ = cmd.MarkFlagRequired("repo")
	return cmd
}

// confirmMgmtEnable confirms a management re-bootstrap. --yes skips the prompt;
// otherwise the operator must type "yes". Extracted so it is unit-testable.
func confirmMgmtEnable(opts *enableOptions, in io.Reader, out io.Writer) error {
	if opts.yes {
		return nil
	}
	fmt.Fprintln(out, "This bootstraps Flux on the MANAGEMENT cluster and can re-point or conflict")
	fmt.Fprint(out, "with the platform's existing GitOps. Type 'yes' to confirm: ")
	line, _ := bufio.NewReader(in).ReadString('\n')
	if strings.TrimSpace(line) != "yes" {
		return fmt.Errorf("confirmation declined; aborted")
	}
	return nil
}

func runMgmtEnable(ctx context.Context, in io.Reader, out io.Writer, opts *enableOptions) error {
	if err := validateRepoFullName(opts.repository); err != nil {
		return err
	}
	if err := confirmMgmtEnable(opts, in, out); err != nil {
		return err
	}

	sh, err := serverhttp.NewWithTimeout(gitopsEnableTimeout)
	if err != nil {
		return err
	}
	req := enableRequest{
		Provider:   opts.provider,
		Repository: opts.repository,
		Branch:     opts.branch,
		Path:       opts.path,
		Private:    opts.private,
	}
	var res enableResult
	if err := sh.Post(ctx, "/api/management/gitops/enable", req, &res); err != nil {
		return translateServerError(err)
	}
	fmt.Fprintln(out, "GitOps enabled on the management cluster.")
	t := output.NewTable(out, "FIELD", "VALUE")
	t.AddRow("Provider", orDefault(res.Provider, opts.provider))
	t.AddRow("Repository", res.RepositoryURL)
	if res.Version != "" {
		t.AddRow("Version", res.Version)
	}
	return t.Flush()
}

// --- disable ---

func newGitopsDisableCmd(_ *log.Logger) *cobra.Command {
	opts := &disableOptions{}
	cmd := &cobra.Command{
		Use:   "disable",
		Short: "Disable (uninstall) GitOps on the management cluster",
		Long: `Disable GitOps on the management cluster: butler-server uninstalls Flux.

DANGER: this is the highest-blast-radius operation in the gitops command set.
It tears down the management cluster's Flux, which manages the platform's own
components (butler-controller, butler-console) and the image automation that
deploys releases. The platform's GitOps stops working until Flux is bootstrapped
again.

There is no --yes shortcut. To proceed you must pass the exact phrase:
  --confirm ` + disableManageConfirm + `
or type that phrase when prompted.

Exit codes:
  0  GitOps disabled, or it was not enabled
  1  client-side error, confirmation mismatch, or unexpected server error

Examples:
  butleradm gitops disable --confirm ` + disableManageConfirm,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runMgmtDisable(cmd.Context(), os.Stdin, os.Stdout, opts)
		},
	}
	cmd.Flags().StringVar(&opts.confirm, "confirm", "", "typed confirmation phrase ("+disableManageConfirm+") to proceed")
	return cmd
}

// confirmMgmtDisable enforces the explicit typed phrase. There is deliberately
// no --yes path: a habitual --yes must not be able to tear down platform Flux.
// Returns nil only when the exact phrase is supplied (by flag or prompt).
func confirmMgmtDisable(opts *disableOptions, in io.Reader, out io.Writer) error {
	if opts.confirm == disableManageConfirm {
		return nil
	}
	if opts.confirm != "" {
		return fmt.Errorf("confirmation %q does not match %q; aborted", opts.confirm, disableManageConfirm)
	}
	fmt.Fprintln(out, "Disabling GitOps on the MANAGEMENT cluster uninstalls Flux, which manages the")
	fmt.Fprintln(out, "platform's own components (butler-controller, butler-console) and the image")
	fmt.Fprintln(out, "automation that deploys releases. This is destructive.")
	fmt.Fprintf(out, "Type %q to confirm: ", disableManageConfirm)
	line, _ := bufio.NewReader(in).ReadString('\n')
	if strings.TrimSpace(line) != disableManageConfirm {
		return fmt.Errorf("confirmation did not match %q; aborted", disableManageConfirm)
	}
	return nil
}

func runMgmtDisable(ctx context.Context, in io.Reader, out io.Writer, opts *disableOptions) error {
	if err := confirmMgmtDisable(opts, in, out); err != nil {
		return err
	}

	sh, err := serverhttp.New()
	if err != nil {
		return err
	}
	var res disableResult
	if err := sh.Delete(ctx, "/api/management/gitops", &res); err != nil {
		var se *serverhttp.ServerError
		if errors.As(err, &se) && se.IsNotFound() {
			fmt.Fprintln(out, "GitOps is not enabled on the management cluster; nothing to disable.")
			return nil
		}
		return translateServerError(err)
	}
	fmt.Fprintln(out, "GitOps disabled on the management cluster.")
	return nil
}
