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

// Package discovery provides provider-specific resource discovery clients
// for the bootstrap wizard. Each provider implements the ProviderDiscovery
// interface to list available infrastructure resources (networks, images,
// clusters, etc.) so the wizard can present them as selectable options.
package discovery

import (
	"context"
	"errors"
	"fmt"
)

// ErrNotSupported is returned by operations that a provider does not support,
// such as image sync on cloud providers.
var ErrNotSupported = errors.New("operation not supported by this provider")

// Resource type keys returned by Resources(). Providers return a subset of
// these depending on their infrastructure model.
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

// ProviderResource represents a selectable infrastructure resource
// discovered from a provider API.
type ProviderResource struct {
	// Name is the human-readable display name shown in the wizard.
	Name string
	// ID is the provider-specific identifier used in configuration
	// (e.g., a UUID, ARN, or namespace/name pair).
	ID string
	// Description provides additional context (e.g., CIDR range, AZ).
	Description string
}

// ProviderDiscovery connects to an infrastructure provider and discovers
// available resources for the bootstrap wizard.
type ProviderDiscovery interface {
	// Connect validates credentials and establishes a connection to the
	// provider. Returns an error if authentication fails or the provider
	// is unreachable.
	Connect(ctx context.Context) error

	// FetchResource fetches a single resource type from the provider.
	// The resourceType parameter is one of the Resource* constants.
	// parentID optionally scopes the query (e.g., namespace for Harvester
	// networks, region for AWS VPCs).
	FetchResource(ctx context.Context, resourceType string, parentID string) ([]ProviderResource, error)

	// ResourceTypes returns the list of resource types this provider
	// can discover, in the order they should be fetched. Resources
	// earlier in the list are "root" resources fetched during the
	// discovery phase; later ones may depend on a parent selection.
	ResourceTypes() []ResourceTypeInfo

	// SyncImage uploads an OS image from the given artifact URL to the
	// provider. Returns a sync ID for polling progress. Only supported
	// by on-prem providers (Harvester, Nutanix). Cloud providers return
	// ErrNotSupported.
	SyncImage(ctx context.Context, artifactURL, displayName string) (syncID string, err error)

	// PollImageSync checks if an image sync operation has completed.
	// Returns the provider image reference on success.
	PollImageSync(ctx context.Context, syncID string) (ready bool, providerRef string, err error)
}

// ResourceTypeInfo describes a resource type and whether it depends on
// a parent resource selection.
type ResourceTypeInfo struct {
	// Type is one of the Resource* constants.
	Type string
	// Label is the human-readable name shown in the wizard (e.g., "Namespace").
	Label string
	// ParentType is the resource type this depends on, or empty for root
	// resources. When set, this resource is fetched lazily via OptionsFunc
	// after the parent is selected.
	ParentType string
}

// NewDiscovery creates the appropriate discovery client for the given provider.
// The credential fields are read from the provided getters. This allows the
// wizard to construct a discovery client from form state without exposing
// the wizardState struct.
func NewDiscovery(provider string, creds CredentialProvider) (ProviderDiscovery, error) {
	switch provider {
	case "harvester":
		return NewHarvesterDiscovery(creds)
	case "nutanix":
		return NewNutanixDiscovery(creds)
	case "gcp":
		return NewGCPDiscovery(creds)
	case "aws":
		return NewAWSDiscovery(creds)
	case "azure":
		return NewAzureDiscovery(creds)
	default:
		return nil, fmt.Errorf("unsupported provider for discovery: %s", provider)
	}
}

// CredentialProvider abstracts credential access so the discovery package
// does not depend on the wizard's internal state types.
type CredentialProvider interface {
	// Get returns the value for the given credential key, or empty string
	// if not set. Keys are provider-specific (e.g., "kubeconfig", "endpoint",
	// "username", "password", "access_key", "region", etc.).
	Get(key string) string
	// GetBool returns a boolean credential value.
	GetBool(key string) bool
}
