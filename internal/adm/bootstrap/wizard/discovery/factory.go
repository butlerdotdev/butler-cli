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

// DefaultFactoryURL is the public Butler Image Factory endpoint.
const DefaultFactoryURL = "https://factory.butlerlabs.dev"

// DefaultTalosSchematic is the default schematic ID for Talos images
// with qemu-guest-agent, iscsi-tools, and util-linux-tools extensions.
const DefaultTalosSchematic = "ce4c980550dd2ab1b17bbf2b08801c7eb59418eafe8f279833297925d67c7515"

// FactoryClient queries the Butler Image Factory API for available
// OS images and builds artifact download URLs.
type FactoryClient struct {
	baseURL    string
	httpClient *http.Client
}

// NewFactoryClient creates a factory client. If baseURL is empty,
// DefaultFactoryURL is used.
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

// CatalogEntry represents an available OS type with its versions and formats.
type CatalogEntry struct {
	OS       string   `json:"os"`
	Versions []string `json:"versions"`
	Formats  []string `json:"formats"`
}

// FetchCatalog returns the list of available OS images from the factory.
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
// Format example: https://factory.butlerlabs.dev/image/{schematicID}/{version}/talos-amd64.qcow2
func (c *FactoryClient) ArtifactURL(schematicID, version, platform, arch, format string) string {
	return fmt.Sprintf("%s/image/%s/%s/%s-%s.%s",
		c.baseURL, schematicID, version, platform, arch, format)
}

// ProviderImageName generates a standardized image name for a synced image.
// Example: talos-v1-12-4-amd64-ce4c9805-butler
func ProviderImageName(platform, version, arch, schematicID string) string {
	v := strings.ReplaceAll(version, ".", "-")
	shortID := schematicID
	if len(shortID) > 8 {
		shortID = shortID[:8]
	}
	return fmt.Sprintf("%s-%s-%s-%s-butler", platform, v, arch, shortID)
}
