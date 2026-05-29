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

// Package image implements butlerctl image commands for managing ImageSync resources.
package image

import (
	"context"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/butlerdotdev/butler/internal/common/client"
	"github.com/butlerdotdev/butler/internal/common/log"
	"github.com/butlerdotdev/butler/internal/common/output"
	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

const (
	// DefaultNamespace is the default namespace for ImageSync resources.
	DefaultNamespace = "butler-system"
)

// NewImageCmd creates the image parent command.
func NewImageCmd(logger *log.Logger) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "image",
		Args:  cobra.NoArgs,
		RunE:  output.ShowHelp,
		Short: "Manage OS images and image sync operations",
		Long: `Manage OS images synced from the Butler Image Factory to infrastructure providers.

Butler Image Sync provides a CRD-driven workflow for building, transferring,
and registering OS images (Talos, Flatcar, etc.) on your infrastructure
providers (Harvester, Nutanix, Proxmox).

Commands:
  list        List ImageSync resources
  sync        Create a new ImageSync to sync an image to a provider
  status      Get detailed status of an ImageSync
  delete      Delete an ImageSync resource
  catalog     Show available images from the factory

Examples:
  # List all image syncs
  butlerctl image list

  # Sync a Talos image to a provider
  butlerctl image sync --schematic-id ce4c9805 --version v1.12.4 --provider butler-system/harvester-prod

  # Check sync status
  butlerctl image status my-image-sync -n butler-system

  # Delete an image sync
  butlerctl image delete my-image-sync -n butler-system`,
	}

	cmd.AddCommand(newListCmd(logger))
	cmd.AddCommand(newSyncCmd(logger))
	cmd.AddCommand(newStatusCmd(logger))
	cmd.AddCommand(newDeleteCmd(logger))
	cmd.AddCommand(newCatalogCmd(logger))

	return cmd
}

// ---------------------------------------------------------------------------
// image list
// ---------------------------------------------------------------------------

type listOptions struct {
	namespace    string
	outputFormat string
	kubeconfig   string
	provider     string
	status       string
	schematic    string
}

func newListCmd(logger *log.Logger) *cobra.Command {
	opts := &listOptions{}

	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List ImageSync resources",
		Long: `List ImageSync resources that track image sync operations.

By default, lists ImageSyncs in the butler-system namespace.

Examples:
  # List all image syncs
  butlerctl image list

  # Filter by provider
  butlerctl image list --provider harvester-prod

  # Filter by status
  butlerctl image list --status Ready

  # Filter by schematic ID prefix
  butlerctl image list --schematic ce4c9805`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runList(cmd.Context(), logger, opts)
		},
	}

	cmd.Flags().StringVarP(&opts.namespace, "namespace", "n", DefaultNamespace, "Namespace to list ImageSyncs from")
	cmd.Flags().StringVarP(&opts.outputFormat, "output", "o", "table", "Output format (table, json, yaml)")
	cmd.Flags().StringVar(&opts.kubeconfig, "kubeconfig", "", "Path to kubeconfig file")
	cmd.Flags().StringVar(&opts.provider, "provider", "", "Filter by provider name")
	cmd.Flags().StringVar(&opts.status, "status", "", "Filter by phase (Pending, Building, Downloading, Uploading, Ready, Failed)")
	cmd.Flags().StringVar(&opts.schematic, "schematic", "", "Filter by schematic ID prefix")

	return cmd
}

func runList(ctx context.Context, logger *log.Logger, opts *listOptions) error {
	format, err := output.ParseFormat(opts.outputFormat)
	if err != nil {
		return err
	}

	c, err := client.New(opts.kubeconfig, "")
	if err != nil {
		return err
	}

	list, err := c.Dynamic.Resource(client.ImageSyncGVR).Namespace(opts.namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("listing ImageSyncs in namespace %s: %w", opts.namespace, err)
	}

	// Apply filters
	var filtered []unstructured.Unstructured
	for _, item := range list.Items {
		if opts.provider != "" {
			providerName := client.GetNestedString(item.Object, "spec", "providerConfigRef", "name")
			if providerName != opts.provider {
				continue
			}
		}
		if opts.status != "" {
			phase := client.GetNestedString(item.Object, "status", "phase")
			if !strings.EqualFold(phase, opts.status) {
				continue
			}
		}
		if opts.schematic != "" {
			schematicID := client.GetNestedString(item.Object, "spec", "factoryRef", "schematicID")
			if !strings.HasPrefix(schematicID, opts.schematic) {
				continue
			}
		}
		filtered = append(filtered, item)
	}

	// Sort by namespace then name
	sort.Slice(filtered, func(i, j int) bool {
		if filtered[i].GetNamespace() != filtered[j].GetNamespace() {
			return filtered[i].GetNamespace() < filtered[j].GetNamespace()
		}
		return filtered[i].GetName() < filtered[j].GetName()
	})

	printer := output.NewPrinter(format, os.Stdout)

	if format == output.FormatJSON || format == output.FormatYAML {
		outputData := make([]map[string]interface{}, len(filtered))
		for i, item := range filtered {
			outputData[i] = extractImageSyncData(&item)
		}
		return printer.Print(outputData, nil)
	}

	return printer.Print(nil, func(w io.Writer) error {
		return printImageSyncTable(w, filtered)
	})
}

func printImageSyncTable(w io.Writer, items []unstructured.Unstructured) error {
	headers := []string{"NAME", "PROVIDER", "PHASE", "SCHEMATIC", "VERSION", "IMAGE REF", "AGE"}
	table := output.NewTable(w, headers...)

	for _, item := range items {
		name := item.GetName()
		provider := client.GetNestedString(item.Object, "spec", "providerConfigRef", "name")
		phase := client.GetNestedString(item.Object, "status", "phase")
		schematicID := client.GetNestedString(item.Object, "spec", "factoryRef", "schematicID")
		version := client.GetNestedString(item.Object, "spec", "factoryRef", "version")
		imageRef := client.GetNestedString(item.Object, "status", "providerImageRef")

		// Truncate schematic ID to first 8 chars
		if len(schematicID) > 8 {
			schematicID = schematicID[:8]
		}

		if phase == "" {
			phase = "Pending"
		}

		if imageRef == "" {
			imageRef = "-"
		}

		age := formatAge(item.GetCreationTimestamp().Time)

		table.AddRow(
			name,
			provider,
			output.ColorizePhase(phase),
			schematicID,
			version,
			imageRef,
			age,
		)
	}

	return table.Flush()
}

// ---------------------------------------------------------------------------
// image sync
// ---------------------------------------------------------------------------

type syncOptions struct {
	schematicID  string
	version      string
	provider     string
	arch         string
	format       string
	transferMode string
	displayName  string
	namespace    string
	kubeconfig   string
	name         string
}

func newSyncCmd(logger *log.Logger) *cobra.Command {
	opts := &syncOptions{
		arch:         "amd64",
		format:       "qcow2",
		transferMode: "direct",
		namespace:    DefaultNamespace,
	}

	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Create an ImageSync to sync an image to a provider",
		Long: `Create an ImageSync resource to sync an OS image from the Butler Image Factory
to an infrastructure provider.

The --provider flag accepts the format "namespace/name" or just "name" (defaults
to butler-system namespace).

Examples:
  # Sync a Talos image to Harvester
  butlerctl image sync --schematic-id ce4c980550dd2ab1b17bbf2b08801c7eb59418eafe8f279833297925d67c7515 \
    --version v1.12.4 --provider harvester-prod

  # Sync with custom name and display name
  butlerctl image sync --name talos-v1.12.4-custom \
    --schematic-id ce4c9805 --version v1.12.4 \
    --provider butler-system/harvester-prod \
    --display-name "Talos v1.12.4 Custom"

  # Sync an arm64 raw image
  butlerctl image sync --schematic-id abc12345 --version v1.12.4 \
    --provider nutanix-prod --arch arm64 --format raw`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSync(cmd.Context(), logger, opts)
		},
	}

	cmd.Flags().StringVar(&opts.schematicID, "schematic-id", "", "Schematic ID from the image factory (required)")
	cmd.Flags().StringVar(&opts.version, "version", "", "OS version (e.g., v1.12.4) (required)")
	cmd.Flags().StringVar(&opts.provider, "provider", "", "Provider config reference (namespace/name or name) (required)")
	cmd.Flags().StringVar(&opts.arch, "arch", opts.arch, "CPU architecture (amd64, arm64)")
	cmd.Flags().StringVar(&opts.format, "format", opts.format, "Image format (qcow2, raw, iso)")
	cmd.Flags().StringVar(&opts.transferMode, "transfer-mode", opts.transferMode, "Transfer mode (direct, proxy)")
	cmd.Flags().StringVar(&opts.displayName, "display-name", "", "Human-readable name for the image on the provider")
	cmd.Flags().StringVarP(&opts.namespace, "namespace", "n", opts.namespace, "Namespace for the ImageSync resource")
	cmd.Flags().StringVar(&opts.kubeconfig, "kubeconfig", "", "Path to kubeconfig file")
	cmd.Flags().StringVar(&opts.name, "name", "", "Name for the ImageSync resource (auto-generated if not set)")

	_ = cmd.MarkFlagRequired("schematic-id")
	_ = cmd.MarkFlagRequired("version")
	_ = cmd.MarkFlagRequired("provider")

	return cmd
}

func runSync(ctx context.Context, logger *log.Logger, opts *syncOptions) error {
	// Parse provider reference
	providerNamespace, providerName, err := parseProviderRef(opts.provider)
	if err != nil {
		return err
	}

	// Auto-generate name if not provided
	name := opts.name
	if name == "" {
		schematicPrefix := opts.schematicID
		if len(schematicPrefix) > 8 {
			schematicPrefix = schematicPrefix[:8]
		}
		// Sanitize version for DNS name (remove 'v' prefix, replace '.' with '-')
		sanitizedVersion := strings.TrimPrefix(opts.version, "v")
		sanitizedVersion = strings.ReplaceAll(sanitizedVersion, ".", "-")
		name = fmt.Sprintf("image-%s-%s-%s", schematicPrefix, sanitizedVersion, providerName)
		// Truncate to 63 chars max (DNS-1123 limit)
		if len(name) > 63 {
			name = name[:63]
		}
		// Ensure it doesn't end with a hyphen
		name = strings.TrimRight(name, "-")
	}

	c, err := client.New(opts.kubeconfig, "")
	if err != nil {
		return err
	}

	// Build the ImageSync resource
	imageSync := &unstructured.Unstructured{}
	imageSync.SetAPIVersion("butler.butlerlabs.dev/v1alpha1")
	imageSync.SetKind("ImageSync")
	imageSync.SetName(name)
	imageSync.SetNamespace(opts.namespace)

	spec := map[string]interface{}{
		"factoryRef": map[string]interface{}{
			"schematicID": opts.schematicID,
			"version":     opts.version,
			"arch":        opts.arch,
		},
		"providerConfigRef": map[string]interface{}{
			"name": providerName,
		},
		"format":       opts.format,
		"transferMode": opts.transferMode,
	}

	if providerNamespace != "" {
		providerRef := spec["providerConfigRef"].(map[string]interface{})
		providerRef["namespace"] = providerNamespace
	}

	if opts.displayName != "" {
		spec["displayName"] = opts.displayName
	}

	imageSync.Object["spec"] = spec

	// Check if it already exists
	_, err = c.Dynamic.Resource(client.ImageSyncGVR).Namespace(opts.namespace).Get(ctx, name, metav1.GetOptions{})
	if err == nil {
		return fmt.Errorf("ImageSync %q already exists in namespace %q", name, opts.namespace)
	}
	if !errors.IsNotFound(err) {
		return fmt.Errorf("checking for existing ImageSync: %w", err)
	}

	logger.Info("creating ImageSync", "name", name, "namespace", opts.namespace)

	created, err := c.Dynamic.Resource(client.ImageSyncGVR).Namespace(opts.namespace).Create(ctx, imageSync, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("creating ImageSync: %w", err)
	}

	logger.Success("ImageSync created", "name", created.GetName())

	// Print summary
	fmt.Printf("\nImageSync %s created:\n", output.Bold(created.GetName()))
	fmt.Printf("  Namespace:     %s\n", opts.namespace)
	fmt.Printf("  Schematic ID:  %s\n", opts.schematicID)
	fmt.Printf("  Version:       %s\n", opts.version)
	fmt.Printf("  Arch:          %s\n", opts.arch)
	fmt.Printf("  Format:        %s\n", opts.format)
	fmt.Printf("  Provider:      %s\n", opts.provider)
	fmt.Printf("  Transfer Mode: %s\n", opts.transferMode)
	if opts.displayName != "" {
		fmt.Printf("  Display Name:  %s\n", opts.displayName)
	}
	fmt.Printf("\nMonitor progress:\n")
	fmt.Printf("  butlerctl image status %s -n %s\n", name, opts.namespace)

	return nil
}

// ---------------------------------------------------------------------------
// image status
// ---------------------------------------------------------------------------

type statusOptions struct {
	namespace    string
	kubeconfig   string
	outputFormat string
}

func newStatusCmd(logger *log.Logger) *cobra.Command {
	opts := &statusOptions{
		namespace: DefaultNamespace,
	}

	cmd := &cobra.Command{
		Use:   "status NAME",
		Short: "Get detailed status of an ImageSync",
		Long: `Get detailed information about an ImageSync resource, including
its current phase, provider image reference, and any failure details.

Examples:
  # Get status of an image sync
  butlerctl image status my-image-sync -n butler-system

  # Output as YAML
  butlerctl image status my-image-sync -n butler-system -o yaml`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runStatus(cmd.Context(), logger, args[0], opts)
		},
	}

	cmd.Flags().StringVarP(&opts.namespace, "namespace", "n", opts.namespace, "Namespace of the ImageSync (required)")
	cmd.Flags().StringVar(&opts.kubeconfig, "kubeconfig", "", "Path to kubeconfig file")
	cmd.Flags().StringVarP(&opts.outputFormat, "output", "o", "", "Output format (yaml, json)")

	return cmd
}

func runStatus(ctx context.Context, logger *log.Logger, name string, opts *statusOptions) error {
	c, err := client.New(opts.kubeconfig, "")
	if err != nil {
		return err
	}

	imageSync, err := c.Dynamic.Resource(client.ImageSyncGVR).Namespace(opts.namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			return fmt.Errorf("ImageSync %q not found in namespace %q", name, opts.namespace)
		}
		return fmt.Errorf("getting ImageSync %s/%s: %w", opts.namespace, name, err)
	}

	// Handle structured output formats
	if opts.outputFormat == "yaml" || opts.outputFormat == "json" {
		format, _ := output.ParseFormat(opts.outputFormat)
		printer := output.NewPrinter(format, os.Stdout)
		return printer.Print(imageSync.Object, nil)
	}

	// Human-readable output
	obj := imageSync.Object
	phase := client.GetNestedString(obj, "status", "phase")
	if phase == "" {
		phase = "Pending"
	}

	schematicID := client.GetNestedString(obj, "spec", "factoryRef", "schematicID")
	version := client.GetNestedString(obj, "spec", "factoryRef", "version")
	arch := client.GetNestedString(obj, "spec", "factoryRef", "arch")
	providerName := client.GetNestedString(obj, "spec", "providerConfigRef", "name")
	providerNS := client.GetNestedString(obj, "spec", "providerConfigRef", "namespace")
	imgFormat := client.GetNestedString(obj, "spec", "format")
	transferMode := client.GetNestedString(obj, "spec", "transferMode")
	displayName := client.GetNestedString(obj, "spec", "displayName")
	providerImageRef := client.GetNestedString(obj, "status", "providerImageRef")
	artifactURL := client.GetNestedString(obj, "status", "artifactURL")
	artifactSHA := client.GetNestedString(obj, "status", "artifactSHA256")
	failureReason := client.GetNestedString(obj, "status", "failureReason")
	failureMessage := client.GetNestedString(obj, "status", "failureMessage")

	providerDisplay := providerName
	if providerNS != "" {
		providerDisplay = providerNS + "/" + providerName
	}

	fmt.Printf("Name:              %s\n", imageSync.GetName())
	fmt.Printf("Namespace:         %s\n", imageSync.GetNamespace())
	fmt.Printf("Phase:             %s\n", output.ColorizePhase(phase))
	fmt.Printf("Schematic ID:      %s\n", schematicID)
	fmt.Printf("Version:           %s\n", version)
	if arch != "" {
		fmt.Printf("Arch:              %s\n", arch)
	}
	if imgFormat != "" {
		fmt.Printf("Format:            %s\n", imgFormat)
	}
	if transferMode != "" {
		fmt.Printf("Transfer Mode:     %s\n", transferMode)
	}
	fmt.Printf("Provider Config:   %s\n", providerDisplay)
	if displayName != "" {
		fmt.Printf("Display Name:      %s\n", displayName)
	}
	if providerImageRef != "" {
		fmt.Printf("Provider Image:    %s\n", providerImageRef)
	}
	if artifactURL != "" {
		fmt.Printf("Artifact URL:      %s\n", artifactURL)
	}
	if artifactSHA != "" {
		fmt.Printf("Artifact SHA256:   %s\n", artifactSHA)
	}
	if failureReason != "" {
		fmt.Printf("Failure Reason:    %s\n", output.Danger(failureReason))
	}
	if failureMessage != "" {
		fmt.Printf("Failure Message:   %s\n", output.Danger(failureMessage))
	}

	// Print age
	age := formatAge(imageSync.GetCreationTimestamp().Time)
	fmt.Printf("Age:               %s\n", age)

	// Print conditions if available
	conditions, found, _ := unstructured.NestedSlice(obj, "status", "conditions")
	if found && len(conditions) > 0 {
		fmt.Println("\nConditions:")
		for _, c := range conditions {
			cond, ok := c.(map[string]interface{})
			if !ok {
				continue
			}
			condType := client.GetNestedString(cond, "type")
			condStatus := client.GetNestedString(cond, "status")
			reason := client.GetNestedString(cond, "reason")
			message := client.GetNestedString(cond, "message")
			line := fmt.Sprintf("  %s: %s", condType, condStatus)
			if reason != "" {
				line += fmt.Sprintf(" (%s)", reason)
			}
			if message != "" {
				line += fmt.Sprintf(" - %s", message)
			}
			fmt.Println(line)
		}
	}

	return nil
}

// ---------------------------------------------------------------------------
// image delete
// ---------------------------------------------------------------------------

type deleteOptions struct {
	namespace  string
	kubeconfig string
	force      bool
}

func newDeleteCmd(logger *log.Logger) *cobra.Command {
	opts := &deleteOptions{
		namespace: DefaultNamespace,
	}

	cmd := &cobra.Command{
		Use:   "delete NAME",
		Short: "Delete an ImageSync resource",
		Long: `Delete an ImageSync resource. This stops any in-progress sync operation
and removes the ImageSync object. It does NOT remove the image from the
provider if it was already uploaded.

Examples:
  # Delete an image sync
  butlerctl image delete my-image-sync -n butler-system

  # Delete without confirmation
  butlerctl image delete my-image-sync -n butler-system --yes`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDelete(cmd.Context(), logger, args[0], opts)
		},
	}

	cmd.Flags().StringVarP(&opts.namespace, "namespace", "n", opts.namespace, "Namespace of the ImageSync (required)")
	cmd.Flags().StringVar(&opts.kubeconfig, "kubeconfig", "", "Path to kubeconfig file")
	cmd.Flags().BoolVarP(&opts.force, "yes", "y", false, "Skip confirmation prompt")

	return cmd
}

func runDelete(ctx context.Context, logger *log.Logger, name string, opts *deleteOptions) error {
	c, err := client.New(opts.kubeconfig, "")
	if err != nil {
		return err
	}

	// Verify it exists
	_, err = c.Dynamic.Resource(client.ImageSyncGVR).Namespace(opts.namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			return fmt.Errorf("ImageSync %q not found in namespace %q", name, opts.namespace)
		}
		return fmt.Errorf("getting ImageSync: %w", err)
	}

	if !opts.force {
		fmt.Printf("Delete ImageSync %q from namespace %q? [y/N]: ", name, opts.namespace)
		var confirm string
		fmt.Scanln(&confirm)
		if strings.ToLower(strings.TrimSpace(confirm)) != "y" {
			fmt.Println("Deletion cancelled.")
			return nil
		}
	}

	logger.Info("deleting ImageSync", "name", name, "namespace", opts.namespace)

	err = c.Dynamic.Resource(client.ImageSyncGVR).Namespace(opts.namespace).Delete(ctx, name, metav1.DeleteOptions{})
	if err != nil {
		return fmt.Errorf("deleting ImageSync: %w", err)
	}

	logger.Success("ImageSync deleted", "name", name)
	return nil
}

// ---------------------------------------------------------------------------
// image catalog
// ---------------------------------------------------------------------------

func newCatalogCmd(logger *log.Logger) *cobra.Command {
	var kubeconfig string

	cmd := &cobra.Command{
		Use:   "catalog",
		Short: "Show available images from existing ImageSync resources",
		Long: `Show a summary of OS images available across all ImageSync resources.

This command aggregates information from existing ImageSync resources
to display what images are available and their sync status.

Examples:
  # Show image catalog
  butlerctl image catalog`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCatalog(cmd.Context(), logger, kubeconfig)
		},
	}

	cmd.Flags().StringVar(&kubeconfig, "kubeconfig", "", "Path to kubeconfig file")

	return cmd
}

// catalogEntry groups ImageSync resources by version.
type catalogEntry struct {
	versions    map[string]bool
	formats     map[string]bool
	readyCount  int
	totalCount  int
}

func runCatalog(ctx context.Context, logger *log.Logger, kubeconfigPath string) error {
	c, err := client.New(kubeconfigPath, "")
	if err != nil {
		return err
	}

	// List ImageSyncs across butler-system namespace
	list, err := c.Dynamic.Resource(client.ImageSyncGVR).Namespace(DefaultNamespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("listing ImageSyncs: %w", err)
	}

	if len(list.Items) == 0 {
		fmt.Println("No ImageSync resources found.")
		fmt.Println("\nTo sync an image from the factory:")
		fmt.Println("  butlerctl image sync --schematic-id <id> --version <ver> --provider <provider>")
		return nil
	}

	// Group by schematic prefix (first 8 chars) for display purposes
	entries := make(map[string]*catalogEntry)

	for _, item := range list.Items {
		schematicID := client.GetNestedString(item.Object, "spec", "factoryRef", "schematicID")
		version := client.GetNestedString(item.Object, "spec", "factoryRef", "version")
		imgFormat := client.GetNestedString(item.Object, "spec", "format")
		phase := client.GetNestedString(item.Object, "status", "phase")

		key := schematicID
		if len(key) > 8 {
			key = key[:8]
		}

		entry, ok := entries[key]
		if !ok {
			entry = &catalogEntry{
				versions: make(map[string]bool),
				formats:  make(map[string]bool),
			}
			entries[key] = entry
		}

		entry.versions[version] = true
		if imgFormat != "" {
			entry.formats[imgFormat] = true
		}
		entry.totalCount++
		if phase == "Ready" {
			entry.readyCount++
		}
	}

	// Sort schematic keys
	keys := make([]string, 0, len(entries))
	for k := range entries {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	table := output.NewTable(os.Stdout, "SCHEMATIC", "VERSIONS", "FORMATS", "READY", "TOTAL")

	for _, key := range keys {
		entry := entries[key]

		versions := sortedKeys(entry.versions)
		formats := sortedKeys(entry.formats)

		table.AddRow(
			key,
			strings.Join(versions, ", "),
			strings.Join(formats, ", "),
			fmt.Sprintf("%d", entry.readyCount),
			fmt.Sprintf("%d", entry.totalCount),
		)
	}

	return table.Flush()
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// parseProviderRef parses a provider reference in "namespace/name" or "name" format.
func parseProviderRef(ref string) (namespace, name string, err error) {
	if ref == "" {
		return "", "", fmt.Errorf("provider reference is required")
	}

	parts := strings.SplitN(ref, "/", 2)
	if len(parts) == 2 {
		return parts[0], parts[1], nil
	}
	return "", parts[0], nil
}

// extractImageSyncData extracts display-friendly data from an ImageSync resource.
func extractImageSyncData(item *unstructured.Unstructured) map[string]interface{} {
	obj := item.Object
	return map[string]interface{}{
		"name":      item.GetName(),
		"namespace": item.GetNamespace(),
		"spec": map[string]interface{}{
			"factoryRef": map[string]interface{}{
				"schematicID": client.GetNestedString(obj, "spec", "factoryRef", "schematicID"),
				"version":     client.GetNestedString(obj, "spec", "factoryRef", "version"),
				"arch":        client.GetNestedString(obj, "spec", "factoryRef", "arch"),
			},
			"providerConfigRef": map[string]interface{}{
				"name":      client.GetNestedString(obj, "spec", "providerConfigRef", "name"),
				"namespace": client.GetNestedString(obj, "spec", "providerConfigRef", "namespace"),
			},
			"format":       client.GetNestedString(obj, "spec", "format"),
			"transferMode": client.GetNestedString(obj, "spec", "transferMode"),
			"displayName":  client.GetNestedString(obj, "spec", "displayName"),
		},
		"status": map[string]interface{}{
			"phase":            client.GetNestedString(obj, "status", "phase"),
			"providerImageRef": client.GetNestedString(obj, "status", "providerImageRef"),
			"artifactURL":      client.GetNestedString(obj, "status", "artifactURL"),
			"artifactSHA256":   client.GetNestedString(obj, "status", "artifactSHA256"),
			"failureReason":    client.GetNestedString(obj, "status", "failureReason"),
			"failureMessage":   client.GetNestedString(obj, "status", "failureMessage"),
		},
		"creationTimestamp": item.GetCreationTimestamp().UTC().Format(time.RFC3339),
	}
}

// formatAge returns a human-readable age from a time.
func formatAge(t time.Time) string {
	if t.IsZero() {
		return "<unknown>"
	}
	return output.FormatAge(t)
}

// sortedKeys returns sorted keys from a map[string]bool.
func sortedKeys(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
