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

// Butler Labs BL monogram rendered in brand green.
const butlerLogo = `
 ██████╗  ██╗
 ██╔══██╗ ██║
 ██████╔╝ ██║
 ██╔══██╗ ██║
 ██████╔╝ ███████╗
 ╚═════╝  ╚══════╝`

// printBanner displays the branded header before the wizard starts.
func printBanner(provider string) {
	green := lipgloss.NewStyle().Foreground(brandGreen).Bold(true)
	dim := lipgloss.NewStyle().Foreground(brandGray)
	blue := lipgloss.NewStyle().Foreground(brandBlue)

	fmt.Println(green.Render(butlerLogo))
	fmt.Println()
	fmt.Println(green.Render("  B U T L E R"))
	fmt.Println(blue.Render("  Bootstrap Wizard"))
	fmt.Println()

	info := []string{
		dim.Render("  Version:  ") + version.Full(),
		dim.Render("  Provider: ") + green.Render(strings.ToUpper(provider)),
	}
	for _, line := range info {
		fmt.Println(line)
	}
	fmt.Println()
}

// Run launches the interactive bootstrap wizard for the given provider.
// It walks the user through all configuration steps and returns a
// fully populated Config ready for the orchestrator.
//
// The wizard flow is:
//  1. Provider credentials (authenticate first for potential dynamic lookups)
//  2. Cluster identity + control plane sizing
//  3. Workers + networking (or just networking for single-node)
//  4. Platform config (Talos, addons, Butler components)
//  5. Console details (if enabled)
//  6. Confirm
func Run(provider string) (*orchestrator.Config, error) {
	printBanner(provider)

	s := newWizardState()
	onPrem := isOnPrem(provider)

	// Step 1: Provider credentials first -- authenticate early so future
	// steps could dynamically pull options from the platform.
	var providerGroup *huh.Group
	switch provider {
	case "harvester":
		providerGroup = harvesterStep(s)
	case "nutanix":
		providerGroup = nutanixStep(s)
	case "gcp":
		providerGroup = gcpStep(s)
	case "aws":
		providerGroup = awsStep(s)
	case "azure":
		providerGroup = azureStep(s)
	default:
		return nil, fmt.Errorf("unsupported provider: %s", provider)
	}

	groups := []*huh.Group{
		providerGroup,                           // 1. Auth
		clusterAndSizingStep(s),                 // 2. Cluster + CP sizing
		workersAndNetworkStep(s, onPrem),         // 3a. Workers + network (HA only)
		networkOnlyStep(s, onPrem),               // 3b. Network only (single-node only)
		platformStep(s, onPrem),                  // 4. Talos + addons
		consoleStep(s),                           // 5. Console (if enabled)
		huh.NewGroup(                             // 6. Confirm
			huh.NewConfirm().
				Title("Start Bootstrap?").
				Description("Review complete. Begin provisioning the management cluster?").
				Affirmative("Start").
				Negative("Cancel").
				Value(&s.confirmed),
		),
	}

	form := huh.NewForm(groups...).WithTheme(butlerTheme())

	if err := form.Run(); err != nil {
		return nil, fmt.Errorf("wizard cancelled: %w", err)
	}

	if !s.confirmed {
		return nil, fmt.Errorf("bootstrap cancelled by user")
	}

	return buildConfig(provider, s)
}

// buildConfig converts the wizard state into an orchestrator.Config.
func buildConfig(provider string, s *wizardState) (*orchestrator.Config, error) {
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
		Provider: provider,
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
	if isOnPrem(provider) {
		cfg.Addons.LoadBalancer = orchestrator.LoadBalancerConfig{
			Type:        s.lbType,
			AddressPool: s.lbPool,
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
	switch provider {
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
			AccessKeyID:    s.awsAccessKey,
			SecretAccessKey: s.awsSecretKey,
			Region:         s.awsRegion,
			VPCID:          s.awsVPCID,
			SubnetID:       s.awsSubnetID,
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
		}
	}

	return cfg, nil
}

// parseInt32 parses a string to int32.
func parseInt32(s string) (int32, error) {
	v, err := strconv.ParseInt(s, 10, 32)
	if err != nil {
		return 0, err
	}
	return int32(v), nil
}
