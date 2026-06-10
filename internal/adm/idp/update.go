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

package idp

import (
	"context"
	"fmt"
	"os"

	"github.com/butlerdotdev/butler/internal/common/client"
	"github.com/butlerdotdev/butler/internal/common/log"
	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"
)

func newUpdateCmd(logger *log.Logger) *cobra.Command {
	var (
		fromFile   string
		kubeconfig string
	)

	cmd := &cobra.Command{
		Use:   "update NAME",
		Short: "Update an identity provider",
		Long: `Update the spec of an existing IdentityProvider from a YAML file.

The YAML file supplies the new spec. Only spec is applied; the existing
status and resourceVersion are preserved, so a spec edit never clobbers the
controller-managed status conditions. This mirrors 'idp create --from-file'
and 'provider update --from-file'.

If the file carries metadata.name, it must match NAME.

Examples:
  # Update an identity provider from a YAML file
  butleradm idp update okta --from-file okta-updated.yaml`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			kubeContext, _ := cmd.Flags().GetString("context")
			return runUpdate(cmd.Context(), logger, args[0], fromFile, kubeconfig, kubeContext)
		},
	}

	cmd.Flags().StringVar(&fromFile, "from-file", "", "path to a YAML file containing the new IdentityProvider spec (required)")
	cmd.Flags().StringVar(&kubeconfig, "kubeconfig", "", "path to management cluster kubeconfig")

	_ = cmd.MarkFlagRequired("from-file")

	return cmd
}

func runUpdate(ctx context.Context, logger *log.Logger, name, fromFile, kubeconfigPath, kubeContext string) error {
	data, err := os.ReadFile(fromFile)
	if err != nil {
		return fmt.Errorf("reading file %s: %w", fromFile, err)
	}

	newObj := &unstructured.Unstructured{}
	if err := yaml.Unmarshal(data, &newObj.Object); err != nil {
		return fmt.Errorf("parsing YAML from %s: %w", fromFile, err)
	}

	if kind := newObj.GetKind(); kind != "" && kind != "IdentityProvider" {
		return fmt.Errorf("expected kind IdentityProvider, got %s", kind)
	}
	if fileName := newObj.GetName(); fileName != "" && fileName != name {
		return fmt.Errorf("metadata.name %q in %s does not match NAME %q", fileName, fromFile, name)
	}

	newSpec, found, err := unstructured.NestedMap(newObj.Object, "spec")
	if err != nil {
		return fmt.Errorf("reading spec from %s: %w", fromFile, err)
	}
	if !found || len(newSpec) == 0 {
		return fmt.Errorf("no spec found in %s", fromFile)
	}

	c, err := client.New(kubeconfigPath, kubeContext)
	if err != nil {
		return fmt.Errorf("connecting to management cluster: %w", err)
	}

	// IdentityProvider is cluster-scoped: no namespace.
	existing, err := c.Dynamic.Resource(client.IdentityProviderGVR).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("getting IdentityProvider %s: %w", name, err)
	}

	if err := mergeIdPSpec(existing, newSpec); err != nil {
		return err
	}

	_, err = c.Dynamic.Resource(client.IdentityProviderGVR).Update(ctx, existing, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("updating IdentityProvider: %w", err)
	}

	logger.Success("identity provider updated", "name", name)
	return nil
}

// mergeIdPSpec applies newSpec onto existing, replacing only the spec. The
// existing status, resourceVersion, and the rest of metadata are left
// untouched so a spec update never clobbers the controller-managed status
// conditions and the Update carries the resourceVersion the API server
// requires for optimistic concurrency. existing is mutated in place. This
// mirrors the provider package's mergeProviderSpec discipline.
func mergeIdPSpec(existing *unstructured.Unstructured, newSpec map[string]interface{}) error {
	if existing == nil || existing.Object == nil {
		return fmt.Errorf("existing IdentityProvider is empty")
	}
	return unstructured.SetNestedMap(existing.Object, newSpec, "spec")
}
