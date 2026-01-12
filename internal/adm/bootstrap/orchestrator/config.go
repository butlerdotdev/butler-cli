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

	"github.com/spf13/viper"
)

// Config represents the bootstrap configuration
type Config struct {
	// Provider is the infrastructure provider (harvester, nutanix, proxmox)
	Provider string `mapstructure:"provider"`

	// Cluster defines the management cluster configuration
	Cluster ClusterConfig `mapstructure:"cluster"`

	// Network defines networking configuration
	Network NetworkConfig `mapstructure:"network"`

	// Talos defines Talos Linux configuration
	Talos TalosConfig `mapstructure:"talos"`

	// Addons defines which addons to install
	Addons AddonsConfig `mapstructure:"addons"`

	// ProviderConfig contains provider-specific settings
	ProviderConfig ProviderConfig `mapstructure:"providerConfig"`
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
}

// TalosConfig defines Talos OS configuration
type TalosConfig struct {
	// Version is the Talos version
	Version string `mapstructure:"version"`

	// Schematic is the Talos schematic ID (for extensions)
	Schematic string `mapstructure:"schematic,omitempty"`
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
		cfg.Talos.Version = "v1.9.0"
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
	if cfg.Addons.GitOps.Type == "" {
		cfg.Addons.GitOps.Type = "flux"
	}

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
			cfg.Addons.Console.Version = "latest"
		}
		// Default ingress host based on cluster name
		if cfg.Addons.Console.Ingress.Enabled && cfg.Addons.Console.Ingress.Host == "" {
			cfg.Addons.Console.Ingress.Host = fmt.Sprintf("butler.%s.local", cfg.Cluster.Name)
		}
	}

	// Provider-specific defaults
	if cfg.Provider == "nutanix" && cfg.ProviderConfig.Nutanix != nil {
		if cfg.ProviderConfig.Nutanix.Port == 0 {
			cfg.ProviderConfig.Nutanix.Port = 9440
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
