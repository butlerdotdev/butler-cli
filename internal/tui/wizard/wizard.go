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

// Package wizard provides an interactive step-by-step configuration wizard
// for the bootstrap process using the huh form library. It targets the
// current main orchestrator.Config schema — see state.go for the fields it
// collects and buildConfig for the assembly mapping.
package wizard

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/huh"

	"github.com/butlerdotdev/butler/internal/adm/bootstrap/orchestrator"
	"github.com/butlerdotdev/butler/internal/tui/wizard/discovery"
)

// Run launches the interactive bootstrap wizard and returns a fully-populated
// orchestrator.Config ready to hand to the bootstrap TUI or orchestrator.
//
// Flow: provider + credentials, async discovery, resource selection plus
// cluster sizing plus networking, review, optional image factory sync.
//
// Harvester and Nutanix are the only supported providers today. Cloud
// providers (AWS/Azure/GCP) require building with -tags cloud_discovery.
func Run() (*orchestrator.Config, error) {
	s := newWizardState()
	theme := butlerTheme()
	km := wizardKeyMap()

	var disc discovery.ProviderDiscovery
	var resources map[string][]discovery.ProviderResource
	confirmed := false

	for {
		// Form 1: Provider selection and credentials. Wrapped in a
		// wizardShell so the fullscreen frame has the same header and
		// key legend as the dashboard TUI. huh's built-in help is
		// suppressed because the shell renders its own legend.
		form1 := huh.NewForm(
			providerSelectGroup(s),
			harvesterCredGroup(s),
			nutanixCredGroup(s),
			awsCredGroup(s),
			azureCredGroup(s),
			gcpCredGroup(s),
		).
			WithTheme(theme).
			WithKeyMap(km).
			WithShowHelp(false)

		if err := runFormShell(form1, "Butler Bootstrap Wizard  Step 1/2: Provider"); err != nil {
			return nil, fmt.Errorf("wizard cancelled: %w", err)
		}
		if form1.State == huh.StateAborted {
			return nil, fmt.Errorf("wizard cancelled by user")
		}

		// Build the discovery client from wizard state and connect to the
		// provider, fetching root resources concurrently.
		creds := &stateCredentials{s: s}
		var err error
		disc, err = discovery.NewDiscovery(s.Provider, creds)
		if err != nil {
			return nil, fmt.Errorf("creating discovery client: %w", err)
		}

		resources, err = runDiscovery(s.Provider, disc)
		if err != nil {
			return nil, err
		}

		// Form 2: Resource selection, cluster sizing, networking, IPAM,
		// platform settings, exposure, console, review.
		form2 := huh.NewForm(
			resourceSelectGroup(s, disc, resources),
			clusterAndSizingStep(s),
			cpReplicasStep(s),
			workersStep(s),
			networkingStep(s),
			onPremNetworkingStep(s),
			ipamStep(s),
			networkPoolStep(s),
			providerNetworkStep(s),
			multiTenancyStep(s),
			exposureModeStep(s),
			exposureIngressStep(s),
			exposureGatewayStep(s),
			consoleStep(s),
			consoleIngressStep(s),
			reviewStep(s, &confirmed),
		).
			WithTheme(theme).
			WithKeyMap(km).
			WithShowHelp(false)

		if err := runFormShell(form2, "Butler Bootstrap Wizard  Step 2/2: Configuration"); err != nil {
			return nil, fmt.Errorf("wizard cancelled: %w", err)
		}
		if form2.State == huh.StateAborted {
			// Ctrl+C on form2 — loop back to form1 so the user can
			// re-enter credentials or switch provider.
			continue
		}

		break
	}

	if !confirmed {
		return nil, fmt.Errorf("bootstrap cancelled by user")
	}

	// Sync Talos image from the Butler Image Factory if requested. Only
	// applies to on-prem providers (Harvester/Nutanix) where the wizard
	// offers a "Sync from Factory" image source option. Cloud providers
	// reference pre-existing images via provider-specific config fields
	// (GCE image name, AMI ID, etc.) and don't support factory sync.
	if isOnPrem(s.Provider) && s.ImageSource == "factory" {
		factory := discovery.NewFactoryClient("")
		artifactURL := factory.ArtifactURL(
			s.TalosSchematic, s.TalosVersion, "talos", "amd64", "qcow2")
		displayName := discovery.ProviderImageName(
			"talos", s.TalosVersion, "amd64", s.TalosSchematic)

		providerRef, err := runImageSync(disc, artifactURL, displayName)
		if err != nil {
			return nil, fmt.Errorf("image sync failed: %w", err)
		}
		switch s.Provider {
		case "harvester":
			s.HarvImage = providerRef
		case "nutanix":
			s.NutImageUUID = providerRef
		}
	}

	return buildConfig(s)
}

// buildConfig converts wizard state into an orchestrator.Config that matches
// the current main schema. Field-by-field drift from the feat branch wizard
// lives here — keep this function in sync with orchestrator/config.go.
func buildConfig(s *wizardState) (*orchestrator.Config, error) {
	cpReplicas, err := parseInt32(s.CPReplicas)
	if err != nil {
		return nil, fmt.Errorf("control plane replicas: %w", err)
	}
	cpCPU, err := parseInt32(s.CPCPU)
	if err != nil {
		return nil, fmt.Errorf("control plane CPU: %w", err)
	}
	cpMem, err := parseInt32(s.CPMemoryMB)
	if err != nil {
		return nil, fmt.Errorf("control plane memory: %w", err)
	}
	cpDisk, err := parseInt32(s.CPDiskGB)
	if err != nil {
		return nil, fmt.Errorf("control plane disk: %w", err)
	}

	cfg := &orchestrator.Config{
		Provider: s.Provider,
		Cluster: orchestrator.ClusterConfig{
			Name:     s.ClusterName,
			Topology: s.Topology,
			ControlPlane: orchestrator.NodePoolConfig{
				Replicas: cpReplicas,
				CPU:      cpCPU,
				MemoryMB: cpMem,
				DiskGB:   cpDisk,
			},
		},
		Network: orchestrator.NetworkConfig{
			PodCIDR:     s.PodCIDR,
			ServiceCIDR: s.ServiceCIDR,
		},
		Talos: orchestrator.TalosConfig{
			Version:   s.TalosVersion,
			Schematic: s.TalosSchematic,
		},
		Addons: orchestrator.AddonsConfig{
			CNI:     orchestrator.CNIConfig{Type: "cilium"},
			Storage: orchestrator.StorageConfig{Type: "longhorn"},
			GitOps:           orchestrator.GitOpsConfig{Type: "flux"},
			CAPI:             orchestrator.CAPIConfig{Enabled: true},
			ButlerController: orchestrator.ButlerControllerConfig{Enabled: true},
			Console: orchestrator.ConsoleConfig{
				Enabled: true,
				Ingress: orchestrator.ConsoleIngressConfig{
					Enabled:   s.ConsoleIngressEnabled,
					Host:      s.ConsoleHost,
					ClassName: s.ConsoleClass,
					TLS:       s.ConsoleTLS,
				},
				Auth: orchestrator.ConsoleAuthConfig{
					AdminPassword: s.ConsoleAdminPassword,
				},
			},
		},
		ControlPlaneExposure: &orchestrator.ControlPlaneExposureConfig{
			Mode:             s.ExposureMode,
			Hostname:         s.ExposureHostname,
			IngressClassName: s.ExposureIngressClass,
			ControllerType:   s.ExposureControllerType,
			GatewayRef:       s.ExposureGatewayRef,
		},
		MultiTenancyMode: s.MultiTenancyMode,
	}

	// On-prem providers use kube-vip (VIP) and MetalLB for LoadBalancer
	// services. Cloud providers use native cloud LBs via CCM/CAPI and
	// set loadBalancer.type to "none" so the bootstrap controller skips
	// MetalLB installation. The CRD validates the type field and rejects
	// empty strings — it must be "metallb", "none", or omitted.
	if isOnPrem(s.Provider) {
		cfg.Network.VIP = s.VIP
		cfg.Network.LoadBalancerPool = &orchestrator.LBPoolConfig{
			Start: s.LBStart,
			End:   s.LBEnd,
		}
		cfg.Addons.LoadBalancer = orchestrator.LoadBalancerConfig{
			Type:        "metallb",
			AddressPool: s.LBStart + "-" + s.LBEnd,
		}
	} else {
		cfg.Addons.LoadBalancer = orchestrator.LoadBalancerConfig{
			Type: "none",
		}
	}

	// IPAM: emit a NetworkPool and wire the ProviderConfig's spec.network
	// section so the management cluster is ready to provision tenants the
	// moment bootstrap completes. When disabled the bootstrap skips both
	// and operators have to stand them up manually.
	if s.IPAMEnabled {
		lbPoolDefault, _ := parseInt32(s.TenantLBPoolPerTenant)
		nodesDefault, _ := parseInt32(s.TenantNodesPerTenant)
		initialPool, _ := parseInt32(s.LBInitialPoolSize)
		defaultPool, _ := parseInt32(s.LBDefaultPoolSize)
		growthInc, _ := parseInt32(s.LBGrowthIncrement)
		maxLB, _ := parseInt32(s.QuotaMaxLoadBalancerIPs)
		maxNodes, _ := parseInt32(s.QuotaMaxNodeIPs)

		poolName := s.ClusterName + "-underlay"

		cfg.NetworkPool = &orchestrator.NetworkPoolConfig{
			Name: poolName,
			CIDR: s.PoolCIDR,
			TenantAllocation: orchestrator.TenantAllocationConfig{
				Start: s.TenantAllocStart,
				End:   s.TenantAllocEnd,
				Defaults: orchestrator.TenantAllocationDefaultsConfig{
					LBPoolPerTenant: lbPoolDefault,
					NodesPerTenant:  nodesDefault,
				},
			},
		}

		// Gateway and DNS are deliberately omitted: Harvester/Nutanix
		// workload networks run DHCP, so tenant VMs pick both up
		// automatically. The ProviderConfig fields stay empty and the
		// orchestrator's builder skips them when serializing.
		cfg.ProviderNetwork = &orchestrator.ProviderNetworkConfig{
			Mode: "ipam",
			PoolRefs: []orchestrator.PoolReferenceConfig{
				{Name: poolName, Priority: 1},
			},
			Subnet: s.PoolCIDR,
			LoadBalancer: orchestrator.LBAllocConfig{
				AllocationMode:  s.LBAllocationMode,
				InitialPoolSize: initialPool,
				DefaultPoolSize: defaultPool,
				GrowthIncrement: growthInc,
			},
			QuotaPerTenant: orchestrator.QuotaPerTenantConfig{
				MaxLoadBalancerIPs: maxLB,
				MaxNodeIPs:         maxNodes,
			},
		}
	}

	// Single-node forces 1 replica, no workers.
	if s.Topology == "single-node" {
		cfg.Cluster.ControlPlane.Replicas = 1
	} else {
		wReplicas, err := parseInt32(s.WorkerReplicas)
		if err != nil {
			return nil, fmt.Errorf("worker replicas: %w", err)
		}
		wCPU, err := parseInt32(s.WorkerCPU)
		if err != nil {
			return nil, fmt.Errorf("worker CPU: %w", err)
		}
		wMem, err := parseInt32(s.WorkerMemoryMB)
		if err != nil {
			return nil, fmt.Errorf("worker memory: %w", err)
		}
		wDisk, err := parseInt32(s.WorkerDiskGB)
		if err != nil {
			return nil, fmt.Errorf("worker disk: %w", err)
		}
		cfg.Cluster.Workers = orchestrator.NodePoolConfig{
			Replicas: wReplicas,
			CPU:      wCPU,
			MemoryMB: wMem,
			DiskGB:   wDisk,
		}
	}

	// Provider-specific config.
	switch s.Provider {
	case "harvester":
		cfg.ProviderConfig.Harvester = &orchestrator.HarvesterProviderConfig{
			// Expand ~ to the home directory. LoadConfig does this for
			// YAML-driven bootstrap, but the wizard bypasses LoadConfig
			// so the expansion has to happen here too.
			KubeconfigPath: orchestrator.ExpandPath(s.HarvKubeconfig),
			Namespace:      s.HarvNamespace,
			NetworkName:    s.HarvNetwork,
			ImageName:      s.HarvImage,
		}
	case "nutanix":
		port, _ := parseInt32(s.NutPort)
		cfg.ProviderConfig.Nutanix = &orchestrator.NutanixProviderConfig{
			Endpoint:    s.NutEndpoint,
			Port:        port,
			Insecure:    s.NutInsecure,
			Username:    s.NutUsername,
			Password:    s.NutPassword,
			ClusterUUID: s.NutClusterUUID,
			SubnetUUID:  s.NutSubnetUUID,
			ImageUUID:   s.NutImageUUID,
		}
	case "aws":
		cfg.ProviderConfig.AWS = &orchestrator.AWSProviderConfig{
			AccessKeyID:     s.AWSAccessKey,
			SecretAccessKey:  s.AWSSecretKey,
			Region:          s.AWSRegion,
			VPCID:           s.AWSVPCID,
			SubnetID:        s.AWSSubnetID,
			SecurityGroupID: s.AWSSecGroupID,
			AMI:             s.AWSAMI,
		}
	case "azure":
		cfg.ProviderConfig.Azure = &orchestrator.AzureProviderConfig{
			ClientID:          s.AZClientID,
			ClientSecret:      s.AZClientSecret,
			TenantID:          s.AZTenantID,
			SubscriptionID:    s.AZSubscriptionID,
			ResourceGroup:     s.AZResourceGroup,
			Location:          s.AZLocation,
			VNetName:          s.AZVNet,
			SubnetName:        s.AZSubnet,
			SecurityGroupName: s.AZSecurityGroup,
			VMSize:            s.AZVMSize,
			ImageURN:          s.AZImageURN,
		}
	case "gcp":
		cfg.ProviderConfig.GCP = &orchestrator.GCPProviderConfig{
			ServiceAccountKeyPath: orchestrator.ExpandPath(s.GCPKeyPath),
			ProjectID:             s.GCPProjectID,
			Region:                s.GCPRegion,
			Zone:                  s.GCPZone,
			Network:               s.GCPNetwork,
			Subnetwork:            s.GCPSubnetwork,
			ImageProject:          s.GCPProjectID, // images are in the same project
			Image:                 s.GCPImage,
		}
	}

	return cfg, nil
}

func parseInt32(s string) (int32, error) {
	v, err := strconv.ParseInt(strings.TrimSpace(s), 10, 32)
	if err != nil {
		return 0, err
	}
	return int32(v), nil
}
