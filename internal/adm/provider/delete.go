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

	"github.com/butlerdotdev/butler/internal/common/client"
	"github.com/butlerdotdev/butler/internal/common/log"
	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func newDeleteCmd(logger *log.Logger) *cobra.Command {
	var (
		namespace  string
		kubeconfig string
	)

	cmd := &cobra.Command{
		Use:   "delete NAME",
		Short: "Delete a provider configuration",
		Long: `Delete a ProviderConfig resource from the management cluster.

This removes the provider configuration. Existing clusters using this
provider are not affected, but no new clusters can be created with it.

Examples:
  # Delete a provider
  butleradm provider delete harvester-prod

  # Delete from a specific namespace
  butleradm provider delete harvester-prod -n my-namespace`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			kubeContext, _ := cmd.Flags().GetString("context")
			return runDelete(cmd.Context(), logger, args[0], namespace, kubeconfig, kubeContext)
		},
	}

	cmd.Flags().StringVarP(&namespace, "namespace", "n", butlerSystem, "namespace of the ProviderConfig")
	cmd.Flags().StringVar(&kubeconfig, "kubeconfig", "", "path to management cluster kubeconfig")

	return cmd
}

func runDelete(ctx context.Context, logger *log.Logger, name, namespace, kubeconfigPath, kubeContext string) error {
	c, err := client.New(kubeconfigPath, kubeContext)
	if err != nil {
		return fmt.Errorf("connecting to management cluster: %w", err)
	}

	err = c.Dynamic.Resource(client.ProviderConfigGVR).Namespace(namespace).Delete(ctx, name, metav1.DeleteOptions{})
	if err != nil {
		return fmt.Errorf("deleting ProviderConfig %s: %w", name, err)
	}

	logger.Success("provider configuration deleted", "name", name, "namespace", namespace)
	return nil
}
