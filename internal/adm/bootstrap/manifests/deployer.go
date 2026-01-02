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

package manifests

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"io/fs"
	"strings"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
)

// Deployer applies embedded manifests to a Kubernetes cluster
type Deployer struct {
	clientset     *kubernetes.Clientset
	dynamicClient dynamic.Interface
}

// NewDeployer creates a new manifest deployer
func NewDeployer(clientset *kubernetes.Clientset, dynamicClient dynamic.Interface) *Deployer {
	return &Deployer{
		clientset:     clientset,
		dynamicClient: dynamicClient,
	}
}

// DeployCRDs deploys all embedded CRD manifests
func (d *Deployer) DeployCRDs(ctx context.Context) error {
	return d.deployFromFS(ctx, CRDs, "crds")
}

// DeployControllers deploys all embedded controller manifests
func (d *Deployer) DeployControllers(ctx context.Context, provider string) error {
	// Deploy bootstrap controller (always needed)
	if err := d.deployFile(ctx, Controllers, "controllers/butler-bootstrap.yaml"); err != nil {
		return fmt.Errorf("deploying butler-bootstrap: %w", err)
	}

	// Deploy provider-specific controller
	providerFile := fmt.Sprintf("controllers/butler-provider-%s.yaml", provider)
	if err := d.deployFile(ctx, Controllers, providerFile); err != nil {
		return fmt.Errorf("deploying butler-provider-%s: %w", provider, err)
	}

	return nil
}

// deployFromFS deploys all YAML files from an embedded filesystem directory
func (d *Deployer) deployFromFS(ctx context.Context, fsys fs.FS, dir string) error {
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return fmt.Errorf("reading directory %s: %w", dir, err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if !strings.HasSuffix(entry.Name(), ".yaml") && !strings.HasSuffix(entry.Name(), ".yml") {
			continue
		}

		path := dir + "/" + entry.Name()
		if err := d.deployFile(ctx, fsys, path); err != nil {
			return fmt.Errorf("deploying %s: %w", path, err)
		}
	}

	return nil
}

// deployFile deploys all resources from a single YAML file
func (d *Deployer) deployFile(ctx context.Context, fsys fs.FS, path string) error {
	data, err := fs.ReadFile(fsys, path)
	if err != nil {
		return fmt.Errorf("reading file: %w", err)
	}

	return d.applyYAML(ctx, data)
}

// applyYAML applies multi-document YAML to the cluster
func (d *Deployer) applyYAML(ctx context.Context, data []byte) error {
	reader := yaml.NewYAMLReader(bufio.NewReader(bytes.NewReader(data)))

	for {
		doc, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("reading YAML document: %w", err)
		}

		// Skip empty documents
		if len(bytes.TrimSpace(doc)) == 0 {
			continue
		}

		// Parse the document
		obj := &unstructured.Unstructured{}
		if err := yaml.Unmarshal(doc, &obj.Object); err != nil {
			return fmt.Errorf("unmarshaling YAML: %w", err)
		}

		// Skip if no kind (comments-only documents)
		if obj.GetKind() == "" {
			continue
		}

		if err := d.applyResource(ctx, obj); err != nil {
			return fmt.Errorf("applying %s %s: %w", obj.GetKind(), obj.GetName(), err)
		}
	}

	return nil
}

// applyResource creates or updates a single resource
func (d *Deployer) applyResource(ctx context.Context, obj *unstructured.Unstructured) error {
	gvk := obj.GroupVersionKind()
	gvr := gvkToGVR(gvk)

	var client dynamic.ResourceInterface
	if obj.GetNamespace() != "" {
		client = d.dynamicClient.Resource(gvr).Namespace(obj.GetNamespace())
	} else {
		client = d.dynamicClient.Resource(gvr)
	}

	// Try to create first
	_, err := client.Create(ctx, obj, metav1.CreateOptions{})
	if err == nil {
		return nil
	}

	// If already exists, update
	if errors.IsAlreadyExists(err) {
		// Get existing to preserve resourceVersion
		existing, getErr := client.Get(ctx, obj.GetName(), metav1.GetOptions{})
		if getErr != nil {
			return fmt.Errorf("getting existing resource: %w", getErr)
		}

		obj.SetResourceVersion(existing.GetResourceVersion())
		_, updateErr := client.Update(ctx, obj, metav1.UpdateOptions{})
		if updateErr != nil {
			return fmt.Errorf("updating resource: %w", updateErr)
		}
		return nil
	}

	return fmt.Errorf("creating resource: %w", err)
}

// gvkToGVR converts GroupVersionKind to GroupVersionResource
// This is a simplified mapping - in production you'd use discovery
func gvkToGVR(gvk schema.GroupVersionKind) schema.GroupVersionResource {
	// Standard Kubernetes resources
	kindToResource := map[string]string{
		"Namespace":                "namespaces",
		"ServiceAccount":           "serviceaccounts",
		"ClusterRole":              "clusterroles",
		"ClusterRoleBinding":       "clusterrolebindings",
		"Role":                     "roles",
		"RoleBinding":              "rolebindings",
		"Deployment":               "deployments",
		"Service":                  "services",
		"ConfigMap":                "configmaps",
		"Secret":                   "secrets",
		"CustomResourceDefinition": "customresourcedefinitions",
	}

	resource, ok := kindToResource[gvk.Kind]
	if !ok {
		// Default: lowercase + 's'
		resource = strings.ToLower(gvk.Kind) + "s"
	}

	return schema.GroupVersionResource{
		Group:    gvk.Group,
		Version:  gvk.Version,
		Resource: resource,
	}
}

// WaitForCRDs waits for CRDs to be established
func (d *Deployer) WaitForCRDs(ctx context.Context, names []string) error {
	crdGVR := schema.GroupVersionResource{
		Group:    "apiextensions.k8s.io",
		Version:  "v1",
		Resource: "customresourcedefinitions",
	}

	for _, name := range names {
		for {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}

			crd, err := d.dynamicClient.Resource(crdGVR).Get(ctx, name, metav1.GetOptions{})
			if err != nil {
				continue
			}

			// Check if established
			conditions, found, _ := unstructured.NestedSlice(crd.Object, "status", "conditions")
			if !found {
				continue
			}

			established := false
			for _, c := range conditions {
				cond, ok := c.(map[string]interface{})
				if !ok {
					continue
				}
				if cond["type"] == "Established" && cond["status"] == "True" {
					established = true
					break
				}
			}

			if established {
				break
			}
		}
	}

	return nil
}

// WaitForDeployment waits for a deployment to be ready
func (d *Deployer) WaitForDeployment(ctx context.Context, namespace, name string) error {
	deployGVR := schema.GroupVersionResource{
		Group:    "apps",
		Version:  "v1",
		Resource: "deployments",
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		deploy, err := d.dynamicClient.Resource(deployGVR).Namespace(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			continue
		}

		replicas, _, _ := unstructured.NestedInt64(deploy.Object, "spec", "replicas")
		readyReplicas, _, _ := unstructured.NestedInt64(deploy.Object, "status", "readyReplicas")

		if readyReplicas >= replicas && replicas > 0 {
			return nil
		}
	}
}
