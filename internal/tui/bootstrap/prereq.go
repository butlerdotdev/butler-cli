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

package bootstrap

import (
	"os/exec"
)

// Check is a single prerequisite validation result. Optional checks
// surface as warnings rather than blocking errors — a failed optional
// check doesn't prevent bootstrap but does hint at a missing tool the
// operator will probably want.
type Check struct {
	Name     string
	Passed   bool
	Optional bool
	Detail   string
}

// CheckAll runs all prerequisite checks for a local bootstrap. When
// localDev is true the kind CLI is promoted from optional to required
// because --local mode shells out to `kind load docker-image` during
// the image-load phase.
//
// The checks cover the binaries and daemons the orchestrator shells
// out to during Run:
//   - docker (required): creates the temporary KIND cluster, docker
//     exec for CA cert install and inotify tuning, docker build/load
//     in local mode
//   - kubectl (required): patches the KIND node for CA certs
//   - kind CLI: only required in --local dev mode, otherwise optional
//     but useful for debugging (`kind get clusters`, `kind export
//     kubeconfig`, etc.)
func CheckAll(localDev bool) []Check {
	checks := []Check{
		checkDocker(),
		checkKubectl(),
		checkKindCLI(localDev),
	}
	return checks
}

// AllPassed reports whether every required check passed. Optional
// checks that fail are ignored — the caller should surface them as
// warnings separately.
func AllPassed(checks []Check) bool {
	for _, c := range checks {
		if !c.Passed && !c.Optional {
			return false
		}
	}
	return true
}

func checkDocker() Check {
	c := Check{Name: "Docker"}
	if _, err := exec.LookPath("docker"); err != nil {
		c.Detail = "docker binary not found on PATH"
		return c
	}
	// `docker info` exits non-zero when the daemon is unreachable — this
	// is the canonical way to distinguish "docker installed but stopped"
	// from "docker missing entirely".
	if err := exec.Command("docker", "info").Run(); err != nil {
		c.Detail = "docker daemon not reachable (is Docker Desktop running?)"
		return c
	}
	c.Passed = true
	c.Detail = "docker daemon reachable"
	return c
}

func checkKubectl() Check {
	c := Check{Name: "kubectl"}
	if _, err := exec.LookPath("kubectl"); err != nil {
		c.Detail = "kubectl binary not found on PATH"
		return c
	}
	c.Passed = true
	c.Detail = "kubectl on PATH"
	return c
}

func checkKindCLI(required bool) Check {
	c := Check{Name: "kind CLI", Optional: !required}
	if _, err := exec.LookPath("kind"); err != nil {
		if required {
			c.Detail = "kind binary not found (required for --local dev mode image loading)"
		} else {
			c.Detail = "kind binary not found (optional — useful for debugging the bootstrap KIND cluster)"
		}
		return c
	}
	c.Passed = true
	c.Detail = "kind CLI on PATH"
	return c
}
