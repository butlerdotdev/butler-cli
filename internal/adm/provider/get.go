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
	"io"
	"os"

	"github.com/butlerdotdev/butler/internal/common/client"
	"github.com/butlerdotdev/butler/internal/common/log"
	"github.com/butlerdotdev/butler/internal/common/output"
	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

type getOptions struct {
	namespace    string
	kubeconfig   string
	outputFormat string
}

func newGetCmd(logger *log.Logger) *cobra.Command {
	opts := &getOptions{}

	cmd := &cobra.Command{
		Use:   "get NAME",
		Short: "Show a provider configuration",
		Long: `Show the details of a single ProviderConfig.

The default output is a field/value summary. Use -o json or -o yaml for the
full ProviderConfig object including spec and status.

Examples:
  # Show a provider
  butleradm provider get nutanix

  # Full object as YAML
  butleradm provider get nutanix -o yaml

  # A team-scoped provider in its team namespace
  butleradm provider get edge-harvester -n team-edge`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.kubeconfig, _ = cmd.Flags().GetString("kubeconfig")
			kubeContext, _ := cmd.Flags().GetString("context")
			return runGet(cmd.Context(), os.Stdout, args[0], kubeContext, opts)
		},
	}

	cmd.Flags().StringVarP(&opts.namespace, "namespace", "n", butlerSystem, "namespace of the ProviderConfig")
	cmd.Flags().StringVar(&opts.kubeconfig, "kubeconfig", "", "path to management cluster kubeconfig")
	cmd.Flags().StringVarP(&opts.outputFormat, "output", "o", "", "output format (json, yaml)")

	return cmd
}

func runGet(ctx context.Context, out io.Writer, name, kubeContext string, opts *getOptions) error {
	c, err := client.New(opts.kubeconfig, kubeContext)
	if err != nil {
		return err
	}

	pc, err := c.Dynamic.Resource(client.ProviderConfigGVR).Namespace(opts.namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("getting ProviderConfig %s: %w", name, err)
	}

	switch opts.outputFormat {
	case "json":
		return output.PrintJSON(out, pc.Object)
	case "yaml":
		return output.PrintYAML(out, pc.Object)
	case "", "table":
		return printProviderDetail(out, pc)
	default:
		return fmt.Errorf("unsupported output format %q (use json or yaml)", opts.outputFormat)
	}
}

// printProviderDetail renders a field/value summary of a ProviderConfig. It
// builds on extractProviderInfo and adds the namespace, scope, network mode,
// and credentials secret so an operator can see the config at a glance.
func printProviderDetail(out io.Writer, pc *unstructured.Unstructured) error {
	info := extractProviderInfo(pc)

	t := output.NewTable(out, "FIELD", "VALUE")
	t.AddRow("Name", info.Name)
	t.AddRow("Namespace", pc.GetNamespace())
	t.AddRow("Provider", info.Provider)
	t.AddRow("Validated", fmt.Sprintf("%t", info.Validated))
	if info.Endpoint != "" {
		t.AddRow("Endpoint", info.Endpoint)
	}

	scope := client.GetNestedString(pc.Object, "spec", "scope", "type")
	if scope == "" {
		scope = "platform"
	}
	t.AddRow("Scope", scope)

	if mode := client.GetNestedString(pc.Object, "spec", "network", "mode"); mode != "" {
		t.AddRow("Network mode", mode)
	}
	if secret := client.GetNestedString(pc.Object, "spec", "credentialsRef", "name"); secret != "" {
		t.AddRow("Credentials secret", secret)
	}
	if info.ValidationError != "" {
		t.AddRow("Validation error", info.ValidationError)
	}
	t.AddRow("Age", output.FormatAge(pc.GetCreationTimestamp().Time))

	return t.Flush()
}
