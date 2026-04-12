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

import "testing"

func TestBuildConfig_HAHarvester(t *testing.T) {
	s := &wizardState{
		Provider:              "harvester",
		ClusterName:           "test-cluster",
		Topology:              "ha",
		CPReplicas:            "3",
		CPCPU:                 "4",
		CPMemoryMB:            "8192",
		CPDiskGB:              "50",
		WorkerReplicas:        "3",
		WorkerCPU:             "4",
		WorkerMemoryMB:        "16384",
		WorkerDiskGB:          "100",
		PodCIDR:               "10.244.0.0/16",
		ServiceCIDR:           "10.96.0.0/12",
		VIP:                   "10.40.0.232",
		LBStart:               "10.40.0.233",
		LBEnd:                 "10.40.0.239",
		ExposureMode:          "LoadBalancer",
		MultiTenancyMode:      "Enforced",
		HarvKubeconfig:        "/tmp/kubeconfig",
		HarvNamespace:         "default",
		HarvNetwork:           "default/vlan40",
		HarvImage:             "default/talos-v1.12.2",
		IPAMEnabled:           true,
		PoolCIDR:              "10.40.0.0/24",
		TenantAllocStart:      "10.40.0.200",
		TenantAllocEnd:        "10.40.0.254",
		TenantLBPoolPerTenant: "2",
		TenantNodesPerTenant:  "5",
		LBAllocationMode:      "static",
		LBInitialPoolSize:     "2",
		LBDefaultPoolSize:     "4",
		LBGrowthIncrement:     "2",
		QuotaMaxLoadBalancerIPs: "8",
		QuotaMaxNodeIPs:        "10",
	}

	cfg, err := buildConfig(s)
	if err != nil {
		t.Fatalf("buildConfig() error: %v", err)
	}

	if cfg.Provider != "harvester" {
		t.Errorf("Provider = %q, want harvester", cfg.Provider)
	}
	if cfg.Cluster.Name != "test-cluster" {
		t.Errorf("Cluster.Name = %q", cfg.Cluster.Name)
	}
	if cfg.Cluster.ControlPlane.Replicas != 3 {
		t.Errorf("CP Replicas = %d, want 3", cfg.Cluster.ControlPlane.Replicas)
	}
	if cfg.Cluster.Workers.Replicas != 3 {
		t.Errorf("Worker Replicas = %d, want 3", cfg.Cluster.Workers.Replicas)
	}
	if cfg.Cluster.Workers.DiskGB != 100 {
		t.Errorf("Worker DiskGB = %d, want 100", cfg.Cluster.Workers.DiskGB)
	}
	if cfg.Network.VIP != "10.40.0.232" {
		t.Errorf("VIP = %q", cfg.Network.VIP)
	}
	if cfg.ProviderConfig.Harvester == nil {
		t.Fatal("Harvester config is nil")
	}
	if cfg.ProviderConfig.Harvester.KubeconfigPath != "/tmp/kubeconfig" {
		t.Errorf("KubeconfigPath = %q", cfg.ProviderConfig.Harvester.KubeconfigPath)
	}
	if cfg.MultiTenancyMode != "Enforced" {
		t.Errorf("MultiTenancyMode = %q, want Enforced", cfg.MultiTenancyMode)
	}

	// IPAM
	if cfg.NetworkPool == nil {
		t.Fatal("NetworkPool is nil")
	}
	if cfg.NetworkPool.Name != "test-cluster-underlay" {
		t.Errorf("Pool Name = %q", cfg.NetworkPool.Name)
	}
	if cfg.NetworkPool.CIDR != "10.40.0.0/24" {
		t.Errorf("Pool CIDR = %q", cfg.NetworkPool.CIDR)
	}
	if cfg.ProviderNetwork == nil {
		t.Fatal("ProviderNetwork is nil")
	}
	if cfg.ProviderNetwork.Mode != "ipam" {
		t.Errorf("ProviderNetwork.Mode = %q, want ipam", cfg.ProviderNetwork.Mode)
	}
	if len(cfg.ProviderNetwork.PoolRefs) != 1 || cfg.ProviderNetwork.PoolRefs[0].Name != "test-cluster-underlay" {
		t.Errorf("PoolRefs = %v", cfg.ProviderNetwork.PoolRefs)
	}
}

func TestBuildConfig_SingleNode(t *testing.T) {
	s := newWizardState()
	s.Provider = "harvester"
	s.ClusterName = "single"
	s.Topology = "single-node"
	s.CPReplicas = "3" // hidden field, still holds default
	s.HarvKubeconfig = "/tmp/kc"
	s.HarvNamespace = "default"
	s.HarvNetwork = "default/net"
	s.HarvImage = "default/img"
	s.IPAMEnabled = false

	cfg, err := buildConfig(s)
	if err != nil {
		t.Fatalf("buildConfig() error: %v", err)
	}

	if cfg.Cluster.ControlPlane.Replicas != 1 {
		t.Errorf("CP Replicas = %d, want 1 (forced by single-node)", cfg.Cluster.ControlPlane.Replicas)
	}
	if cfg.Cluster.Workers.Replicas != 0 {
		t.Errorf("Worker Replicas = %d, want 0", cfg.Cluster.Workers.Replicas)
	}
}

func TestBuildConfig_IPAMDisabled(t *testing.T) {
	s := newWizardState()
	s.Provider = "harvester"
	s.ClusterName = "no-ipam"
	s.Topology = "single-node"
	s.HarvKubeconfig = "/tmp/kc"
	s.HarvNamespace = "default"
	s.HarvNetwork = "default/net"
	s.HarvImage = "default/img"
	s.IPAMEnabled = false

	cfg, err := buildConfig(s)
	if err != nil {
		t.Fatalf("buildConfig() error: %v", err)
	}

	if cfg.NetworkPool != nil {
		t.Error("NetworkPool should be nil when IPAM is disabled")
	}
	if cfg.ProviderNetwork != nil {
		t.Error("ProviderNetwork should be nil when IPAM is disabled")
	}
}

func TestBuildConfig_Nutanix(t *testing.T) {
	s := newWizardState()
	s.Provider = "nutanix"
	s.ClusterName = "nut-test"
	s.Topology = "single-node"
	s.NutEndpoint = "prism.example.com"
	s.NutPort = "9440"
	s.NutUsername = "admin"
	s.NutPassword = "secret"
	s.NutClusterUUID = "uuid-1"
	s.NutSubnetUUID = "uuid-2"
	s.NutImageUUID = "uuid-3"
	s.IPAMEnabled = false

	cfg, err := buildConfig(s)
	if err != nil {
		t.Fatalf("buildConfig() error: %v", err)
	}

	if cfg.ProviderConfig.Nutanix == nil {
		t.Fatal("Nutanix config is nil")
	}
	if cfg.ProviderConfig.Harvester != nil {
		t.Error("Harvester config should be nil for nutanix provider")
	}
	if cfg.ProviderConfig.Nutanix.Endpoint != "prism.example.com" {
		t.Errorf("Endpoint = %q", cfg.ProviderConfig.Nutanix.Endpoint)
	}
	if cfg.ProviderConfig.Nutanix.Port != 9440 {
		t.Errorf("Port = %d", cfg.ProviderConfig.Nutanix.Port)
	}
}

func TestBuildConfig_InvalidNumeric(t *testing.T) {
	s := newWizardState()
	s.Provider = "harvester"
	s.ClusterName = "bad"
	s.Topology = "ha"
	s.CPReplicas = "abc"

	_, err := buildConfig(s)
	if err == nil {
		t.Fatal("expected error for non-numeric CPReplicas")
	}
}
