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
	"github.com/charmbracelet/huh"
)

// wizardState holds intermediate form values as strings. huh's Input
// fields bind to *string, so numeric values are stored as strings and
// parsed to int32 when building the final Config.
type wizardState struct {
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

	// Provider credentials (populated by provider-specific steps)
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
// one page. Worker fields are hidden for single-node topology. VIP is
// only shown for on-prem providers.
func workersAndNetworkStep(s *wizardState, showVIP bool) *huh.Group {
	fields := []huh.Field{
		huh.NewNote().
			Title("Workers & Networking").
			Description("Configure worker nodes and cluster networking."),
	}

	// Worker fields (hidden for single-node via WithHideFunc on the group)
	fields = append(fields,
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
	)

	// Network fields
	fields = append(fields,
		huh.NewInput().
			Title("Pod CIDR").
			Value(&s.podCIDR).
			Validate(validateCIDR),

		huh.NewInput().
			Title("Service CIDR").
			Value(&s.serviceCIDR).
			Validate(validateCIDR),
	)

	if showVIP {
		fields = append(fields,
			huh.NewInput().
				Title("Control Plane VIP").
				Description("Virtual IP for HA (leave empty if not needed)").
				Value(&s.vip).
				Validate(validateOptional(validateIP)),
		)
	}

	return huh.NewGroup(fields...).WithHideFunc(func() bool {
		return s.topology == "single-node"
	})
}

// networkOnlyStep shows just networking fields for single-node topology
// (workers are irrelevant but networking still matters).
func networkOnlyStep(s *wizardState, showVIP bool) *huh.Group {
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
	}

	if showVIP {
		fields = append(fields,
			huh.NewInput().
				Title("Control Plane VIP").
				Description("Virtual IP for HA (leave empty if not needed)").
				Value(&s.vip).
				Validate(validateOptional(validateIP)),
		)
	}

	return huh.NewGroup(fields...).WithHideFunc(func() bool {
		return s.topology != "single-node"
	})
}

// platformStep combines Talos config, addons, and console settings into
// a single dense page.
func platformStep(s *wizardState, showLB bool) *huh.Group {
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
	}

	if showLB {
		fields = append(fields,
			huh.NewSelect[string]().
				Title("Load Balancer").
				Options(huh.NewOption("MetalLB", "metallb")).
				Value(&s.lbType),

			huh.NewInput().
				Title("LB Address Pool").
				Description("IP range for LoadBalancer services (e.g., 10.40.0.100-10.40.0.200)").
				Value(&s.lbPool).
				Validate(validateNotEmpty),
		)
	}

	fields = append(fields,
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
	)

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
