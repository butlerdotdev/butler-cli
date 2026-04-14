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
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// NutanixDiscovery discovers resources on a Nutanix Prism Central cluster
// using the v3 REST API.
type NutanixDiscovery struct {
	endpoint   string
	port       string
	insecure   bool
	username   string
	password   string
	baseURL    string
	authHeader string
	httpClient *http.Client
}

func init() {
	Register("nutanix", func(creds CredentialProvider) (ProviderDiscovery, error) {
		return NewNutanixDiscovery(creds)
	})
}

// NewNutanixDiscovery creates a Nutanix discovery client from credentials.
func NewNutanixDiscovery(creds CredentialProvider) (*NutanixDiscovery, error) {
	endpoint := creds.Get("endpoint")
	if endpoint == "" {
		return nil, fmt.Errorf("nutanix endpoint is required")
	}
	username := creds.Get("username")
	if username == "" {
		return nil, fmt.Errorf("nutanix username is required")
	}
	password := creds.Get("password")
	if password == "" {
		return nil, fmt.Errorf("nutanix password is required")
	}

	port := creds.Get("port")
	if port == "" {
		port = "9440"
	}

	return &NutanixDiscovery{
		endpoint: endpoint,
		port:     port,
		insecure: creds.GetBool("insecure"),
		username: username,
		password: password,
	}, nil
}

func (n *NutanixDiscovery) Connect(ctx context.Context) error {
	// Normalize endpoint: ensure https:// prefix, strip trailing slashes.
	endpoint := strings.TrimRight(n.endpoint, "/")
	if !strings.HasPrefix(endpoint, "https://") && !strings.HasPrefix(endpoint, "http://") {
		endpoint = "https://" + endpoint
	}
	n.baseURL = fmt.Sprintf("%s:%s/api/nutanix/v3", endpoint, n.port)
	n.authHeader = "Basic " + base64.StdEncoding.EncodeToString(
		[]byte(n.username+":"+n.password))

	n.httpClient = &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: n.insecure, //nolint:gosec // user-configured
			},
		},
	}

	// Validate connectivity by listing clusters. Some Prism Central
	// versions reject small length values, so use 20.
	_, err := n.listResource(ctx, "clusters", 20)
	if err != nil {
		return fmt.Errorf("connecting to prism central: %w", err)
	}

	return nil
}

func (n *NutanixDiscovery) ResourceTypes() []ResourceTypeInfo {
	return []ResourceTypeInfo{
		{Type: ResourceClusters, Label: "Cluster", ParentType: ""},
		{Type: ResourceSubnets, Label: "Subnet", ParentType: ""},
		{Type: ResourceImages, Label: "Image", ParentType: ""},
	}
}

func (n *NutanixDiscovery) FetchResource(ctx context.Context, resourceType string, parentID string) ([]ProviderResource, error) {
	switch resourceType {
	case ResourceClusters:
		return n.listClusters(ctx)
	case ResourceSubnets:
		return n.listSubnets(ctx)
	case ResourceImages:
		return n.listImages(ctx)
	default:
		return nil, fmt.Errorf("unsupported resource type for nutanix: %s", resourceType)
	}
}

func (n *NutanixDiscovery) listClusters(ctx context.Context) ([]ProviderResource, error) {
	entities, err := n.listResource(ctx, "clusters", 500)
	if err != nil {
		return nil, fmt.Errorf("listing clusters: %w", err)
	}

	var resources []ProviderResource
	for _, e := range entities {
		name := jsonString(e, "spec", "name")
		uuid := jsonString(e, "metadata", "uuid")
		if name == "" || uuid == "" {
			continue
		}
		resources = append(resources, ProviderResource{
			Name: name,
			ID:   uuid,
		})
	}
	return resources, nil
}

func (n *NutanixDiscovery) listSubnets(ctx context.Context) ([]ProviderResource, error) {
	entities, err := n.listResource(ctx, "subnets", 500)
	if err != nil {
		return nil, fmt.Errorf("listing subnets: %w", err)
	}

	var resources []ProviderResource
	for _, e := range entities {
		name := jsonString(e, "spec", "name")
		uuid := jsonString(e, "metadata", "uuid")
		if name == "" || uuid == "" {
			continue
		}

		desc := ""
		if vlanID := jsonFloat(e, "spec", "resources", "vlan_id"); vlanID > 0 {
			desc = fmt.Sprintf("VLAN %d", int(vlanID))
		}

		resources = append(resources, ProviderResource{
			Name:        name,
			ID:          uuid,
			Description: desc,
		})
	}
	return resources, nil
}

func (n *NutanixDiscovery) listImages(ctx context.Context) ([]ProviderResource, error) {
	entities, err := n.listResource(ctx, "images", 500)
	if err != nil {
		return nil, fmt.Errorf("listing images: %w", err)
	}

	var resources []ProviderResource
	for _, e := range entities {
		name := jsonString(e, "spec", "name")
		uuid := jsonString(e, "metadata", "uuid")
		if name == "" || uuid == "" {
			continue
		}
		resources = append(resources, ProviderResource{
			Name: name,
			ID:   uuid,
		})
	}
	return resources, nil
}

func (n *NutanixDiscovery) SyncImage(ctx context.Context, artifactURL, displayName string) (string, error) {
	body := map[string]interface{}{
		"spec": map[string]interface{}{
			"name":        displayName,
			"description": "Synced by Butler bootstrap wizard",
			"resources": map[string]interface{}{
				"image_type": "DISK_IMAGE",
				"source_uri": artifactURL,
			},
		},
		"metadata": map[string]interface{}{
			"kind": "image",
		},
	}

	data, err := json.Marshal(body)
	if err != nil {
		return "", fmt.Errorf("marshaling image request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, n.baseURL+"/images", bytes.NewReader(data))
	if err != nil {
		return "", fmt.Errorf("building request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", n.authHeader)

	resp, err := n.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("creating image: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted && resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("create image returned status %d", resp.StatusCode)
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decoding response: %w", err)
	}

	// The task UUID is in status.execution_context.task_uuid.
	taskUUID := jsonString(result, "status", "execution_context", "task_uuid")
	if taskUUID == "" {
		// Fall back to metadata UUID for polling.
		taskUUID = jsonString(result, "metadata", "uuid")
	}

	return taskUUID, nil
}

func (n *NutanixDiscovery) PollImageSync(ctx context.Context, syncID string) (bool, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, n.baseURL+"/tasks/"+syncID, nil)
	if err != nil {
		return false, "", fmt.Errorf("building request: %w", err)
	}
	req.Header.Set("Authorization", n.authHeader)

	resp, err := n.httpClient.Do(req)
	if err != nil {
		return false, "", fmt.Errorf("polling task: %w", err)
	}
	defer resp.Body.Close()

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return false, "", fmt.Errorf("decoding task response: %w", err)
	}

	status := jsonString(result, "status")
	switch status {
	case "SUCCEEDED":
		// Extract the entity UUID from entity_reference_list.
		refs, ok := result["entity_reference_list"].([]interface{})
		if ok && len(refs) > 0 {
			if ref, ok := refs[0].(map[string]interface{}); ok {
				return true, jsonString(ref, "uuid"), nil
			}
		}
		return true, syncID, nil
	case "FAILED":
		msg := jsonString(result, "error_detail")
		return false, "", fmt.Errorf("image sync failed: %s", msg)
	default:
		return false, "", nil
	}
}

// listResource posts to the Prism Central v3 list endpoint. The URL
// path uses the plural resource name (e.g., /clusters/list) but the
// request body's "kind" field must be singular (e.g., "cluster").
func (n *NutanixDiscovery) listResource(ctx context.Context, kind string, limit int) ([]map[string]interface{}, error) {
	body := map[string]interface{}{
		"kind":   strings.TrimSuffix(kind, "s"),
		"length": limit,
	}

	data, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}

	url := fmt.Sprintf("%s/%s/list", n.baseURL, kind)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", n.authHeader)

	resp, err := n.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return nil, fmt.Errorf("authentication failed (401) — verify credentials are for a local Prism Central account, not SSO")
	}
	if resp.StatusCode != http.StatusOK {
		// Read the response body for diagnostic detail — Prism Central
		// often returns a JSON error with a message_list explaining why
		// the request was rejected.
		var errBody bytes.Buffer
		errBody.ReadFrom(resp.Body)
		return nil, fmt.Errorf("list %s returned status %d: %s", kind, resp.StatusCode, errBody.String())
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	entities, ok := result["entities"].([]interface{})
	if !ok {
		return nil, nil
	}

	var out []map[string]interface{}
	for _, e := range entities {
		if m, ok := e.(map[string]interface{}); ok {
			out = append(out, m)
		}
	}
	return out, nil
}

// jsonString traverses a nested map to extract a string value.
func jsonString(obj map[string]interface{}, keys ...string) string {
	current := obj
	for i, key := range keys {
		if i == len(keys)-1 {
			if v, ok := current[key].(string); ok {
				return v
			}
			return ""
		}
		next, ok := current[key].(map[string]interface{})
		if !ok {
			return ""
		}
		current = next
	}
	return ""
}

// jsonFloat traverses a nested map to extract a float64 value.
func jsonFloat(obj map[string]interface{}, keys ...string) float64 {
	current := obj
	for i, key := range keys {
		if i == len(keys)-1 {
			if v, ok := current[key].(float64); ok {
				return v
			}
			return 0
		}
		next, ok := current[key].(map[string]interface{})
		if !ok {
			return 0
		}
		current = next
	}
	return 0
}
