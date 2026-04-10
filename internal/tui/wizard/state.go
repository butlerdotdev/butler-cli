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
	"github.com/butlerdotdev/butler/internal/tui/wizard/discovery"
)

// wizardState holds all user input collected across wizard steps.
// Fields are intentionally restricted to those that map directly to
// orchestrator.Config on main. New config fields should be added here
// when the wizard needs to surface them.
type wizardState struct {
	// Provider and image source
	provider    string // "harvester" or "nutanix"
	imageSource string // "factory" or "existing"

	// Cluster identity and topology
	clusterName string
	topology    string // "ha" or "single-node"

	// Control plane sizing
	cpReplicas string
	cpCPU      string
	cpMemoryMB string
	cpDiskGB   string

	// Worker sizing (unused for single-node)
	workerReplicas string
	workerCPU      string
	workerMemoryMB string
	workerDiskGB   string

	// Cluster networking
	podCIDR     string
	serviceCIDR string
	vip         string

	// Management cluster LB pool (MetalLB)
	lbStart string
	lbEnd   string

	// Talos image (populated from factory or existing)
	talosVersion   string
	talosSchematic string

	// Harvester provider inputs
	harvKubeconfig string
	harvNamespace  string
	harvNetwork    string
	harvImage      string

	// Nutanix provider inputs
	nutEndpoint    string
	nutPort        string
	nutInsecure    bool
	nutUsername    string
	nutPassword    string
	nutClusterUUID string
	nutSubnetUUID  string
	nutImageUUID   string
}

// newWizardState returns a state seeded with sensible defaults so the
// wizard fields are pre-populated with reasonable starting values.
func newWizardState() *wizardState {
	return &wizardState{
		topology:       "ha",
		cpReplicas:     "3",
		cpCPU:          "4",
		cpMemoryMB:     "8192",
		cpDiskGB:       "50",
		workerReplicas: "3",
		workerCPU:      "4",
		workerMemoryMB: "16384",
		workerDiskGB:   "100",
		podCIDR:        "10.244.0.0/16",
		serviceCIDR:    "10.96.0.0/12",
		imageSource:    "factory",
		talosVersion:   "v1.12.2",
		talosSchematic: discovery.DefaultTalosSchematic,
		nutPort:        "9440",
	}
}

// stateCredentials adapts wizardState to discovery.CredentialProvider so
// the discovery layer can read provider-specific credentials uniformly.
type stateCredentials struct {
	s *wizardState
}

func (c *stateCredentials) Get(key string) string {
	switch c.s.provider {
	case "harvester":
		if key == "kubeconfig" {
			return c.s.harvKubeconfig
		}
	case "nutanix":
		switch key {
		case "endpoint":
			return c.s.nutEndpoint
		case "port":
			return c.s.nutPort
		case "username":
			return c.s.nutUsername
		case "password":
			return c.s.nutPassword
		}
	}
	return ""
}

func (c *stateCredentials) GetBool(key string) bool {
	if c.s.provider == "nutanix" && key == "insecure" {
		return c.s.nutInsecure
	}
	return false
}
