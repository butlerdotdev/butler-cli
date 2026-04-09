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
	"encoding/json"
	"fmt"
	"os"
	"os/exec"

	"github.com/butlerdotdev/butler/internal/common/client"
	"github.com/butlerdotdev/butler/internal/common/log"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/yaml"
)

// editOptions holds options for the edit command.
type editOptions struct {
	name              string
	namespace         string
	kubeconfig        string
	kubeContext       string
	workersReplicas   int32
	workersCPU        int32
	workersMemory     int32
	kubernetesVersion string
	logger            *log.Logger
	// changedFlags tracks which flags were explicitly set by the user.
	changedFlags map[string]bool
}

func newEditCmd(logger *log.Logger) *cobra.Command {
	opts := &editOptions{
		namespace: DefaultTenantNamespace,
		logger:    logger,
	}

	cmd := &cobra.Command{
		Use:   "edit NAME",
		Short: "Edit a tenant cluster configuration",
		Long: `Edit a TenantCluster resource interactively or with inline flags.

By default, opens the resource in your $EDITOR (falls back to vi).
When inline flags are provided, patches the resource directly without
opening an editor.

Interactive mode:
  Opens the TenantCluster as YAML in your editor. On save, the changes
  are applied to the cluster. If no changes are detected, the operation
  is skipped.

Inline flag mode:
  Patches specific fields without opening an editor. Useful for
  scripting and automation.

Examples:
  # Edit interactively
  butlerctl cluster edit my-cluster

  # Edit with inline flags
  butlerctl cluster edit my-cluster --workers-replicas 5
  butlerctl cluster edit my-cluster --kubernetes-version v1.32.0
  butlerctl cluster edit my-cluster --workers-replicas 3 --workers-cpu 8`,
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeClusterNames,
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.name = args[0]
			opts.kubeContext, _ = cmd.Flags().GetString("context")
			opts.changedFlags = make(map[string]bool)
			cmd.Flags().Visit(func(f *pflag.Flag) {
				opts.changedFlags[f.Name] = true
			})
			return runEdit(cmd.Context(), opts)
		},
	}

	cmd.Flags().StringVarP(&opts.namespace, "namespace", "n", opts.namespace, "namespace of the TenantCluster")
	cmd.Flags().StringVar(&opts.kubeconfig, "kubeconfig", "", "path to management cluster kubeconfig")
	cmd.Flags().Int32Var(&opts.workersReplicas, "workers-replicas", 0, "set worker node replica count")
	cmd.Flags().Int32Var(&opts.workersCPU, "workers-cpu", 0, "set worker node CPU cores")
	cmd.Flags().Int32Var(&opts.workersMemory, "workers-memory", 0, "set worker node memory in GiB")
	cmd.Flags().StringVar(&opts.kubernetesVersion, "kubernetes-version", "", "set Kubernetes version")

	return cmd
}

// hasInlineFlags reports whether any inline-edit flags were set.
func (opts *editOptions) hasInlineFlags() bool {
	return opts.changedFlags["workers-replicas"] || opts.changedFlags["workers-cpu"] ||
		opts.changedFlags["workers-memory"] || opts.changedFlags["kubernetes-version"]
}

func runEdit(ctx context.Context, opts *editOptions) error {
	c, err := client.New(opts.kubeconfig, opts.kubeContext)
	if err != nil {
		return fmt.Errorf("connecting to management cluster: %w", err)
	}

	if opts.hasInlineFlags() {
		return runEditInline(ctx, c, opts)
	}
	return runEditInteractive(ctx, c, opts)
}

// buildEditPatch constructs the merge-patch map from inline flag values.
func (opts *editOptions) buildEditPatch() map[string]interface{} {
	spec := make(map[string]interface{})

	hasWorkerFlag := opts.changedFlags["workers-replicas"] || opts.changedFlags["workers-cpu"] || opts.changedFlags["workers-memory"]
	if hasWorkerFlag {
		workers := make(map[string]interface{})
		machineTemplate := make(map[string]interface{})
		if opts.changedFlags["workers-replicas"] {
			workers["replicas"] = int64(opts.workersReplicas)
		}
		if opts.changedFlags["workers-cpu"] {
			machineTemplate["cpu"] = int64(opts.workersCPU)
		}
		if opts.changedFlags["workers-memory"] {
			machineTemplate["memoryGiB"] = int64(opts.workersMemory)
		}
		if len(machineTemplate) > 0 {
			workers["machineTemplate"] = machineTemplate
		}
		spec["workers"] = workers
	}

	if opts.kubernetesVersion != "" {
		spec["kubernetesVersion"] = opts.kubernetesVersion
	}

	return map[string]interface{}{
		"spec": spec,
	}
}

// runEditInline patches the TenantCluster using inline flags.
func runEditInline(ctx context.Context, c *client.Client, opts *editOptions) error {
	patch := opts.buildEditPatch()

	patchBytes, err := json.Marshal(patch)
	if err != nil {
		return fmt.Errorf("marshaling patch: %w", err)
	}

	_, err = c.Dynamic.Resource(client.TenantClusterGVR).Namespace(opts.namespace).Patch(
		ctx,
		opts.name,
		types.MergePatchType,
		patchBytes,
		metav1.PatchOptions{},
	)
	if err != nil {
		return fmt.Errorf("patching TenantCluster: %w", err)
	}

	opts.logger.Success("cluster updated", "name", opts.name)
	return nil
}

// runEditInteractive opens the resource in $EDITOR for interactive editing.
func runEditInteractive(ctx context.Context, c *client.Client, opts *editOptions) error {
	// Get the TenantCluster
	tc, err := c.Dynamic.Resource(client.TenantClusterGVR).Namespace(opts.namespace).Get(ctx, opts.name, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("getting TenantCluster %s/%s: %w", opts.namespace, opts.name, err)
	}

	// Marshal to YAML
	original, err := yaml.Marshal(tc.Object)
	if err != nil {
		return fmt.Errorf("marshaling TenantCluster to YAML: %w", err)
	}

	// Write to temp file
	tmpFile, err := os.CreateTemp("", fmt.Sprintf("butlerctl-edit-%s-*.yaml", opts.name))
	if err != nil {
		return fmt.Errorf("creating temp file: %w", err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.Write(original); err != nil {
		tmpFile.Close()
		return fmt.Errorf("writing temp file: %w", err)
	}
	tmpFile.Close()

	// Determine editor
	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = os.Getenv("VISUAL")
	}
	if editor == "" {
		editor = "vi"
	}

	// Open editor
	editorCmd := exec.CommandContext(ctx, editor, tmpFile.Name())
	editorCmd.Stdin = os.Stdin
	editorCmd.Stdout = os.Stdout
	editorCmd.Stderr = os.Stderr

	if err := editorCmd.Run(); err != nil {
		return fmt.Errorf("editor exited with error: %w", err)
	}

	// Read back the edited file
	edited, err := os.ReadFile(tmpFile.Name())
	if err != nil {
		return fmt.Errorf("reading edited file: %w", err)
	}

	// Check if anything changed
	if string(edited) == string(original) {
		opts.logger.Info("edit cancelled, no changes made")
		return nil
	}

	// Parse the edited YAML
	updated := &unstructured.Unstructured{}
	if err := yaml.Unmarshal(edited, &updated.Object); err != nil {
		return fmt.Errorf("parsing edited YAML: %w", err)
	}

	// Preserve the resourceVersion for optimistic concurrency
	updated.SetResourceVersion(tc.GetResourceVersion())

	// Apply the update
	_, err = c.Dynamic.Resource(client.TenantClusterGVR).Namespace(opts.namespace).Update(ctx, updated, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("updating TenantCluster: %w", err)
	}

	opts.logger.Success("cluster updated", "name", opts.name)
	return nil
}
