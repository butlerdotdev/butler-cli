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
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/clientcmd/api"
)

type kubeconfigOptions struct {
	namespace      string
	outputPath     string
	merge          bool
	setContext     bool
	kubeconfigPath string
}

// newKubeconfigCmd creates the cluster kubeconfig command
func newKubeconfigCmd(logger *log.Logger) *cobra.Command {
	opts := &kubeconfigOptions{}

	cmd := &cobra.Command{
		Use:   "kubeconfig NAME",
		Short: "Get kubeconfig for a tenant cluster",
		Long: `Download the kubeconfig for accessing a tenant cluster.

By default, outputs the kubeconfig to stdout for piping.
Use --output to save to a file, or --merge to add to your default kubeconfig.

The kubeconfig is fetched from the management cluster, where it's stored
in a Secret within the tenant cluster's dedicated namespace.

Examples:
  # Output kubeconfig to stdout (for piping)
  butlerctl cluster kubeconfig my-cluster

  # Save to a specific file
  butlerctl cluster kubeconfig my-cluster -o ~/.kube/my-cluster.yaml

  # Merge into default kubeconfig and set as current context
  butlerctl cluster kubeconfig my-cluster --merge

  # Merge without switching context
  butlerctl cluster kubeconfig my-cluster --merge --set-context=false

  # Use a specific management cluster kubeconfig
  butlerctl cluster kubeconfig my-cluster --kubeconfig ~/.butler/butler-ntnx-kubeconfig`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runKubeconfig(cmd.Context(), logger, args[0], opts)
		},
	}

	cmd.Flags().StringVarP(&opts.namespace, "namespace", "n", DefaultTenantNamespace, "namespace of the TenantCluster")
	cmd.Flags().StringVarP(&opts.outputPath, "output", "o", "", "output file path (use - for stdout, default)")
	cmd.Flags().BoolVar(&opts.merge, "merge", false, "merge into default kubeconfig (~/.kube/config)")
	cmd.Flags().BoolVar(&opts.setContext, "set-context", true, "set as current context when merging (only with --merge)")
	cmd.Flags().StringVar(&opts.kubeconfigPath, "kubeconfig", "", "path to management cluster kubeconfig")

	return cmd
}

func runKubeconfig(ctx context.Context, logger *log.Logger, clusterName string, opts *kubeconfigOptions) error {
	// Connect to management cluster
	var c *client.Client
	var err error
	if opts.kubeconfigPath != "" {
		c, err = client.NewFromKubeconfig(opts.kubeconfigPath)
	} else {
		c, err = client.NewFromDefault()
	}
	if err != nil {
		return fmt.Errorf("connecting to management cluster: %w", err)
	}

	// Get the TenantCluster to find the tenant namespace
	tc, err := c.GetTenantCluster(ctx, opts.namespace, clusterName)
	if err != nil {
		return fmt.Errorf("getting TenantCluster %s/%s: %w", opts.namespace, clusterName, err)
	}

	// Extract tenant namespace from status
	tenantNS := GetNestedString(tc.Object, "status", "tenantNamespace")
	if tenantNS == "" {
		return fmt.Errorf("TenantCluster %s does not have a tenant namespace yet (phase: %s)",
			clusterName, GetNestedString(tc.Object, "status", "phase"))
	}

	// The kubeconfig secret follows Steward's pattern: <name>-admin-kubeconfig
	secretName := clusterName + "-admin-kubeconfig"

	// Fetch the secret from the tenant namespace
	secret, err := c.Clientset.CoreV1().Secrets(tenantNS).Get(ctx, secretName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("getting kubeconfig secret %s/%s: %w", tenantNS, secretName, err)
	}

	// Steward stores kubeconfig in 'admin.conf' key
	kubeconfigData, ok := secret.Data["admin.conf"]
	if !ok {
		// Try alternative keys
		kubeconfigData, ok = secret.Data["kubeconfig"]
		if !ok {
			kubeconfigData, ok = secret.Data["value"]
			if !ok {
				return fmt.Errorf("kubeconfig secret %s/%s does not contain kubeconfig data (keys: admin.conf, kubeconfig, or value)",
					tenantNS, secretName)
			}
		}
	}

	// Handle merge mode
	if opts.merge {
		return mergeKubeconfig(logger, clusterName, kubeconfigData, opts.setContext)
	}

	// Handle file output
	if opts.outputPath != "" && opts.outputPath != "-" {
		// Expand ~ if present
		outputPath := expandPath(opts.outputPath)

		// Ensure directory exists
		dir := filepath.Dir(outputPath)
		if err := os.MkdirAll(dir, 0700); err != nil {
			return fmt.Errorf("creating directory %s: %w", dir, err)
		}

		// Write file
		if err := os.WriteFile(outputPath, kubeconfigData, 0600); err != nil {
			return fmt.Errorf("writing kubeconfig to %s: %w", outputPath, err)
		}

		logger.Success("kubeconfig saved", "path", outputPath)
		logger.Info("Use: export KUBECONFIG=" + outputPath)
		return nil
	}

	// Default: output to stdout
	fmt.Print(string(kubeconfigData))
	return nil
}

// mergeKubeconfig merges the tenant kubeconfig into the active kubeconfig
func mergeKubeconfig(logger *log.Logger, clusterName string, kubeconfigData []byte, setCurrentContext bool) error {
	// Parse the tenant kubeconfig
	tenantConfig, err := clientcmd.Load(kubeconfigData)
	if err != nil {
		return fmt.Errorf("parsing tenant kubeconfig: %w", err)
	}

	// Determine which kubeconfig file to merge into
	// Priority: KUBECONFIG env var (first path) -> ~/.kube/config
	var targetPath string
	if kubeconfigEnv := os.Getenv("KUBECONFIG"); kubeconfigEnv != "" {
		// KUBECONFIG can have multiple paths; use the first one
		paths := strings.Split(kubeconfigEnv, string(os.PathListSeparator))
		for _, p := range paths {
			p = strings.TrimSpace(p)
			if p != "" {
				targetPath = expandPath(p)
				break
			}
		}
	}
	if targetPath == "" {
		targetPath = clientcmd.RecommendedHomeFile
	}

	// Load the target kubeconfig
	targetConfig, err := clientcmd.LoadFromFile(targetPath)
	if err != nil {
		if os.IsNotExist(err) {
			// Create new config if it doesn't exist
			targetConfig = api.NewConfig()
		} else {
			return fmt.Errorf("loading kubeconfig from %s: %w", targetPath, err)
		}
	}

	// Determine names for the merged entries
	// Use the cluster name as the context name for clarity
	contextName := clusterName
	clusterEntryName := clusterName
	userName := clusterName + "-admin"

	// Find the first cluster from tenant config (Steward typically creates one)
	var tenantCluster *api.Cluster
	for _, cluster := range tenantConfig.Clusters {
		tenantCluster = cluster
		break
	}
	if tenantCluster == nil {
		return fmt.Errorf("tenant kubeconfig contains no clusters")
	}

	// Find the first user from tenant config
	var tenantUser *api.AuthInfo
	for _, user := range tenantConfig.AuthInfos {
		tenantUser = user
		break
	}
	if tenantUser == nil {
		return fmt.Errorf("tenant kubeconfig contains no users")
	}

	// Initialize maps if nil (safety check)
	if targetConfig.Clusters == nil {
		targetConfig.Clusters = make(map[string]*api.Cluster)
	}
	if targetConfig.AuthInfos == nil {
		targetConfig.AuthInfos = make(map[string]*api.AuthInfo)
	}
	if targetConfig.Contexts == nil {
		targetConfig.Contexts = make(map[string]*api.Context)
	}

	// Add/update cluster entry
	targetConfig.Clusters[clusterEntryName] = tenantCluster

	// Add/update user entry
	targetConfig.AuthInfos[userName] = tenantUser

	// Add/update context entry
	targetConfig.Contexts[contextName] = &api.Context{
		Cluster:  clusterEntryName,
		AuthInfo: userName,
	}

	// Set as current context if requested
	if setCurrentContext {
		targetConfig.CurrentContext = contextName
	}

	// Write back to kubeconfig
	if err := clientcmd.WriteToFile(*targetConfig, targetPath); err != nil {
		return fmt.Errorf("writing kubeconfig to %s: %w", targetPath, err)
	}

	logger.Success("kubeconfig merged", "context", contextName, "file", targetPath)
	if setCurrentContext {
		logger.Info("Current context set to: " + contextName)
	} else {
		logger.Info("Use: kubectl config use-context " + contextName)
	}

	return nil
}

// expandPath expands ~ to home directory
func expandPath(path string) string {
	if len(path) > 0 && path[0] == '~' {
		home, err := os.UserHomeDir()
		if err != nil {
			return path
		}
		return filepath.Join(home, path[1:])
	}
	return path
}
