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
	PlatformAddonGVR = schema.GroupVersionResource{
		Group:    ButlerAPIGroup,
		Version:  ButlerAPIVersion,
		Resource: "platformaddons",
	}
	ClusterAccessGVR = schema.GroupVersionResource{
		Group:    ButlerAPIGroup,
		Version:  ButlerAPIVersion,
		Resource: "clusteraccesses",
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
		return nil, fmt.Errorf("building config: %w", err)
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

// NewFromDefault creates a client from the default kubeconfig location
func NewFromDefault() (*Client, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("getting home directory: %w", err)
	}

	// Try Butler-specific kubeconfig first
	butlerConfig := filepath.Join(home, ".butler", "kubeconfig")
	if _, err := os.Stat(butlerConfig); err == nil {
		return NewFromKubeconfig(butlerConfig)
	}

	// Fall back to standard kubeconfig
	defaultConfig := filepath.Join(home, ".kube", "config")
	return NewFromKubeconfig(defaultConfig)
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
		namespace = "butler-system"
	}
	return c.Dynamic.Resource(TenantClusterGVR).Namespace(namespace).Create(ctx, tc, metav1.CreateOptions{})
}

// DeleteTenantCluster deletes a TenantCluster
func (c *Client) DeleteTenantCluster(ctx context.Context, namespace, name string) error {
	return c.Dynamic.Resource(TenantClusterGVR).Namespace(namespace).Delete(ctx, name, metav1.DeleteOptions{})
}
