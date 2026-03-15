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

package wizard

import (
	"context"
	"fmt"
	"time"

	"github.com/charmbracelet/huh"

	"github.com/butlerdotdev/butler/internal/adm/bootstrap/wizard/discovery"
)

// wizardState holds intermediate form values as strings. huh's Input
// fields bind to *string, so numeric values are stored as strings and
// parsed to int32 when building the final Config.
type wizardState struct {
	// Provider selection
	provider string

	// Cluster basics
	clusterName string
	topology    string

	// Control plane sizing
	cpReplicas string
	cpCPU      string
	cpMemoryMB string
	cpDiskGB   string

	// Worker sizing
	workerReplicas string
	workerCPU      string
	workerMemoryMB string
	workerDiskGB   string

	// Networking
	podCIDR     string
	serviceCIDR string
	vip         string

	// Talos
	talosVersion   string
	talosSchematic string

	// Addons
	cniType      string
	storageType  string
	lbType       string
	lbPool       string
	capiEnabled  bool
	capiVersion  string
	ctrlEnabled  bool
	ctrlVersion  string
	ctrlImage    string
	consEnabled  bool
	consVersion  string
	consIngress  bool
	consHost     string
	consClass    string
	consTLS      bool
	consPassword string

	// Provider credentials
	// Harvester
	harvKubeconfig string
	harvNamespace  string
	harvNetwork    string
	harvImage      string

	// Nutanix
	nutEndpoint    string
	nutPort        string
	nutInsecure    bool
	nutUsername     string
	nutPassword    string
	nutClusterUUID string
	nutSubnetUUID  string
	nutImageUUID   string

	// GCP
	gcpKeyPath    string
	gcpProjectID  string
	gcpRegion     string
	gcpZone       string
	gcpNetwork    string
	gcpSubnetwork string

	// AWS
	awsAccessKey  string
	awsSecretKey  string
	awsRegion     string
	awsVPCID      string
	awsSubnetID   string
	awsSecGroupID string

	// Azure
	azClientID       string
	azClientSecret   string
	azTenantID       string
	azSubscriptionID string
	azResourceGroup  string
	azLocation       string
	azVNet           string
	azSubnet         string

	// Review
	confirmed bool
}

func newWizardState() *wizardState {
	return &wizardState{
		topology:       "ha",
		cpReplicas:     "3",
		cpCPU:          "4",
		cpMemoryMB:     "8192",
		cpDiskGB:       "50",
		workerReplicas: "3",
		workerCPU:      "4",
		workerMemoryMB: "16384",
		workerDiskGB:   "100",
		podCIDR:        "10.244.0.0/16",
		serviceCIDR:    "10.96.0.0/12",
		talosVersion:   "v1.9.0",
		cniType:        "cilium",
		storageType:    "longhorn",
		lbType:         "metallb",
		capiEnabled:    true,
		capiVersion:    "v1.9.0",
		ctrlEnabled:    true,
		ctrlVersion:    "latest",
		consEnabled:    true,
		consVersion:    "latest",
	}
}

// stateCredentials implements discovery.CredentialProvider by reading
// from the wizard state fields populated by Form 1.
type stateCredentials struct {
	s *wizardState
}

func (c *stateCredentials) Get(key string) string {
	switch key {
	// Harvester
	case "kubeconfig":
		return c.s.harvKubeconfig
	// Nutanix
	case "endpoint":
		return c.s.nutEndpoint
	case "port":
		return c.s.nutPort
	case "username":
		return c.s.nutUsername
	case "password":
		return c.s.nutPassword
	// GCP
	case "key_path":
		return c.s.gcpKeyPath
	case "project_id":
		return c.s.gcpProjectID
	// AWS
	case "access_key":
		return c.s.awsAccessKey
	case "secret_key":
		return c.s.awsSecretKey
	case "region":
		switch c.s.provider {
		case "gcp":
			return c.s.gcpRegion
		case "aws":
			return c.s.awsRegion
		default:
			return ""
		}
	// Azure
	case "client_id":
		return c.s.azClientID
	case "client_secret":
		return c.s.azClientSecret
	case "tenant_id":
		return c.s.azTenantID
	case "subscription_id":
		return c.s.azSubscriptionID
	default:
		return ""
	}
}

func (c *stateCredentials) GetBool(key string) bool {
	switch key {
	case "insecure":
		return c.s.nutInsecure
	default:
		return false
	}
}

// providerSelectGroup asks the user to choose an infrastructure provider.
func providerSelectGroup(s *wizardState) *huh.Group {
	return huh.NewGroup(
		huh.NewNote().
			Title("Provider").
			Description("Select your infrastructure provider."),

		huh.NewSelect[string]().
			Title("Infrastructure Provider").
			Options(
				huh.NewOption("Harvester (HCI)", "harvester"),
				huh.NewOption("Nutanix (AHV)", "nutanix"),
				huh.NewOption("Google Cloud (GCP)", "gcp"),
				huh.NewOption("Amazon Web Services (AWS)", "aws"),
				huh.NewOption("Microsoft Azure", "azure"),
			).
			Value(&s.provider),
	)
}

// resourceSelectGroup dynamically builds a resource selection group
// from discovered provider resources. Root resources are presented as
// static selects. Child resources that depend on a parent use OptionsFunc
// for lazy loading when the parent value changes.
func resourceSelectGroup(s *wizardState, disc discovery.ProviderDiscovery, resources map[string][]discovery.ProviderResource) *huh.Group {
	switch s.provider {
	case "harvester":
		return harvesterResourceGroup(s, disc, resources)
	case "nutanix":
		return nutanixResourceGroup(s, disc, resources)
	case "gcp":
		return gcpResourceGroup(s, disc, resources)
	case "aws":
		return awsResourceGroup(s, disc, resources)
	case "azure":
		return azureResourceGroup(s, disc, resources)
	default:
		return huh.NewGroup(
			huh.NewNote().Title("Resources").Description("No resources to configure."),
		)
	}
}

// resourcesToOptions converts discovered resources into huh Select options.
func resourcesToOptions(resources []discovery.ProviderResource) []huh.Option[string] {
	if len(resources) == 0 {
		return []huh.Option[string]{huh.NewOption("(none found)", "")}
	}
	opts := make([]huh.Option[string], 0, len(resources))
	for _, r := range resources {
		label := r.Name
		if r.Description != "" {
			label = fmt.Sprintf("%s (%s)", r.Name, r.Description)
		}
		opts = append(opts, huh.NewOption(label, r.ID))
	}
	return opts
}

// fetchOptions returns an OptionsFunc that lazily fetches child resources
// from the discovery client based on a parent selection.
func fetchOptions(disc discovery.ProviderDiscovery, resourceType string, parentID *string) func() []huh.Option[string] {
	return func() []huh.Option[string] {
		if parentID == nil || *parentID == "" {
			return []huh.Option[string]{huh.NewOption("(select parent first)", "")}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		results, err := disc.FetchResource(ctx, resourceType, *parentID)
		if err != nil {
			return []huh.Option[string]{huh.NewOption(fmt.Sprintf("error: %v", err), "")}
		}
		return resourcesToOptions(results)
	}
}

// harvesterResourceGroup builds resource selection for Harvester.
// Namespaces are pre-fetched; networks and images cascade from namespace.
func harvesterResourceGroup(s *wizardState, disc discovery.ProviderDiscovery, resources map[string][]discovery.ProviderResource) *huh.Group {
	nsOpts := resourcesToOptions(resources[discovery.ResourceNamespaces])

	return huh.NewGroup(
		huh.NewNote().
			Title("Infrastructure Resources").
			Description("Select the Harvester resources for VM placement."),

		huh.NewSelect[string]().
			Title("Namespace").
			Options(nsOpts...).
			Value(&s.harvNamespace),

		huh.NewSelect[string]().
			Title("Network").
			OptionsFunc(fetchOptions(disc, discovery.ResourceNetworks, &s.harvNamespace), &s.harvNamespace).
			Value(&s.harvNetwork),

		huh.NewSelect[string]().
			Title("Image").
			Description("Select a Talos image or sync a new one").
			OptionsFunc(fetchOptions(disc, discovery.ResourceImages, &s.harvNamespace), &s.harvNamespace).
			Value(&s.harvImage),
	)
}

// nutanixResourceGroup builds resource selection for Nutanix.
// All resources are top-level (no cascading).
func nutanixResourceGroup(s *wizardState, _ discovery.ProviderDiscovery, resources map[string][]discovery.ProviderResource) *huh.Group {
	clusterOpts := resourcesToOptions(resources[discovery.ResourceClusters])
	subnetOpts := resourcesToOptions(resources[discovery.ResourceSubnets])
	imageOpts := resourcesToOptions(resources[discovery.ResourceImages])

	return huh.NewGroup(
		huh.NewNote().
			Title("Infrastructure Resources").
			Description("Select the Nutanix resources for VM placement."),

		huh.NewSelect[string]().
			Title("Cluster").
			Options(clusterOpts...).
			Value(&s.nutClusterUUID),

		huh.NewSelect[string]().
			Title("Subnet").
			Options(subnetOpts...).
			Value(&s.nutSubnetUUID),

		huh.NewSelect[string]().
			Title("Image").
			Description("Select a Talos image or sync a new one").
			Options(imageOpts...).
			Value(&s.nutImageUUID),
	)
}

// gcpResourceGroup builds resource selection for GCP.
// Regions and networks are pre-fetched; zones and subnetworks cascade.
func gcpResourceGroup(s *wizardState, disc discovery.ProviderDiscovery, resources map[string][]discovery.ProviderResource) *huh.Group {
	regionOpts := resourcesToOptions(resources[discovery.ResourceRegions])
	networkOpts := resourcesToOptions(resources[discovery.ResourceNetworks])

	return huh.NewGroup(
		huh.NewNote().
			Title("Infrastructure Resources").
			Description("Select the GCP resources for VM placement."),

		huh.NewSelect[string]().
			Title("Region").
			Options(regionOpts...).
			Value(&s.gcpRegion),

		huh.NewSelect[string]().
			Title("Zone").
			OptionsFunc(fetchOptions(disc, discovery.ResourceZones, &s.gcpRegion), &s.gcpRegion).
			Value(&s.gcpZone),

		huh.NewSelect[string]().
			Title("Network").
			Options(networkOpts...).
			Value(&s.gcpNetwork),

		huh.NewSelect[string]().
			Title("Subnetwork").
			OptionsFunc(fetchOptions(disc, discovery.ResourceSubnets, &s.gcpRegion), &s.gcpRegion).
			Value(&s.gcpSubnetwork),
	)
}

// awsResourceGroup builds resource selection for AWS.
// Regions are pre-fetched; VPCs cascade from region, subnets and SGs
// cascade from VPC.
func awsResourceGroup(s *wizardState, disc discovery.ProviderDiscovery, resources map[string][]discovery.ProviderResource) *huh.Group {
	regionOpts := resourcesToOptions(resources[discovery.ResourceRegions])

	return huh.NewGroup(
		huh.NewNote().
			Title("Infrastructure Resources").
			Description("Select the AWS resources for VM placement."),

		huh.NewSelect[string]().
			Title("Region").
			Options(regionOpts...).
			Value(&s.awsRegion),

		huh.NewSelect[string]().
			Title("VPC").
			OptionsFunc(fetchOptions(disc, discovery.ResourceVPCs, &s.awsRegion), &s.awsRegion).
			Value(&s.awsVPCID),

		huh.NewSelect[string]().
			Title("Subnet").
			OptionsFunc(fetchOptions(disc, discovery.ResourceSubnets, &s.awsVPCID), &s.awsVPCID).
			Value(&s.awsSubnetID),

		huh.NewSelect[string]().
			Title("Security Group").
			OptionsFunc(fetchOptions(disc, discovery.ResourceSecurityGroups, &s.awsVPCID), &s.awsVPCID).
			Value(&s.awsSecGroupID),
	)
}

// azureResourceGroup builds resource selection for Azure.
// Locations and resource groups are pre-fetched; VNets cascade from
// resource group, subnets cascade from VNet.
func azureResourceGroup(s *wizardState, disc discovery.ProviderDiscovery, resources map[string][]discovery.ProviderResource) *huh.Group {
	locationOpts := resourcesToOptions(resources[discovery.ResourceLocations])
	rgOpts := resourcesToOptions(resources[discovery.ResourceResourceGroups])

	return huh.NewGroup(
		huh.NewNote().
			Title("Infrastructure Resources").
			Description("Select the Azure resources for VM placement."),

		huh.NewSelect[string]().
			Title("Location").
			Options(locationOpts...).
			Value(&s.azLocation),

		huh.NewSelect[string]().
			Title("Resource Group").
			Options(rgOpts...).
			Value(&s.azResourceGroup),

		huh.NewSelect[string]().
			Title("Virtual Network").
			OptionsFunc(fetchOptions(disc, discovery.ResourceVNets, &s.azResourceGroup), &s.azResourceGroup).
			Value(&s.azVNet),

		huh.NewSelect[string]().
			Title("Subnet").
			OptionsFunc(fetchOptions(disc, discovery.ResourceSubnets, &s.azVNet), &s.azVNet).
			Value(&s.azSubnet),
	)
}

// clusterAndSizingStep combines cluster identity and control plane sizing
// into a single dense page.
func clusterAndSizingStep(s *wizardState) *huh.Group {
	return huh.NewGroup(
		huh.NewNote().
			Title("Cluster Configuration").
			Description("Define the management cluster identity and control plane resources."),

		huh.NewInput().
			Title("Cluster Name").
			Description("Lowercase alphanumeric with hyphens, max 63 chars").
			Value(&s.clusterName).
			Validate(validateClusterName),

		huh.NewSelect[string]().
			Title("Topology").
			Options(
				huh.NewOption("High Availability (3 CP + workers)", "ha"),
				huh.NewOption("Single Node (1 node, no workers)", "single-node"),
			).
			Value(&s.topology),

		huh.NewInput().
			Title("CP Replicas").
			Description("Control plane nodes (odd for etcd quorum)").
			Value(&s.cpReplicas).
			Validate(validateIntRange(1, 7)),

		huh.NewInput().
			Title("CP vCPUs").
			Value(&s.cpCPU).
			Validate(validateIntRange(1, 64)),

		huh.NewInput().
			Title("CP Memory (MB)").
			Value(&s.cpMemoryMB).
			Validate(validateIntRange(1024, 131072)),

		huh.NewInput().
			Title("CP Disk (GB)").
			Value(&s.cpDiskGB).
			Validate(validateIntRange(20, 2048)),
	)
}

// workersAndNetworkStep combines worker sizing and network config into
// one page. Hidden for single-node topology. VIP is only shown for
// on-prem providers.
func workersAndNetworkStep(s *wizardState) *huh.Group {
	fields := []huh.Field{
		huh.NewNote().
			Title("Workers & Networking").
			Description("Configure worker nodes and cluster networking."),

		huh.NewInput().
			Title("Worker Replicas").
			Value(&s.workerReplicas).
			Validate(validateIntRange(1, 100)),

		huh.NewInput().
			Title("Worker vCPUs").
			Value(&s.workerCPU).
			Validate(validateIntRange(1, 128)),

		huh.NewInput().
			Title("Worker Memory (MB)").
			Value(&s.workerMemoryMB).
			Validate(validateIntRange(1024, 524288)),

		huh.NewInput().
			Title("Worker Disk (GB)").
			Value(&s.workerDiskGB).
			Validate(validateIntRange(20, 4096)),

		huh.NewInput().
			Title("Pod CIDR").
			Value(&s.podCIDR).
			Validate(validateCIDR),

		huh.NewInput().
			Title("Service CIDR").
			Value(&s.serviceCIDR).
			Validate(validateCIDR),

		huh.NewInput().
			Title("Control Plane VIP").
			Description("Virtual IP for HA. Required for on-prem, skipped for cloud.").
			Value(&s.vip).
			Validate(validateOptional(validateIP)),
	}

	return huh.NewGroup(fields...).WithHideFunc(func() bool {
		return s.topology == "single-node"
	})
}

// networkOnlyStep shows just networking fields for single-node topology.
func networkOnlyStep(s *wizardState) *huh.Group {
	fields := []huh.Field{
		huh.NewNote().
			Title("Networking").
			Description("Configure cluster networking."),

		huh.NewInput().
			Title("Pod CIDR").
			Value(&s.podCIDR).
			Validate(validateCIDR),

		huh.NewInput().
			Title("Service CIDR").
			Value(&s.serviceCIDR).
			Validate(validateCIDR),

		huh.NewInput().
			Title("Control Plane VIP").
			Description("Virtual IP. Required for on-prem, skipped for cloud.").
			Value(&s.vip).
			Validate(validateOptional(validateIP)),
	}

	return huh.NewGroup(fields...).WithHideFunc(func() bool {
		return s.topology != "single-node"
	})
}

// platformStep combines Talos config, addons, and component toggles.
func platformStep(s *wizardState) *huh.Group {
	fields := []huh.Field{
		huh.NewNote().
			Title("Platform & Addons").
			Description("Configure Talos OS, networking stack, and Butler components."),

		huh.NewInput().
			Title("Talos Version").
			Value(&s.talosVersion).
			Validate(validateNotEmpty),

		huh.NewInput().
			Title("Talos Schematic").
			Description("Extension schematic ID (leave empty for default)").
			Value(&s.talosSchematic),

		huh.NewSelect[string]().
			Title("CNI").
			Options(huh.NewOption("Cilium", "cilium")).
			Value(&s.cniType),

		huh.NewSelect[string]().
			Title("Storage").
			Options(huh.NewOption("Longhorn", "longhorn")).
			Value(&s.storageType),

		huh.NewSelect[string]().
			Title("Load Balancer").
			Description("MetalLB for on-prem, skipped for cloud").
			Options(
				huh.NewOption("MetalLB", "metallb"),
				huh.NewOption("None (cloud provider)", "none"),
			).
			Value(&s.lbType),

		huh.NewInput().
			Title("LB Address Pool").
			Description("IP range for LoadBalancer services (e.g., 10.40.0.100-10.40.0.200)").
			Value(&s.lbPool),

		huh.NewConfirm().
			Title("Install CAPI").
			Description("Cluster API for machine lifecycle").
			Value(&s.capiEnabled),

		huh.NewConfirm().
			Title("Install Butler Controller").
			Value(&s.ctrlEnabled),

		huh.NewConfirm().
			Title("Install Butler Console").
			Value(&s.consEnabled),
	}

	return huh.NewGroup(fields...)
}

// consoleStep builds the console configuration step.
// Only shown when console is enabled.
func consoleStep(s *wizardState) *huh.Group {
	return huh.NewGroup(
		huh.NewNote().
			Title("Console Configuration").
			Description("Configure the Butler web console."),

		huh.NewConfirm().
			Title("Enable Ingress").
			Description("Create an Ingress resource for the console").
			Value(&s.consIngress),

		huh.NewInput().
			Title("Console Hostname").
			Description("e.g., butler.example.com").
			Value(&s.consHost),

		huh.NewInput().
			Title("Ingress Class").
			Description("Leave empty for cluster default").
			Value(&s.consClass),

		huh.NewConfirm().
			Title("Enable TLS").
			Value(&s.consTLS),

		huh.NewInput().
			Title("Admin Password").
			Description("Initial admin password (default: admin)").
			Value(&s.consPassword),
	).WithHideFunc(func() bool {
		return !s.consEnabled
	})
}
