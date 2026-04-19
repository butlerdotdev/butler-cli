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
	if cfg.ProviderConfig.Nutanix.Endpoint != "https://prism.example.com" {
		t.Errorf("Endpoint = %q, want %q", cfg.ProviderConfig.Nutanix.Endpoint, "https://prism.example.com")
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

func TestBuildConfig_NTPServers(t *testing.T) {
	s := newWizardState()
	s.Provider = "nutanix"
	s.ClusterName = "ntp-test"
	s.Topology = "single-node"
	s.NTPServers = "10.92.92.2, 10.92.92.4"
	s.NutEndpoint = "https://prism.example.com"
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

	if len(cfg.Talos.TimeServers) != 2 {
		t.Fatalf("TimeServers len = %d, want 2", len(cfg.Talos.TimeServers))
	}
	if cfg.Talos.TimeServers[0] != "10.92.92.2" {
		t.Errorf("TimeServers[0] = %q, want 10.92.92.2", cfg.Talos.TimeServers[0])
	}
	if cfg.Talos.TimeServers[1] != "10.92.92.4" {
		t.Errorf("TimeServers[1] = %q, want 10.92.92.4", cfg.Talos.TimeServers[1])
	}
}

func TestBuildConfig_NTPDefault(t *testing.T) {
	s := newWizardState()
	s.Provider = "harvester"
	s.ClusterName = "ntp-default"
	s.Topology = "single-node"
	s.HarvKubeconfig = "/tmp/kc"
	s.HarvNamespace = "default"
	s.HarvNetwork = "default/net"
	s.HarvImage = "default/img"
	s.IPAMEnabled = false
	// NTPServers defaults to "time.cloudflare.com" from newWizardState()

	cfg, err := buildConfig(s)
	if err != nil {
		t.Fatalf("buildConfig() error: %v", err)
	}

	if len(cfg.Talos.TimeServers) != 1 || cfg.Talos.TimeServers[0] != "time.cloudflare.com" {
		t.Errorf("TimeServers = %v, want [time.cloudflare.com]", cfg.Talos.TimeServers)
	}
}

func TestBuildConfig_NetworkMTU(t *testing.T) {
	base := func() *wizardState {
		s := newWizardState()
		s.Provider = "nutanix"
		s.ClusterName = "mtu-test"
		s.Topology = "single-node"
		s.NutEndpoint = "https://prism.example.com"
		s.NutPort = "9440"
		s.NutUsername = "admin"
		s.NutPassword = "secret"
		s.NutClusterUUID = "uuid-1"
		s.NutSubnetUUID = "uuid-2"
		s.NutImageUUID = "uuid-3"
		s.IPAMEnabled = false
		return s
	}

	tests := []struct {
		name        string
		mtu         string
		jumboFrames bool
		wantMTU     int
	}{
		{"valid MTU", "1380", false, 1380},
		{"empty string", "", false, 0},
		{"whitespace only", "   ", false, 0},
		{"non-numeric (clamped to 0)", "abc", false, 0},
		{"below lower bound (clamped to 0)", "100", false, 0},
		{"above upper bound (clamped to 0)", "10000", false, 0},
		{"exact lower bound", "576", false, 576},
		{"exact upper bound with jumbo", "9000", true, 9000},
		{"standard Ethernet", "1500", false, 1500},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := base()
			s.NetworkMTU = tt.mtu
			s.NetworkJumboFrames = tt.jumboFrames

			cfg, err := buildConfig(s)
			if err != nil {
				t.Fatalf("buildConfig() error: %v", err)
			}

			if cfg.Network.MTU != tt.wantMTU {
				t.Errorf("Network.MTU = %d, want %d", cfg.Network.MTU, tt.wantMTU)
			}
			if cfg.Network.JumboFrames != tt.jumboFrames {
				t.Errorf("Network.JumboFrames = %v, want %v", cfg.Network.JumboFrames, tt.jumboFrames)
			}
		})
	}
}

// TestBuildConfig_JumboFramesCrossValidation proves that the wizard's
// final ValidateMTU gate refuses to emit an un-opted-in jumbo MTU even
// when the individual MTU Input validator lets the value through. This
// is the failure mode the gate was added to catch: an operator enters
// MTU > 1500, the jumbo confirmation step is shown, they Cancel (which
// leaves NetworkJumboFrames false), and buildConfig must refuse.
func TestBuildConfig_JumboFramesCrossValidation(t *testing.T) {
	base := func() *wizardState {
		s := newWizardState()
		s.Provider = "nutanix"
		s.ClusterName = "jumbo-test"
		s.Topology = "single-node"
		s.NutEndpoint = "https://prism.example.com"
		s.NutPort = "9440"
		s.NutUsername = "admin"
		s.NutPassword = "secret"
		s.NutClusterUUID = "uuid-1"
		s.NutSubnetUUID = "uuid-2"
		s.NutImageUUID = "uuid-3"
		s.IPAMEnabled = false
		return s
	}

	tests := []struct {
		name         string
		mtu          string
		jumboFrames  bool
		wantErr      bool
		errSubstring string
	}{
		{"jumbo MTU with opt-in passes", "9000", true, false, ""},
		{"jumbo MTU without opt-in fails", "9000", false, true, "set network.jumboFrames: true"},
		{"mid jumbo without opt-in fails", "1501", false, true, "set network.jumboFrames: true"},
		{"below jumbo threshold ignores flag", "1380", false, false, ""},
		{"unset MTU ignores flag", "", true, false, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := base()
			s.NetworkMTU = tt.mtu
			s.NetworkJumboFrames = tt.jumboFrames

			_, err := buildConfig(s)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("buildConfig() = nil, want error containing %q", tt.errSubstring)
				}
				if !strings.Contains(err.Error(), tt.errSubstring) {
					t.Errorf("buildConfig() error = %q, want substring %q", err.Error(), tt.errSubstring)
				}
				return
			}
			if err != nil {
				t.Errorf("buildConfig() = %v, want nil", err)
			}
		})
	}
}

func TestParseMTU(t *testing.T) {
	tests := []struct {
		in   string
		want int
	}{
		{"1380", 1380},
		{"", 0},
		{"  ", 0},
		{"abc", 0},
		{"-1", 0},
		{"0", 0},
		{"575", 0},
		{"9001", 0},
		{"576", 576},
		{"9000", 9000},
		{" 1500 ", 1500},
	}
	for _, tt := range tests {
		got := parseMTU(tt.in)
		if got != tt.want {
			t.Errorf("parseMTU(%q) = %d, want %d", tt.in, got, tt.want)
		}
	}
}

func TestSplitCSV(t *testing.T) {
	tests := []struct {
		input string
		want  []string
	}{
		{"a, b, c", []string{"a", "b", "c"}},
		{"single", []string{"single"}},
		{" , , ", nil},
		{"10.0.0.1,10.0.0.2", []string{"10.0.0.1", "10.0.0.2"}},
	}
	for _, tt := range tests {
		got := splitCSV(tt.input)
		if len(got) != len(tt.want) {
			t.Errorf("splitCSV(%q) = %v, want %v", tt.input, got, tt.want)
			continue
		}
		for i := range got {
			if got[i] != tt.want[i] {
				t.Errorf("splitCSV(%q)[%d] = %q, want %q", tt.input, i, got[i], tt.want[i])
			}
		}
	}
}

func TestRangeToCIDRs(t *testing.T) {
	tests := []struct {
		name     string
		start    string
		end      string
		wantLen  int
		wantFirst string
		wantLast  string
	}{
		{"clean /27", "10.40.0.0", "10.40.0.31", 1, "10.40.0.0/27", "10.40.0.0/27"},
		{"single IP", "10.0.0.5", "10.0.0.5", 1, "10.0.0.5/32", "10.0.0.5/32"},
		{".1 to .10", "10.92.90.1", "10.92.90.10", 5, "10.92.90.1/32", "10.92.90.10/32"},
		{".1 to .63", "10.92.90.1", "10.92.90.63", 6, "10.92.90.1/32", "10.92.90.32/27"},
		{"empty start", "", "10.0.0.5", 0, "", ""},
		{"reversed", "10.0.0.10", "10.0.0.5", 1, "10.0.0.10/32", "10.0.0.10/32"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cidrs := rangeToCIDRs(tt.start, tt.end)
			if len(cidrs) != tt.wantLen {
				t.Errorf("got %d CIDRs, want %d: %v", len(cidrs), tt.wantLen, cidrs)
			}
			if tt.wantLen > 0 && len(cidrs) > 0 {
				if cidrs[0] != tt.wantFirst {
					t.Errorf("first = %q, want %q", cidrs[0], tt.wantFirst)
				}
				if cidrs[len(cidrs)-1] != tt.wantLast {
					t.Errorf("last = %q, want %q", cidrs[len(cidrs)-1], tt.wantLast)
				}
			}
		})
	}
}
