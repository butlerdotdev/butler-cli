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
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

const DefaultFactoryURL = "https://factory.butlerlabs.dev"

// DefaultTalosSchematic includes qemu-guest-agent, iscsi-tools, util-linux-tools.
// For Harvester (KVM/QEMU). Registered with k8s v1.32.0 + Steward worker mode.
const DefaultTalosSchematic = "75d479b0960e56ef9ecc3a11c0572e4fe41f8b1e8a24c70ad38fc34e89431524"

// NutanixTalosSchematic includes iscsi-tools, util-linux-tools (no qemu-guest-agent).
// qemu-guest-agent causes issues on Nutanix AHV which uses its own guest tools.
const NutanixTalosSchematic = "026b6ae9b4e53a6a40e07e471d6f26617473ab59fdd0ab1fa00e643c170b91f2"

// SchematicForProvider returns the appropriate Talos schematic ID for the
// given provider. Nutanix gets a schematic without qemu-guest-agent;
// all others use the default with qemu-guest-agent.
func SchematicForProvider(provider string) string {
	if provider == "nutanix" {
		return NutanixTalosSchematic
	}
	return DefaultTalosSchematic
}

// FactoryClient queries the Butler Image Factory API.
type FactoryClient struct {
	baseURL    string
	httpClient *http.Client
}

// NewFactoryClient creates a factory client. Defaults to DefaultFactoryURL.
func NewFactoryClient(baseURL string) *FactoryClient {
	if baseURL == "" {
		baseURL = DefaultFactoryURL
	}
	return &FactoryClient{
		baseURL: strings.TrimSuffix(baseURL, "/"),
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// CatalogEntry represents an available OS image in the factory.
type CatalogEntry struct {
	OS       string   `json:"os"`
	Versions []string `json:"versions"`
	Formats  []string `json:"formats"`
}

// FetchCatalog returns available OS images from the factory.
func (c *FactoryClient) FetchCatalog(ctx context.Context) ([]CatalogEntry, error) {
	url := c.baseURL + "/v1/catalog"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("building request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching catalog: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("catalog returned status %d", resp.StatusCode)
	}

	var catalog []CatalogEntry
	if err := json.NewDecoder(resp.Body).Decode(&catalog); err != nil {
		return nil, fmt.Errorf("decoding catalog: %w", err)
	}

	return catalog, nil
}

// ArtifactURL builds the download URL for a specific image artifact.
func (c *FactoryClient) ArtifactURL(schematicID, version, platform, arch, format string) string {
	return fmt.Sprintf("%s/image/%s/%s/%s-%s.%s",
		c.baseURL, schematicID, version, platform, arch, format)
}

// ProviderImageName generates a standardized name (e.g., talos-v1-12-4-amd64-ce4c9805-butler).
func ProviderImageName(platform, version, arch, schematicID string) string {
	v := strings.ReplaceAll(version, ".", "-")
	shortID := schematicID
	if len(shortID) > 8 {
		shortID = shortID[:8]
	}
	return fmt.Sprintf("%s-%s-%s-%s-butler", platform, v, arch, shortID)
}
