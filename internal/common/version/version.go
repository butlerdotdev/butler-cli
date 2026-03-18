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

// Package version holds build-time version info injected via ldflags.
//
// GoReleaser injects these via:
//
//	-X github.com/butlerdotdev/butler/internal/common/version.Version={{.Version}}
//	-X github.com/butlerdotdev/butler/internal/common/version.Commit={{.Commit}}
//	-X github.com/butlerdotdev/butler/internal/common/version.Date={{.Date}}
package version

// Build-time variables set via ldflags. Defaults match the latest
// release so dev builds show a meaningful version.
var (
	Version = "v0.5.0"
	Commit  = "none"
	Date    = "unknown"
)

// Full returns a formatted version string like "v0.5.0 (abc1234)".
func Full() string {
	v := Version
	if Commit != "none" && len(Commit) >= 7 {
		v += " (" + Commit[:7] + ")"
	}
	return v
}
