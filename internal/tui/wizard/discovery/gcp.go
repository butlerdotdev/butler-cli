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
	"path/filepath"
	"strings"

	"golang.org/x/oauth2/google"
	"google.golang.org/api/compute/v1"
	"google.golang.org/api/option"
)

func init() {
	Register("gcp", func(creds CredentialProvider) (ProviderDiscovery, error) {
		return NewGCPDiscovery(creds)
	})
}

// GCPDiscovery discovers resources on Google Cloud Platform using the
// Compute Engine API.
type GCPDiscovery struct {
	keyPath   string
	projectID string
	service   *compute.Service
}

// NewGCPDiscovery creates a GCP discovery client from credentials.
func NewGCPDiscovery(creds CredentialProvider) (*GCPDiscovery, error) {
	keyPath := creds.Get("key_path")
	if keyPath == "" {
		return nil, fmt.Errorf("GCP service account key path is required")
	}
	projectID := creds.Get("project_id")
	if projectID == "" {
		return nil, fmt.Errorf("GCP project ID is required")
	}

	return &GCPDiscovery{
		keyPath:   keyPath,
		projectID: projectID,
	}, nil
}

func (g *GCPDiscovery) Connect(ctx context.Context) error {
	path := g.keyPath
	if strings.HasPrefix(path, "~") {
		home, _ := os.UserHomeDir()
		path = filepath.Join(home, path[1:])
	}

	keyData, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("reading service account key %s: %w", path, err)
	}

	creds, err := google.CredentialsFromJSON(ctx, keyData, compute.ComputeReadonlyScope)
	if err != nil {
		return fmt.Errorf("parsing service account key: %w", err)
	}

	g.service, err = compute.NewService(ctx, option.WithCredentials(creds))
	if err != nil {
		return fmt.Errorf("creating compute service: %w", err)
	}

	// Validate connectivity by listing regions with a small page.
	_, err = g.service.Regions.List(g.projectID).MaxResults(1).Do()
	if err != nil {
		return fmt.Errorf("connecting to GCP project %s: %w", g.projectID, err)
	}

	return nil
}

func (g *GCPDiscovery) ResourceTypes() []ResourceTypeInfo {
	return []ResourceTypeInfo{
		{Type: ResourceRegions, Label: "Region", ParentType: ""},
		{Type: ResourceNetworks, Label: "Network", ParentType: ""},
		{Type: ResourceImages, Label: "Image", ParentType: ""},
		{Type: ResourceZones, Label: "Zone", ParentType: ResourceRegions},
		{Type: ResourceSubnets, Label: "Subnetwork", ParentType: ResourceRegions},
	}
}

func (g *GCPDiscovery) FetchResource(ctx context.Context, resourceType string, parentID string) ([]ProviderResource, error) {
	switch resourceType {
	case ResourceRegions:
		return g.listRegions(ctx)
	case ResourceZones:
		return g.listZones(ctx, parentID)
	case ResourceNetworks:
		return g.listNetworks(ctx)
	case ResourceSubnets:
		return g.listSubnetworks(ctx, parentID)
	case ResourceImages:
		return g.listImages(ctx)
	default:
		return nil, fmt.Errorf("unsupported resource type for GCP: %s", resourceType)
	}
}

func (g *GCPDiscovery) listRegions(ctx context.Context) ([]ProviderResource, error) {
	result, err := g.service.Regions.List(g.projectID).Do()
	if err != nil {
		return nil, fmt.Errorf("listing regions: %w", err)
	}

	var resources []ProviderResource
	for _, r := range result.Items {
		if r.Status != "UP" {
			continue
		}
		resources = append(resources, ProviderResource{
			Name: r.Name,
			ID:   r.Name,
		})
	}
	return resources, nil
}

func (g *GCPDiscovery) listZones(ctx context.Context, region string) ([]ProviderResource, error) {
	call := g.service.Zones.List(g.projectID)
	if region != "" {
		call = call.Filter(fmt.Sprintf("region eq .*/%s", region))
	}

	result, err := call.Do()
	if err != nil {
		return nil, fmt.Errorf("listing zones: %w", err)
	}

	var resources []ProviderResource
	for _, z := range result.Items {
		if z.Status != "UP" {
			continue
		}
		resources = append(resources, ProviderResource{
			Name: z.Name,
			ID:   z.Name,
		})
	}
	return resources, nil
}

func (g *GCPDiscovery) listNetworks(ctx context.Context) ([]ProviderResource, error) {
	result, err := g.service.Networks.List(g.projectID).Do()
	if err != nil {
		return nil, fmt.Errorf("listing networks: %w", err)
	}

	var resources []ProviderResource
	for _, n := range result.Items {
		desc := ""
		if n.AutoCreateSubnetworks {
			desc = "auto-mode"
		} else {
			desc = "custom-mode"
		}
		resources = append(resources, ProviderResource{
			Name:        n.Name,
			ID:          n.Name,
			Description: desc,
		})
	}
	return resources, nil
}

func (g *GCPDiscovery) listSubnetworks(ctx context.Context, region string) ([]ProviderResource, error) {
	if region == "" {
		return nil, fmt.Errorf("region is required for subnetwork listing")
	}

	result, err := g.service.Subnetworks.List(g.projectID, region).Do()
	if err != nil {
		return nil, fmt.Errorf("listing subnetworks: %w", err)
	}

	var resources []ProviderResource
	for _, s := range result.Items {
		resources = append(resources, ProviderResource{
			Name:        s.Name,
			ID:          s.Name,
			Description: s.IpCidrRange,
		})
	}
	return resources, nil
}

func (g *GCPDiscovery) listImages(ctx context.Context) ([]ProviderResource, error) {
	resp, err := g.service.Images.List(g.projectID).Context(ctx).Do()
	if err != nil {
		return nil, fmt.Errorf("listing GCE images: %w", err)
	}
	var resources []ProviderResource
	for _, img := range resp.Items {
		if img.Deprecated != nil && img.Deprecated.State == "DEPRECATED" {
			continue
		}
		resources = append(resources, ProviderResource{
			Name:        img.Name,
			ID:          img.Name,
			Description: fmt.Sprintf("%s (%d GB)", img.Family, img.DiskSizeGb),
		})
	}
	return resources, nil
}

func (g *GCPDiscovery) SyncImage(_ context.Context, _, _ string) (string, error) {
	return "", ErrNotSupported
}

func (g *GCPDiscovery) PollImageSync(_ context.Context, _ string) (bool, string, error) {
	return false, "", ErrNotSupported
}
