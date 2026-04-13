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
	"github.com/butlerdotdev/butler/internal/tui/wizard/discovery"
)

// wizardState holds all user input collected across wizard steps.
//
// Fields are exported so that huh's DescriptionFunc binding can hash
// them via hashstructure, which only considers exported fields. The
// type itself is unexported so the struct stays package-internal —
// only the fields inside are visible to hashing.
//
// Fields are intentionally restricted to those that map directly to
// orchestrator.Config on main. New config fields should be added here
// when the wizard needs to surface them.
type wizardState struct {
	// Provider and image source
	Provider    string // "harvester" or "nutanix"
	ImageSource string // "factory" or "existing"

	// Cluster identity and topology
	ClusterName string
	Topology    string // "ha" or "single-node"

	// Control plane sizing
	CPReplicas string
	CPCPU      string
	CPMemoryMB string
	CPDiskGB   string

	// Worker sizing (unused for single-node)
	WorkerReplicas string
	WorkerCPU      string
	WorkerMemoryMB string
	WorkerDiskGB   string

	// Cluster networking
	PodCIDR     string
	ServiceCIDR string
	VIP         string

	// Management cluster LB pool (MetalLB)
	LBStart string
	LBEnd   string

	// Talos image (populated from factory or existing)
	TalosVersion   string
	TalosSchematic string

	// Control plane exposure mode for tenant clusters. Determines how
	// tenant-cluster Kubernetes API servers are reached from outside.
	//   LoadBalancer (default): 1 LoadBalancer IP per tenant
	//   Ingress:                shared IP via ingress + TLS passthrough
	//   Gateway:                shared IP via Gateway API TLSRoute
	ExposureMode           string // "LoadBalancer" | "Ingress" | "Gateway"
	ExposureHostname       string // "*.k8s.example.com" — required for Ingress/Gateway
	ExposureIngressClass   string // "traefik", "nginx", "haproxy", ...
	ExposureControllerType string // "traefik" | "nginx" | "haproxy" | "generic"
	ExposureGatewayRef     string // "namespace/name"

	// Butler Console
	ConsoleIngressEnabled bool
	ConsoleHost           string
	ConsoleClass          string
	ConsoleTLS            bool
	ConsoleAdminPassword  string

	// Platform settings applied to ButlerConfig after bootstrap.
	MultiTenancyMode string // "Optional" or "Enforced"

	// IPAM / NetworkPool. When IPAMEnabled is true the bootstrap creates
	// a NetworkPool CR in butler-system and wires the ProviderConfig's
	// spec.network section to use it. This is the difference between a
	// management cluster that can immediately provision tenants and one
	// that needs manual IPAM setup post-bootstrap.
	//
	// Gateway and DNS are intentionally not collected here: on Harvester
	// and Nutanix the workload network runs DHCP so tenant VMs pick up
	// both automatically. Operators who need static gateway/DNS can add
	// them post-bootstrap with kubectl edit providerconfig.
	IPAMEnabled             bool
	PoolCIDR                string // e.g., 10.40.0.0/22 — the full network the pool manages
	TenantAllocStart        string // first IP allocatable to tenants
	TenantAllocEnd          string // last IP allocatable to tenants
	TenantLBPoolPerTenant   string // default LB IPs per tenant (int as string for huh input)
	TenantNodesPerTenant    string // default node IPs per tenant
	LBAllocationMode        string // static | elastic
	LBInitialPoolSize       string
	LBDefaultPoolSize       string
	LBGrowthIncrement       string
	QuotaMaxLoadBalancerIPs string
	QuotaMaxNodeIPs         string

	// Harvester provider inputs
	HarvKubeconfig string
	HarvNamespace  string
	HarvNetwork    string
	HarvImage      string

	// Nutanix provider inputs
	NutEndpoint    string
	NutPort        string
	NutInsecure    bool
	NutUsername    string
	NutPassword    string
	NutClusterUUID string
	NutSubnetUUID  string
	NutImageUUID   string

	// AWS provider inputs
	AWSAccessKey  string
	AWSSecretKey  string
	AWSRegion     string
	AWSVPCID      string
	AWSSubnetID   string
	AWSSecGroupID string
	AWSAMI        string // AMI ID for Talos image

	// Azure provider inputs
	AZClientID       string
	AZClientSecret   string
	AZTenantID       string
	AZSubscriptionID string
	AZResourceGroup  string
	AZLocation       string
	AZVNet           string
	AZSubnet         string
	AZImageURN       string // VM image reference

	// GCP provider inputs
	GCPKeyPath      string
	GCPProjectID    string
	GCPRegion       string
	GCPZone         string
	GCPNetwork      string
	GCPSubnetwork   string
	GCPImageProject string // GCE project containing the Talos image
	GCPImage        string // GCE image name
}

// newWizardState returns a state seeded with sensible defaults so the
// wizard fields are pre-populated with reasonable starting values.
func newWizardState() *wizardState {
	return &wizardState{
		Topology:               "ha",
		CPReplicas:             "3",
		CPCPU:                  "4",
		CPMemoryMB:             "8192",
		CPDiskGB:               "50",
		WorkerReplicas:         "3",
		WorkerCPU:              "4",
		WorkerMemoryMB:         "16384",
		WorkerDiskGB:           "100",
		PodCIDR:                "10.244.0.0/16",
		ServiceCIDR:            "10.96.0.0/12",
		ImageSource:            "factory",
		TalosVersion:           "v1.12.2",
		TalosSchematic:         discovery.DefaultTalosSchematic,
		NutPort:                "9440",
		MultiTenancyMode:        "Optional",
		ExposureMode:            "LoadBalancer",
		ExposureIngressClass:    "traefik",
		ExposureControllerType:  "traefik",
		ConsoleIngressEnabled:   false,
		ConsoleClass:            "traefik",
		ConsoleTLS:              false,
		ConsoleAdminPassword:    "admin",
		IPAMEnabled:             true,
		TenantLBPoolPerTenant:   "2",
		TenantNodesPerTenant:    "5",
		LBAllocationMode:        "static",
		LBInitialPoolSize:       "2",
		LBDefaultPoolSize:       "4",
		LBGrowthIncrement:       "2",
		QuotaMaxLoadBalancerIPs: "8",
		QuotaMaxNodeIPs:         "10",
	}
}

// stateCredentials adapts wizardState to discovery.CredentialProvider so
// the discovery layer can read provider-specific credentials uniformly.
type stateCredentials struct {
	s *wizardState
}

func (c *stateCredentials) Get(key string) string {
	switch c.s.Provider {
	case "harvester":
		if key == "kubeconfig" {
			return c.s.HarvKubeconfig
		}
	case "nutanix":
		switch key {
		case "endpoint":
			return c.s.NutEndpoint
		case "port":
			return c.s.NutPort
		case "username":
			return c.s.NutUsername
		case "password":
			return c.s.NutPassword
		}
	case "aws":
		switch key {
		case "access_key":
			return c.s.AWSAccessKey
		case "secret_key":
			return c.s.AWSSecretKey
		case "region":
			return c.s.AWSRegion
		}
	case "azure":
		switch key {
		case "client_id":
			return c.s.AZClientID
		case "client_secret":
			return c.s.AZClientSecret
		case "tenant_id":
			return c.s.AZTenantID
		case "subscription_id":
			return c.s.AZSubscriptionID
		}
	case "gcp":
		switch key {
		case "key_path":
			return c.s.GCPKeyPath
		case "project_id":
			return c.s.GCPProjectID
		}
	}
	return ""
}

func (c *stateCredentials) GetBool(key string) bool {
	if c.s.Provider == "nutanix" && key == "insecure" {
		return c.s.NutInsecure
	}
	return false
}
