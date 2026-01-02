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

// Package main is the entry point for butleradm - the Butler platform administration CLI.
//
// butleradm is designed for Platform Operators who manage the Butler platform itself.
// It handles management cluster lifecycle: bootstrap, upgrade, backup, restore, and status.

// Package main is the entry point for butleradm - the Butler platform administration CLI.
//
// butleradm is designed for Platform Operators who manage the Butler platform itself.
// It handles management cluster lifecycle: bootstrap, upgrade, backup, restore, and status.
package main

import (
	"os"

	"github.com/butlerdotdev/butler/internal/adm/cmd"
	"github.com/butlerdotdev/butler/internal/common/log"
)

func main() {
	logger := log.New("butleradm")

	if err := cmd.Execute(logger); err != nil {
		logger.Error("command failed", "error", err)
		os.Exit(1)
	}
}
