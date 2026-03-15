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

// harvesterStep builds the Harvester provider credentials step.
func harvesterStep(s *wizardState) *huh.Group {
	return huh.NewGroup(
		huh.NewNote().
			Title("Harvester Provider").
			Description("Connect to your Harvester HCI cluster. These credentials are used\nto provision VMs for the management cluster."),

		huh.NewInput().
			Title("Harvester Kubeconfig Path").
			Description("Path to the Harvester cluster kubeconfig file").
			Value(&s.harvKubeconfig).
			Validate(validateNotEmpty),

		huh.NewInput().
			Title("VM Namespace").
			Description("Namespace for VMs in Harvester").
			Value(&s.harvNamespace).
			Validate(validateNotEmpty),

		huh.NewInput().
			Title("Network Name").
			Description("Harvester network (namespace/name format)").
			Value(&s.harvNetwork).
			Validate(validateNotEmpty),

		huh.NewInput().
			Title("Talos Image Name").
			Description("Talos image in Harvester (namespace/name format)").
			Value(&s.harvImage).
			Validate(validateNotEmpty),
	)
}

// nutanixStep builds the Nutanix provider credentials step.
func nutanixStep(s *wizardState) *huh.Group {
	if s.nutPort == "" {
		s.nutPort = "9440"
	}
	return huh.NewGroup(
		huh.NewNote().
			Title("Nutanix Provider").
			Description("Connect to Prism Central. These credentials are used to provision\nAHV VMs for the management cluster."),

		huh.NewInput().
			Title("Prism Central Endpoint").
			Description("URL of the Prism Central instance").
			Value(&s.nutEndpoint).
			Validate(validateNotEmpty),

		huh.NewInput().
			Title("API Port").
			Value(&s.nutPort).
			Validate(validatePort),

		huh.NewConfirm().
			Title("Allow Insecure TLS").
			Description("Skip TLS verification (self-signed certs)").
			Value(&s.nutInsecure),

		huh.NewInput().
			Title("Username").
			Value(&s.nutUsername).
			Validate(validateNotEmpty),

		huh.NewInput().
			Title("Password").
			Value(&s.nutPassword).
			EchoMode(huh.EchoModePassword).
			Validate(validateNotEmpty),

		huh.NewInput().
			Title("Cluster UUID").
			Description("Target Nutanix cluster").
			Value(&s.nutClusterUUID).
			Validate(validateNotEmpty),

		huh.NewInput().
			Title("Subnet UUID").
			Description("Network subnet for VMs").
			Value(&s.nutSubnetUUID).
			Validate(validateNotEmpty),

		huh.NewInput().
			Title("Image UUID").
			Description("Talos image in Prism Central").
			Value(&s.nutImageUUID).
			Validate(validateNotEmpty),
	)
}

// gcpStep builds the GCP provider credentials step.
func gcpStep(s *wizardState) *huh.Group {
	return huh.NewGroup(
		huh.NewNote().
			Title("GCP Provider").
			Description("Connect to Google Cloud Platform. These credentials are used to\nprovision Compute Engine VMs for the management cluster."),

		huh.NewInput().
			Title("Service Account Key Path").
			Description("Path to the GCP service account key JSON file").
			Value(&s.gcpKeyPath).
			Validate(validateNotEmpty),

		huh.NewInput().
			Title("Project ID").
			Value(&s.gcpProjectID).
			Validate(validateNotEmpty),

		huh.NewInput().
			Title("Region").
			Description("e.g., us-central1").
			Value(&s.gcpRegion).
			Validate(validateNotEmpty),

		huh.NewInput().
			Title("Zone").
			Description("e.g., us-central1-a (defaults to {region}-a)").
			Value(&s.gcpZone),

		huh.NewInput().
			Title("VPC Network").
			Value(&s.gcpNetwork).
			Validate(validateNotEmpty),

		huh.NewInput().
			Title("Subnetwork").
			Description("Optional").
			Value(&s.gcpSubnetwork),
	)
}

// awsStep builds the AWS provider credentials step.
func awsStep(s *wizardState) *huh.Group {
	return huh.NewGroup(
		huh.NewNote().
			Title("AWS Provider").
			Description("Connect to Amazon Web Services. These credentials are used to\nprovision EC2 instances for the management cluster."),

		huh.NewInput().
			Title("Access Key ID").
			Value(&s.awsAccessKey).
			Validate(validateNotEmpty),

		huh.NewInput().
			Title("Secret Access Key").
			Value(&s.awsSecretKey).
			EchoMode(huh.EchoModePassword).
			Validate(validateNotEmpty),

		huh.NewInput().
			Title("Region").
			Description("e.g., us-east-1").
			Value(&s.awsRegion).
			Validate(validateNotEmpty),

		huh.NewInput().
			Title("VPC ID").
			Description("Optional").
			Value(&s.awsVPCID),

		huh.NewInput().
			Title("Subnet ID").
			Description("Optional").
			Value(&s.awsSubnetID),

		huh.NewInput().
			Title("Security Group ID").
			Description("Optional").
			Value(&s.awsSecGroupID),
	)
}

// azureStep builds the Azure provider credentials step.
func azureStep(s *wizardState) *huh.Group {
	return huh.NewGroup(
		huh.NewNote().
			Title("Azure Provider").
			Description("Connect to Microsoft Azure. These credentials are used to\nprovision VMs for the management cluster."),

		huh.NewInput().
			Title("Client ID").
			Description("Service principal app ID").
			Value(&s.azClientID).
			Validate(validateNotEmpty),

		huh.NewInput().
			Title("Client Secret").
			Value(&s.azClientSecret).
			EchoMode(huh.EchoModePassword).
			Validate(validateNotEmpty),

		huh.NewInput().
			Title("Tenant ID").
			Value(&s.azTenantID).
			Validate(validateNotEmpty),

		huh.NewInput().
			Title("Subscription ID").
			Value(&s.azSubscriptionID).
			Validate(validateNotEmpty),

		huh.NewInput().
			Title("Resource Group").
			Value(&s.azResourceGroup).
			Validate(validateNotEmpty),

		huh.NewInput().
			Title("Location").
			Description("e.g., eastus").
			Value(&s.azLocation).
			Validate(validateNotEmpty),
	)
}
