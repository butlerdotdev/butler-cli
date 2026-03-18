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

// Package wizard provides an interactive step-by-step configuration
// wizard for the bootstrap process using the huh form library.
package wizard

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"

	"github.com/butlerdotdev/butler/internal/adm/bootstrap/orchestrator"
	"github.com/butlerdotdev/butler/internal/adm/bootstrap/wizard/discovery"
	"github.com/butlerdotdev/butler/internal/common/version"
)

// isOnPrem returns true for providers that need VIP and MetalLB config.
func isOnPrem(provider string) bool {
	switch provider {
	case "harvester", "nutanix", "proxmox":
		return true
	default:
		return false
	}
}

const butlerLogo = `
 ██████╗  ██╗
 ██╔══██╗ ██║
 ██████╔╝ ██║
 ██╔══██╗ ██║
 ██████╔╝ ███████╗
 ╚═════╝  ╚══════╝`

// printBanner displays the Butler logo and keyboard hints.
func printBanner() {
	green := lipgloss.NewStyle().Foreground(brandGreen).Bold(true)
	dim := lipgloss.NewStyle().Foreground(brandGray)
	blue := lipgloss.NewStyle().Foreground(brandBlue)

	fmt.Println(green.Render(butlerLogo))
	fmt.Println()
	fmt.Println(green.Render("  B U T L E R"))
	fmt.Println(blue.Render("  Bootstrap Wizard"))
	fmt.Println()
	fmt.Println(dim.Render("  Version: ") + version.Full())
	fmt.Println(dim.Render("  ↑/↓ navigate  |  Enter next  |  Shift+Tab prev field  |  Esc back"))
	fmt.Println()
}

// Run launches the interactive bootstrap wizard.
// Flow: provider+creds → discovery → resources+config+review → image sync → config.
func Run() (*orchestrator.Config, error) {
	printBanner()

	s := newWizardState()
	theme := butlerTheme()
	km := wizardKeyMap()

	var disc discovery.ProviderDiscovery
	for {
		// Form 1: Provider selection and credentials.
		form1 := huh.NewForm(
			providerSelectGroup(s),
			harvesterCredGroup(s),
			nutanixCredGroup(s),
			gcpCredGroup(s),
			awsCredGroup(s),
			azureCredGroup(s),
		).WithTheme(theme).WithKeyMap(km)

		if err := form1.Run(); err != nil {
			return nil, fmt.Errorf("wizard cancelled")
		}

		// Phase 2: Connect to provider and discover resources.
		creds := &stateCredentials{s: s}
		var err error
		disc, err = discovery.NewDiscovery(s.provider, creds)
		if err != nil {
			return nil, fmt.Errorf("creating discovery client: %w", err)
		}

		resources, err := runDiscovery(s.provider, disc)
		if err != nil {
			return nil, err
		}

		// Form 2: Resource selection and cluster configuration.
		form2 := huh.NewForm(
			resourceSelectGroup(s, disc, resources),
			clusterAndSizingStep(s),
			workersAndNetworkStep(s),
			networkOnlyStep(s),
			networkingStep(s),
			exposureModeStep(s),
			exposureIngressStep(s),
			exposureGatewayStep(s),
			platformStep(s),
			consoleStep(s),
			consoleIngressStep(s),
			reviewStep(s),
		).WithTheme(theme).WithKeyMap(km)

		if err := form2.Run(); err != nil {
			// Ctrl+C on form2 → loop back to form1
			continue
		}

		break
	}

	if !s.confirmed {
		return nil, fmt.Errorf("bootstrap cancelled by user")
	}

	// Sync image from factory if requested (on-prem only).
	if isOnPrem(s.provider) && s.imageSource == "factory" {
		factory := discovery.NewFactoryClient("")
		artifactURL := factory.ArtifactURL(
			s.talosSchematic, s.talosVersion, "talos", "amd64", "qcow2")
		displayName := discovery.ProviderImageName(
			"talos", s.talosVersion, "amd64", s.talosSchematic)

		providerRef, err := runImageSync(disc, artifactURL, displayName)
		if err != nil {
			return nil, fmt.Errorf("image sync failed: %w", err)
		}
		switch s.provider {
		case "harvester":
			s.harvImage = providerRef
		case "nutanix":
			s.nutImageUUID = providerRef
		}
	}

	return buildConfig(s)
}

// buildConfig converts wizard state into an orchestrator.Config.
func buildConfig(s *wizardState) (*orchestrator.Config, error) {
	cpReplicas, err := parseInt32(s.cpReplicas)
	if err != nil {
		return nil, fmt.Errorf("control plane replicas: %w", err)
	}
	cpCPU, err := parseInt32(s.cpCPU)
	if err != nil {
		return nil, fmt.Errorf("control plane CPU: %w", err)
	}
	cpMem, err := parseInt32(s.cpMemoryMB)
	if err != nil {
		return nil, fmt.Errorf("control plane memory: %w", err)
	}
	cpDisk, err := parseInt32(s.cpDiskGB)
	if err != nil {
		return nil, fmt.Errorf("control plane disk: %w", err)
	}

	cfg := &orchestrator.Config{
		Provider: s.provider,
		Cluster: orchestrator.ClusterConfig{
			Name:     s.clusterName,
			Topology: s.topology,
			ControlPlane: orchestrator.NodePoolConfig{
				Replicas: cpReplicas,
				CPU:      cpCPU,
				MemoryMB: cpMem,
				DiskGB:   cpDisk,
			},
		},
		Network: orchestrator.NetworkConfig{
			PodCIDR:     s.podCIDR,
			ServiceCIDR: s.serviceCIDR,
			VIP:         s.vip,
		},
		Talos: orchestrator.TalosConfig{
			Version:   s.talosVersion,
			Schematic: s.talosSchematic,
		},
		Addons: orchestrator.AddonsConfig{
			CNI:     orchestrator.CNIConfig{Type: s.cniType},
			Storage: orchestrator.StorageConfig{Type: s.storageType},
		},
	}

	// Single-node forces 1 replica, no workers
	if s.topology == "single-node" {
		cfg.Cluster.ControlPlane.Replicas = 1
	} else {
		wReplicas, err := parseInt32(s.workerReplicas)
		if err != nil {
			return nil, fmt.Errorf("worker replicas: %w", err)
		}
		wCPU, err := parseInt32(s.workerCPU)
		if err != nil {
			return nil, fmt.Errorf("worker CPU: %w", err)
		}
		wMem, err := parseInt32(s.workerMemoryMB)
		if err != nil {
			return nil, fmt.Errorf("worker memory: %w", err)
		}
		wDisk, err := parseInt32(s.workerDiskGB)
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

	// Load balancer (on-prem only)
	if isOnPrem(s.provider) && s.lbType != "none" {
		cfg.Addons.LoadBalancer = orchestrator.LoadBalancerConfig{
			Type:  s.lbType,
			Start: s.lbStart,
			End:   s.lbEnd,
		}
	}

	// Control plane exposure
	cfg.ControlPlaneExposure = orchestrator.ControlPlaneExposureConfig{
		Mode: s.exposureMode,
	}
	if s.exposureMode == "Ingress" {
		cfg.ControlPlaneExposure.Hostname = s.exposureHostname
		cfg.ControlPlaneExposure.IngressClassName = s.exposureIngressClass
		cfg.ControlPlaneExposure.ControllerType = s.exposureControllerType
	}
	if s.exposureMode == "Gateway" {
		cfg.ControlPlaneExposure.Hostname = s.exposureHostname
		cfg.ControlPlaneExposure.GatewayRef = s.exposureGatewayRef
	}

	// Provider network config
	if isOnPrem(s.provider) {
		lbPoolSize, _ := parseInt32(s.lbPoolSize)
		if lbPoolSize == 0 {
			lbPoolSize = 1
		}
		cfg.ProviderNetwork = orchestrator.ProviderNetworkConfig{
			Mode:        "ipam",
			Gateway:     s.providerGateway,
			LBAllocMode: s.lbAllocMode,
			LBPoolSize:  lbPoolSize,
		}
		if s.providerDNS != "" {
			for _, dns := range strings.Split(s.providerDNS, ",") {
				if trimmed := strings.TrimSpace(dns); trimmed != "" {
					cfg.ProviderNetwork.DNSServers = append(cfg.ProviderNetwork.DNSServers, trimmed)
				}
			}
		}
	} else {
		cfg.ProviderNetwork = orchestrator.ProviderNetworkConfig{
			Mode: "cloud",
		}
	}

	// CAPI
	cfg.Addons.CAPI = orchestrator.CAPIConfig{
		Enabled: s.capiEnabled,
		Version: s.capiVersion,
	}

	// Butler controller
	cfg.Addons.ButlerController = orchestrator.ButlerControllerConfig{
		Enabled: s.ctrlEnabled,
		Version: s.ctrlVersion,
		Image:   s.ctrlImage,
	}

	// Console
	if s.consEnabled {
		password := s.consPassword
		if password == "" {
			password = "admin"
		}
		cfg.Addons.Console = orchestrator.ConsoleConfig{
			Enabled: true,
			Version: s.consVersion,
			Ingress: orchestrator.ConsoleIngressConfig{
				Enabled:   s.consIngress,
				Host:      s.consHost,
				ClassName: s.consClass,
				TLS:       s.consTLS,
			},
			Auth: orchestrator.ConsoleAuthConfig{
				AdminPassword: password,
			},
		}
	}

	// Provider-specific config
	switch s.provider {
	case "harvester":
		cfg.ProviderConfig.Harvester = &orchestrator.HarvesterProviderConfig{
			KubeconfigPath: s.harvKubeconfig,
			Namespace:      s.harvNamespace,
			NetworkName:    s.harvNetwork,
			ImageName:      s.harvImage,
		}
	case "nutanix":
		port, _ := parseInt32(s.nutPort)
		cfg.ProviderConfig.Nutanix = &orchestrator.NutanixProviderConfig{
			Endpoint:    s.nutEndpoint,
			Port:        port,
			Insecure:    s.nutInsecure,
			Username:    s.nutUsername,
			Password:    s.nutPassword,
			ClusterUUID: s.nutClusterUUID,
			SubnetUUID:  s.nutSubnetUUID,
			ImageUUID:   s.nutImageUUID,
		}
	case "gcp":
		cfg.ProviderConfig.GCP = &orchestrator.GCPProviderConfig{
			ServiceAccountKeyPath: s.gcpKeyPath,
			ProjectID:             s.gcpProjectID,
			Region:                s.gcpRegion,
			Zone:                  s.gcpZone,
			Network:               s.gcpNetwork,
			Subnetwork:            s.gcpSubnetwork,
		}
	case "aws":
		cfg.ProviderConfig.AWS = &orchestrator.AWSProviderConfig{
			AccessKeyID:     s.awsAccessKey,
			SecretAccessKey:  s.awsSecretKey,
			Region:          s.awsRegion,
			VPCID:           s.awsVPCID,
			SubnetID:        s.awsSubnetID,
			SecurityGroupID: s.awsSecGroupID,
		}
	case "azure":
		cfg.ProviderConfig.Azure = &orchestrator.AzureProviderConfig{
			ClientID:       s.azClientID,
			ClientSecret:   s.azClientSecret,
			TenantID:       s.azTenantID,
			SubscriptionID: s.azSubscriptionID,
			ResourceGroup:  s.azResourceGroup,
			Location:       s.azLocation,
			VNetName:       s.azVNet,
			SubnetName:     s.azSubnet,
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
