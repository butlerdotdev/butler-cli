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

package discovery

import (
	"context"
	"fmt"
	"os"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/tools/clientcmd"
)

var (
	nadGVR = schema.GroupVersionResource{
		Group:    "k8s.cni.cncf.io",
		Version:  "v1",
		Resource: "network-attachment-definitions",
	}
	vmiImageGVR = schema.GroupVersionResource{
		Group:    "harvesterhci.io",
		Version:  "v1beta1",
		Resource: "virtualmachineimages",
	}
)

// HarvesterDiscovery discovers resources on a Harvester HCI cluster
// using the Kubernetes dynamic client.
type HarvesterDiscovery struct {
	kubeconfigPath string
	client         dynamic.Interface
}

// NewHarvesterDiscovery creates a Harvester discovery client from credentials.
func NewHarvesterDiscovery(creds CredentialProvider) (*HarvesterDiscovery, error) {
	path := creds.Get("kubeconfig")
	if path == "" {
		return nil, fmt.Errorf("harvester kubeconfig path is required")
	}
	return &HarvesterDiscovery{kubeconfigPath: path}, nil
}

func (h *HarvesterDiscovery) Connect(ctx context.Context) error {
	data, err := os.ReadFile(h.kubeconfigPath)
	if err != nil {
		return fmt.Errorf("reading kubeconfig %s: %w", h.kubeconfigPath, err)
	}

	restConfig, err := clientcmd.RESTConfigFromKubeConfig(data)
	if err != nil {
		return fmt.Errorf("parsing kubeconfig: %w", err)
	}
	restConfig.Timeout = 15 * time.Second

	h.client, err = dynamic.NewForConfig(restConfig)
	if err != nil {
		return fmt.Errorf("creating kubernetes client: %w", err)
	}

	// Validate connectivity by listing namespaces.
	_, err = h.client.Resource(schema.GroupVersionResource{
		Group: "", Version: "v1", Resource: "namespaces",
	}).List(ctx, metav1.ListOptions{Limit: 1})
	if err != nil {
		return fmt.Errorf("connecting to harvester cluster: %w", err)
	}

	return nil
}

func (h *HarvesterDiscovery) ResourceTypes() []ResourceTypeInfo {
	return []ResourceTypeInfo{
		{Type: ResourceNamespaces, Label: "Namespace", ParentType: ""},
		{Type: ResourceNetworks, Label: "Network", ParentType: ResourceNamespaces},
		{Type: ResourceImages, Label: "Image", ParentType: ResourceNamespaces},
	}
}

func (h *HarvesterDiscovery) FetchResource(ctx context.Context, resourceType string, parentID string) ([]ProviderResource, error) {
	switch resourceType {
	case ResourceNamespaces:
		return h.listNamespaces(ctx)
	case ResourceNetworks:
		return h.listNetworks(ctx, parentID)
	case ResourceImages:
		return h.listImages(ctx, parentID)
	default:
		return nil, fmt.Errorf("unsupported resource type for harvester: %s", resourceType)
	}
}

func (h *HarvesterDiscovery) listNamespaces(ctx context.Context) ([]ProviderResource, error) {
	nsGVR := schema.GroupVersionResource{Group: "", Version: "v1", Resource: "namespaces"}
	list, err := h.client.Resource(nsGVR).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("listing namespaces: %w", err)
	}

	var resources []ProviderResource
	for _, item := range list.Items {
		name := item.GetName()
		// Skip system namespaces that are not useful for VM placement.
		if isSystemNamespace(name) {
			continue
		}
		resources = append(resources, ProviderResource{
			Name: name,
			ID:   name,
		})
	}
	return resources, nil
}

func (h *HarvesterDiscovery) listNetworks(ctx context.Context, namespace string) ([]ProviderResource, error) {
	if namespace == "" {
		return nil, fmt.Errorf("namespace is required for network listing")
	}

	list, err := h.client.Resource(nadGVR).Namespace(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("listing networks in %s: %w", namespace, err)
	}

	var resources []ProviderResource
	for _, item := range list.Items {
		ref := namespace + "/" + item.GetName()
		resources = append(resources, ProviderResource{
			Name: item.GetName(),
			ID:   ref,
		})
	}
	return resources, nil
}

func (h *HarvesterDiscovery) listImages(ctx context.Context, namespace string) ([]ProviderResource, error) {
	if namespace == "" {
		return nil, fmt.Errorf("namespace is required for image listing")
	}

	list, err := h.client.Resource(vmiImageGVR).Namespace(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("listing images in %s: %w", namespace, err)
	}

	var resources []ProviderResource
	for _, item := range list.Items {
		ref := namespace + "/" + item.GetName()
		displayName := item.GetName()

		// Try to get the display name from spec if available.
		if dn, ok, _ := unstructured.NestedString(item.Object, "spec", "displayName"); ok && dn != "" {
			displayName = dn
		}

		resources = append(resources, ProviderResource{
			Name:        displayName,
			ID:          ref,
			Description: ref,
		})
	}
	return resources, nil
}

func (h *HarvesterDiscovery) SyncImage(ctx context.Context, artifactURL, displayName string) (string, error) {
	// Create a VirtualMachineImage resource with sourceType=download.
	vmi := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "harvesterhci.io/v1beta1",
			"kind":       "VirtualMachineImage",
			"metadata": map[string]interface{}{
				"name":      displayName,
				"namespace": "default",
			},
			"spec": map[string]interface{}{
				"displayName": displayName,
				"sourceType":  "download",
				"url":         artifactURL,
			},
		},
	}

	created, err := h.client.Resource(vmiImageGVR).Namespace("default").Create(ctx, vmi, metav1.CreateOptions{})
	if err != nil {
		return "", fmt.Errorf("creating image: %w", err)
	}

	return "default/" + created.GetName(), nil
}

func (h *HarvesterDiscovery) PollImageSync(ctx context.Context, syncID string) (bool, string, error) {
	// syncID is "namespace/name" format.
	ns, name := splitRef(syncID)

	obj, err := h.client.Resource(vmiImageGVR).Namespace(ns).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return false, "", fmt.Errorf("getting image status: %w", err)
	}

	conditions, found, err := unstructured.NestedSlice(obj.Object, "status", "conditions")
	if err != nil || !found {
		return false, "", nil
	}

	for _, c := range conditions {
		cond, ok := c.(map[string]interface{})
		if !ok {
			continue
		}
		if cond["type"] == "Imported" && cond["status"] == "True" {
			return true, syncID, nil
		}
	}

	return false, "", nil
}

func isSystemNamespace(name string) bool {
	switch name {
	case "kube-system", "kube-public", "kube-node-lease",
		"cattle-system", "cattle-fleet-system", "cattle-fleet-local-system",
		"harvester-system", "longhorn-system", "local":
		return true
	}
	return false
}

func splitRef(ref string) (string, string) {
	for i, c := range ref {
		if c == '/' {
			return ref[:i], ref[i+1:]
		}
	}
	return "", ref
}
