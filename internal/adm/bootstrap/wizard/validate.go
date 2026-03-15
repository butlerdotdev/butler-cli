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

// validateClusterName checks that a cluster name is a valid DNS label.
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

// validateCIDR validates an IP CIDR notation string.
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

// validateIP validates a single IP address.
func validateIP(s string) error {
	if s == "" {
		return nil // Optional fields
	}
	if net.ParseIP(s) == nil {
		return fmt.Errorf("invalid IP address: %s", s)
	}
	return nil
}

// validateNotEmpty checks that a string is not empty.
func validateNotEmpty(s string) error {
	if strings.TrimSpace(s) == "" {
		return fmt.Errorf("value is required")
	}
	return nil
}

// validateIntRange validates a string is a valid integer within a range.
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

// validatePort validates a string is a valid port number.
func validatePort(s string) error {
	return validateIntRange(1, 65535)(s)
}

// validateOptional wraps a validator to allow empty values.
func validateOptional(fn func(string) error) func(string) error {
	return func(s string) error {
		if strings.TrimSpace(s) == "" {
			return nil
		}
		return fn(s)
	}
}
