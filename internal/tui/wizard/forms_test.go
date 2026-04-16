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
	"strings"
	"testing"
)

// --- buildSummary tests ---

func TestBuildSummary_HA(t *testing.T) {
	s := &wizardState{
		Provider:       "harvester",
		ClusterName:    "my-cluster",
		Topology:       "ha",
		CPReplicas:     "3",
		CPCPU:          "4",
		CPMemoryMB:     "8192",
		CPDiskGB:       "50",
		WorkerReplicas: "3",
		WorkerCPU:      "4",
		WorkerMemoryMB: "16384",
		WorkerDiskGB:   "100",
		PodCIDR:        "10.244.0.0/16",
		ServiceCIDR:    "10.96.0.0/12",
		VIP:            "10.40.0.232",
		LBStart:        "10.40.0.233",
		LBEnd:          "10.40.0.239",
		MultiTenancyMode: "Enforced",
		IPAMEnabled:    true,
		PoolCIDR:       "10.40.0.0/24",
		TenantAllocStart: "10.40.0.200",
		TenantAllocEnd:   "10.40.0.254",
		LBAllocationMode: "static",
		LBInitialPoolSize: "2",
		LBDefaultPoolSize: "4",
		LBGrowthIncrement: "2",
		QuotaMaxLoadBalancerIPs: "8",
		QuotaMaxNodeIPs: "10",
		ExposureMode:   "LoadBalancer",
		HarvNamespace:  "default",
		HarvNetwork:    "default/vlan40",
		HarvImage:      "default/talos-img",
	}

	summary := buildSummary(s)

	expects := []string{
		"Provider:       harvester",
		"Cluster Name:   my-cluster",
		"Topology:       ha",
		"Control Plane:  3",
		"Workers:        3",
		"Multi-Tenancy:  Enforced",
		"IPAM:           enabled",
		"Pool CIDR:      10.40.0.0/24",
		"Exposure:       LoadBalancer",
		"Namespace:      default",
		"Network:        default/vlan40",
	}
	for _, e := range expects {
		if !strings.Contains(summary, e) {
			t.Errorf("summary missing %q\ngot:\n%s", e, summary)
		}
	}
}

func TestBuildSummary_SingleNode(t *testing.T) {
	s := &wizardState{
		Provider:    "harvester",
		ClusterName: "single",
		Topology:    "single-node",
		CPReplicas:  "3", // default, hidden in wizard
		CPCPU:       "4",
		CPMemoryMB:  "8192",
		CPDiskGB:    "50",
		IPAMEnabled: false,
		ExposureMode: "LoadBalancer",
		HarvNamespace: "default",
		HarvNetwork:   "default/net",
		ImageSource:   "factory",
	}

	summary := buildSummary(s)

	// Single-node should show 1, not 3
	if strings.Contains(summary, "Control Plane:  3") {
		t.Error("single-node should render CP replicas as 1, not 3")
	}
	if !strings.Contains(summary, "Control Plane:  1") {
		t.Errorf("expected 'Control Plane:  1' in summary\ngot:\n%s", summary)
	}
	// No worker line
	if strings.Contains(summary, "Workers:") {
		t.Error("single-node should not show Workers line")
	}
	// IPAM disabled
	if !strings.Contains(summary, "disabled") {
		t.Error("expected IPAM disabled note in summary")
	}
}

func TestBuildSummary_IngressExposure(t *testing.T) {
	s := &wizardState{
		Provider:             "harvester",
		ClusterName:          "ing",
		Topology:             "single-node",
		CPReplicas:           "1",
		CPCPU:                "4",
		CPMemoryMB:           "8192",
		CPDiskGB:             "50",
		ExposureMode:         "Ingress",
		ExposureHostname:     "*.k8s.example.com",
		ExposureIngressClass: "traefik",
		ExposureControllerType: "traefik",
		IPAMEnabled:          false,
		HarvNamespace:        "default",
		HarvNetwork:          "default/net",
		ImageSource:          "factory",
	}

	summary := buildSummary(s)
	if !strings.Contains(summary, "Ingress") || !strings.Contains(summary, "*.k8s.example.com") {
		t.Errorf("expected Ingress exposure with hostname\ngot:\n%s", summary)
	}
}

func TestBuildSummary_ConsoleIngress(t *testing.T) {
	s := &wizardState{
		Provider:              "harvester",
		ClusterName:           "cons",
		Topology:              "single-node",
		CPReplicas:            "1",
		CPCPU:                 "4",
		CPMemoryMB:            "8192",
		CPDiskGB:              "50",
		ExposureMode:          "LoadBalancer",
		ConsoleIngressEnabled: true,
		ConsoleHost:           "butler.example.com",
		ConsoleClass:          "traefik",
		ConsoleTLS:            true,
		IPAMEnabled:           false,
		HarvNamespace:         "default",
		HarvNetwork:           "default/net",
		ImageSource:           "factory",
	}

	summary := buildSummary(s)
	if !strings.Contains(summary, "butler.example.com") || !strings.Contains(summary, "TLS") {
		t.Errorf("expected console ingress with host and TLS\ngot:\n%s", summary)
	}
}

func TestBuildSummary_ConsolePortForward(t *testing.T) {
	s := &wizardState{
		Provider:              "harvester",
		ClusterName:           "pf",
		Topology:              "single-node",
		CPReplicas:            "1",
		CPCPU:                 "4",
		CPMemoryMB:            "8192",
		CPDiskGB:              "50",
		ExposureMode:          "LoadBalancer",
		ConsoleIngressEnabled: false,
		IPAMEnabled:           false,
		HarvNamespace:         "default",
		HarvNetwork:           "default/net",
		ImageSource:           "factory",
	}

	summary := buildSummary(s)
	if !strings.Contains(summary, "port-forward") {
		t.Errorf("expected 'port-forward' for disabled console ingress\ngot:\n%s", summary)
	}
}

func TestBuildSummary_NTPServers(t *testing.T) {
	s := &wizardState{
		Provider:       "nutanix",
		ClusterName:    "ntp-test",
		Topology:       "single-node",
		CPReplicas:     "1",
		CPCPU:          "4",
		CPMemoryMB:     "8192",
		CPDiskGB:       "50",
		NTPServers:     "10.92.92.2, 10.92.92.4",
		ExposureMode:   "LoadBalancer",
		IPAMEnabled:    false,
		NutEndpoint:    "https://prism.example.com",
		ImageSource:    "factory",
	}

	summary := buildSummary(s)
	if !strings.Contains(summary, "NTP Servers:    10.92.92.2, 10.92.92.4") {
		t.Errorf("expected NTP servers in summary\ngot:\n%s", summary)
	}
}

// --- validateConfig tests ---

func validState() *wizardState {
	return &wizardState{
		Provider:      "harvester",
		ClusterName:   "valid",
		HarvNamespace: "default",
		HarvNetwork:   "default/net",
		ImageSource:   "factory",
		ExposureMode:  "LoadBalancer",
	}
}

func TestValidateConfig_Valid(t *testing.T) {
	if err := validateConfig(validState()); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidateConfig_MissingClusterName(t *testing.T) {
	s := validState()
	s.ClusterName = ""
	if err := validateConfig(s); err == nil {
		t.Error("expected error for missing cluster name")
	}
}

func TestValidateConfig_MissingProvider(t *testing.T) {
	s := validState()
	s.Provider = ""
	if err := validateConfig(s); err == nil {
		t.Error("expected error for missing provider")
	}
}

func TestValidateConfig_HarvesterMissingNamespace(t *testing.T) {
	s := validState()
	s.HarvNamespace = ""
	if err := validateConfig(s); err == nil {
		t.Error("expected error for missing Harvester namespace")
	}
}

func TestValidateConfig_HarvesterMissingNetwork(t *testing.T) {
	s := validState()
	s.HarvNetwork = ""
	if err := validateConfig(s); err == nil {
		t.Error("expected error for missing Harvester network")
	}
}

func TestValidateConfig_HarvesterImageNotRequiredForFactory(t *testing.T) {
	s := validState()
	s.ImageSource = "factory"
	s.HarvImage = ""
	if err := validateConfig(s); err != nil {
		t.Errorf("factory image source should not require HarvImage: %v", err)
	}
}

func TestValidateConfig_HarvesterImageRequiredForExisting(t *testing.T) {
	s := validState()
	s.ImageSource = "existing"
	s.HarvImage = ""
	if err := validateConfig(s); err == nil {
		t.Error("expected error for missing Harvester image with existing source")
	}
}

func TestValidateConfig_IngressMissingHostname(t *testing.T) {
	s := validState()
	s.ExposureMode = "Ingress"
	s.ExposureHostname = ""
	if err := validateConfig(s); err == nil {
		t.Error("expected error for Ingress without hostname")
	}
}

func TestValidateConfig_GatewayMissingRef(t *testing.T) {
	s := validState()
	s.ExposureMode = "Gateway"
	s.ExposureHostname = "*.k8s.example.com"
	s.ExposureGatewayRef = ""
	if err := validateConfig(s); err == nil {
		t.Error("expected error for Gateway without ref")
	}
}

func TestValidateConfig_ConsoleIngressMissingHost(t *testing.T) {
	s := validState()
	s.ConsoleIngressEnabled = true
	s.ConsoleHost = ""
	s.ConsoleClass = "traefik"
	if err := validateConfig(s); err == nil {
		t.Error("expected error for console ingress without host")
	}
}

func TestValidateConfig_IPAMMissingPoolCIDR(t *testing.T) {
	s := validState()
	s.IPAMEnabled = true
	s.PoolCIDR = ""
	s.TenantAllocStart = "10.40.0.200"
	s.TenantAllocEnd = "10.40.0.254"
	if err := validateConfig(s); err == nil {
		t.Error("expected error for IPAM without pool CIDR")
	}
}

func TestValidateConfig_IPAMMissingTenantAlloc(t *testing.T) {
	s := validState()
	s.IPAMEnabled = true
	s.PoolCIDR = "10.40.0.0/24"
	s.TenantAllocStart = ""
	s.TenantAllocEnd = ""
	if err := validateConfig(s); err == nil {
		t.Error("expected error for IPAM without tenant allocation range")
	}
}
