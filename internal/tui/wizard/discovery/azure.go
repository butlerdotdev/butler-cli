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
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/network/armnetwork/v6"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/resources/armresources"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/resources/armsubscriptions"
)

func init() {
	Register("azure", func(creds CredentialProvider) (ProviderDiscovery, error) {
		return NewAzureDiscovery(creds)
	})
}

// AzureDiscovery discovers resources on Microsoft Azure using the
// ARM resource manager SDKs.
type AzureDiscovery struct {
	clientID       string
	clientSecret   string
	tenantID       string
	subscriptionID string

	subClient *armsubscriptions.Client
	rgClient  *armresources.ResourceGroupsClient
	netFact   *armnetwork.ClientFactory
}

// NewAzureDiscovery creates an Azure discovery client from credentials.
func NewAzureDiscovery(creds CredentialProvider) (*AzureDiscovery, error) {
	clientID := creds.Get("client_id")
	if clientID == "" {
		return nil, fmt.Errorf("Azure client ID is required")
	}
	clientSecret := creds.Get("client_secret")
	if clientSecret == "" {
		return nil, fmt.Errorf("Azure client secret is required")
	}
	tenantID := creds.Get("tenant_id")
	if tenantID == "" {
		return nil, fmt.Errorf("Azure tenant ID is required")
	}
	subscriptionID := creds.Get("subscription_id")
	if subscriptionID == "" {
		return nil, fmt.Errorf("Azure subscription ID is required")
	}

	return &AzureDiscovery{
		clientID:       clientID,
		clientSecret:   clientSecret,
		tenantID:       tenantID,
		subscriptionID: subscriptionID,
	}, nil
}

func (a *AzureDiscovery) Connect(ctx context.Context) error {
	cred, err := azidentity.NewClientSecretCredential(a.tenantID, a.clientID, a.clientSecret, nil)
	if err != nil {
		return fmt.Errorf("creating Azure credential: %w", err)
	}

	a.subClient, err = armsubscriptions.NewClient(cred, nil)
	if err != nil {
		return fmt.Errorf("creating subscriptions client: %w", err)
	}

	a.rgClient, err = armresources.NewResourceGroupsClient(a.subscriptionID, cred, nil)
	if err != nil {
		return fmt.Errorf("creating resource groups client: %w", err)
	}

	a.netFact, err = armnetwork.NewClientFactory(a.subscriptionID, cred, nil)
	if err != nil {
		return fmt.Errorf("creating network client: %w", err)
	}

	// Validate connectivity by listing locations.
	pager := a.subClient.NewListLocationsPager(a.subscriptionID, nil)
	if pager.More() {
		_, err = pager.NextPage(ctx)
		if err != nil {
			return fmt.Errorf("connecting to Azure subscription %s: %w", a.subscriptionID, err)
		}
	}

	return nil
}

func (a *AzureDiscovery) ResourceTypes() []ResourceTypeInfo {
	return []ResourceTypeInfo{
		{Type: ResourceLocations, Label: "Location", ParentType: ""},
		{Type: ResourceResourceGroups, Label: "Resource Group", ParentType: ""},
		{Type: ResourceVNets, Label: "Virtual Network", ParentType: ResourceResourceGroups},
		{Type: ResourceSubnets, Label: "Subnet", ParentType: ResourceVNets},
		{Type: ResourceSecurityGroups, Label: "Network Security Group", ParentType: ResourceResourceGroups},
		{Type: ResourceVMSizes, Label: "VM Size", ParentType: ResourceLocations},
	}
}

func (a *AzureDiscovery) FetchResource(ctx context.Context, resourceType string, parentID string) ([]ProviderResource, error) {
	switch resourceType {
	case ResourceLocations:
		return a.listLocations(ctx)
	case ResourceResourceGroups:
		return a.listResourceGroups(ctx)
	case ResourceVNets:
		return a.listVNets(ctx, parentID)
	case ResourceSubnets:
		return a.listSubnets(ctx, parentID)
	case ResourceSecurityGroups:
		return a.listNSGs(ctx, parentID)
	case ResourceVMSizes:
		return a.listVMSizes(ctx, parentID)
	default:
		return nil, fmt.Errorf("unsupported resource type for Azure: %s", resourceType)
	}
}

func (a *AzureDiscovery) listLocations(ctx context.Context) ([]ProviderResource, error) {
	pager := a.subClient.NewListLocationsPager(a.subscriptionID, nil)

	var resources []ProviderResource
	for pager.More() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("listing locations: %w", err)
		}
		for _, loc := range page.Value {
			if loc.Name == nil || loc.DisplayName == nil {
				continue
			}
			resources = append(resources, ProviderResource{
				Name:        *loc.DisplayName,
				ID:          *loc.Name,
				Description: *loc.Name,
			})
		}
	}
	return resources, nil
}

func (a *AzureDiscovery) listResourceGroups(ctx context.Context) ([]ProviderResource, error) {
	pager := a.rgClient.NewListPager(nil)

	var resources []ProviderResource
	for pager.More() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("listing resource groups: %w", err)
		}
		for _, rg := range page.Value {
			if rg.Name == nil {
				continue
			}
			loc := ""
			if rg.Location != nil {
				loc = *rg.Location
			}
			resources = append(resources, ProviderResource{
				Name:        *rg.Name,
				ID:          *rg.Name,
				Description: loc,
			})
		}
	}
	return resources, nil
}

func (a *AzureDiscovery) listVNets(ctx context.Context, resourceGroup string) ([]ProviderResource, error) {
	if resourceGroup == "" {
		return nil, fmt.Errorf("resource group is required for VNet listing")
	}

	client := a.netFact.NewVirtualNetworksClient()
	pager := client.NewListPager(resourceGroup, nil)

	var resources []ProviderResource
	for pager.More() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("listing VNets: %w", err)
		}
		for _, vnet := range page.Value {
			if vnet.Name == nil {
				continue
			}
			desc := ""
			if vnet.Properties != nil && vnet.Properties.AddressSpace != nil {
				var cidrs []string
				for _, prefix := range vnet.Properties.AddressSpace.AddressPrefixes {
					if prefix != nil {
						cidrs = append(cidrs, *prefix)
					}
				}
				desc = strings.Join(cidrs, ", ")
			}
			// Encode resource group in the ID for subnet lookups.
			resources = append(resources, ProviderResource{
				Name:        *vnet.Name,
				ID:          resourceGroup + "/" + *vnet.Name,
				Description: desc,
			})
		}
	}
	return resources, nil
}

func (a *AzureDiscovery) listSubnets(ctx context.Context, vnetRef string) ([]ProviderResource, error) {
	// vnetRef is "resourceGroup/vnetName" format.
	rg, vnetName := splitRef(vnetRef)
	if rg == "" || vnetName == "" {
		return nil, fmt.Errorf("VNet reference must be in 'resourceGroup/vnetName' format")
	}

	client := a.netFact.NewSubnetsClient()
	pager := client.NewListPager(rg, vnetName, nil)

	var resources []ProviderResource
	for pager.More() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("listing subnets: %w", err)
		}
		for _, subnet := range page.Value {
			if subnet.Name == nil {
				continue
			}
			desc := ""
			if subnet.Properties != nil && subnet.Properties.AddressPrefix != nil {
				desc = *subnet.Properties.AddressPrefix
			}
			resources = append(resources, ProviderResource{
				Name:        *subnet.Name,
				ID:          *subnet.Name,
				Description: desc,
			})
		}
	}
	return resources, nil
}

func (a *AzureDiscovery) listNSGs(ctx context.Context, resourceGroup string) ([]ProviderResource, error) {
	nsgClient := a.netFact.NewSecurityGroupsClient()
	var resources []ProviderResource
	pager := nsgClient.NewListPager(resourceGroup, nil)
	for pager.More() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("listing NSGs: %w", err)
		}
		for _, nsg := range page.Value {
			name := ""
			if nsg.Name != nil {
				name = *nsg.Name
			}
			resources = append(resources, ProviderResource{
				Name: name,
				ID:   name,
			})
		}
	}
	return resources, nil
}

func (a *AzureDiscovery) listVMSizes(ctx context.Context, location string) ([]ProviderResource, error) {
	// VM sizes require the compute SDK. Use the REST API via the network
	// factory's subscription to avoid pulling another SDK module. For now,
	// return common sizes as static options — the full list is 700+ entries
	// and overwhelms a Select field.
	common := []ProviderResource{
		{Name: "Standard_D2s_v3", ID: "Standard_D2s_v3", Description: "2 vCPU, 8 GB"},
		{Name: "Standard_D4s_v3", ID: "Standard_D4s_v3", Description: "4 vCPU, 16 GB"},
		{Name: "Standard_D8s_v3", ID: "Standard_D8s_v3", Description: "8 vCPU, 32 GB"},
		{Name: "Standard_D16s_v3", ID: "Standard_D16s_v3", Description: "16 vCPU, 64 GB"},
		{Name: "Standard_DC4s_v3", ID: "Standard_DC4s_v3", Description: "4 vCPU, 32 GB (confidential)"},
		{Name: "Standard_E4s_v3", ID: "Standard_E4s_v3", Description: "4 vCPU, 32 GB (memory-opt)"},
		{Name: "Standard_E8s_v3", ID: "Standard_E8s_v3", Description: "8 vCPU, 64 GB (memory-opt)"},
		{Name: "Standard_F4s_v2", ID: "Standard_F4s_v2", Description: "4 vCPU, 8 GB (compute-opt)"},
		{Name: "Standard_F8s_v2", ID: "Standard_F8s_v2", Description: "8 vCPU, 16 GB (compute-opt)"},
	}
	return common, nil
}

func (a *AzureDiscovery) SyncImage(_ context.Context, _, _ string) (string, error) {
	return "", ErrNotSupported
}

func (a *AzureDiscovery) PollImageSync(_ context.Context, _ string) (bool, string, error) {
	return false, "", ErrNotSupported
}
