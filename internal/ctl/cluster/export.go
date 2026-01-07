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

package cluster

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/butlerdotdev/butler/internal/common/client"
	"github.com/butlerdotdev/butler/internal/common/log"
	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"
)

// ExportOptions holds options for the export command.
type ExportOptions struct {
	// Cluster selection
	Name         string
	Namespace    string
	AllClusters  bool
	AllNamespace bool

	// Output control
	OutputPath    string
	AsName        string // Rename the exported cluster
	IncludeStatus bool

	// Internal
	Logger *log.Logger
}

// DefaultExportOptions returns ExportOptions with sensible defaults.
func DefaultExportOptions(logger *log.Logger) *ExportOptions {
	return &ExportOptions{
		Namespace: DefaultTenantNamespace,
		Logger:    logger,
	}
}

// NewExportCmd creates the cluster export command.
func NewExportCmd(logger *log.Logger) *cobra.Command {
	opts := DefaultExportOptions(logger)

	cmd := &cobra.Command{
		Use:   "export NAME",
		Short: "Export a cluster's declarative configuration",
		Long: `Export a TenantCluster's configuration as clean, reusable YAML.

Unlike 'kubectl get -o yaml', this produces clean output suitable for:
- Checking into Git (GitOps workflows)
- Using as a template for new clusters
- Sharing for support/debugging
- Disaster recovery backups

The output strips:
- metadata.resourceVersion, uid, creationTimestamp, generation
- metadata.managedFields (the 50+ lines of noise)
- status (unless --include-status is specified)

Examples:
  # Export to stdout
  butlerctl cluster export my-cluster

  # Export to file
  butlerctl cluster export my-cluster -o my-cluster.yaml

  # Export as a template with new name
  butlerctl cluster export my-cluster --as team-beta-cluster

  # Export all clusters to a directory
  butlerctl cluster export --all -o clusters/

  # Include status for debugging
  butlerctl cluster export my-cluster --include-status`,
		Args:              cobra.MaximumNArgs(1),
		ValidArgsFunction: completeClusterNames,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Get name from args if not using --all
			if len(args) > 0 {
				opts.Name = args[0]
			}

			// Resolve namespace from flag
			if ns, _ := cmd.Flags().GetString("namespace"); ns != "" {
				opts.Namespace = ns
			}

			return runExport(cmd.Context(), opts)
		},
	}

	cmd.Flags().StringVarP(&opts.OutputPath, "output", "o", "", "Output file or directory (stdout if not specified)")
	cmd.Flags().StringVarP(&opts.Namespace, "namespace", "n", opts.Namespace, "Namespace of the TenantCluster")
	cmd.Flags().StringVar(&opts.AsName, "as", "", "Rename the cluster in the exported YAML")
	cmd.Flags().BoolVar(&opts.AllClusters, "all", false, "Export all clusters in namespace")
	cmd.Flags().BoolVarP(&opts.AllNamespace, "all-namespaces", "A", false, "Export from all namespaces (with --all)")
	cmd.Flags().BoolVar(&opts.IncludeStatus, "include-status", false, "Include status in output (excluded by default)")

	return cmd
}

// runExport executes the export operation.
func runExport(ctx context.Context, opts *ExportOptions) error {
	// Validate options
	if !opts.AllClusters && opts.Name == "" {
		return fmt.Errorf("cluster name is required (or use --all)")
	}
	if opts.AsName != "" && opts.AllClusters {
		return fmt.Errorf("--as cannot be used with --all")
	}

	c, err := client.NewFromDefault()
	if err != nil {
		return fmt.Errorf("creating client: %w", err)
	}

	// Collect clusters to export
	var clusters []unstructured.Unstructured

	if opts.AllClusters {
		clusters, err = listClustersForExport(ctx, c, opts)
		if err != nil {
			return err
		}
		if len(clusters) == 0 {
			opts.Logger.Warn("no TenantClusters found")
			return nil
		}
	} else {
		tc, err := c.Dynamic.Resource(client.TenantClusterGVR).Namespace(opts.Namespace).Get(ctx, opts.Name, metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("getting TenantCluster %q: %w", opts.Name, err)
		}
		clusters = []unstructured.Unstructured{*tc}
	}

	// Export
	if opts.AllClusters && opts.OutputPath != "" {
		return exportMultipleToDir(clusters, opts)
	}

	return exportClusters(clusters, opts)
}

// listClustersForExport lists clusters based on export options.
func listClustersForExport(ctx context.Context, c *client.Client, opts *ExportOptions) ([]unstructured.Unstructured, error) {
	if opts.AllNamespace {
		list, err := c.Dynamic.Resource(client.TenantClusterGVR).List(ctx, metav1.ListOptions{})
		if err != nil {
			return nil, fmt.Errorf("listing TenantClusters: %w", err)
		}
		return list.Items, nil
	}

	list, err := c.Dynamic.Resource(client.TenantClusterGVR).Namespace(opts.Namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("listing TenantClusters in namespace %s: %w", opts.Namespace, err)
	}
	return list.Items, nil
}

// exportClusters exports clusters to stdout or a single file.
func exportClusters(clusters []unstructured.Unstructured, opts *ExportOptions) error {
	var output strings.Builder

	for i, tc := range clusters {
		cleaned := cleanForExport(&tc, opts)

		data, err := yaml.Marshal(cleaned)
		if err != nil {
			return fmt.Errorf("marshaling YAML: %w", err)
		}

		if i > 0 {
			output.WriteString("---\n")
		}
		output.Write(data)
	}

	// Write output
	if opts.OutputPath == "" {
		fmt.Print(output.String())
		return nil
	}

	if err := os.WriteFile(opts.OutputPath, []byte(output.String()), 0644); err != nil {
		return fmt.Errorf("writing file %s: %w", opts.OutputPath, err)
	}

	opts.Logger.Success("exported to file", "path", opts.OutputPath, "clusters", len(clusters))
	return nil
}

// exportMultipleToDir exports multiple clusters to individual files in a directory.
func exportMultipleToDir(clusters []unstructured.Unstructured, opts *ExportOptions) error {
	// Create directory if needed
	if err := os.MkdirAll(opts.OutputPath, 0755); err != nil {
		return fmt.Errorf("creating directory %s: %w", opts.OutputPath, err)
	}

	for _, tc := range clusters {
		cleaned := cleanForExport(&tc, opts)

		data, err := yaml.Marshal(cleaned)
		if err != nil {
			return fmt.Errorf("marshaling YAML for %s: %w", tc.GetName(), err)
		}

		filename := fmt.Sprintf("%s.yaml", tc.GetName())
		// Include namespace in filename if exporting from multiple namespaces
		if opts.AllNamespace {
			filename = fmt.Sprintf("%s-%s.yaml", tc.GetNamespace(), tc.GetName())
		}

		path := filepath.Join(opts.OutputPath, filename)
		if err := os.WriteFile(path, data, 0644); err != nil {
			return fmt.Errorf("writing file %s: %w", path, err)
		}

		opts.Logger.Info("exported", "cluster", tc.GetName(), "file", path)
	}

	opts.Logger.Success("exported clusters", "count", len(clusters), "directory", opts.OutputPath)
	return nil
}

// cleanForExport removes noise from a TenantCluster for clean export.
// This is the core value-add over 'kubectl get -o yaml'.
func cleanForExport(tc *unstructured.Unstructured, opts *ExportOptions) map[string]interface{} {
	// Deep copy to avoid modifying original
	obj := tc.DeepCopy().Object

	// Clean metadata - keep only the essentials
	if metadata, ok := obj["metadata"].(map[string]interface{}); ok {
		cleanedMeta := map[string]interface{}{}

		// Keep name (possibly renamed)
		if opts.AsName != "" {
			cleanedMeta["name"] = opts.AsName
		} else if name, ok := metadata["name"]; ok {
			cleanedMeta["name"] = name
		}

		// Keep namespace
		if ns, ok := metadata["namespace"]; ok {
			cleanedMeta["namespace"] = ns
		}

		// Keep labels if present (user-defined, valuable)
		if labels, ok := metadata["labels"].(map[string]interface{}); ok && len(labels) > 0 {
			// Filter out system-managed labels
			cleanedLabels := filterUserLabels(labels)
			if len(cleanedLabels) > 0 {
				cleanedMeta["labels"] = cleanedLabels
			}
		}

		// Keep annotations if present (user-defined, valuable)
		if annotations, ok := metadata["annotations"].(map[string]interface{}); ok && len(annotations) > 0 {
			// Filter out system-managed annotations
			cleanedAnnotations := filterUserAnnotations(annotations)
			if len(cleanedAnnotations) > 0 {
				cleanedMeta["annotations"] = cleanedAnnotations
			}
		}

		obj["metadata"] = cleanedMeta
	}

	// Remove status unless explicitly requested
	if !opts.IncludeStatus {
		delete(obj, "status")
	}

	return obj
}

// filterUserLabels removes system-managed labels.
func filterUserLabels(labels map[string]interface{}) map[string]interface{} {
	result := make(map[string]interface{})

	systemPrefixes := []string{
		"app.kubernetes.io/managed-by",
		"controller-uid",
		"kubectl.kubernetes.io/",
		"helm.sh/",
	}

	for k, v := range labels {
		isSystem := false
		for _, prefix := range systemPrefixes {
			if strings.HasPrefix(k, prefix) {
				isSystem = true
				break
			}
		}
		if !isSystem {
			result[k] = v
		}
	}

	return result
}

// filterUserAnnotations removes system-managed annotations.
func filterUserAnnotations(annotations map[string]interface{}) map[string]interface{} {
	result := make(map[string]interface{})

	systemPrefixes := []string{
		"kubectl.kubernetes.io/",
		"kubernetes.io/",
		"control-plane.alpha.kubernetes.io/",
		"helm.sh/",
	}

	for k, v := range annotations {
		isSystem := false
		for _, prefix := range systemPrefixes {
			if strings.HasPrefix(k, prefix) {
				isSystem = true
				break
			}
		}
		if !isSystem {
			result[k] = v
		}
	}

	return result
}
