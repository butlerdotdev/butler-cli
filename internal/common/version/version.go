// Copyright 2026 The Butler Authors.
// SPDX-License-Identifier: Apache-2.0

package version

// Set via ldflags at build time. See .goreleaser.yaml and Makefile.
var (
	Version = "dev"
	Commit  = "unknown"
	Date    = "unknown"
)
