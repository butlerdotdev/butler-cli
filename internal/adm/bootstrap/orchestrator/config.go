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

package orchestrator

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/viper"
)

// Config represents the bootstrap configuration
type Config struct {
	// Provider is the infrastructure provider (harvester, nutanix, proxmox, aws, gcp, azure)
	Provider string `mapstructure:"provider"`

	// Cluster defines the management cluster configuration
	Cluster ClusterConfig `mapstructure:"cluster"`

	// Network defines networking configuration
	Network NetworkConfig `mapstructure:"network"`

	// Talos defines Talos Linux configuration
	Talos TalosConfig `mapstructure:"talos"`

	// Addons defines which addons to install
	Addons AddonsConfig `mapstructure:"addons"`

	// ControlPlaneExposure configures how tenant control planes are exposed.
	// If omitted, defaults to LoadBalancer mode.
	ControlPlaneExposure *ControlPlaneExposureConfig `mapstructure:"controlPlaneExposure,omitempty"`

	// MultiTenancyMode sets the ButlerConfig multi-tenancy mode on the
	// management cluster after bootstrap. "Optional" (default) lets
	// clusters exist without a team; "Enforced" requires every cluster
	// to belong to a team with quota enforcement.
	MultiTenancyMode string `mapstructure:"multiTenancyMode,omitempty"`

	// NetworkPool defines the platform-level IPAM pool created during bootstrap.
	// If nil, no NetworkPool is created and tenant IP allocation must be
	// configured manually post-bootstrap.
	NetworkPool *NetworkPoolConfig `mapstructure:"networkPool,omitempty"`

	// ProviderNetwork configures the ProviderConfig CR's spec.network section,
	// binding the provider to the NetworkPool for tenant IPAM. If nil, the
	// provider's ProviderConfig is created without a network section (no IPAM).
	ProviderNetwork *ProviderNetworkConfig `mapstructure:"providerNetwork,omitempty"`

	// ProviderConfig contains provider-specific settings
	ProviderConfig ProviderConfig `mapstructure:"providerConfig"`
}

// NetworkPoolConfig defines a platform-level IP pool for tenant IPAM.
// When set, the orchestrator creates a NetworkPool CR in butler-system
// during bootstrap, ready for tenant cluster allocations.
type NetworkPoolConfig struct {
	// Name is the NetworkPool resource name (defaults to "<cluster>-underlay").
	Name string `mapstructure:"name"`

	// CIDR is the network range the pool manages, e.g., "10.40.0.0/22".
	CIDR string `mapstructure:"cidr"`

	// Reserved ranges are excluded from allocation. Use this to carve out
	// the management cluster's own IPs from the tenant-allocatable space.
	Reserved []ReservedRangeConfig `mapstructure:"reserved,omitempty"`

	// TenantAllocation defines the allocatable sub-range for tenants.
	TenantAllocation TenantAllocationConfig `mapstructure:"tenantAllocation"`
}

// ReservedRangeConfig is a CIDR range excluded from tenant allocation.
type ReservedRangeConfig struct {
	CIDR        string `mapstructure:"cidr"`
	Description string `mapstructure:"description,omitempty"`
}

// TenantAllocationConfig configures the allocatable sub-range and defaults.
type TenantAllocationConfig struct {
	Start    string                         `mapstructure:"start"`
	End      string                         `mapstructure:"end"`
	Defaults TenantAllocationDefaultsConfig `mapstructure:"defaults"`
}

// TenantAllocationDefaultsConfig defines default allocation sizes per tenant.
type TenantAllocationDefaultsConfig struct {
	LBPoolPerTenant int32 `mapstructure:"lbPoolPerTenant"`
	NodesPerTenant  int32 `mapstructure:"nodesPerTenant"`
}

// ProviderNetworkConfig configures the ProviderConfig.spec.network section
// that binds a provider to a NetworkPool for IPAM-based allocation.
type ProviderNetworkConfig struct {
	// Mode is "ipam" (pool-based) or "cloud" (provider-native networking).
	Mode string `mapstructure:"mode"`

	// PoolRefs references the NetworkPools to allocate from, priority-ordered.
	PoolRefs []PoolReferenceConfig `mapstructure:"poolRefs,omitempty"`

	// Gateway is the network gateway address used by tenant VMs.
	Gateway string `mapstructure:"gateway,omitempty"`

	// DNSServers are the DNS server addresses injected into tenant VMs.
	DNSServers []string `mapstructure:"dnsServers,omitempty"`

	// Subnet is the CIDR the provider advertises to the IPAM layer.
	Subnet string `mapstructure:"subnet,omitempty"`

	// LoadBalancer configures per-tenant LB IP allocation behaviour.
	LoadBalancer LBAllocConfig `mapstructure:"loadBalancer,omitempty"`

	// QuotaPerTenant caps per-tenant IP usage.
	QuotaPerTenant QuotaPerTenantConfig `mapstructure:"quotaPerTenant,omitempty"`
}

// PoolReferenceConfig is a priority-ordered reference to a NetworkPool.
type PoolReferenceConfig struct {
	Name     string `mapstructure:"name"`
	Priority int32  `mapstructure:"priority"`
}

// LBAllocConfig controls how LoadBalancer IPs are allocated per tenant.
type LBAllocConfig struct {
	AllocationMode  string `mapstructure:"allocationMode"` // static | elastic
	InitialPoolSize int32  `mapstructure:"initialPoolSize"`
	DefaultPoolSize int32  `mapstructure:"defaultPoolSize"`
	GrowthIncrement int32  `mapstructure:"growthIncrement"`
}

// QuotaPerTenantConfig caps per-tenant IP usage.
type QuotaPerTenantConfig struct {
	MaxLoadBalancerIPs int32 `mapstructure:"maxLoadBalancerIPs"`
	MaxNodeIPs         int32 `mapstructure:"maxNodeIPs"`
}

// ControlPlaneExposureConfig configures how tenant control planes are exposed.
// This is a platform-level setting written to ButlerConfig during bootstrap.
type ControlPlaneExposureConfig struct {
	// Mode determines how tenant API servers are exposed.
	// LoadBalancer: 1 IP per tenant, direct access (default)
	// Ingress: shared IP via Ingress controller with TLS passthrough
	// Gateway: shared IP via Gateway API TLSRoute
	Mode string `mapstructure:"mode"`

	// Hostname is the wildcard domain for tenant API servers.
	// Required when Mode is Ingress or Gateway.
	// Example: "*.k8s.platform.example.com"
	Hostname string `mapstructure:"hostname,omitempty"`

	// IngressClassName specifies the Ingress class when Mode is Ingress.
	IngressClassName string `mapstructure:"ingressClassName,omitempty"`

	// ControllerType specifies the ingress controller type for TLS passthrough.
	// Used when Mode is Ingress. Supported: haproxy, nginx, traefik, generic.
	ControllerType string `mapstructure:"controllerType,omitempty"`

	// GatewayRef references the Gateway resource when Mode is Gateway.
	// Format: "namespace/name"
	GatewayRef string `mapstructure:"gatewayRef,omitempty"`
}

// ClusterConfig defines cluster specifications
type ClusterConfig struct {
	// Name is the cluster name (used for VM names, kubeconfig context)
	Name string `mapstructure:"name"`

	// Topology defines the cluster topology
	// - "single-node": Single control plane node that also runs workloads
	// - "ha": High-availability with separate control plane and worker nodes (default)
	Topology string `mapstructure:"topology"`

	// ControlPlane defines control plane node configuration
	ControlPlane NodePoolConfig `mapstructure:"controlPlane"`

	// Workers defines worker node configuration
	// Ignored when topology is "single-node"
	Workers NodePoolConfig `mapstructure:"workers"`
}

// NodePoolConfig defines a pool of nodes
type NodePoolConfig struct {
	// Replicas is the number of nodes
	Replicas int32 `mapstructure:"replicas"`

	// CPU is the number of vCPUs per node
	CPU int32 `mapstructure:"cpu"`

	// MemoryMB is the memory in MB per node
	MemoryMB int32 `mapstructure:"memoryMB"`

	// DiskGB is the boot disk size in GB
	DiskGB int32 `mapstructure:"diskGB"`

	// ExtraDisks are additional disks (for storage)
	ExtraDisks []DiskConfig `mapstructure:"extraDisks"`
}

// DiskConfig defines an additional disk
type DiskConfig struct {
	// SizeGB is the disk size in GB
	SizeGB int32 `mapstructure:"sizeGB"`

	// StorageClass is the optional storage class for this disk
	StorageClass string `mapstructure:"storageClass,omitempty"`
}

// NetworkConfig defines network configuration
type NetworkConfig struct {
	// PodCIDR is the pod network CIDR
	PodCIDR string `mapstructure:"podCIDR"`

	// ServiceCIDR is the service network CIDR
	ServiceCIDR string `mapstructure:"serviceCIDR"`

	// VIP is the control plane VIP address
	VIP string `mapstructure:"vip"`

	// LoadBalancerPool defines the IP range for MetalLB LoadBalancer services.
	// Uses validated start/end format. Takes precedence over addons.loadBalancer.addressPool.
	LoadBalancerPool *LBPoolConfig `mapstructure:"loadBalancerPool"`

	// MTU overrides the default network interface MTU on Talos nodes.
	// Set when the path traverses a sub-1500 link (IPsec/NAT-T, VPN,
	// ZTNA, SD-WAN) and PMTUD is unreliable. Cilium auto-detects device
	// MTU and derives tunnel MTU from this value, so no separate Cilium
	// knob is required. Typical range 1380-1420. Omit (or zero) for the
	// Talos default (1500). Values above 1500 additionally require
	// JumboFrames to be set.
	MTU int `mapstructure:"mtu,omitempty"`

	// JumboFrames is the explicit opt-in required for MTU > 1500. Gated
	// separately from MTU because a misconfigured jumbo path silently
	// breaks traffic — ARP/ICMP and small control packets succeed, but
	// the first large TCP flow stalls on PMTUD timeouts. Requiring this
	// flag forces the operator to confirm every hop on the end-to-end
	// path (switches, NICs, cloud peering, tunnel MTUs) supports the
	// requested frame size before the bootstrap will emit an MTU patch
	// above 1500.
	JumboFrames bool `mapstructure:"jumboFrames,omitempty"`
}

// ValidateMTU returns an error when the requested MTU is outside the
// supported range or when a jumbo-frame MTU is configured without an
// explicit opt-in. A return of nil with mtu == 0 signals "no MTU patch
// will be emitted" and is the common case. Shared between the config-file
// path (LoadConfig) and the wizard so both enforce identical rules.
func ValidateMTU(mtu int, jumboFrames bool) error {
	if mtu == 0 {
		return nil
	}
	if mtu < 576 {
		return fmt.Errorf("network.mtu %d is below the minimum of 576", mtu)
	}
	if mtu > 9000 {
		return fmt.Errorf("network.mtu %d exceeds the supported maximum of 9000", mtu)
	}
	if mtu > 1500 && !jumboFrames {
		return fmt.Errorf("network.mtu %d exceeds standard Ethernet (1500); set network.jumboFrames: true to opt in after verifying end-to-end path support (switches, NICs, tunnels, cloud peering)", mtu)
	}
	return nil
}

// LBPoolConfig defines a validated IP address range for LoadBalancer services
type LBPoolConfig struct {
	// Start is the first IP in the pool (inclusive)
	Start string `mapstructure:"start"`

	// End is the last IP in the pool (inclusive)
	End string `mapstructure:"end"`
}

// TalosConfig defines Talos OS configuration
type TalosConfig struct {
	// Version is the Talos version
	Version string `mapstructure:"version"`

	// Schematic is the Talos schematic ID (for extensions)
	Schematic string `mapstructure:"schematic,omitempty"`

	// TimeServers overrides the default NTP servers used by Talos.
	// Required on networks without external connectivity where the
	// default (time.cloudflare.com) is unreachable.
	TimeServers []string `mapstructure:"timeServers,omitempty"`
}

// AddonsConfig defines which addons to install
type AddonsConfig struct {
	// CNI defines CNI configuration
	CNI CNIConfig `mapstructure:"cni"`

	// Storage defines storage configuration
	Storage StorageConfig `mapstructure:"storage"`

	// LoadBalancer defines load balancer configuration
	LoadBalancer LoadBalancerConfig `mapstructure:"loadBalancer"`

	// GitOps defines GitOps configuration
	GitOps GitOpsConfig `mapstructure:"gitOps"`

	// CAPI defines Cluster API configuration
	CAPI CAPIConfig `mapstructure:"capi"`

	// ButlerController defines Butler Controller configuration
	ButlerController ButlerControllerConfig `mapstructure:"butlerController"`

	// Console defines Butler Console configuration
	Console ConsoleConfig `mapstructure:"console"`
}

// CNIConfig defines CNI configuration
type CNIConfig struct {
	// Type is the CNI type (cilium)
	Type string `mapstructure:"type"`
}

// StorageConfig defines storage configuration
type StorageConfig struct {
	// Type is the storage type (longhorn)
	Type string `mapstructure:"type"`
}

// LoadBalancerConfig defines load balancer configuration
type LoadBalancerConfig struct {
	// Type is the load balancer type (metallb)
	Type string `mapstructure:"type"`

	// AddressPool is the IP address range for LoadBalancer services
	AddressPool string `mapstructure:"addressPool"`
}

// GitOpsConfig defines GitOps configuration
type GitOpsConfig struct {
	// Type is the GitOps type (flux)
	Type string `mapstructure:"type"`
}

// CAPIConfig defines CAPI configuration
type CAPIConfig struct {
	Enabled bool   `mapstructure:"enabled"`
	Version string `mapstructure:"version"`
}

// ButlerControllerConfig defines Butler Controller configuration
type ButlerControllerConfig struct {
	Enabled bool   `mapstructure:"enabled"`
	Version string `mapstructure:"version"`
	Image   string `mapstructure:"image"`
}

// ConsoleConfig defines Butler Console configuration
type ConsoleConfig struct {
	// Enabled controls whether to install the console
	Enabled bool `mapstructure:"enabled"`

	// Version is the chart/image version (defaults to "latest")
	Version string `mapstructure:"version"`

	// Ingress configures ingress for the console
	Ingress ConsoleIngressConfig `mapstructure:"ingress"`

	// Auth configures authentication settings
	Auth ConsoleAuthConfig `mapstructure:"auth"`
}

// ConsoleIngressConfig defines ingress configuration for the console
type ConsoleIngressConfig struct {
	// Enabled controls whether to create an Ingress resource
	Enabled bool `mapstructure:"enabled"`

	// Host is the hostname for the console (e.g., "butler.example.com")
	// If not set and ingress is enabled, uses "butler.<cluster-name>.local"
	Host string `mapstructure:"host"`

	// ClassName is the ingress class (e.g., "traefik", "nginx")
	// If not set, uses cluster default
	ClassName string `mapstructure:"className"`

	// TLS enables TLS termination
	TLS bool `mapstructure:"tls"`

	// TLSSecretName is the name of the TLS secret (auto-generated if empty and TLS enabled)
	TLSSecretName string `mapstructure:"tlsSecretName"`
}

// ConsoleAuthConfig defines authentication configuration
type ConsoleAuthConfig struct {
	// AdminPassword sets the initial admin password
	// If not set, defaults to "admin" (should be changed post-install)
	AdminPassword string `mapstructure:"adminPassword"`

	// JWTSecret is the secret for JWT signing
	// If not set, a random secret is generated
	JWTSecret string `mapstructure:"jwtSecret"`
}

// ProviderConfig contains provider-specific settings
type ProviderConfig struct {
	// Harvester contains Harvester-specific settings
	Harvester *HarvesterProviderConfig `mapstructure:"harvester,omitempty"`

	// Nutanix contains Nutanix-specific settings
	Nutanix *NutanixProviderConfig `mapstructure:"nutanix,omitempty"`

	// Proxmox contains Proxmox-specific settings
	Proxmox *ProxmoxProviderConfig `mapstructure:"proxmox,omitempty"`

	// GCP contains GCP-specific settings
	GCP *GCPProviderConfig `mapstructure:"gcp,omitempty"`

	// AWS contains AWS-specific settings
	AWS *AWSProviderConfig `mapstructure:"aws,omitempty"`

	// Azure contains Azure-specific settings
	Azure *AzureProviderConfig `mapstructure:"azure,omitempty"`
}

// HarvesterProviderConfig contains Harvester-specific settings
type HarvesterProviderConfig struct {
	// KubeconfigPath is the path to the Harvester kubeconfig
	KubeconfigPath string `mapstructure:"kubeconfigPath"`

	// Namespace is the Harvester namespace for VMs
	Namespace string `mapstructure:"namespace"`

	// NetworkName is the Harvester network name (namespace/name format)
	NetworkName string `mapstructure:"networkName"`

	// ImageName is the Talos image name in Harvester (namespace/name format)
	ImageName string `mapstructure:"imageName"`
}

// NutanixProviderConfig contains Nutanix-specific settings
type NutanixProviderConfig struct {
	// Endpoint is the Prism Central URL (e.g., https://prism-central.example.com)
	Endpoint string `mapstructure:"endpoint"`

	// Port is the Prism Central API port (default: 9440)
	Port int32 `mapstructure:"port"`

	// Insecure allows insecure TLS connections (for self-signed certs)
	Insecure bool `mapstructure:"insecure"`

	// Username is the Prism Central username
	Username string `mapstructure:"username"`

	// Password is the Prism Central password
	Password string `mapstructure:"password"`

	// ClusterUUID is the target Nutanix cluster UUID
	ClusterUUID string `mapstructure:"clusterUUID"`

	// SubnetUUID is the network subnet UUID for VMs
	SubnetUUID string `mapstructure:"subnetUUID"`

	// ImageUUID is the Talos image UUID in Prism Central
	ImageUUID string `mapstructure:"imageUUID"`

	// StorageContainerUUID is the storage container for VM disks (optional)
	StorageContainerUUID string `mapstructure:"storageContainerUUID,omitempty"`

	// HostAliases adds /etc/hosts entries to the KIND node for corporate DNS.
	HostAliases []string `mapstructure:"hostAliases,omitempty"`
}

// ProxmoxProviderConfig contains Proxmox-specific settings
type ProxmoxProviderConfig struct {
	// Endpoint is the Proxmox API URL
	Endpoint string `mapstructure:"endpoint"`

	// Insecure allows insecure TLS connections
	Insecure bool `mapstructure:"insecure"`

	// Username is the Proxmox username
	Username string `mapstructure:"username"`

	// Password is the Proxmox password
	Password string `mapstructure:"password"`

	// Nodes is the list of Proxmox nodes available for VM placement
	Nodes []string `mapstructure:"nodes"`

	// Storage is the storage location for VM disks
	Storage string `mapstructure:"storage"`

	// TemplateID is the VM template ID to clone (optional)
	TemplateID int32 `mapstructure:"templateID,omitempty"`

	// VMIDStart is the start of the VM ID range
	VMIDStart int32 `mapstructure:"vmidStart,omitempty"`

	// VMIDEnd is the end of the VM ID range
	VMIDEnd int32 `mapstructure:"vmidEnd,omitempty"`

	// HostAliases adds /etc/hosts entries to the KIND node for corporate DNS.
	HostAliases []string `mapstructure:"hostAliases,omitempty"`
}

// GCPProviderConfig contains GCP-specific settings
type GCPProviderConfig struct {
	// ServiceAccountKeyPath is the path to the GCP service account key JSON file
	ServiceAccountKeyPath string `mapstructure:"serviceAccountKeyPath"`

	// ProjectID is the GCP project ID
	ProjectID string `mapstructure:"projectID"`

	// Region is the GCP region (e.g., "us-central1")
	Region string `mapstructure:"region"`

	// Zone is the GCP zone (e.g., "us-central1-a"). Defaults to "{region}-a"
	Zone string `mapstructure:"zone,omitempty"`

	// Network is the VPC network name
	Network string `mapstructure:"network"`

	// Subnetwork is the VPC subnetwork name
	Subnetwork string `mapstructure:"subnetwork,omitempty"`

	// MachineType is the default GCE machine type (e.g., "n2-standard-4")
	MachineType string `mapstructure:"machineType,omitempty"`

	// ImageProject is the GCP project containing the source image
	ImageProject string `mapstructure:"imageProject,omitempty"`

	// ImageFamily is the image family (e.g., "ubuntu-2204-lts")
	ImageFamily string `mapstructure:"imageFamily,omitempty"`

	// Image is the specific image name. Takes precedence over ImageFamily.
	Image string `mapstructure:"image,omitempty"`
}

// AWSProviderConfig contains AWS-specific settings
type AWSProviderConfig struct {
	// AccessKeyPath is the path to an AWS credentials file or access key ID directly
	AccessKeyID string `mapstructure:"accessKeyID"`

	// SecretAccessKey is the AWS secret access key
	SecretAccessKey string `mapstructure:"secretAccessKey"`

	// Region is the AWS region (e.g., "us-east-1")
	Region string `mapstructure:"region"`

	// VPCID is the VPC identifier
	VPCID string `mapstructure:"vpcID,omitempty"`

	// SubnetID is the subnet identifier for VM placement
	SubnetID string `mapstructure:"subnetID,omitempty"`

	// SecurityGroupID is the security group identifier
	SecurityGroupID string `mapstructure:"securityGroupID,omitempty"`

	// InstanceType is the EC2 instance type (e.g., "m5.xlarge")
	InstanceType string `mapstructure:"instanceType,omitempty"`

	// AMI is the Amazon Machine Image ID
	AMI string `mapstructure:"ami,omitempty"`
}

// AzureProviderConfig contains Azure-specific settings
type AzureProviderConfig struct {
	// ClientID is the Azure service principal client ID
	ClientID string `mapstructure:"clientID"`

	// ClientSecret is the Azure service principal client secret
	ClientSecret string `mapstructure:"clientSecret"`

	// TenantID is the Azure Active Directory tenant ID
	TenantID string `mapstructure:"tenantID"`

	// SubscriptionID is the Azure subscription ID
	SubscriptionID string `mapstructure:"subscriptionID"`

	// ResourceGroup is the Azure resource group
	ResourceGroup string `mapstructure:"resourceGroup"`

	// Location is the Azure region (e.g., "eastus")
	Location string `mapstructure:"location"`

	// VNetName is the Azure Virtual Network name
	VNetName string `mapstructure:"vnetName,omitempty"`

	// SubnetName is the subnet within the VNet
	SubnetName string `mapstructure:"subnetName,omitempty"`

	// SecurityGroupName is the Azure Network Security Group name
	SecurityGroupName string `mapstructure:"securityGroupName,omitempty"`

	// VMSize is the Azure VM size (e.g., "Standard_D4s_v3")
	VMSize string `mapstructure:"vmSize,omitempty"`

	// ImageURN is the VM image reference (URN, managed image ID, or gallery image ID)
	ImageURN string `mapstructure:"imageURN,omitempty"`
}

// LoadConfig loads the bootstrap configuration from viper
func LoadConfig() (*Config, error) {
	var cfg Config
	if err := viper.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("unmarshaling config: %w", err)
	}

	// Set defaults
	if cfg.Network.PodCIDR == "" {
		cfg.Network.PodCIDR = "10.244.0.0/16"
	}
	if cfg.Network.ServiceCIDR == "" {
		cfg.Network.ServiceCIDR = "10.96.0.0/12"
	}
	if cfg.Talos.Version == "" {
		cfg.Talos.Version = "v1.12.1"
	}
	if cfg.Addons.CNI.Type == "" {
		cfg.Addons.CNI.Type = "cilium"
	}
	if cfg.Addons.Storage.Type == "" {
		cfg.Addons.Storage.Type = "longhorn"
	}
	if cfg.Addons.LoadBalancer.Type == "" {
		cfg.Addons.LoadBalancer.Type = "metallb"
	}
	// GitOps is not enabled during bootstrap. It can be configured
	// post-bootstrap via ButlerConfig or TenantCluster addons.

	// Topology defaults and validation
	if cfg.Cluster.Topology == "" {
		cfg.Cluster.Topology = "ha" // Default to HA
	}

	// Validate topology
	if cfg.Cluster.Topology != "single-node" && cfg.Cluster.Topology != "ha" {
		return nil, fmt.Errorf("invalid topology %q, must be 'single-node' or 'ha'", cfg.Cluster.Topology)
	}

	// Single-node topology validation and enforcement
	if cfg.Cluster.Topology == "single-node" {
		// Force control plane replicas to 1
		if cfg.Cluster.ControlPlane.Replicas != 1 {
			if cfg.Cluster.ControlPlane.Replicas > 1 {
				fmt.Printf("Warning: single-node topology forces controlPlane.replicas=1 (was %d)\n",
					cfg.Cluster.ControlPlane.Replicas)
			}
			cfg.Cluster.ControlPlane.Replicas = 1
		}
		// Workers are ignored in single-node mode, but we don't modify them
		// The controller will skip creating worker MachineRequests
	}

	// Console defaults
	if cfg.Addons.Console.Enabled {
		if cfg.Addons.Console.Version == "" {
			cfg.Addons.Console.Version = "0.14.1"
		}
		// Default ingress host based on cluster name
		if cfg.Addons.Console.Ingress.Enabled && cfg.Addons.Console.Ingress.Host == "" {
			cfg.Addons.Console.Ingress.Host = fmt.Sprintf("butler.%s.local", cfg.Cluster.Name)
		}
	}

	// Network MTU validation. Shared with the wizard path via ValidateMTU
	// so YAML configs cannot sneak in out-of-range or un-opted-in jumbo
	// MTUs that the wizard validator would reject.
	if err := ValidateMTU(cfg.Network.MTU, cfg.Network.JumboFrames); err != nil {
		return nil, err
	}

	// ControlPlaneExposure validation
	if cfg.ControlPlaneExposure != nil && cfg.ControlPlaneExposure.Mode != "" {
		mode := cfg.ControlPlaneExposure.Mode
		if mode != "LoadBalancer" && mode != "Ingress" && mode != "Gateway" {
			return nil, fmt.Errorf("invalid controlPlaneExposure.mode %q, must be 'LoadBalancer', 'Ingress', or 'Gateway'", mode)
		}
		if (mode == "Ingress" || mode == "Gateway") && cfg.ControlPlaneExposure.Hostname == "" {
			return nil, fmt.Errorf("controlPlaneExposure.hostname is required when mode is %q", mode)
		}
		if mode == "Gateway" && cfg.ControlPlaneExposure.GatewayRef == "" {
			return nil, fmt.Errorf("controlPlaneExposure.gatewayRef is required when mode is 'Gateway'")
		}
	}

	// Provider-specific defaults
	if cfg.Provider == "nutanix" && cfg.ProviderConfig.Nutanix != nil {
		if cfg.ProviderConfig.Nutanix.Port == 0 {
			cfg.ProviderConfig.Nutanix.Port = 9440
		}
		// Normalize endpoint URL: ensure https:// prefix so the provider
		// controller can reach Prism Central. Matches the normalization in
		// discovery/nutanix.go Connect() and wizard buildConfig().
		ep := strings.TrimRight(cfg.ProviderConfig.Nutanix.Endpoint, "/")
		if ep != "" && !strings.HasPrefix(ep, "https://") && !strings.HasPrefix(ep, "http://") {
			ep = "https://" + ep
		}
		cfg.ProviderConfig.Nutanix.Endpoint = ep
	}

	// GCP defaults
	if cfg.Provider == "gcp" && cfg.ProviderConfig.GCP != nil {
		if cfg.ProviderConfig.GCP.Zone == "" && cfg.ProviderConfig.GCP.Region != "" {
			cfg.ProviderConfig.GCP.Zone = cfg.ProviderConfig.GCP.Region + "-a"
		}
		if cfg.ProviderConfig.GCP.ServiceAccountKeyPath != "" {
			cfg.ProviderConfig.GCP.ServiceAccountKeyPath = expandPath(cfg.ProviderConfig.GCP.ServiceAccountKeyPath)
		}
	}

	// Expand home directory in paths
	if cfg.ProviderConfig.Harvester != nil && cfg.ProviderConfig.Harvester.KubeconfigPath != "" {
		cfg.ProviderConfig.Harvester.KubeconfigPath = expandPath(cfg.ProviderConfig.Harvester.KubeconfigPath)
	}

	return &cfg, nil
}

// expandPath expands ~ to home directory
func expandPath(path string) string {
	return ExpandPath(path)
}

// ExpandPath expands a leading ~ in a path to the user's home directory.
// Callers outside LoadConfig (e.g., the wizard) use this to normalize
// user-entered paths before they reach os.ReadFile.
func ExpandPath(path string) string {
	if len(path) > 0 && path[0] == '~' {
		home, err := os.UserHomeDir()
		if err != nil {
			return path
		}
		return filepath.Join(home, path[1:])
	}
	return path
}

// IsSingleNode returns true if this is a single-node topology
func (c *Config) IsSingleNode() bool {
	return c.Cluster.Topology == "single-node"
}

// GetConsoleURL returns the console URL based on configuration
func (c *Config) GetConsoleURL() string {
	if !c.Addons.Console.Enabled {
		return ""
	}

	if c.Addons.Console.Ingress.Enabled {
		scheme := "http"
		if c.Addons.Console.Ingress.TLS {
			scheme = "https"
		}
		return fmt.Sprintf("%s://%s", scheme, c.Addons.Console.Ingress.Host)
	}

	// If no ingress, will need to get LoadBalancer IP at runtime
	return ""
}
