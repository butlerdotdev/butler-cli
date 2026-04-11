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

// clusterAndSizingStep configures cluster name, topology, and control plane
// sizing (except CP Replicas which lives in its own group so it can be
// hidden for single-node topology).
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

// cpReplicasStep asks for the number of control plane nodes. Hidden for
// single-node topology since that's always 1 CP regardless of input.
func cpReplicasStep(s *wizardState) *huh.Group {
	return huh.NewGroup(
		huh.NewInput().
			Title("CP Replicas").
			Description("Control plane nodes (odd number for etcd quorum)").
			Value(&s.CPReplicas).
			Validate(validateIntRange(1, 7)),
	).WithHideFunc(func() bool {
		return s.Topology == "single-node"
	})
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
			Description("HA topology requires at least 50 GB. Longhorn reserves ~30%% for overhead, and Steward etcd + Butler Console DB replicas need headroom.").
			Value(&s.WorkerDiskGB).
			Validate(validateWorkerDiskGB(s)),
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

// ipamStep toggles whether the bootstrap creates a NetworkPool CR for
// tenant IPAM. The explanatory Note makes it clear that disabling IPAM
// means operators have to stand up the NetworkPool + ProviderConfig
// network section manually after bootstrap, which is the historical
// path and a common source of "tenant creation hangs on IP allocation"
// failures.
func ipamStep(s *wizardState) *huh.Group {
	return huh.NewGroup(
		huh.NewNote().
			Title("Tenant IP Allocation (IPAM)").
			Description(
				"Butler uses a NetworkPool CR to allocate IPs to tenant clusters\n" +
					"on on-prem providers (Harvester, Nutanix, Proxmox). Without one,\n" +
					"tenant creation fails at IP allocation and you have to build the\n" +
					"NetworkPool and ProviderConfig.spec.network by hand later.\n\n" +
					"Enabling IPAM here makes the bootstrap emit the NetworkPool and\n" +
					"wire the ProviderConfig to use it — the management cluster is\n" +
					"tenant-ready the moment bootstrap completes.\n\n" +
					"Cloud providers (AWS, Azure, GCP) use native networking and\n" +
					"should leave this disabled."),

		huh.NewConfirm().
			Title("Enable IPAM for tenant clusters?").
			Affirmative("Enable").
			Negative("Skip").
			Value(&s.IPAMEnabled),
	)
}

// networkPoolStep collects the NetworkPool CIDR, tenant allocation
// sub-range, and per-tenant defaults. Hidden when IPAM is disabled.
func networkPoolStep(s *wizardState) *huh.Group {
	return huh.NewGroup(
		huh.NewNote().
			Title("NetworkPool").
			Description(
				"Pool CIDR is the full network range the pool manages. The\n" +
					"Tenant Allocation sub-range is what the pool will hand out to\n" +
					"tenant clusters — leave room outside it for the management\n" +
					"cluster's own nodes, VIP, and LoadBalancer pool.\n\n" +
					"Example: pool CIDR 10.40.0.0/22, management cluster lives in\n" +
					"10.40.0.0/24 and 10.40.1.0/24, tenant allocation runs from\n" +
					"10.40.2.0 to 10.40.3.254."),

		huh.NewInput().
			Title("Pool CIDR").
			Description("Full network range the NetworkPool manages.").
			Placeholder("10.40.0.0/22").
			Value(&s.PoolCIDR),

		huh.NewInput().
			Title("Tenant Allocation Start").
			Description("First IP allocatable to tenant clusters.").
			Placeholder("10.40.2.0").
			Value(&s.TenantAllocStart),

		huh.NewInput().
			Title("Tenant Allocation End").
			Description("Last IP allocatable to tenant clusters.").
			Placeholder("10.40.3.254").
			Value(&s.TenantAllocEnd),

		huh.NewInput().
			Title("Default LB IPs per Tenant").
			Description("How many LoadBalancer IPs each tenant gets by default.").
			Value(&s.TenantLBPoolPerTenant).
			Validate(validateIntRange(1, 64)),

		huh.NewInput().
			Title("Default Nodes per Tenant").
			Description("How many node IPs each tenant gets by default.").
			Value(&s.TenantNodesPerTenant).
			Validate(validateIntRange(1, 64)),
	).WithHideFunc(func() bool {
		return !s.IPAMEnabled
	})
}

// providerNetworkStep collects the ProviderConfig.spec.network settings
// that bind the provider to the NetworkPool. Gateway and DNS servers are
// required for tenant VMs to get a working network. LB allocation and
// per-tenant quotas are less common to tune and ship with butler-beta's
// defaults. Hidden when IPAM is disabled.
func providerNetworkStep(s *wizardState) *huh.Group {
	return huh.NewGroup(
		huh.NewNote().
			Title("Provider Network").
			Description(
				"These settings go into ProviderConfig.spec.network and tell the\n" +
					"provider how to wire tenant VMs into the network: gateway, DNS,\n" +
					"and how aggressively to allocate LB IPs per tenant.\n\n" +
					"Defaults match butler-beta's production values — safe starting\n" +
					"point that you can tune later via kubectl edit providerconfig."),

		huh.NewInput().
			Title("Gateway IP").
			Description("Network gateway tenant VMs route through.").
			Placeholder("10.40.0.1").
			Value(&s.ProviderGateway),

		huh.NewInput().
			Title("DNS Servers").
			Description("Comma-separated DNS IPs injected into tenant VMs.").
			Placeholder("10.40.0.1, 1.1.1.1").
			Value(&s.ProviderDNSServers),

		huh.NewSelect[string]().
			Title("LB Allocation Mode").
			Description("static: fixed block per tenant. elastic: starts small, grows with demand.").
			Options(
				huh.NewOption("Static (fixed pool)", "static"),
				huh.NewOption("Elastic (auto-scale)", "elastic"),
			).
			Value(&s.LBAllocationMode),

		huh.NewInput().
			Title("Initial LB Pool Size").
			Description("Starting LB IPs per tenant.").
			Value(&s.LBInitialPoolSize).
			Validate(validateIntRange(1, 64)),

		huh.NewInput().
			Title("Default LB Pool Size").
			Description("Target LB IPs per tenant in static mode.").
			Value(&s.LBDefaultPoolSize).
			Validate(validateIntRange(1, 64)),

		huh.NewInput().
			Title("Growth Increment").
			Description("Elastic mode: IPs added per expansion step.").
			Value(&s.LBGrowthIncrement).
			Validate(validateIntRange(1, 64)),

		huh.NewInput().
			Title("Max LB IPs per Tenant").
			Description("Hard cap on LB IP consumption per tenant.").
			Value(&s.QuotaMaxLoadBalancerIPs).
			Validate(validateIntRange(1, 256)),

		huh.NewInput().
			Title("Max Node IPs per Tenant").
			Description("Hard cap on node IP consumption per tenant.").
			Value(&s.QuotaMaxNodeIPs).
			Validate(validateIntRange(1, 256)),
	).WithHideFunc(func() bool {
		return !s.IPAMEnabled
	})
}

// exposureModeStep asks how tenant-cluster API servers will be exposed.
// The Note spells out the trade-offs so operators can make an informed
// choice instead of defaulting blindly.
func exposureModeStep(s *wizardState) *huh.Group {
	return huh.NewGroup(
		huh.NewNote().
			Title("Control Plane Exposure").
			Description(
				"How should tenant-cluster Kubernetes API servers be reached?\n\n" +
					"LoadBalancer (recommended for starters):\n" +
					"  1 LoadBalancer IP per tenant cluster. Simplest setup.\n" +
					"  Burns 1 IP from the MetalLB pool per tenant.\n\n" +
					"Ingress:\n" +
					"  Shared IP via an ingress controller with TLS passthrough.\n" +
					"  IP-efficient. Requires steward-tcp-proxy and a wildcard DNS\n" +
					"  record pointing at the ingress.\n\n" +
					"Gateway:\n" +
					"  Shared IP via Gateway API TLSRoute. Modern alternative to\n" +
					"  Ingress. Requires a configured Gateway resource.\n\n" +
					"This setting is changeable later by editing the ButlerConfig CR."),

		huh.NewSelect[string]().
			Title("Exposure Mode").
			Options(
				huh.NewOption("LoadBalancer (1 IP per tenant)", "LoadBalancer"),
				huh.NewOption("Ingress (shared IP via SNI)", "Ingress"),
				huh.NewOption("Gateway (shared IP via Gateway API)", "Gateway"),
			).
			Value(&s.ExposureMode),
	)
}

// exposureIngressStep collects the extra fields required when mode is
// Ingress. Hidden for LoadBalancer/Gateway.
//
// Fields intentionally have no hard validators: huh's form.go blocks
// prevGroup navigation whenever the current group has any validation
// error, so a failed Next on an empty required field would trap the
// operator inside the group with no way to go back and pick a different
// exposure mode. Empty values are caught later by the orchestrator when
// buildConfig hands the Config off for bootstrap.
func exposureIngressStep(s *wizardState) *huh.Group {
	return huh.NewGroup(
		huh.NewInput().
			Title("Wildcard Hostname").
			Description("Wildcard domain for tenant API servers. A wildcard DNS record\nmust point this domain at the ingress load balancer IP.").
			Placeholder("*.k8s.example.com").
			Value(&s.ExposureHostname),

		huh.NewInput().
			Title("Ingress Class Name").
			Description("Which ingress controller class handles TLS passthrough").
			Placeholder("traefik").
			Value(&s.ExposureIngressClass),

		huh.NewSelect[string]().
			Title("Ingress Controller Type").
			Description("Controls the TLS passthrough annotation/config style.").
			Options(
				huh.NewOption("Traefik", "traefik"),
				huh.NewOption("HAProxy", "haproxy"),
				huh.NewOption("NGINX", "nginx"),
				huh.NewOption("Generic", "generic"),
			).
			Value(&s.ExposureControllerType),
	).WithHideFunc(func() bool {
		return s.ExposureMode != "Ingress"
	})
}

// exposureGatewayStep collects Gateway-specific fields. Hidden for
// LoadBalancer/Ingress. No hard validators — see exposureIngressStep.
func exposureGatewayStep(s *wizardState) *huh.Group {
	return huh.NewGroup(
		huh.NewInput().
			Title("Wildcard Hostname").
			Description("Wildcard domain for tenant API servers, pointed at the Gateway IP.").
			Placeholder("*.k8s.example.com").
			Value(&s.ExposureHostname),

		huh.NewInput().
			Title("Gateway Reference").
			Description("namespace/name of an existing Gateway resource that will\nterminate TLS passthrough for tenant API traffic.").
			Placeholder("steward-system/steward-gateway").
			Value(&s.ExposureGatewayRef),
	).WithHideFunc(func() bool {
		return s.ExposureMode != "Gateway"
	})
}

// consoleStep collects Butler Console configuration: admin password
// and whether to expose via ingress.
func consoleStep(s *wizardState) *huh.Group {
	return huh.NewGroup(
		huh.NewNote().
			Title("Butler Console").
			Description(
				"Butler Console is the web UI for managing tenant clusters, teams,\n" +
					"and platform settings. It's installed automatically.\n\n" +
					"By default the console is reachable only via port-forward:\n" +
					"  kubectl port-forward -n butler-system svc/butler-console-frontend 3000:80\n\n" +
					"Enable ingress to expose it at a hostname instead. You'll need\n" +
					"a DNS record pointing at the ingress IP."),

		huh.NewInput().
			Title("Admin Password").
			Description("Initial admin password for the console. Change it after first login.").
			EchoMode(huh.EchoModePassword).
			Value(&s.ConsoleAdminPassword),

		huh.NewConfirm().
			Title("Expose Butler Console via ingress?").
			Description("Leave disabled to use kubectl port-forward.").
			Value(&s.ConsoleIngressEnabled),
	)
}

// consoleIngressStep collects ingress details when the operator has opted
// to expose the console. Hidden if ingress is disabled. No hard validators
// — see exposureIngressStep for rationale.
func consoleIngressStep(s *wizardState) *huh.Group {
	return huh.NewGroup(
		huh.NewInput().
			Title("Console Hostname").
			Description("DNS name pointing at the ingress IP.").
			Placeholder("butler.example.com").
			Value(&s.ConsoleHost),

		huh.NewInput().
			Title("Ingress Class Name").
			Placeholder("traefik").
			Value(&s.ConsoleClass),

		huh.NewConfirm().
			Title("Enable TLS termination?").
			Description("Terminate TLS at the ingress. Requires cert-manager or a manually-created TLS secret.").
			Value(&s.ConsoleTLS),
	).WithHideFunc(func() bool {
		return !s.ConsoleIngressEnabled
	})
}

// reviewStep shows a summary of the collected config and asks for
// confirmation. The summary is rendered via DescriptionFunc so huh
// re-evaluates it whenever any wizardState field changes — a static
// Description would be captured at form construction time, before the
// user has filled in namespace, cluster name, sizing, etc.
//
// The Launch confirm runs a soft validation pass via Confirm.Validate
// that checks for missing required fields based on the selected
// exposure and console modes. If something is missing, the confirm
// blocks with an inline error and the operator can press Esc/Shift+Tab
// to go back and fill it in.
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
			Value(confirmed).
			Validate(func(v bool) error {
				if !v {
					// Cancelling is always allowed; the wizard Run
					// loop will exit with a cancelled error.
					return nil
				}
				return validateConfig(s)
			}),
	)
}

// validateConfig performs a final sanity check over the wizardState
// just before the bootstrap is launched. It catches conditional
// required fields that the per-field validators can't block (see
// exposureIngressStep for the reason the per-field validators were
// removed) and returns the first problem as a single-line error so
// the Confirm can display it inline.
func validateConfig(s *wizardState) error {
	if s.ClusterName == "" {
		return fmt.Errorf("cluster name is required")
	}
	if s.Provider == "" {
		return fmt.Errorf("provider is required")
	}

	// Provider-specific resource selection.
	switch s.Provider {
	case "harvester":
		if s.HarvNamespace == "" {
			return fmt.Errorf("Harvester namespace must be selected")
		}
		if s.HarvNetwork == "" {
			return fmt.Errorf("Harvester network must be selected")
		}
		if s.ImageSource != "factory" && s.HarvImage == "" {
			return fmt.Errorf("Harvester image must be selected (or choose 'Sync from Factory')")
		}
	case "nutanix":
		if s.NutClusterUUID == "" {
			return fmt.Errorf("Nutanix cluster must be selected")
		}
		if s.NutSubnetUUID == "" {
			return fmt.Errorf("Nutanix subnet must be selected")
		}
		if s.ImageSource != "factory" && s.NutImageUUID == "" {
			return fmt.Errorf("Nutanix image must be selected (or choose 'Sync from Factory')")
		}
	}

	// Exposure mode conditional fields.
	switch s.ExposureMode {
	case "Ingress":
		if s.ExposureHostname == "" {
			return fmt.Errorf("Ingress exposure mode requires a Wildcard Hostname (go back to the Exposure step)")
		}
		if s.ExposureIngressClass == "" {
			return fmt.Errorf("Ingress exposure mode requires an Ingress Class Name")
		}
	case "Gateway":
		if s.ExposureHostname == "" {
			return fmt.Errorf("Gateway exposure mode requires a Wildcard Hostname")
		}
		if s.ExposureGatewayRef == "" {
			return fmt.Errorf("Gateway exposure mode requires a Gateway Reference (namespace/name)")
		}
	}

	// Console ingress conditional fields.
	if s.ConsoleIngressEnabled {
		if s.ConsoleHost == "" {
			return fmt.Errorf("Console ingress requires a Console Hostname (go back to the Console step)")
		}
		if s.ConsoleClass == "" {
			return fmt.Errorf("Console ingress requires an Ingress Class Name")
		}
	}

	// IPAM conditional fields.
	if s.IPAMEnabled {
		if s.PoolCIDR == "" {
			return fmt.Errorf("IPAM requires a Pool CIDR (go back to the NetworkPool step)")
		}
		if s.TenantAllocStart == "" || s.TenantAllocEnd == "" {
			return fmt.Errorf("IPAM requires Tenant Allocation Start and End")
		}
		if s.ProviderGateway == "" {
			return fmt.Errorf("IPAM requires a Gateway IP (go back to the Provider Network step)")
		}
	}

	return nil
}

// buildSummary renders a human-readable summary of the collected wizard state
// for display on the review screen.
func buildSummary(s *wizardState) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Provider:       %s\n", s.Provider)
	fmt.Fprintf(&b, "Cluster Name:   %s\n", s.ClusterName)
	fmt.Fprintf(&b, "Topology:       %s\n", s.Topology)

	// Single-node forces 1 CP replica regardless of what the wizard
	// state field says — the CP Replicas input is hidden for single-node
	// so the underlying string still holds its default of "3". Render
	// the effective value to match what buildConfig will emit.
	cpReplicas := s.CPReplicas
	if s.Topology == "single-node" {
		cpReplicas = "1"
	}
	fmt.Fprintf(&b, "Control Plane:  %s × (%s vCPU, %s MB, %s GB)\n",
		cpReplicas, s.CPCPU, s.CPMemoryMB, s.CPDiskGB)
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

	// IPAM / NetworkPool
	if s.IPAMEnabled {
		fmt.Fprintf(&b, "IPAM:           enabled\n")
		fmt.Fprintf(&b, "Pool CIDR:      %s\n", s.PoolCIDR)
		fmt.Fprintf(&b, "Tenant Range:   %s - %s\n", s.TenantAllocStart, s.TenantAllocEnd)
		fmt.Fprintf(&b, "Gateway:        %s\n", s.ProviderGateway)
		if s.ProviderDNSServers != "" {
			fmt.Fprintf(&b, "DNS:            %s\n", s.ProviderDNSServers)
		}
	} else {
		fmt.Fprintf(&b, "IPAM:           disabled (set up NetworkPool manually post-bootstrap)\n")
	}

	// Control plane exposure
	fmt.Fprintf(&b, "Exposure:       %s", s.ExposureMode)
	switch s.ExposureMode {
	case "Ingress":
		fmt.Fprintf(&b, " (%s via %s/%s)", s.ExposureHostname, s.ExposureIngressClass, s.ExposureControllerType)
	case "Gateway":
		fmt.Fprintf(&b, " (%s via %s)", s.ExposureHostname, s.ExposureGatewayRef)
	}
	b.WriteString("\n")

	// Butler Console
	if s.ConsoleIngressEnabled {
		tlsHint := ""
		if s.ConsoleTLS {
			tlsHint = " + TLS"
		}
		fmt.Fprintf(&b, "Console:        ingress %s via %s%s\n", s.ConsoleHost, s.ConsoleClass, tlsHint)
	} else {
		fmt.Fprintf(&b, "Console:        port-forward only\n")
	}

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

	// Disclosure: what gets installed with defaults the wizard does not
	// currently prompt for. Operators can override these by editing the
	// bootstrap YAML directly or via butleradm config after bootstrap.
	b.WriteString("\n")
	b.WriteString("Defaults that will be installed (edit bootstrap YAML to override):\n")
	b.WriteString("  CNI:      cilium         Storage:        longhorn\n")
	b.WriteString("  LB:       metallb        GitOps:         flux\n")
	b.WriteString("  CAPI:     enabled        Butler Console: enabled\n")

	return b.String()
}
