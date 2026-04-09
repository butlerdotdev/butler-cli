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

// Package logout implements the butleradm logout command.
package logout

import (
	"fmt"

	"github.com/butlerdotdev/butler/internal/common/auth"
	"github.com/butlerdotdev/butler/internal/common/log"
	"github.com/spf13/cobra"
)

// NewLogoutCmd creates the logout command for butleradm.
func NewLogoutCmd(logger *log.Logger) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "logout",
		Short: "Remove stored credentials for the active server",
		Long: `Remove the stored credentials for the active Butler server.

This deletes the access token, refresh token, and embedded kubeconfig
from ~/.butler/credentials.json. It does not invalidate the session
on the server side.

Examples:
  # Logout from the active server
  butleradm logout`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runLogout(logger)
		},
	}

	return cmd
}

func runLogout(logger *log.Logger) error {
	creds, err := auth.LoadCredentials()
	if err != nil {
		return fmt.Errorf("loading credential file: %w", err)
	}

	if creds.ActiveServer == "" {
		fmt.Println("No active server. Nothing to do.")
		return nil
	}

	server := creds.ActiveServer

	if _, ok := creds.Servers[server]; !ok {
		fmt.Printf("No credentials stored for %s. Nothing to do.\n", server)
		return nil
	}

	delete(creds.Servers, server)

	// If the active server was removed, clear it. Pick another server if one
	// remains, otherwise leave it empty.
	creds.ActiveServer = ""
	for s := range creds.Servers {
		creds.ActiveServer = s
		break
	}

	if err := creds.Save(); err != nil {
		return fmt.Errorf("saving credential file: %w", err)
	}

	fmt.Printf("Logged out from %s.\n", server)
	fmt.Printf("Credentials removed from %s\n", auth.CredentialPath())

	return nil
}
