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

// Package config implements butleradm config commands for managing
// the ButlerConfig singleton.
package config

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/butlerdotdev/butler/internal/common/client"
	"github.com/butlerdotdev/butler/internal/common/log"
	"github.com/butlerdotdev/butler/internal/common/output"
	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

const (
	// butlerConfigName is the well-known singleton name for ButlerConfig.
	butlerConfigName = "butler"
)

// NewConfigCmd creates the config parent command for butleradm.
func NewConfigCmd(logger *log.Logger) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Manage the Butler platform configuration",
		Long: `Manage the ButlerConfig singleton that controls platform-wide settings.

ButlerConfig is a cluster-scoped resource named "butler" that defines
multi-tenancy mode, default provider, team limits, and other platform
configuration.

Examples:
  butleradm config get
  butleradm config get -o yaml
  butleradm config set spec.multiTenancy.enabled=true
  butleradm config set spec.defaultProvider=harvester-prod`,
	}

	cmd.AddCommand(newGetCmd(logger))
	cmd.AddCommand(newSetCmd(logger))

	return cmd
}

func newGetCmd(logger *log.Logger) *cobra.Command {
	var (
		outputFmt  string
		kubeconfig string
	)

	cmd := &cobra.Command{
		Use:   "get",
		Short: "Get the Butler platform configuration",
		Long: `Get the ButlerConfig singleton resource.

Displays the current platform configuration including multi-tenancy
settings, default provider, and team limits.

Examples:
  butleradm config get
  butleradm config get -o yaml
  butleradm config get -o json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			kubeContext, _ := cmd.Flags().GetString("context")
			return runGet(cmd.Context(), logger, kubeconfig, kubeContext, outputFmt)
		},
	}

	cmd.Flags().StringVarP(&outputFmt, "output", "o", "", "output format (json, yaml)")
	cmd.Flags().StringVar(&kubeconfig, "kubeconfig", "", "path to management cluster kubeconfig")

	return cmd
}

func runGet(ctx context.Context, logger *log.Logger, kubeconfigPath, kubeContext, outputFormat string) error {
	c, err := client.New(kubeconfigPath, kubeContext)
	if err != nil {
		return fmt.Errorf("connecting to management cluster: %w", err)
	}

	// ButlerConfig is cluster-scoped
	bc, err := c.Dynamic.Resource(client.ButlerConfigGVR).Get(ctx, butlerConfigName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("getting ButlerConfig: %w", err)
	}

	if outputFormat == "json" {
		return output.PrintJSON(os.Stdout, bc.Object)
	}
	if outputFormat == "yaml" {
		return output.PrintYAML(os.Stdout, bc.Object)
	}

	// Human-readable table view of key fields
	mode := client.GetNestedString(bc.Object, "spec", "mode")
	defaultProvider := client.GetNestedString(bc.Object, "spec", "defaultProvider")
	mtEnabled := client.GetNestedBool(bc.Object, "spec", "multiTenancy", "enabled")
	mtMode := client.GetNestedString(bc.Object, "spec", "multiTenancy", "mode")
	gitProvider := client.GetNestedString(bc.Object, "spec", "gitProvider", "type")
	phase := client.GetNestedString(bc.Object, "status", "phase")
	age := output.FormatAge(bc.GetCreationTimestamp().Time)

	fmt.Printf("Name:               %s\n", bc.GetName())
	fmt.Printf("Mode:               %s\n", orDefault(mode, "<not set>"))
	fmt.Printf("Default Provider:   %s\n", orDefault(defaultProvider, "<not set>"))
	fmt.Printf("Multi-Tenancy:      %v\n", mtEnabled)
	if mtMode != "" {
		fmt.Printf("Multi-Tenancy Mode: %s\n", mtMode)
	}
	if gitProvider != "" {
		fmt.Printf("Git Provider:       %s\n", gitProvider)
	}
	fmt.Printf("Phase:              %s\n", output.ColorizePhase(orDefault(phase, "<unknown>")))
	fmt.Printf("Age:                %s\n", age)

	// Show default control plane resources if set
	apiserverCPU := client.GetNestedString(bc.Object, "spec", "defaultControlPlaneResources", "apiserver", "requests", "cpu")
	if apiserverCPU != "" {
		fmt.Printf("\nDefault Control Plane Resources:\n")
		fmt.Printf("  API Server:\n")
		fmt.Printf("    Requests: cpu=%s, memory=%s\n",
			apiserverCPU,
			client.GetNestedString(bc.Object, "spec", "defaultControlPlaneResources", "apiserver", "requests", "memory"))
		fmt.Printf("    Limits:   cpu=%s, memory=%s\n",
			client.GetNestedString(bc.Object, "spec", "defaultControlPlaneResources", "apiserver", "limits", "cpu"),
			client.GetNestedString(bc.Object, "spec", "defaultControlPlaneResources", "apiserver", "limits", "memory"))
	}

	return nil
}

func newSetCmd(logger *log.Logger) *cobra.Command {
	var kubeconfig string

	cmd := &cobra.Command{
		Use:   "set KEY=VALUE [KEY=VALUE ...]",
		Short: "Set fields on the Butler platform configuration",
		Long: `Patch individual fields on the ButlerConfig singleton.

Accepts one or more KEY=VALUE pairs where KEY is a dot-separated path
into the ButlerConfig spec. Uses JSON merge patch to update the resource.

Supported key patterns:
  spec.mode                      - Platform mode (e.g., LoadBalancer, Gateway)
  spec.defaultProvider           - Default ProviderConfig name
  spec.multiTenancy.enabled      - Enable/disable multi-tenancy (true/false)
  spec.multiTenancy.mode         - Multi-tenancy mode

Examples:
  butleradm config set spec.multiTenancy.enabled=true
  butleradm config set spec.defaultProvider=harvester-prod
  butleradm config set spec.mode=LoadBalancer spec.defaultProvider=nutanix-prod`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			kubeContext, _ := cmd.Flags().GetString("context")
			return runSet(cmd.Context(), logger, args, kubeconfig, kubeContext)
		},
	}

	cmd.Flags().StringVar(&kubeconfig, "kubeconfig", "", "path to management cluster kubeconfig")

	return cmd
}

// parseKVPairs parses KEY=VALUE arguments into a nested map suitable for
// JSON merge patch. Boolean strings ("true"/"false") are coerced to bool.
func parseKVPairs(kvPairs []string) (map[string]interface{}, error) {
	patch := make(map[string]interface{})
	for _, kv := range kvPairs {
		parts := strings.SplitN(kv, "=", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid argument %q: expected KEY=VALUE format", kv)
		}
		key := parts[0]
		value := parts[1]

		var parsed interface{}
		switch strings.ToLower(value) {
		case "true":
			parsed = true
		case "false":
			parsed = false
		default:
			parsed = value
		}

		setNestedField(patch, strings.Split(key, "."), parsed)
	}
	return patch, nil
}

func runSet(ctx context.Context, logger *log.Logger, kvPairs []string, kubeconfigPath, kubeContext string) error {
	patch, err := parseKVPairs(kvPairs)
	if err != nil {
		return err
	}

	patchBytes, err := json.Marshal(patch)
	if err != nil {
		return fmt.Errorf("marshaling patch: %w", err)
	}

	c, err := client.New(kubeconfigPath, kubeContext)
	if err != nil {
		return fmt.Errorf("connecting to management cluster: %w", err)
	}

	// ButlerConfig is cluster-scoped
	result, err := c.Dynamic.Resource(client.ButlerConfigGVR).Patch(
		ctx,
		butlerConfigName,
		types.MergePatchType,
		patchBytes,
		metav1.PatchOptions{},
	)
	if err != nil {
		return fmt.Errorf("patching ButlerConfig: %w", err)
	}

	logger.Success("butler configuration updated")

	// Print the updated key fields
	mode := client.GetNestedString(result.Object, "spec", "mode")
	defaultProvider := client.GetNestedString(result.Object, "spec", "defaultProvider")
	mtEnabled := client.GetNestedBool(result.Object, "spec", "multiTenancy", "enabled")

	fmt.Printf("Mode:             %s\n", orDefault(mode, "<not set>"))
	fmt.Printf("Default Provider: %s\n", orDefault(defaultProvider, "<not set>"))
	fmt.Printf("Multi-Tenancy:    %v\n", mtEnabled)

	return nil
}

// setNestedField sets a value in a nested map given a path of keys.
func setNestedField(m map[string]interface{}, keys []string, value interface{}) {
	if len(keys) == 1 {
		m[keys[0]] = value
		return
	}

	// Create or retrieve the next level
	next, ok := m[keys[0]].(map[string]interface{})
	if !ok {
		next = make(map[string]interface{})
		m[keys[0]] = next
	}
	setNestedField(next, keys[1:], value)
}

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}
