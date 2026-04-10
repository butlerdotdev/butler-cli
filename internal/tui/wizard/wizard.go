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

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"

	"github.com/butlerdotdev/butler/internal/adm/bootstrap/orchestrator"
	"github.com/butlerdotdev/butler/internal/tui/wizard/discovery"
)

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
	fmt.Println(dim.Render("  ↑/↓ navigate  |  Enter next  |  Shift+Tab prev field  |  Esc back"))
	fmt.Println()
}

// Run launches the interactive bootstrap wizard and returns a fully-populated
// orchestrator.Config ready to hand to the bootstrap TUI or orchestrator.
//
// Flow: provider + credentials → async discovery → resource selection +
// cluster sizing + networking → review → optional image factory sync.
//
// Harvester and Nutanix are the only supported providers today. Cloud
// providers (AWS/Azure/GCP) require building with -tags cloud_discovery.
func Run() (*orchestrator.Config, error) {
	printBanner()

	s := newWizardState()
	theme := butlerTheme()
	km := wizardKeyMap()

	var disc discovery.ProviderDiscovery
	var resources map[string][]discovery.ProviderResource
	confirmed := false

	for {
		// Form 1: Provider selection and credentials. Runs fullscreen
		// (alt-screen) to match the dashboard and bootstrap TUI feel.
		form1 := huh.NewForm(
			providerSelectGroup(s),
			harvesterCredGroup(s),
			nutanixCredGroup(s),
		).
			WithTheme(theme).
			WithKeyMap(km).
			WithProgramOptions(tea.WithAltScreen())

		if err := form1.Run(); err != nil {
			return nil, fmt.Errorf("wizard cancelled: %w", err)
		}

		// Build the discovery client from wizard state and connect to the
		// provider, fetching root resources concurrently.
		creds := &stateCredentials{s: s}
		var err error
		disc, err = discovery.NewDiscovery(s.provider, creds)
		if err != nil {
			return nil, fmt.Errorf("creating discovery client: %w", err)
		}

		resources, err = runDiscovery(s.provider, disc)
		if err != nil {
			return nil, err
		}

		// Form 2: Resource selection, cluster sizing, networking, review.
		form2 := huh.NewForm(
			resourceSelectGroup(s, disc, resources),
			clusterAndSizingStep(s),
			workersStep(s),
			networkingStep(s),
			reviewStep(s, buildSummary(s), &confirmed),
		).
			WithTheme(theme).
			WithKeyMap(km).
			WithProgramOptions(tea.WithAltScreen())

		if err := form2.Run(); err != nil {
			// Ctrl+C on form2 → loop back to form1 so the user can
			// re-enter credentials or switch provider.
			continue
		}

		break
	}

	if !confirmed {
		return nil, fmt.Errorf("bootstrap cancelled by user")
	}

	// Sync Talos image from the Butler Image Factory if requested. The
	// factory upload polls until the provider reports the image as ready,
	// then we store the provider-specific reference (Harvester image name,
	// Nutanix image UUID) back into wizard state before building Config.
	if s.imageSource == "factory" {
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

// buildConfig converts wizard state into an orchestrator.Config that matches
// the current main schema. Field-by-field drift from the feat branch wizard
// lives here — keep this function in sync with orchestrator/config.go.
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
			LoadBalancerPool: &orchestrator.LBPoolConfig{
				Start: s.lbStart,
				End:   s.lbEnd,
			},
		},
		Talos: orchestrator.TalosConfig{
			Version:   s.talosVersion,
			Schematic: s.talosSchematic,
		},
		Addons: orchestrator.AddonsConfig{
			CNI:     orchestrator.CNIConfig{Type: "cilium"},
			Storage: orchestrator.StorageConfig{Type: "longhorn"},
			LoadBalancer: orchestrator.LoadBalancerConfig{
				Type:        "metallb",
				AddressPool: s.lbStart + "-" + s.lbEnd,
			},
			GitOps:           orchestrator.GitOpsConfig{Type: "flux"},
			CAPI:             orchestrator.CAPIConfig{Enabled: true},
			ButlerController: orchestrator.ButlerControllerConfig{Enabled: true},
			Console:          orchestrator.ConsoleConfig{Enabled: true},
		},
	}

	// Single-node forces 1 replica, no workers.
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

	// Provider-specific config.
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
