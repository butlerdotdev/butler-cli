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

// Package login implements the butlerctl login command.
package login

import (
	"context"
	"fmt"
	"os/signal"
	"syscall"
	"time"

	"github.com/butlerdotdev/butler/internal/common/auth"
	"github.com/butlerdotdev/butler/internal/common/log"
	"github.com/butlerdotdev/butler/internal/common/output"
	"github.com/spf13/cobra"
)

// NewLoginCmd creates the login command.
func NewLoginCmd(logger *log.Logger) *cobra.Command {
	var (
		serverURL string
		noBrowser bool
	)

	cmd := &cobra.Command{
		Use:   "login",
		Short: "Authenticate with a Butler server",
		Long: `Authenticate with a Butler server using the device authorization flow.

The login command opens your browser to approve the CLI session. Once
approved, your credentials are stored locally at ~/.butler/credentials.json
and subsequent commands use the embedded kubeconfig automatically.

Examples:
  # Login to a Butler server
  butlerctl login --server https://butler.example.com

  # Login without opening a browser (copy the URL manually)
  butlerctl login --server https://butler.example.com --no-browser`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runLogin(cmd.Context(), logger, serverURL, noBrowser)
		},
	}

	cmd.Flags().StringVar(&serverURL, "server", "", "Butler server URL (required)")
	cmd.Flags().BoolVar(&noBrowser, "no-browser", false, "Do not open the browser automatically")

	_ = cmd.MarkFlagRequired("server")

	return cmd
}

func runLogin(ctx context.Context, logger *log.Logger, serverURL string, noBrowser bool) error {
	// Allow graceful cancellation with ctrl+c
	ctx, stop := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Initiate device flow
	logger.Info("requesting device code", "server", serverURL)

	dfr, err := auth.InitiateDeviceFlow(serverURL)
	if err != nil {
		return fmt.Errorf("initiating login: %w", err)
	}

	// Display the code and URL
	fmt.Println()
	fmt.Printf("  Your one-time code: %s\n", output.Bold(dfr.UserCode))
	fmt.Printf("  Open this URL:      %s\n", dfr.VerificationURI)
	fmt.Println()

	// Open browser unless suppressed
	if !noBrowser {
		if err := auth.OpenBrowser(dfr.VerificationURI); err != nil {
			logger.Debug("could not open browser", "error", err)
			fmt.Println("  Could not open browser automatically. Please open the URL above.")
		}
	}

	fmt.Print("  Waiting for approval...")

	// Poll with a simple dot animation
	pollCtx, pollCancel := context.WithTimeout(ctx, time.Duration(dfr.ExpiresIn)*time.Second)
	defer pollCancel()

	// Start dot ticker in a goroutine
	dotDone := make(chan struct{})
	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-dotDone:
				return
			case <-ticker.C:
				fmt.Print(".")
			}
		}
	}()

	tr, err := auth.PollForToken(pollCtx, serverURL, dfr.DeviceCode, dfr.Interval)
	close(dotDone)
	fmt.Println() // end the dots line

	if err != nil {
		return fmt.Errorf("waiting for approval: %w", err)
	}

	// Save credentials
	creds, err := auth.LoadCredentials()
	if err != nil {
		return fmt.Errorf("loading credential file: %w", err)
	}

	sc := &auth.ServerCredential{
		User:             tr.User,
		Kubeconfig:       tr.Kubeconfig,
		ExpiresAt:        tr.ExpiresAt,
		RefreshToken:     tr.RefreshToken,
		RefreshExpiresAt: tr.RefreshExpiresAt,
	}

	// Set active team to the first team if available
	if len(tr.User.Teams) > 0 {
		sc.ActiveTeam = tr.User.Teams[0].Name
	}

	creds.Servers[serverURL] = sc
	creds.ActiveServer = serverURL

	if err := creds.Save(); err != nil {
		return fmt.Errorf("saving credentials: %w", err)
	}

	// Print success summary
	fmt.Println()
	fmt.Printf("  Logged in as %s\n", output.Bold(tr.User.Name))
	fmt.Printf("  Email:  %s\n", tr.User.Email)

	if len(tr.User.Teams) > 0 {
		fmt.Println()
		fmt.Printf("  Teams:\n")
		for _, tm := range tr.User.Teams {
			marker := "  "
			if tm.Name == sc.ActiveTeam {
				marker = "* "
			}
			fmt.Printf("    %s%s (%s)\n", marker, tm.Name, tm.Role)
		}
		fmt.Println()
		fmt.Printf("  Active team: %s (namespace: team-%s)\n", output.Bold(sc.ActiveTeam), sc.ActiveTeam)
	}

	if tr.User.IsPlatformAdmin {
		fmt.Printf("  Platform admin: yes\n")
	}

	fmt.Println()
	fmt.Printf("  Credentials saved to %s\n", auth.CredentialPath())

	return nil
}
