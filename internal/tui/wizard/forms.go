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
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/huh"

	"github.com/butlerdotdev/butler/internal/tui/wizard/discovery"
)

// providerSelectGroup presents the provider selection dropdown. Only
// providers with discovery support built into this binary are offered.
func providerSelectGroup(s *wizardState) *huh.Group {
	return huh.NewGroup(
		huh.NewNote().
			Title("Provider").
			Description("Select your infrastructure provider."),

		huh.NewSelect[string]().
			Title("Infrastructure Provider").
			Options(
				huh.NewOption("Harvester (HCI)", "harvester"),
				huh.NewOption("Nutanix (AHV)", "nutanix"),
			).
			Value(&s.Provider),
	)
}

// harvesterCredGroup collects the Harvester kubeconfig path.
func harvesterCredGroup(s *wizardState) *huh.Group {
	return huh.NewGroup(
		huh.NewNote().
			Title("Harvester Provider").
			Description("Connect to your Harvester HCI cluster."),

		huh.NewInput().
			Title("Kubeconfig Path").
			Description("Path to the Harvester cluster kubeconfig file").
			Value(&s.HarvKubeconfig).
			Validate(validateNotEmpty),
	).WithHideFunc(func() bool {
		return s.Provider != "harvester"
	})
}

// nutanixCredGroup collects Prism Central endpoint, port, and credentials.
func nutanixCredGroup(s *wizardState) *huh.Group {
	return huh.NewGroup(
		huh.NewNote().
			Title("Nutanix Provider").
			Description("Connect to Prism Central."),

		huh.NewInput().
			Title("Prism Central Endpoint").
			Description("URL of the Prism Central instance").
			Value(&s.NutEndpoint).
			Validate(validateNotEmpty),

		huh.NewInput().
			Title("API Port").
			Value(&s.NutPort).
			Validate(validatePort),

		huh.NewConfirm().
			Title("Allow Insecure TLS").
			Description("Skip TLS verification (self-signed certs)").
			Value(&s.NutInsecure),

		huh.NewInput().
			Title("Username").
			Value(&s.NutUsername).
			Validate(validateNotEmpty),

		huh.NewInput().
			Title("Password").
			Value(&s.NutPassword).
			EchoMode(huh.EchoModePassword).
			Validate(validateNotEmpty),
	).WithHideFunc(func() bool {
		return s.Provider != "nutanix"
	})
}

// resourcesToOptions converts discovered resources to huh select options.
func resourcesToOptions(resources []discovery.ProviderResource) []huh.Option[string] {
	if len(resources) == 0 {
		return []huh.Option[string]{huh.NewOption("(none found)", "")}
	}
	opts := make([]huh.Option[string], 0, len(resources))
	for _, r := range resources {
		label := r.Name
		if r.Description != "" {
			label = fmt.Sprintf("%s (%s)", r.Name, r.Description)
		}
		opts = append(opts, huh.NewOption(label, r.ID))
	}
	return opts
}

// fetchOptions lazily fetches child resources when a parent selection changes.
func fetchOptions(disc discovery.ProviderDiscovery, resourceType string, parentID *string) func() []huh.Option[string] {
	return func() []huh.Option[string] {
		if parentID == nil || *parentID == "" {
			return []huh.Option[string]{huh.NewOption("(select parent first)", "")}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		results, err := disc.FetchResource(ctx, resourceType, *parentID)
		if err != nil {
			return []huh.Option[string]{huh.NewOption(fmt.Sprintf("error: %v", err), "")}
		}
		return resourcesToOptions(results)
	}
}

// resourceSelectGroup dispatches to the provider-specific resource group.
func resourceSelectGroup(s *wizardState, disc discovery.ProviderDiscovery, resources map[string][]discovery.ProviderResource) *huh.Group {
	switch s.Provider {
	case "harvester":
		return harvesterResourceGroup(s, disc, resources)
	case "nutanix":
		return nutanixResourceGroup(s, resources)
	}
	return huh.NewGroup(
		huh.NewNote().Title("Resources").Description("No resources to configure."),
	)
}

// harvesterResourceGroup builds Harvester resource selection (namespace, network, image).
func harvesterResourceGroup(s *wizardState, disc discovery.ProviderDiscovery, resources map[string][]discovery.ProviderResource) *huh.Group {
	nsOpts := resourcesToOptions(resources[discovery.ResourceNamespaces])

	return huh.NewGroup(
		huh.NewNote().
			Title("Infrastructure Resources").
			Description("Select the Harvester resources for VM placement."),

		huh.NewSelect[string]().
			Title("Namespace").
			Options(nsOpts...).
			Value(&s.HarvNamespace),

		huh.NewSelect[string]().
			Title("Network").
			OptionsFunc(fetchOptions(disc, discovery.ResourceNetworks, &s.HarvNamespace), &s.HarvNamespace).
			Value(&s.HarvNetwork),

		huh.NewSelect[string]().
			Title("Image Source").
			Description("Sync the latest Talos image from Butler Image Factory,\nor pick an image already uploaded to Harvester.").
			Options(
				huh.NewOption("Sync from Image Factory (recommended)", "factory"),
				huh.NewOption("Use existing provider image", "existing"),
			).
			Value(&s.ImageSource),

		huh.NewSelect[string]().
			Title("Image").
			Description("Talos image for VM boot disks").
			OptionsFunc(func() []huh.Option[string] {
				if s.ImageSource == "factory" {
					return []huh.Option[string]{
						huh.NewOption("(will sync after configuration)", "factory-pending"),
					}
				}
				return fetchOptions(disc, discovery.ResourceImages, &s.HarvNamespace)()
			}, &s.ImageSource).
			Value(&s.HarvImage),
	)
}

// nutanixResourceGroup builds Nutanix resource selection (cluster, subnet, image).
func nutanixResourceGroup(s *wizardState, resources map[string][]discovery.ProviderResource) *huh.Group {
	clusterOpts := resourcesToOptions(resources[discovery.ResourceClusters])
	subnetOpts := resourcesToOptions(resources[discovery.ResourceSubnets])

	return huh.NewGroup(
		huh.NewNote().
			Title("Infrastructure Resources").
			Description("Select the Nutanix resources for VM placement."),

		huh.NewSelect[string]().
			Title("Cluster").
			Options(clusterOpts...).
			Value(&s.NutClusterUUID),

		huh.NewSelect[string]().
			Title("Subnet").
			Options(subnetOpts...).
			Value(&s.NutSubnetUUID),

		huh.NewSelect[string]().
			Title("Image Source").
			Description("Sync the latest Talos image from Butler Image Factory,\nor pick an image already uploaded to Nutanix.").
			Options(
				huh.NewOption("Sync from Image Factory (recommended)", "factory"),
				huh.NewOption("Use existing provider image", "existing"),
			).
			Value(&s.ImageSource),

		huh.NewSelect[string]().
			Title("Image").
			Description("Talos image for VM boot disks").
			OptionsFunc(func() []huh.Option[string] {
				if s.ImageSource == "factory" {
					return []huh.Option[string]{
						huh.NewOption("(will sync after configuration)", "factory-pending"),
					}
				}
				return resourcesToOptions(resources[discovery.ResourceImages])
			}, &s.ImageSource).
			Value(&s.NutImageUUID),
	)
}

// clusterAndSizingStep configures cluster name, topology, and control plane sizing.
func clusterAndSizingStep(s *wizardState) *huh.Group {
	return huh.NewGroup(
		huh.NewNote().
			Title("Cluster Configuration").
			Description("Define the management cluster identity and control plane resources."),

		huh.NewInput().
			Title("Cluster Name").
			Placeholder("butler-mgmt").
			Description("Lowercase alphanumeric with hyphens, max 63 chars").
			Value(&s.ClusterName).
			Validate(validateClusterName),

		huh.NewSelect[string]().
			Title("Topology").
			Options(
				huh.NewOption("High Availability (3 CP + workers)", "ha"),
				huh.NewOption("Single Node (1 node, no workers)", "single-node"),
			).
			Value(&s.Topology),

		huh.NewInput().
			Title("CP Replicas").
			Description("Control plane nodes (odd number for etcd quorum)").
			Value(&s.CPReplicas).
			Validate(validateIntRange(1, 7)),

		huh.NewInput().
			Title("CP vCPUs").
			Value(&s.CPCPU).
			Validate(validateIntRange(1, 64)),

		huh.NewInput().
			Title("CP Memory (MB)").
			Value(&s.CPMemoryMB).
			Validate(validateIntRange(1024, 131072)),

		huh.NewInput().
			Title("CP Disk (GB)").
			Value(&s.CPDiskGB).
			Validate(validateIntRange(20, 2048)),
	)
}

// workersStep configures worker sizing (hidden for single-node topology).
func workersStep(s *wizardState) *huh.Group {
	return huh.NewGroup(
		huh.NewNote().
			Title("Worker Nodes").
			Description("Configure worker nodes (ignored for single-node topology)."),

		huh.NewInput().
			Title("Worker Replicas").
			Description("HA topology requires at least 3 workers so Longhorn can place its default 3 replicas. Fewer workers will fail during addon install (Steward etcd, butler-console, etc.)").
			Value(&s.WorkerReplicas).
			Validate(validateWorkerReplicas(s)),

		huh.NewInput().
			Title("Worker vCPUs").
			Value(&s.WorkerCPU).
			Validate(validateIntRange(1, 128)),

		huh.NewInput().
			Title("Worker Memory (MB)").
			Value(&s.WorkerMemoryMB).
			Validate(validateIntRange(1024, 524288)),

		huh.NewInput().
			Title("Worker Disk (GB)").
			Value(&s.WorkerDiskGB).
			Validate(validateIntRange(20, 4096)),
	).WithHideFunc(func() bool {
		return s.Topology == "single-node"
	})
}

// networkingStep configures pod/service CIDRs, VIP, and MetalLB pool.
func networkingStep(s *wizardState) *huh.Group {
	return huh.NewGroup(
		huh.NewNote().
			Title("Networking").
			Description("Cluster networking and MetalLB load balancer pool."),

		huh.NewInput().
			Title("Pod CIDR").
			Value(&s.PodCIDR).
			Validate(validateCIDR),

		huh.NewInput().
			Title("Service CIDR").
			Value(&s.ServiceCIDR).
			Validate(validateCIDR),

		huh.NewInput().
			Title("Control Plane VIP").
			Description("Unused IP for kube-vip HA. Must be outside DHCP range.").
			Placeholder("10.40.0.100").
			Value(&s.VIP).
			Validate(validateOptional(validateIP)),

		huh.NewInput().
			Title("LB Pool Start").
			Description("First IP for MetalLB LoadBalancer services").
			Placeholder("10.40.0.200").
			Value(&s.LBStart).
			Validate(validateIP),

		huh.NewInput().
			Title("LB Pool End").
			Description("Last IP for MetalLB LoadBalancer services").
			Placeholder("10.40.0.250").
			Value(&s.LBEnd).
			Validate(validateIP),
	)
}

// reviewStep shows a summary of the collected config and asks for
// confirmation. The summary is rendered via DescriptionFunc so huh
// re-evaluates it whenever any wizardState field changes — a static
// Description would be captured at form construction time, before the
// user has filled in namespace, cluster name, sizing, etc.
func reviewStep(s *wizardState, confirmed *bool) *huh.Group {
	return huh.NewGroup(
		huh.NewNote().
			Title("Review").
			DescriptionFunc(func() string {
				return buildSummary(s)
			}, s),

		huh.NewConfirm().
			Title("Launch bootstrap with this configuration?").
			Affirmative("Launch").
			Negative("Cancel").
			Value(confirmed),
	)
}

// buildSummary renders a human-readable summary of the collected wizard state
// for display on the review screen.
func buildSummary(s *wizardState) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Provider:       %s\n", s.Provider)
	fmt.Fprintf(&b, "Cluster Name:   %s\n", s.ClusterName)
	fmt.Fprintf(&b, "Topology:       %s\n", s.Topology)
	fmt.Fprintf(&b, "Control Plane:  %s × (%s vCPU, %s MB, %s GB)\n",
		s.CPReplicas, s.CPCPU, s.CPMemoryMB, s.CPDiskGB)
	if s.Topology != "single-node" {
		fmt.Fprintf(&b, "Workers:        %s × (%s vCPU, %s MB, %s GB)\n",
			s.WorkerReplicas, s.WorkerCPU, s.WorkerMemoryMB, s.WorkerDiskGB)
	}
	fmt.Fprintf(&b, "Pod CIDR:       %s\n", s.PodCIDR)
	fmt.Fprintf(&b, "Service CIDR:   %s\n", s.ServiceCIDR)
	if s.VIP != "" {
		fmt.Fprintf(&b, "VIP:            %s\n", s.VIP)
	}
	fmt.Fprintf(&b, "LB Pool:        %s - %s\n", s.LBStart, s.LBEnd)

	switch s.Provider {
	case "harvester":
		fmt.Fprintf(&b, "Namespace:      %s\n", s.HarvNamespace)
		fmt.Fprintf(&b, "Network:        %s\n", s.HarvNetwork)
		if s.ImageSource == "factory" {
			fmt.Fprintf(&b, "Image:          (will sync from Butler Image Factory)\n")
		} else {
			fmt.Fprintf(&b, "Image:          %s\n", s.HarvImage)
		}
	case "nutanix":
		fmt.Fprintf(&b, "Cluster:        %s\n", s.NutClusterUUID)
		fmt.Fprintf(&b, "Subnet:         %s\n", s.NutSubnetUUID)
		if s.ImageSource == "factory" {
			fmt.Fprintf(&b, "Image:          (will sync from Butler Image Factory)\n")
		} else {
			fmt.Fprintf(&b, "Image:          %s\n", s.NutImageUUID)
		}
	}
	return b.String()
}
