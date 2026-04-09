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

package provider

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

func newCreateCmd(logger *log.Logger) *cobra.Command {
	var (
		namespace  string
		fromFile   string
		kubeconfig string
	)

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a provider configuration",
		Long: `Create a new ProviderConfig resource from a YAML file.

The YAML file should contain a complete ProviderConfig manifest including
the apiVersion, kind, metadata, and spec fields. This is equivalent to
kubectl apply but validates that the resource is a ProviderConfig.

Examples:
  # Create from a YAML file
  butleradm provider create --from-file provider-harvester.yaml

  # Create in a specific namespace
  butleradm provider create --from-file provider.yaml -n my-namespace`,
		RunE: func(cmd *cobra.Command, args []string) error {
			kubeContext, _ := cmd.Flags().GetString("context")
			return runCreate(cmd.Context(), logger, namespace, fromFile, kubeconfig, kubeContext)
		},
	}

	cmd.Flags().StringVarP(&namespace, "namespace", "n", butlerSystem, "namespace for the ProviderConfig")
	cmd.Flags().StringVar(&fromFile, "from-file", "", "path to a YAML file containing the ProviderConfig manifest (required)")
	cmd.Flags().StringVar(&kubeconfig, "kubeconfig", "", "path to management cluster kubeconfig")

	_ = cmd.MarkFlagRequired("from-file")

	return cmd
}

func runCreate(ctx context.Context, logger *log.Logger, namespace, fromFile, kubeconfigPath, kubeContext string) error {
	// Read the YAML file
	data, err := os.ReadFile(fromFile)
	if err != nil {
		return fmt.Errorf("reading file %s: %w", fromFile, err)
	}

	// Parse into unstructured object
	obj := &unstructured.Unstructured{}
	if err := yaml.Unmarshal(data, &obj.Object); err != nil {
		return fmt.Errorf("parsing YAML from %s: %w", fromFile, err)
	}

	// Validate it looks like a ProviderConfig
	kind := obj.GetKind()
	if kind != "" && kind != "ProviderConfig" {
		return fmt.Errorf("expected kind ProviderConfig, got %s", kind)
	}

	// Set apiVersion and kind if not present
	if obj.GetAPIVersion() == "" {
		obj.SetAPIVersion("butler.butlerlabs.dev/v1alpha1")
	}
	if obj.GetKind() == "" {
		obj.SetKind("ProviderConfig")
	}

	// Override namespace if specified
	if namespace != "" {
		obj.SetNamespace(namespace)
	}
	if obj.GetNamespace() == "" {
		obj.SetNamespace(butlerSystem)
	}

	name := obj.GetName()
	if name == "" {
		return fmt.Errorf("metadata.name is required in the YAML file")
	}

	c, err := client.New(kubeconfigPath, kubeContext)
	if err != nil {
		return fmt.Errorf("connecting to management cluster: %w", err)
	}

	_, err = c.Dynamic.Resource(client.ProviderConfigGVR).Namespace(obj.GetNamespace()).Create(ctx, obj, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("creating ProviderConfig: %w", err)
	}

	logger.Success("provider configuration created", "name", name, "namespace", obj.GetNamespace())
	return nil
}
