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

// Package doctor implements the butleradm doctor command.
package doctor

import (
	"context"
	"fmt"
	"strings"

	"github.com/butlerdotdev/butler/internal/common/client"
	"github.com/butlerdotdev/butler/internal/common/log"
	"github.com/butlerdotdev/butler/internal/common/output"
	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

const (
	butlerSystem  = "butler-system"
	stewardSystem = "steward-system"
)

type doctorOptions struct {
	kubeconfigPath string
	kubeContext    string
}

// NewDoctorCmd creates the doctor command.
func NewDoctorCmd(logger *log.Logger) *cobra.Command {
	opts := &doctorOptions{}

	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Check management cluster health",
		Long: `Run health checks against the Butler management cluster.

Verifies that all Butler system components are installed and running
correctly. Checks are run in order and each reports pass, fail, or
warn status.

Checks performed:
  1. Kubernetes API connectivity
  2. butler-system namespace exists
  3. ButlerConfig singleton exists
  4. Butler CRDs are installed
  5. butler-controller deployment is ready
  6. butler-server deployment is ready
  7. Steward deployment is ready
  8. ProviderConfig validation status
  9. TenantCluster summary by phase

Examples:
  # Run health checks
  butleradm doctor

  # Run against a specific cluster
  butleradm doctor --kubeconfig ~/.butler/butler-beta-kubeconfig`,
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.kubeContext, _ = cmd.Flags().GetString("context")
			return runDoctor(cmd.Context(), logger, opts)
		},
	}

	cmd.Flags().StringVar(&opts.kubeconfigPath, "kubeconfig", "", "path to management cluster kubeconfig")

	return cmd
}

func runDoctor(ctx context.Context, logger *log.Logger, opts *doctorOptions) error {
	fmt.Println(output.Bold("Butler Management Cluster Health Check"))
	fmt.Println(strings.Repeat("=", 45))
	fmt.Println()

	// Connect to cluster
	c, err := client.New(opts.kubeconfigPath, opts.kubeContext)
	if err != nil {
		printResult("K8s API connectivity", "fail", fmt.Sprintf("cannot connect: %v", err))
		return fmt.Errorf("cannot connect to cluster: %w", err)
	}

	// 1. K8s API connectivity
	serverVersion, err := c.Clientset.Discovery().ServerVersion()
	if err != nil {
		printResult("K8s API connectivity", "fail", fmt.Sprintf("API unreachable: %v", err))
		return fmt.Errorf("API server unreachable: %w", err)
	}
	printResult("K8s API connectivity", "pass", fmt.Sprintf("server %s", serverVersion.GitVersion))

	// 2. butler-system namespace exists
	_, err = c.Clientset.CoreV1().Namespaces().Get(ctx, butlerSystem, metav1.GetOptions{})
	if err != nil {
		printResult("butler-system namespace", "fail", "namespace does not exist")
		fmt.Println()
		fmt.Println("The butler-system namespace is required. Bootstrap a management cluster first:")
		fmt.Println("  butleradm bootstrap <provider> --config <config.yaml>")
		return nil
	}
	printResult("butler-system namespace", "pass", "exists")

	// 3. ButlerConfig singleton exists
	bc, err := c.Dynamic.Resource(client.ButlerConfigGVR).Get(ctx, "butler", metav1.GetOptions{})
	if err != nil {
		printResult("ButlerConfig singleton", "fail", "not found")
	} else {
		phase := client.GetNestedString(bc.Object, "status", "phase")
		if phase == "" {
			phase = "no phase set"
		}
		printResult("ButlerConfig singleton", "pass", fmt.Sprintf("phase: %s", phase))
	}

	// 4. Butler CRDs are installed (check for TenantCluster CRD via discovery)
	_, err = c.Dynamic.Resource(client.TenantClusterGVR).Namespace(butlerSystem).List(ctx, metav1.ListOptions{Limit: 1})
	if err != nil {
		errStr := err.Error()
		if strings.Contains(errStr, "the server could not find the requested resource") ||
			strings.Contains(errStr, "no matches for kind") {
			printResult("Butler CRDs installed", "fail", "TenantCluster CRD not registered")
		} else {
			// Other errors (RBAC, network) are not CRD issues
			printResult("Butler CRDs installed", "warn", fmt.Sprintf("could not verify: %v", err))
		}
	} else {
		printResult("Butler CRDs installed", "pass", "TenantCluster CRD found")
	}

	// 5. butler-controller deployment
	checkDeploymentHealth(ctx, c, butlerSystem, "butler-controller", "butler-controller")

	// 6. butler-server deployment (deployed as butler-console-server)
	checkDeploymentHealth(ctx, c, butlerSystem, "butler-console-server", "butler-server")

	// 7. Steward deployment
	checkDeploymentHealth(ctx, c, stewardSystem, "steward", "steward")

	// 8. ProviderConfigs validation status
	fmt.Println()
	fmt.Println(output.Bold("Provider Configs:"))
	checkProviderConfigs(ctx, c)

	// 9. TenantCluster summary by phase
	fmt.Println()
	fmt.Println(output.Bold("Tenant Clusters:"))
	checkTenantClusters(ctx, c)

	fmt.Println()
	return nil
}

// checkDeploymentHealth checks if a deployment exists and is ready.
func checkDeploymentHealth(ctx context.Context, c *client.Client, namespace, name, displayName string) {
	deploy, err := c.Clientset.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		printResult(displayName, "fail", "deployment not found")
		return
	}

	desired := int32(1)
	if deploy.Spec.Replicas != nil {
		desired = *deploy.Spec.Replicas
	}
	ready := deploy.Status.ReadyReplicas

	if ready >= desired && desired > 0 {
		printResult(displayName, "pass", fmt.Sprintf("%d/%d ready", ready, desired))
	} else if ready > 0 {
		printResult(displayName, "warn", fmt.Sprintf("%d/%d ready", ready, desired))
	} else {
		printResult(displayName, "fail", fmt.Sprintf("%d/%d ready", ready, desired))
	}
}

// checkProviderConfigs lists ProviderConfigs and shows their validation status.
func checkProviderConfigs(ctx context.Context, c *client.Client) {
	list, err := c.Dynamic.Resource(client.ProviderConfigGVR).Namespace(butlerSystem).List(ctx, metav1.ListOptions{})
	if err != nil {
		fmt.Printf("  %s Could not list ProviderConfigs: %v\n", output.Danger("[FAIL]"), err)
		return
	}

	if len(list.Items) == 0 {
		fmt.Printf("  %s No ProviderConfigs found\n", output.Warning("[WARN]"))
		return
	}

	for _, pc := range list.Items {
		name := pc.GetName()
		provider, _, _ := unstructured.NestedString(pc.Object, "spec", "provider")
		validated, _, _ := unstructured.NestedBool(pc.Object, "status", "validated")

		if validated {
			fmt.Printf("  %s %s (%s) - validated\n", output.Success("[PASS]"), name, provider)
		} else {
			fmt.Printf("  %s %s (%s) - not validated\n", output.Warning("[WARN]"), name, provider)
		}
	}
}

// checkTenantClusters counts TenantClusters by phase.
func checkTenantClusters(ctx context.Context, c *client.Client) {
	list, err := c.Dynamic.Resource(client.TenantClusterGVR).List(ctx, metav1.ListOptions{})
	if err != nil {
		fmt.Printf("  %s Could not list TenantClusters: %v\n", output.Danger("[FAIL]"), err)
		return
	}

	if len(list.Items) == 0 {
		fmt.Printf("  No tenant clusters found\n")
		return
	}

	// Count by phase
	phases := make(map[string]int)
	for _, tc := range list.Items {
		phase, _, _ := unstructured.NestedString(tc.Object, "status", "phase")
		if phase == "" {
			phase = "Unknown"
		}
		phases[phase]++
	}

	total := len(list.Items)
	ready := phases["Ready"]
	provisioning := phases["Provisioning"] + phases["Installing"]
	failed := phases["Failed"]

	fmt.Printf("  Total: %d", total)
	if ready > 0 {
		fmt.Printf("  %s", output.Success(fmt.Sprintf("Ready: %d", ready)))
	}
	if provisioning > 0 {
		fmt.Printf("  %s", output.Warning(fmt.Sprintf("Provisioning: %d", provisioning)))
	}
	if failed > 0 {
		fmt.Printf("  %s", output.Danger(fmt.Sprintf("Failed: %d", failed)))
	}
	fmt.Println()
}

// printResult prints a health check result with pass/fail/warn indicator.
func printResult(name, status, detail string) {
	var indicator string
	switch status {
	case "pass":
		indicator = output.Success("[PASS]")
	case "fail":
		indicator = output.Danger("[FAIL]")
	case "warn":
		indicator = output.Warning("[WARN]")
	default:
		indicator = "[????]"
	}

	fmt.Printf("  %s %-30s %s\n", indicator, name, detail)
}
