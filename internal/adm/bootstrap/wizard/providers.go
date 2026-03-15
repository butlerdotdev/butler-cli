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

// harvesterCredGroup builds the Harvester credential fields.
// Hidden unless provider == "harvester".
func harvesterCredGroup(s *wizardState) *huh.Group {
	return huh.NewGroup(
		huh.NewNote().
			Title("Harvester Provider").
			Description("Connect to your Harvester HCI cluster."),

		huh.NewInput().
			Title("Kubeconfig Path").
			Description("Path to the Harvester cluster kubeconfig file").
			Value(&s.harvKubeconfig).
			Validate(validateNotEmpty),
	).WithHideFunc(func() bool {
		return s.provider != "harvester"
	})
}

// nutanixCredGroup builds the Nutanix credential fields.
// Hidden unless provider == "nutanix".
func nutanixCredGroup(s *wizardState) *huh.Group {
	if s.nutPort == "" {
		s.nutPort = "9440"
	}
	return huh.NewGroup(
		huh.NewNote().
			Title("Nutanix Provider").
			Description("Connect to Prism Central."),

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
	).WithHideFunc(func() bool {
		return s.provider != "nutanix"
	})
}

// gcpCredGroup builds the GCP credential fields.
// Hidden unless provider == "gcp".
func gcpCredGroup(s *wizardState) *huh.Group {
	return huh.NewGroup(
		huh.NewNote().
			Title("GCP Provider").
			Description("Connect to Google Cloud Platform."),

		huh.NewInput().
			Title("Service Account Key Path").
			Description("Path to the GCP service account key JSON file").
			Value(&s.gcpKeyPath).
			Validate(validateNotEmpty),

		huh.NewInput().
			Title("Project ID").
			Value(&s.gcpProjectID).
			Validate(validateNotEmpty),
	).WithHideFunc(func() bool {
		return s.provider != "gcp"
	})
}

// awsCredGroup builds the AWS credential fields.
// Hidden unless provider == "aws".
func awsCredGroup(s *wizardState) *huh.Group {
	return huh.NewGroup(
		huh.NewNote().
			Title("AWS Provider").
			Description("Connect to Amazon Web Services."),

		huh.NewInput().
			Title("Access Key ID").
			Value(&s.awsAccessKey).
			Validate(validateNotEmpty),

		huh.NewInput().
			Title("Secret Access Key").
			Value(&s.awsSecretKey).
			EchoMode(huh.EchoModePassword).
			Validate(validateNotEmpty),
	).WithHideFunc(func() bool {
		return s.provider != "aws"
	})
}

// azureCredGroup builds the Azure credential fields.
// Hidden unless provider == "azure".
func azureCredGroup(s *wizardState) *huh.Group {
	return huh.NewGroup(
		huh.NewNote().
			Title("Azure Provider").
			Description("Connect to Microsoft Azure."),

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
	).WithHideFunc(func() bool {
		return s.provider != "azure"
	})
}
