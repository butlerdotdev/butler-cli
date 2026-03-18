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
	"strings"
	"time"

	"github.com/charmbracelet/huh"

	"github.com/butlerdotdev/butler/internal/adm/bootstrap/wizard/discovery"
)

type wizardState struct {
	provider    string
	imageSource string // "factory" or "existing"

	clusterName string
	topology    string

	cpReplicas string
	cpCPU      string
	cpMemoryMB string
	cpDiskGB   string

	workerReplicas string
	workerCPU      string
	workerMemoryMB string
	workerDiskGB   string

	podCIDR     string
	serviceCIDR string
	vip         string

	talosVersion   string
	talosSchematic string

	exposureMode           string
	exposureHostname       string
	exposureIngressClass   string
	exposureControllerType string
	exposureGatewayRef     string

	providerGateway string
	providerDNS     string
	lbAllocMode     string
	lbPoolSize      string

	cniType     string
	storageType string
	lbType      string
	lbStart     string
	lbEnd       string
	capiEnabled bool
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

	harvKubeconfig string
	harvNamespace  string
	harvNetwork    string
	harvImage      string

	nutEndpoint    string
	nutPort        string
	nutInsecure    bool
	nutUsername     string
	nutPassword    string
	nutClusterUUID string
	nutSubnetUUID  string
	nutImageUUID   string

	gcpKeyPath    string
	gcpProjectID  string
	gcpRegion     string
	gcpZone       string
	gcpNetwork    string
	gcpSubnetwork string

	awsAccessKey  string
	awsSecretKey  string
	awsRegion     string
	awsVPCID      string
	awsSubnetID   string
	awsSecGroupID string

	azClientID       string
	azClientSecret   string
	azTenantID       string
	azSubscriptionID string
	azResourceGroup  string
	azLocation       string
	azVNet           string
	azSubnet         string

	confirmed bool
}

func newWizardState() *wizardState {
	return &wizardState{
		imageSource:    "factory",
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
		talosVersion:   "v1.12.2",
		talosSchematic: discovery.DefaultTalosSchematic,
		exposureMode:   "LoadBalancer",
		lbAllocMode:    "static",
		lbPoolSize:     "1",
		cniType:        "cilium",
		storageType:    "longhorn",
		lbType:         "metallb",
		capiEnabled:    true,
		capiVersion:    "v1.9.4",
		ctrlEnabled:    true,
		ctrlVersion:    "latest",
		consEnabled:    true,
		consVersion:    "latest",
		consPassword:   "admin",
	}
}

// stateCredentials adapts wizardState to the discovery.CredentialProvider interface.
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

// providerSelectGroup presents the provider selection dropdown.
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

// resourceSelectGroup dispatches to the provider-specific resource group.
func resourceSelectGroup(s *wizardState, disc discovery.ProviderDiscovery, resources map[string][]discovery.ProviderResource) *huh.Group {
	switch s.provider {
	case "harvester":
		return harvesterResourceGroup(s, disc, resources)
	case "nutanix":
		return nutanixResourceGroup(s, resources)
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

// resourcesToOptions converts discovered resources to huh select options.
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

// fetchOptions lazily fetches child resources when the parent selection changes.
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

// harvesterResourceGroup builds Harvester resource selection (namespace, network, image).
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
			Title("Image Source").
			Description("Sync the latest Talos image from Butler Image Factory,\nor pick an image already uploaded to Harvester.").
			Options(
				huh.NewOption("Sync from Image Factory (recommended)", "factory"),
				huh.NewOption("Use existing provider image", "existing"),
			).
			Value(&s.imageSource),

		huh.NewSelect[string]().
			Title("Image").
			Description("Talos image for VM boot disks").
			OptionsFunc(func() []huh.Option[string] {
				if s.imageSource == "factory" {
					return []huh.Option[string]{
						huh.NewOption("(will sync after configuration)", "factory-pending"),
					}
				}
				return fetchOptions(disc, discovery.ResourceImages, &s.harvNamespace)()
			}, &s.imageSource).
			Value(&s.harvImage),
	)
}

// nutanixResourceGroup builds Nutanix resource selection (cluster, subnet, image).
func nutanixResourceGroup(s *wizardState, resources map[string][]discovery.ProviderResource) *huh.Group {
	clusterOpts := resourcesToOptions(resources[discovery.ResourceClusters])
	subnetOpts := resourcesToOptions(resources[discovery.ResourceSubnets])

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
			Title("Image Source").
			Description("Sync the latest Talos image from Butler Image Factory,\nor pick an image already uploaded to Nutanix.").
			Options(
				huh.NewOption("Sync from Image Factory (recommended)", "factory"),
				huh.NewOption("Use existing provider image", "existing"),
			).
			Value(&s.imageSource),

		huh.NewSelect[string]().
			Title("Image").
			Description("Talos image for VM boot disks").
			OptionsFunc(func() []huh.Option[string] {
				if s.imageSource == "factory" {
					return []huh.Option[string]{
						huh.NewOption("(will sync after configuration)", "factory-pending"),
					}
				}
				return resourcesToOptions(resources[discovery.ResourceImages])
			}, &s.imageSource).
			Value(&s.nutImageUUID),
	)
}

// gcpResourceGroup builds GCP resource selection (region, zone, network, subnet).
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

// awsResourceGroup builds AWS resource selection (region, VPC, subnet, security group).
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

// azureResourceGroup builds Azure resource selection (location, RG, VNet, subnet).
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

// clusterAndSizingStep configures cluster name, topology, and control plane sizing.
func clusterAndSizingStep(s *wizardState) *huh.Group {
	return huh.NewGroup(
		huh.NewNote().
			Title("Cluster Configuration").
			Description("Define the management cluster identity and control plane resources."),

		huh.NewInput().
			Title("Cluster Name").
			Placeholder("butler-mgmt").
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
			Description("Control plane nodes (odd number for etcd quorum)").
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

// workersAndNetworkStep configures worker sizing and CIDRs (hidden for single-node).
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
	}

	return huh.NewGroup(fields...).WithHideFunc(func() bool {
		return s.topology == "single-node"
	})
}

// networkOnlyStep shows CIDRs only (single-node topology).
func networkOnlyStep(s *wizardState) *huh.Group {
	return huh.NewGroup(
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
	).WithHideFunc(func() bool {
		return s.topology != "single-node"
	})
}

// networkingStep covers VIP, MetalLB pool, and tenant IPAM settings (on-prem only).
func networkingStep(s *wizardState) *huh.Group {
	return huh.NewGroup(
		huh.NewNote().
			Title("Networking").
			Description("Management cluster VIP and MetalLB pool, plus tenant IPAM\nallocation settings stored on the ProviderConfig."),

		// --- Management cluster networking ---
		huh.NewInput().
			Title("Control Plane VIP").
			Description("Unused IP for kube-vip HA. Must be outside DHCP range.").
			Placeholder("10.40.0.100").
			Value(&s.vip).
			Validate(validateOptional(validateIP)),

		huh.NewInput().
			Title("Management LB Pool Start").
			Description("First IP for management cluster LoadBalancer services\n(console, steward TCPs in LB mode). Used by MetalLB.").
			Placeholder("10.40.0.200").
			Value(&s.lbStart).
			Validate(validateIP),

		huh.NewInput().
			Title("Management LB Pool End").
			Description("Last IP for management cluster LoadBalancer services").
			Placeholder("10.40.0.250").
			Value(&s.lbEnd).
			Validate(validateIP),

		// --- Tenant IPAM allocation ---
		huh.NewSelect[string]().
			Title("Tenant LB Allocation Mode").
			Description("How LoadBalancer IPs are allocated to tenant clusters.\nStatic: fixed pool per tenant. Elastic: grows/shrinks with demand.").
			Options(
				huh.NewOption("Static (fixed pool per tenant)", "static"),
				huh.NewOption("Elastic (auto-scale with demand)", "elastic"),
			).
			Value(&s.lbAllocMode),

		huh.NewInput().
			Title("Default LB IPs per Tenant").
			Description("Number of LoadBalancer IPs allocated per tenant cluster").
			Value(&s.lbPoolSize).
			Validate(validateIntRange(1, 256)),
	).WithHideFunc(func() bool {
		return !isOnPrem(s.provider)
	})
}

// exposureModeStep selects LoadBalancer, Ingress, or Gateway exposure mode.
func exposureModeStep(s *wizardState) *huh.Group {
	return huh.NewGroup(
		huh.NewSelect[string]().
			Title("Control Plane Exposure Mode").
			Description("How tenant API servers are exposed to clients.\nLoadBalancer: 1 IP per tenant (default, simplest)\nIngress: shared IP via SNI (requires tcp-proxy)\nGateway: shared IP via Gateway API (requires tcp-proxy)").
			Options(
				huh.NewOption("LoadBalancer (1 IP per tenant)", "LoadBalancer"),
				huh.NewOption("Ingress (shared IP, SNI routing)", "Ingress"),
				huh.NewOption("Gateway (shared IP, Gateway API)", "Gateway"),
			).
			Value(&s.exposureMode),
	).WithHideFunc(func() bool {
		return s.topology == "single-node"
	})
}

// exposureIngressStep configures hostname and ingress class for Ingress mode.
func exposureIngressStep(s *wizardState) *huh.Group {
	return huh.NewGroup(
		huh.NewInput().
			Title("Hostname Pattern").
			Description("Wildcard domain for tenant API servers\n(e.g., *.k8s.example.com)").
			Placeholder("*.k8s.example.com").
			Value(&s.exposureHostname).
			Validate(validateNotEmpty),

		huh.NewInput().
			Title("Ingress Class").
			Description("Ingress class name for TCP routing").
			Placeholder("traefik").
			Value(&s.exposureIngressClass).
			Validate(validateNotEmpty),

		huh.NewSelect[string]().
			Title("Controller Type").
			Description("Ingress controller type for tcp-proxy configuration").
			Options(
				huh.NewOption("HAProxy", "haproxy"),
				huh.NewOption("Nginx", "nginx"),
				huh.NewOption("Traefik", "traefik"),
				huh.NewOption("Generic", "generic"),
			).
			Value(&s.exposureControllerType),
	).WithHideFunc(func() bool {
		return s.exposureMode != "Ingress"
	})
}

// exposureGatewayStep configures hostname and gateway ref for Gateway mode.
func exposureGatewayStep(s *wizardState) *huh.Group {
	return huh.NewGroup(
		huh.NewInput().
			Title("Hostname Pattern").
			Description("Wildcard domain for tenant API servers\n(e.g., *.k8s.example.com)").
			Placeholder("*.k8s.example.com").
			Value(&s.exposureHostname).
			Validate(validateNotEmpty),

		huh.NewInput().
			Title("Gateway Reference").
			Description("Gateway resource in namespace/name format").
			Placeholder("butler-system/tcp-gateway").
			Value(&s.exposureGatewayRef).
			Validate(validateNotEmpty),
	).WithHideFunc(func() bool {
		return s.exposureMode != "Gateway"
	})
}

// platformStep configures Talos version/schematic and addon toggles.
func platformStep(s *wizardState) *huh.Group {
	return huh.NewGroup(
		huh.NewNote().
			Title("Platform & Addons").
			Description("Configure Talos OS and Butler components.\nCNI: Cilium  |  Storage: Longhorn"),

		huh.NewInput().
			Title("Talos Version").
			Value(&s.talosVersion).
			Validate(validateNotEmpty),

		huh.NewInput().
			Title("Talos Schematic").
			Description("Image Factory schematic for system extensions.\nDefault includes qemu-guest-agent, iscsi-tools, util-linux-tools.").
			Value(&s.talosSchematic),

		huh.NewConfirm().
			Title("Install CAPI").
			Description("Cluster API for machine lifecycle management").
			Value(&s.capiEnabled),

		huh.NewConfirm().
			Title("Install Butler Controller").
			Description("Reconciler for TenantCluster and addon lifecycle").
			Value(&s.ctrlEnabled),

		huh.NewConfirm().
			Title("Install Butler Console").
			Description("Web dashboard for cluster management").
			Value(&s.consEnabled),
	)
}

// consoleStep configures admin password and ingress toggle.
func consoleStep(s *wizardState) *huh.Group {
	return huh.NewGroup(
		huh.NewNote().
			Title("Console Configuration").
			Description("Configure the Butler web console."),

		huh.NewInput().
			Title("Admin Password").
			Description("Initial admin password for the console").
			Value(&s.consPassword).
			EchoMode(huh.EchoModePassword),

		huh.NewConfirm().
			Title("Enable Ingress").
			Description("Create an Ingress resource for external access").
			Value(&s.consIngress),
	).WithHideFunc(func() bool {
		return !s.consEnabled
	})
}

// consoleIngressStep configures console hostname, class, and TLS.
func consoleIngressStep(s *wizardState) *huh.Group {
	return huh.NewGroup(
		huh.NewInput().
			Title("Console Hostname").
			Placeholder("butler.example.com").
			Value(&s.consHost).
			Validate(validateNotEmpty),

		huh.NewInput().
			Title("Ingress Class").
			Description("Leave empty for cluster default").
			Value(&s.consClass),

		huh.NewConfirm().
			Title("Enable TLS").
			Value(&s.consTLS),
	).WithHideFunc(func() bool {
		return !s.consEnabled || !s.consIngress
	})
}

// reviewStep shows a configuration summary and confirm prompt.
func reviewStep(s *wizardState) *huh.Group {
	return huh.NewGroup(
		huh.NewNote().
			Title("Review").
			DescriptionFunc(func() string {
				return buildReviewSummary(s)
			}, &s.confirmed),

		huh.NewConfirm().
			Title("Start Bootstrap?").
			Description("Begin provisioning the management cluster?").
			Affirmative("Start").
			Negative("Cancel").
			Value(&s.confirmed),
	)
}

// buildReviewSummary formats the wizard state for the review page.
func buildReviewSummary(s *wizardState) string {
	var b strings.Builder

	b.WriteString(fmt.Sprintf("Provider:     %s\n", providerDisplayName(s.provider)))
	b.WriteString(fmt.Sprintf("Cluster:      %s\n", s.clusterName))
	b.WriteString(fmt.Sprintf("Topology:     %s\n", s.topology))

	if s.topology == "single-node" {
		b.WriteString(fmt.Sprintf("Node:         %s vCPU, %s MB RAM, %s GB disk\n",
			s.cpCPU, s.cpMemoryMB, s.cpDiskGB))
	} else {
		b.WriteString(fmt.Sprintf("Control Plane: %sx (%s vCPU, %s MB RAM, %s GB disk)\n",
			s.cpReplicas, s.cpCPU, s.cpMemoryMB, s.cpDiskGB))
		b.WriteString(fmt.Sprintf("Workers:       %sx (%s vCPU, %s MB RAM, %s GB disk)\n",
			s.workerReplicas, s.workerCPU, s.workerMemoryMB, s.workerDiskGB))
	}

	b.WriteString(fmt.Sprintf("Pod CIDR:     %s\n", s.podCIDR))
	b.WriteString(fmt.Sprintf("Service CIDR: %s\n", s.serviceCIDR))

	if isOnPrem(s.provider) && s.vip != "" {
		b.WriteString(fmt.Sprintf("VIP:          %s\n", s.vip))
	}

	if isOnPrem(s.provider) && s.lbStart != "" && s.lbEnd != "" {
		b.WriteString(fmt.Sprintf("Mgmt LB:      %s - %s\n", s.lbStart, s.lbEnd))
	}

	if isOnPrem(s.provider) {
		b.WriteString(fmt.Sprintf("Tenant LB:    %s, %s IPs/tenant\n", s.lbAllocMode, s.lbPoolSize))
	}

	if s.topology != "single-node" {
		b.WriteString(fmt.Sprintf("Exposure:     %s", s.exposureMode))
		if s.exposureMode == "Ingress" || s.exposureMode == "Gateway" {
			b.WriteString(fmt.Sprintf(" (%s)", s.exposureHostname))
		}
		b.WriteString("\n")
	}

	if isOnPrem(s.provider) {
		if s.imageSource == "factory" {
			b.WriteString("Image:        Sync from Image Factory\n")
		} else {
			ref := s.harvImage
			if s.provider == "nutanix" {
				ref = s.nutImageUUID
			}
			b.WriteString(fmt.Sprintf("Image:        %s (existing)\n", ref))
		}
	}

	b.WriteString(fmt.Sprintf("Talos:        %s\n", s.talosVersion))

	var components []string
	components = append(components, "Cilium", "Longhorn")
	if isOnPrem(s.provider) {
		components = append(components, "MetalLB")
	}
	if s.capiEnabled {
		components = append(components, "CAPI")
	}
	if s.ctrlEnabled {
		components = append(components, "Butler Controller")
	}
	if s.consEnabled {
		components = append(components, "Console")
	}
	b.WriteString(fmt.Sprintf("Addons:       %s", strings.Join(components, ", ")))

	return b.String()
}
