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

// Package client provides Kubernetes client utilities for Butler CLIs.
package client

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// Butler API group constants
const (
	ButlerAPIGroup   = "butler.butlerlabs.dev"
	ButlerAPIVersion = "v1alpha1"
)

// GVR definitions for Butler CRDs
var (
	TenantClusterGVR = schema.GroupVersionResource{
		Group:    ButlerAPIGroup,
		Version:  ButlerAPIVersion,
		Resource: "tenantclusters",
	}
	ClusterBootstrapGVR = schema.GroupVersionResource{
		Group:    ButlerAPIGroup,
		Version:  ButlerAPIVersion,
		Resource: "clusterbootstraps",
	}
	ProviderConfigGVR = schema.GroupVersionResource{
		Group:    ButlerAPIGroup,
		Version:  ButlerAPIVersion,
		Resource: "providerconfigs",
	}
	MachineRequestGVR = schema.GroupVersionResource{
		Group:    ButlerAPIGroup,
		Version:  ButlerAPIVersion,
		Resource: "machinerequests",
	}
	TeamGVR = schema.GroupVersionResource{
		Group:    ButlerAPIGroup,
		Version:  ButlerAPIVersion,
		Resource: "teams",
	}
	ButlerConfigGVR = schema.GroupVersionResource{
		Group:    ButlerAPIGroup,
		Version:  ButlerAPIVersion,
		Resource: "butlerconfigs",
	}
	// CAPI resources
	MachineDeploymentGVR = schema.GroupVersionResource{
		Group:    "cluster.x-k8s.io",
		Version:  "v1beta1",
		Resource: "machinedeployments",
	}
	ClusterGVR = schema.GroupVersionResource{
		Group:    "cluster.x-k8s.io",
		Version:  "v1beta1",
		Resource: "clusters",
	}
)

// Client wraps Kubernetes clients for Butler operations
type Client struct {
	// Clientset for core Kubernetes resources
	Clientset *kubernetes.Clientset

	// Dynamic client for Butler CRDs
	Dynamic dynamic.Interface

	// Config is the underlying REST config
	Config *rest.Config
}

// NewFromKubeconfig creates a client from a kubeconfig path
func NewFromKubeconfig(kubeconfigPath string) (*Client, error) {
	config, err := clientcmd.BuildConfigFromFlags("", kubeconfigPath)
	if err != nil {
		return nil, fmt.Errorf("building config from %s: %w", kubeconfigPath, err)
	}
	return newClient(config)
}

// NewFromBytes creates a client from kubeconfig bytes
func NewFromBytes(kubeconfig []byte) (*Client, error) {
	config, err := clientcmd.RESTConfigFromKubeConfig(kubeconfig)
	if err != nil {
		return nil, fmt.Errorf("parsing kubeconfig: %w", err)
	}
	return newClient(config)
}

// NewFromDefault creates a client using standard kubeconfig discovery.
// Priority order:
//  1. KUBECONFIG environment variable
//  2. Butler kubeconfigs in ~/.butler/ (files ending in -kubeconfig)
//  3. Standard ~/.kube/config
func NewFromDefault() (*Client, error) {
	// 1. Check KUBECONFIG environment variable first (standard kubectl behavior)
	if kubeconfigEnv := os.Getenv("KUBECONFIG"); kubeconfigEnv != "" {
		// KUBECONFIG can contain multiple paths separated by ":"
		// Use the first one that exists
		paths := strings.Split(kubeconfigEnv, string(os.PathListSeparator))
		for _, p := range paths {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			if _, err := os.Stat(p); err == nil {
				return NewFromKubeconfig(p)
			}
		}
		// If KUBECONFIG is set but files don't exist, return error
		return nil, fmt.Errorf("KUBECONFIG is set but no valid kubeconfig found at: %s", kubeconfigEnv)
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("getting home directory: %w", err)
	}

	// 2. Try Butler-specific kubeconfigs in ~/.butler/
	butlerDir := filepath.Join(home, ".butler")
	if kubeconfigPath := findButlerKubeconfig(butlerDir); kubeconfigPath != "" {
		return NewFromKubeconfig(kubeconfigPath)
	}

	// 3. Fall back to standard kubeconfig
	defaultConfig := filepath.Join(home, ".kube", "config")
	if _, err := os.Stat(defaultConfig); err == nil {
		return NewFromKubeconfig(defaultConfig)
	}

	return nil, fmt.Errorf("no kubeconfig found; set KUBECONFIG env var, use --kubeconfig flag, or ensure ~/.kube/config exists")
}

// findButlerKubeconfig looks for kubeconfig files in the Butler directory
func findButlerKubeconfig(butlerDir string) string {
	entries, err := os.ReadDir(butlerDir)
	if err != nil {
		return ""
	}

	// Look for files ending in -kubeconfig
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasSuffix(name, "-kubeconfig") {
			return filepath.Join(butlerDir, name)
		}
	}

	// Try just "kubeconfig" if no suffixed files found
	kubeconfig := filepath.Join(butlerDir, "kubeconfig")
	if _, err := os.Stat(kubeconfig); err == nil {
		return kubeconfig
	}

	return ""
}

// newClient creates a client from a rest config
func newClient(config *rest.Config) (*Client, error) {
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("creating clientset: %w", err)
	}

	dynamicClient, err := dynamic.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("creating dynamic client: %w", err)
	}

	return &Client{
		Clientset: clientset,
		Dynamic:   dynamicClient,
		Config:    config,
	}, nil
}

// GetTenantCluster gets a TenantCluster by name
func (c *Client) GetTenantCluster(ctx context.Context, namespace, name string) (*unstructured.Unstructured, error) {
	return c.Dynamic.Resource(TenantClusterGVR).Namespace(namespace).Get(ctx, name, metav1.GetOptions{})
}

// ListTenantClusters lists all TenantClusters in a namespace
func (c *Client) ListTenantClusters(ctx context.Context, namespace string) (*unstructured.UnstructuredList, error) {
	return c.Dynamic.Resource(TenantClusterGVR).Namespace(namespace).List(ctx, metav1.ListOptions{})
}

// CreateTenantCluster creates a new TenantCluster
func (c *Client) CreateTenantCluster(ctx context.Context, tc *unstructured.Unstructured) (*unstructured.Unstructured, error) {
	namespace := tc.GetNamespace()
	if namespace == "" {
		namespace = "butler-tenants"
	}
	return c.Dynamic.Resource(TenantClusterGVR).Namespace(namespace).Create(ctx, tc, metav1.CreateOptions{})
}

// DeleteTenantCluster deletes a TenantCluster
func (c *Client) DeleteTenantCluster(ctx context.Context, namespace, name string) error {
	return c.Dynamic.Resource(TenantClusterGVR).Namespace(namespace).Delete(ctx, name, metav1.DeleteOptions{})
}

// ListProviderConfigs lists all ProviderConfigs in a namespace
func (c *Client) ListProviderConfigs(ctx context.Context, namespace string) (*unstructured.UnstructuredList, error) {
	return c.Dynamic.Resource(ProviderConfigGVR).Namespace(namespace).List(ctx, metav1.ListOptions{})
}

// GetProviderConfig gets a ProviderConfig by name
func (c *Client) GetProviderConfig(ctx context.Context, namespace, name string) (*unstructured.Unstructured, error) {
	return c.Dynamic.Resource(ProviderConfigGVR).Namespace(namespace).Get(ctx, name, metav1.GetOptions{})
}
