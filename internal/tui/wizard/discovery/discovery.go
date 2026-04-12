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

// Package discovery provides provider-specific resource discovery for the
// bootstrap wizard.
package discovery

import (
	"context"
	"errors"
	"fmt"
)

// ErrNotSupported is returned by operations a provider does not support.
var ErrNotSupported = errors.New("operation not supported by this provider")

// Resource type keys used in discovery and form group construction.
const (
	ResourceNamespaces     = "namespaces"
	ResourceNetworks       = "networks"
	ResourceImages         = "images"
	ResourceClusters       = "clusters"
	ResourceSubnets        = "subnets"
	ResourceRegions        = "regions"
	ResourceZones          = "zones"
	ResourceVPCs           = "vpcs"
	ResourceSecurityGroups = "security_groups"
	ResourceResourceGroups = "resource_groups"
	ResourceVNets          = "vnets"
	ResourceLocations      = "locations"
)

// ProviderResource represents a selectable infrastructure resource.
type ProviderResource struct {
	Name        string // Display name
	ID          string // Provider-specific identifier (UUID, ARN, namespace/name)
	Description string // Additional context (CIDR, AZ, etc.)
}

// ProviderDiscovery connects to a provider and discovers available resources.
type ProviderDiscovery interface {
	// Connect validates credentials and establishes a provider connection.
	Connect(ctx context.Context) error

	// FetchResource fetches resources of the given type, optionally scoped by parentID.
	FetchResource(ctx context.Context, resourceType string, parentID string) ([]ProviderResource, error)

	// ResourceTypes returns discoverable resource types in fetch order.
	ResourceTypes() []ResourceTypeInfo

	// SyncImage uploads an OS image to the provider. Returns a sync ID for polling.
	SyncImage(ctx context.Context, artifactURL, displayName string) (syncID string, err error)

	// PollImageSync checks if an image sync has completed.
	PollImageSync(ctx context.Context, syncID string) (ready bool, providerRef string, err error)
}

// ResourceTypeInfo describes a discoverable resource type.
type ResourceTypeInfo struct {
	Type       string // Resource* constant
	Label      string // Display label (e.g., "Namespace")
	ParentType string // Empty for root resources, set for cascading children
}

// Constructor builds a ProviderDiscovery from credential input. Providers
// register via init() so cloud providers can live behind the cloud_discovery
// build tag without this file ever referencing them.
type Constructor func(CredentialProvider) (ProviderDiscovery, error)

var registry = map[string]Constructor{}

// Register adds a provider constructor. Called from each provider's init().
// Not safe for concurrent use — expected to run only during package init.
func Register(provider string, fn Constructor) {
	registry[provider] = fn
}

// NewDiscovery creates the discovery client for the given provider. Returns
// an error if the provider is unknown or was compiled out (cloud providers
// are gated behind the cloud_discovery build tag).
func NewDiscovery(provider string, creds CredentialProvider) (ProviderDiscovery, error) {
	fn, ok := registry[provider]
	if !ok {
		return nil, fmt.Errorf("discovery not available for provider %q (cloud providers require building with -tags cloud_discovery)", provider)
	}
	return fn(creds)
}

// CredentialProvider abstracts credential access from the wizard state.
type CredentialProvider interface {
	Get(key string) string
	GetBool(key string) bool
}
