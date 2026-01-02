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

	// ControlPlane defines control plane node configuration
	ControlPlane NodePoolConfig `mapstructure:"controlPlane"`

	// Workers defines worker node configuration
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

	// StorageClass is the storage class to use (provider-specific)
	StorageClass string `mapstructure:"storageClass,omitempty"`
}

// NetworkConfig defines networking configuration
type NetworkConfig struct {
	// PodCIDR is the pod network CIDR
	PodCIDR string `mapstructure:"podCIDR"`

	// ServiceCIDR is the service network CIDR
	ServiceCIDR string `mapstructure:"serviceCIDR"`

	// VIP is the control plane virtual IP
	VIP string `mapstructure:"vip"`

	// VIPInterface is the network interface for VIP (optional, auto-detected)
	VIPInterface string `mapstructure:"vipInterface,omitempty"`
}

// TalosConfig defines Talos Linux configuration
type TalosConfig struct {
	// Version is the Talos version (e.g., v1.9.0)
	Version string `mapstructure:"version"`

	// Schematic is the Talos schematic ID (for custom images with extensions)
	Schematic string `mapstructure:"schematic,omitempty"`
}

// AddonsConfig defines addon configuration
type AddonsConfig struct {
	// CNI defines the CNI configuration
	CNI CNIConfig `mapstructure:"cni"`

	// Storage defines storage addon configuration
	Storage StorageConfig `mapstructure:"storage"`

	// LoadBalancer defines load balancer configuration
	LoadBalancer LoadBalancerConfig `mapstructure:"loadBalancer"`

	// GitOps defines GitOps configuration
	GitOps GitOpsConfig `mapstructure:"gitOps"`

	CAPI CAPIConfig `mapstructure:"capi"`

	ButlerController ButlerControllerConfig `mapstructure:"butlerController"`
}

// CNIConfig defines CNI configuration
type CNIConfig struct {
	// Type is the CNI type (cilium)
	Type string `mapstructure:"type"`
}

// StorageConfig defines storage addon configuration
type StorageConfig struct {
	// Type is the storage type (longhorn, linstor)
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

// CapiConfig defines CAPI configuration
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
	// Endpoint is the Prism Central endpoint
	Endpoint string `mapstructure:"endpoint"`

	// Username is the Prism Central username
	Username string `mapstructure:"username"`

	// Password is the Prism Central password (or reference to secret)
	Password string `mapstructure:"password,omitempty"`

	// PasswordFile is the path to a file containing the password
	PasswordFile string `mapstructure:"passwordFile,omitempty"`

	// Cluster is the Nutanix cluster name
	Cluster string `mapstructure:"cluster"`

	// Subnet is the subnet name for VMs
	Subnet string `mapstructure:"subnet"`

	// Image is the Talos image name in Nutanix
	Image string `mapstructure:"image"`
}

// ProxmoxProviderConfig contains Proxmox-specific settings
type ProxmoxProviderConfig struct {
	// Endpoint is the Proxmox API endpoint
	Endpoint string `mapstructure:"endpoint"`

	// TokenID is the API token ID
	TokenID string `mapstructure:"tokenId"`

	// TokenSecret is the API token secret
	TokenSecret string `mapstructure:"tokenSecret,omitempty"`

	// TokenSecretFile is the path to a file containing the token secret
	TokenSecretFile string `mapstructure:"tokenSecretFile,omitempty"`

	// Node is the Proxmox node name
	Node string `mapstructure:"node"`

	// Storage is the storage pool for VMs
	Storage string `mapstructure:"storage"`

	// Network is the network bridge
	Network string `mapstructure:"network"`

	// Template is the Talos template ID
	Template int `mapstructure:"template"`
}

// LoadConfig loads and validates the bootstrap configuration
func LoadConfig() (*Config, error) {
	var cfg Config
	if err := viper.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("unmarshaling config: %w", err)
	}

	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	// Expand paths
	if cfg.ProviderConfig.Harvester != nil && cfg.ProviderConfig.Harvester.KubeconfigPath != "" {
		expanded, err := expandPath(cfg.ProviderConfig.Harvester.KubeconfigPath)
		if err != nil {
			return nil, fmt.Errorf("expanding kubeconfig path: %w", err)
		}
		cfg.ProviderConfig.Harvester.KubeconfigPath = expanded
	}

	return &cfg, nil
}

// Validate validates the configuration
func (c *Config) Validate() error {
	if c.Provider == "" {
		return fmt.Errorf("provider is required")
	}

	if c.Cluster.Name == "" {
		return fmt.Errorf("cluster.name is required")
	}

	if c.Cluster.ControlPlane.Replicas < 1 {
		return fmt.Errorf("cluster.controlPlane.replicas must be at least 1")
	}

	if c.Network.VIP == "" {
		return fmt.Errorf("network.vip is required")
	}

	if c.Talos.Version == "" {
		return fmt.Errorf("talos.version is required")
	}

	// Validate provider-specific config
	switch c.Provider {
	case "harvester":
		if c.ProviderConfig.Harvester == nil {
			return fmt.Errorf("providerConfig.harvester is required for harvester provider")
		}
		if c.ProviderConfig.Harvester.KubeconfigPath == "" {
			return fmt.Errorf("providerConfig.harvester.kubeconfigPath is required")
		}
	case "nutanix":
		if c.ProviderConfig.Nutanix == nil {
			return fmt.Errorf("providerConfig.nutanix is required for nutanix provider")
		}
	case "proxmox":
		if c.ProviderConfig.Proxmox == nil {
			return fmt.Errorf("providerConfig.proxmox is required for proxmox provider")
		}
	default:
		return fmt.Errorf("unsupported provider: %s", c.Provider)
	}

	return nil
}

// expandPath expands ~ to home directory
func expandPath(path string) (string, error) {
	if path == "" {
		return "", nil
	}
	if path[0] == '~' {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		path = filepath.Join(home, path[1:])
	}
	return filepath.Abs(path)
}
