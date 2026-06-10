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

func newCreateCmd(logger *log.Logger) *cobra.Command {
	var (
		fromFile   string
		kubeconfig string
	)

	cmd := &cobra.Command{
		Use:   "create NAME",
		Short: "Create an identity provider",
		Long: `Create an IdentityProvider resource from a YAML file.

The YAML file should contain a complete IdentityProvider manifest including
apiVersion, kind, metadata, and spec. The OAuth2 client secret is supplied by
reference: spec.oidc.clientSecretRef must point at a Secret you create
separately (for example with kubectl create secret). This matches
'provider create --from-file'.

The file's spec can carry any IdentityProvider field, including advanced
options the console form does not expose (spec.oidc.insecureSkipVerify and
spec.oidc.googleWorkspace).

If the file carries metadata.name, it must match NAME.

Examples:
  # Create from a YAML file
  butleradm idp create okta --from-file okta.yaml`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			kubeContext, _ := cmd.Flags().GetString("context")
			return runCreate(cmd.Context(), logger, args[0], fromFile, kubeconfig, kubeContext)
		},
	}

	cmd.Flags().StringVar(&fromFile, "from-file", "", "path to a YAML file containing the IdentityProvider manifest (required)")
	cmd.Flags().StringVar(&kubeconfig, "kubeconfig", "", "path to management cluster kubeconfig")

	_ = cmd.MarkFlagRequired("from-file")

	return cmd
}

func runCreate(ctx context.Context, logger *log.Logger, name, fromFile, kubeconfigPath, kubeContext string) error {
	data, err := os.ReadFile(fromFile)
	if err != nil {
		return fmt.Errorf("reading file %s: %w", fromFile, err)
	}

	obj := &unstructured.Unstructured{}
	if err := yaml.Unmarshal(data, &obj.Object); err != nil {
		return fmt.Errorf("parsing YAML from %s: %w", fromFile, err)
	}

	if kind := obj.GetKind(); kind != "" && kind != "IdentityProvider" {
		return fmt.Errorf("expected kind IdentityProvider, got %s", kind)
	}
	if fileName := obj.GetName(); fileName != "" && fileName != name {
		return fmt.Errorf("metadata.name %q in %s does not match NAME %q", fileName, fromFile, name)
	}

	if obj.GetAPIVersion() == "" {
		obj.SetAPIVersion("butler.butlerlabs.dev/v1alpha1")
	}
	obj.SetKind("IdentityProvider")
	obj.SetName(name)

	c, err := client.New(kubeconfigPath, kubeContext)
	if err != nil {
		return fmt.Errorf("connecting to management cluster: %w", err)
	}

	// IdentityProvider is cluster-scoped: no namespace.
	_, err = c.Dynamic.Resource(client.IdentityProviderGVR).Create(ctx, obj, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("creating IdentityProvider: %w", err)
	}

	logger.Success("identity provider created", "name", name)
	return nil
}
