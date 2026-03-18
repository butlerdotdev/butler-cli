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

// Config represents the bootstrap configuration.
type Config struct {
	Provider             string                     `mapstructure:"provider"`
	Cluster              ClusterConfig              `mapstructure:"cluster"`
	Network              NetworkConfig              `mapstructure:"network"`
	Talos                TalosConfig                `mapstructure:"talos"`
	Addons               AddonsConfig               `mapstructure:"addons"`
	ProviderConfig       ProviderConfig             `mapstructure:"providerConfig"`
	ControlPlaneExposure ControlPlaneExposureConfig `mapstructure:"controlPlaneExposure"`
	ProviderNetwork      ProviderNetworkConfig      `mapstructure:"providerNetwork"`
}

// ControlPlaneExposureConfig defines how tenant API servers are exposed.
type ControlPlaneExposureConfig struct {
	Mode             string `mapstructure:"mode"`             // LoadBalancer, Ingress, or Gateway
	Hostname         string `mapstructure:"hostname"`         // Wildcard domain (Ingress/Gateway)
	IngressClassName string `mapstructure:"ingressClassName"` // Ingress mode only
	ControllerType   string `mapstructure:"controllerType"`   // haproxy, nginx, traefik, generic
	GatewayRef       string `mapstructure:"gatewayRef"`       // namespace/name (Gateway mode only)
}

// ProviderNetworkConfig configures IPAM and networking on the ProviderConfig CRD.
type ProviderNetworkConfig struct {
	Mode        string   `mapstructure:"mode"`        // "ipam" (on-prem) or "cloud"
	Gateway     string   `mapstructure:"gateway"`
	DNSServers  []string `mapstructure:"dnsServers"`
	LBAllocMode string   `mapstructure:"lbAllocMode"` // "static" or "elastic"
	LBPoolSize  int32    `mapstructure:"lbPoolSize"`  // LB IPs per tenant
}

// ClusterConfig defines cluster specifications.
type ClusterConfig struct {
	Name         string         `mapstructure:"name"`
	Topology     string         `mapstructure:"topology"` // "ha" or "single-node"
	ControlPlane NodePoolConfig `mapstructure:"controlPlane"`
	Workers      NodePoolConfig `mapstructure:"workers"` // Ignored for single-node
}

// NodePoolConfig defines a pool of nodes.
type NodePoolConfig struct {
	Replicas   int32        `mapstructure:"replicas"`
	CPU        int32        `mapstructure:"cpu"`
	MemoryMB   int32        `mapstructure:"memoryMB"`
	DiskGB     int32        `mapstructure:"diskGB"`
	ExtraDisks []DiskConfig `mapstructure:"extraDisks"`
}

// DiskConfig defines an additional disk.
type DiskConfig struct {
	SizeGB       int32  `mapstructure:"sizeGB"`
	StorageClass string `mapstructure:"storageClass,omitempty"`
}

// NetworkConfig defines network configuration.
type NetworkConfig struct {
	PodCIDR     string `mapstructure:"podCIDR"`
	ServiceCIDR string `mapstructure:"serviceCIDR"`
	VIP         string `mapstructure:"vip"`
}

// TalosConfig defines Talos OS configuration.
type TalosConfig struct {
	Version   string `mapstructure:"version"`
	Schematic string `mapstructure:"schematic,omitempty"`
}

// AddonsConfig defines which addons to install.
type AddonsConfig struct {
	CNI              CNIConfig              `mapstructure:"cni"`
	Storage          StorageConfig          `mapstructure:"storage"`
	LoadBalancer     LoadBalancerConfig     `mapstructure:"loadBalancer"`
	GitOps           GitOpsConfig           `mapstructure:"gitOps"`
	CAPI             CAPIConfig             `mapstructure:"capi"`
	ButlerController ButlerControllerConfig `mapstructure:"butlerController"`
	Console          ConsoleConfig          `mapstructure:"console"`
}

// CNIConfig defines CNI configuration.
type CNIConfig struct {
	Type string `mapstructure:"type"` // cilium
}

// StorageConfig defines storage configuration.
type StorageConfig struct {
	Type string `mapstructure:"type"` // longhorn
}

// LoadBalancerConfig defines load balancer configuration.
type LoadBalancerConfig struct {
	Type        string `mapstructure:"type"`        // metallb
	AddressPool string `mapstructure:"addressPool"` // Deprecated: use Start/End
	Start       string `mapstructure:"start"`
	End         string `mapstructure:"end"`
}

// GitOpsConfig defines GitOps configuration.
type GitOpsConfig struct {
	Type string `mapstructure:"type"` // flux
}

// CAPIConfig defines Cluster API configuration.
type CAPIConfig struct {
	Enabled bool   `mapstructure:"enabled"`
	Version string `mapstructure:"version"`
}

// ButlerControllerConfig defines Butler Controller configuration.
type ButlerControllerConfig struct {
	Enabled bool   `mapstructure:"enabled"`
	Version string `mapstructure:"version"`
	Image   string `mapstructure:"image"`
}

// ConsoleConfig defines Butler Console configuration.
type ConsoleConfig struct {
	Enabled bool                 `mapstructure:"enabled"`
	Version string               `mapstructure:"version"`
	Ingress ConsoleIngressConfig `mapstructure:"ingress"`
	Auth    ConsoleAuthConfig    `mapstructure:"auth"`
}

// ConsoleIngressConfig defines ingress for the console.
type ConsoleIngressConfig struct {
	Enabled       bool   `mapstructure:"enabled"`
	Host          string `mapstructure:"host"`
	ClassName     string `mapstructure:"className"`
	TLS           bool   `mapstructure:"tls"`
	TLSSecretName string `mapstructure:"tlsSecretName"`
}

// ConsoleAuthConfig defines authentication configuration.
type ConsoleAuthConfig struct {
	AdminPassword string `mapstructure:"adminPassword"`
	JWTSecret     string `mapstructure:"jwtSecret"`
}

// ProviderConfig contains provider-specific settings.
type ProviderConfig struct {
	Harvester *HarvesterProviderConfig `mapstructure:"harvester,omitempty"`
	Nutanix   *NutanixProviderConfig   `mapstructure:"nutanix,omitempty"`
	Proxmox   *ProxmoxProviderConfig   `mapstructure:"proxmox,omitempty"`
	GCP       *GCPProviderConfig       `mapstructure:"gcp,omitempty"`
	AWS       *AWSProviderConfig       `mapstructure:"aws,omitempty"`
	Azure     *AzureProviderConfig     `mapstructure:"azure,omitempty"`
}

// HarvesterProviderConfig contains Harvester-specific settings.
type HarvesterProviderConfig struct {
	KubeconfigPath string `mapstructure:"kubeconfigPath"`
	Namespace      string `mapstructure:"namespace"`
	NetworkName    string `mapstructure:"networkName"` // namespace/name
	ImageName      string `mapstructure:"imageName"`   // namespace/name
}

// NutanixProviderConfig contains Nutanix Prism Central settings.
type NutanixProviderConfig struct {
	Endpoint             string   `mapstructure:"endpoint"`
	Port                 int32    `mapstructure:"port"`
	Insecure             bool     `mapstructure:"insecure"`
	Username             string   `mapstructure:"username"`
	Password             string   `mapstructure:"password"`
	ClusterUUID          string   `mapstructure:"clusterUUID"`
	SubnetUUID           string   `mapstructure:"subnetUUID"`
	ImageUUID            string   `mapstructure:"imageUUID"`
	StorageContainerUUID string   `mapstructure:"storageContainerUUID,omitempty"`
	HostAliases          []string `mapstructure:"hostAliases,omitempty"`
}

// ProxmoxProviderConfig contains Proxmox-specific settings.
type ProxmoxProviderConfig struct {
	Endpoint    string   `mapstructure:"endpoint"`
	Insecure    bool     `mapstructure:"insecure"`
	Username    string   `mapstructure:"username"`
	Password    string   `mapstructure:"password"`
	Nodes       []string `mapstructure:"nodes"`
	Storage     string   `mapstructure:"storage"`
	TemplateID  int32    `mapstructure:"templateID,omitempty"`
	VMIDStart   int32    `mapstructure:"vmidStart,omitempty"`
	VMIDEnd     int32    `mapstructure:"vmidEnd,omitempty"`
	HostAliases []string `mapstructure:"hostAliases,omitempty"`
}

// GCPProviderConfig contains GCP-specific settings.
type GCPProviderConfig struct {
	ServiceAccountKeyPath string `mapstructure:"serviceAccountKeyPath"`
	ProjectID             string `mapstructure:"projectID"`
	Region                string `mapstructure:"region"`
	Zone                  string `mapstructure:"zone,omitempty"`
	Network               string `mapstructure:"network"`
	Subnetwork            string `mapstructure:"subnetwork,omitempty"`
	MachineType           string `mapstructure:"machineType,omitempty"`
	ImageProject          string `mapstructure:"imageProject,omitempty"`
	ImageFamily           string `mapstructure:"imageFamily,omitempty"`
	Image                 string `mapstructure:"image,omitempty"`
}

// AWSProviderConfig contains AWS-specific settings.
type AWSProviderConfig struct {
	AccessKeyID     string `mapstructure:"accessKeyID"`
	SecretAccessKey  string `mapstructure:"secretAccessKey"`
	Region          string `mapstructure:"region"`
	VPCID           string `mapstructure:"vpcID,omitempty"`
	SubnetID        string `mapstructure:"subnetID,omitempty"`
	SecurityGroupID string `mapstructure:"securityGroupID,omitempty"`
	InstanceType    string `mapstructure:"instanceType,omitempty"`
	AMI             string `mapstructure:"ami,omitempty"`
}

// AzureProviderConfig contains Azure-specific settings.
type AzureProviderConfig struct {
	ClientID       string `mapstructure:"clientID"`
	ClientSecret   string `mapstructure:"clientSecret"`
	TenantID       string `mapstructure:"tenantID"`
	SubscriptionID string `mapstructure:"subscriptionID"`
	ResourceGroup  string `mapstructure:"resourceGroup"`
	Location       string `mapstructure:"location"`
	VNetName       string `mapstructure:"vnetName,omitempty"`
	SubnetName     string `mapstructure:"subnetName,omitempty"`
	VMSize         string `mapstructure:"vmSize,omitempty"`
	Image          string `mapstructure:"image,omitempty"`
}

// LoadConfig loads the bootstrap configuration from viper.
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
		cfg.Talos.Version = "v1.12.2"
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

	// Control plane exposure defaults
	if cfg.ControlPlaneExposure.Mode == "" {
		cfg.ControlPlaneExposure.Mode = "LoadBalancer"
	}

	// Parse deprecated addressPool into structured Start/End
	if cfg.Addons.LoadBalancer.Start == "" && cfg.Addons.LoadBalancer.End == "" && cfg.Addons.LoadBalancer.AddressPool != "" {
		if parts := strings.SplitN(cfg.Addons.LoadBalancer.AddressPool, "-", 2); len(parts) == 2 {
			cfg.Addons.LoadBalancer.Start = strings.TrimSpace(parts[0])
			cfg.Addons.LoadBalancer.End = strings.TrimSpace(parts[1])
		}
	}

	// Auto-set provider network mode based on provider type
	if cfg.ProviderNetwork.Mode == "" {
		switch cfg.Provider {
		case "harvester", "nutanix", "proxmox":
			cfg.ProviderNetwork.Mode = "ipam"
		case "gcp", "aws", "azure":
			cfg.ProviderNetwork.Mode = "cloud"
		}
	}

	// IPAM defaults for on-prem
	if cfg.ProviderNetwork.Mode == "ipam" {
		if cfg.ProviderNetwork.LBAllocMode == "" {
			cfg.ProviderNetwork.LBAllocMode = "static"
		}
		if cfg.ProviderNetwork.LBPoolSize == 0 {
			cfg.ProviderNetwork.LBPoolSize = 1
		}
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

// IsSingleNode returns true for single-node topology.
func (c *Config) IsSingleNode() bool {
	return c.Cluster.Topology == "single-node"
}

// GetConsoleURL returns the console URL, or empty if no ingress is configured.
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
