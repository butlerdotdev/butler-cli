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
	"fmt"
	"net"
	"regexp"
	"strconv"
	"strings"
)

var clusterNameRe = regexp.MustCompile(`^[a-z][a-z0-9-]*[a-z0-9]$`)

// validateClusterName checks for a valid DNS label.
func validateClusterName(s string) error {
	if s == "" {
		return fmt.Errorf("cluster name is required")
	}
	if len(s) > 63 {
		return fmt.Errorf("cluster name must be at most 63 characters")
	}
	if !clusterNameRe.MatchString(s) {
		return fmt.Errorf("must be lowercase alphanumeric with hyphens, starting with a letter")
	}
	return nil
}

func validateCIDR(s string) error {
	if s == "" {
		return fmt.Errorf("CIDR is required")
	}
	_, _, err := net.ParseCIDR(s)
	if err != nil {
		return fmt.Errorf("invalid CIDR: %s", err)
	}
	return nil
}

func validateIP(s string) error {
	if s == "" {
		return fmt.Errorf("IP address is required")
	}
	if net.ParseIP(s) == nil {
		return fmt.Errorf("invalid IP address: %s", s)
	}
	return nil
}

func validateNotEmpty(s string) error {
	if strings.TrimSpace(s) == "" {
		return fmt.Errorf("value is required")
	}
	return nil
}

func validateIntRange(min, max int) func(string) error {
	return func(s string) error {
		v, err := strconv.Atoi(s)
		if err != nil {
			return fmt.Errorf("must be a number")
		}
		if v < min || v > max {
			return fmt.Errorf("must be between %d and %d", min, max)
		}
		return nil
	}
}

func validatePort(s string) error {
	return validateIntRange(1, 65535)(s)
}

func validateCSVIPs(s string) error {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	for _, part := range strings.Split(s, ",") {
		ip := strings.TrimSpace(part)
		if ip == "" {
			continue
		}
		if net.ParseIP(ip) == nil {
			return fmt.Errorf("invalid IP address: %s", ip)
		}
	}
	return nil
}

func validateOptional(fn func(string) error) func(string) error {
	return func(s string) error {
		if strings.TrimSpace(s) == "" {
			return nil
		}
		return fn(s)
	}
}

// validateWorkerReplicas combines the standard 1-100 range check with a
// topology-aware floor: HA topology requires at least 3 workers because
// Longhorn's default numberOfReplicas=3 needs 3 schedulable nodes, and
// chart-backed addons like Steward (3-replica etcd) can't finish
// installing with fewer worker nodes available for volume placement.
func validateWorkerReplicas(state *wizardState) func(string) error {
	return func(s string) error {
		v, err := strconv.Atoi(s)
		if err != nil {
			return fmt.Errorf("must be a number")
		}
		if v < 1 || v > 100 {
			return fmt.Errorf("must be between 1 and 100")
		}
		if state.Topology == "ha" && v < 3 {
			return fmt.Errorf("HA topology requires at least 3 workers (Longhorn needs 3 schedulable nodes for default replica placement)")
		}
		return nil
	}
}

// validateWorkerDiskGB combines the standard 20-4096 range check with a
// topology-aware floor: HA topology requires at least 50GB worker disks
// because Longhorn reserves ~30%% of each disk for overhead, and the full
// Butler management cluster footprint (Steward 3x8GB etcd + Butler Console
// DB + monitoring PVCs, all with default 3-replica placement) needs
// enough headroom to schedule across the 3 workers. 25GB disks leave only
// ~16GB usable per node after the reservation, which is too tight for
// multi-volume Longhorn scheduling.
func validateWorkerDiskGB(state *wizardState) func(string) error {
	return func(s string) error {
		v, err := strconv.Atoi(s)
		if err != nil {
			return fmt.Errorf("must be a number")
		}
		if v < 20 || v > 4096 {
			return fmt.Errorf("must be between 20 and 4096")
		}
		if state.Topology == "ha" && v < 50 {
			return fmt.Errorf("HA topology requires at least 50 GB worker disks (Longhorn reserves ~30%% overhead, leaving too little for Steward etcd + Console DB replicas on smaller disks)")
		}
		return nil
	}
}
