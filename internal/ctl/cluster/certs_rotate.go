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

package cluster

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/butlerdotdev/butler/internal/common/log"
	"github.com/butlerdotdev/butler/internal/common/output"
	"github.com/butlerdotdev/butler/internal/common/serverhttp"
	"github.com/spf13/cobra"
)

const (
	rotateTypeAll          = "all"
	rotateTypeKubeconfigs  = "kubeconfigs"
	rotateTypeCA           = "ca"
	rotationTimeoutMinutes = 5
	rotationPollInterval   = 5 * time.Second

	exitRotationFailed  = 2
	exitRotationTimeout = 3
)

// rotationEvent mirrors butler-server's internal/certificates.RotationEvent
// JSON shape. We do not import butler-server types; the contract is the
// HTTP wire format documented in ADR-016 amendment.
type rotationEvent struct {
	Type            string     `json:"type"`
	InitiatedBy     string     `json:"initiatedBy,omitempty"`
	InitiatedAt     time.Time  `json:"initiatedAt"`
	CompletedAt     *time.Time `json:"completedAt,omitempty"`
	Status          string     `json:"status"`
	AffectedSecrets []string   `json:"affectedSecrets,omitempty"`
	Message         string     `json:"message,omitempty"`
}

type rotateRequest struct {
	Type        string `json:"type"`
	Acknowledge bool   `json:"acknowledge"`
}

type rotateOptions struct {
	namespace          string
	rotationType       string
	yes                bool
	noWait             bool
	caAcknowledge      bool
	clusterNameConfirm string
	outputFormat       string
}

// newCertsRotateCmd creates the `cluster certs rotate` subcommand.
func newCertsRotateCmd(logger *log.Logger) *cobra.Command {
	opts := &rotateOptions{}

	cmd := &cobra.Command{
		Use:   "rotate NAME",
		Short: "Rotate certificates for a tenant cluster",
		Long: `Rotate certificates on a tenant cluster's hosted control plane.

Three rotation scopes:
  kubeconfigs  Regenerate kubeconfig client certificates only. Existing
               downloaded kubeconfigs become invalid.
  all          Regenerate all certificates except the root CA. The control
               plane apiserver restarts; brief operational blip.
  ca           Regenerate the root CA and all dependent certificates.
               Disruptive: worker nodes go NotReady until they receive the
               new CA trust bundle. Requires admin role on the cluster's
               team. Requires --ca-acknowledge. When run non-interactively
               (--yes) requires --cluster-name-confirm matching the
               cluster name as a typed gate.

Exit codes:
  0  rotation completed successfully (or --no-wait)
  1  client-side error, validation failure, or unexpected server error
  2  rotation reported a terminal failed status
  3  rotation did not reach a terminal state within 5 minutes

Examples:
  # Interactive kubeconfig rotation (prompts to confirm)
  butlerctl cluster certs rotate my-cluster --type=kubeconfigs

  # Non-interactive rotation in CI
  butlerctl cluster certs rotate my-cluster --type=all --yes

  # CA rotation (interactive; prompts to type the cluster name)
  butlerctl cluster certs rotate my-cluster --type=ca --ca-acknowledge

  # CA rotation in CI (typed-name gate required)
  butlerctl cluster certs rotate my-cluster --type=ca \
      --ca-acknowledge --yes --cluster-name-confirm=my-cluster

  # Initiate and return immediately (no polling)
  butlerctl cluster certs rotate my-cluster --type=kubeconfigs --yes --no-wait`,
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeClusterNames,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCertsRotate(cmd.Context(), logger, args[0], opts)
		},
	}

	cmd.Flags().StringVarP(&opts.namespace, "namespace", "n", DefaultTenantNamespace, "namespace of the TenantCluster")
	cmd.Flags().StringVar(&opts.rotationType, "type", "", "rotation scope: kubeconfigs, all, or ca (required)")
	_ = cmd.MarkFlagRequired("type")
	cmd.Flags().BoolVar(&opts.yes, "yes", false, "skip the interactive confirmation prompt")
	cmd.Flags().BoolVar(&opts.caAcknowledge, "ca-acknowledge", false, "acknowledge CA rotation impact (required for --type=ca)")
	cmd.Flags().StringVar(&opts.clusterNameConfirm, "cluster-name-confirm", "", "type the cluster name to confirm CA rotation with --yes")
	cmd.Flags().BoolVar(&opts.noWait, "no-wait", false, "return after initiating rotation; do not poll for terminal state")
	cmd.Flags().StringVarP(&opts.outputFormat, "output", "o", "", "output format (json, yaml)")

	return cmd
}

func runCertsRotate(ctx context.Context, _ *log.Logger, name string, opts *rotateOptions) error {
	if err := validateRotateOptions(name, opts); err != nil {
		return err
	}

	if !opts.yes {
		if err := confirmRotation(name, opts, os.Stdin, os.Stdout); err != nil {
			return err
		}
	}

	sh, err := serverhttp.New()
	if err != nil {
		return err
	}

	initPath := fmt.Sprintf("/api/clusters/%s/%s/certificates/rotate", opts.namespace, name)
	var initial rotationEvent
	postErr := sh.Post(ctx, initPath, rotateRequest{Type: opts.rotationType, Acknowledge: opts.caAcknowledge}, &initial)
	if postErr != nil {
		return translateServerError(postErr)
	}

	if err := printEvent(&initial, opts.outputFormat, "initiated"); err != nil {
		return err
	}

	if opts.noWait {
		return nil
	}

	statusPath := fmt.Sprintf("/api/clusters/%s/%s/certificates/rotation-status", opts.namespace, name)
	return pollUntilDone(ctx, sh, statusPath, opts.outputFormat)
}

// validateRotateOptions enforces client-side gates before any network call.
// The CA tier gate (--ca-acknowledge plus typed cluster name) mirrors the
// console's RotationModals.tsx confirmation flow and prevents a single
// erroneous flag in CI from rotating the CA.
func validateRotateOptions(name string, opts *rotateOptions) error {
	switch opts.rotationType {
	case rotateTypeAll, rotateTypeKubeconfigs:
		return nil
	case rotateTypeCA:
		if !opts.caAcknowledge {
			return fmt.Errorf("CA rotation requires --ca-acknowledge (confirms understanding of cluster downtime and manual worker recovery)")
		}
		if opts.yes && opts.clusterNameConfirm != name {
			return fmt.Errorf("CA rotation with --yes requires --cluster-name-confirm=%s (typed-name gate; matches console behavior)", name)
		}
		return nil
	default:
		return fmt.Errorf("--type must be one of: kubeconfigs, all, ca (got %q)", opts.rotationType)
	}
}

// confirmRotation runs the interactive prompt for the non-yes path. CA gets a
// typed-cluster-name gate; other types get a simple yes/no prompt with an
// impact note.
func confirmRotation(name string, opts *rotateOptions, in io.Reader, out io.Writer) error {
	reader := bufio.NewReader(in)

	if opts.rotationType == rotateTypeCA {
		fmt.Fprintln(out)
		fmt.Fprintln(out, "CA rotation is DISRUPTIVE.")
		fmt.Fprintln(out, "The cluster apiserver will restart and worker nodes will go NotReady")
		fmt.Fprintln(out, "until they receive the new CA trust bundle. Recovery requires action")
		fmt.Fprintln(out, "on each worker:")
		fmt.Fprintln(out)
		fmt.Fprintln(out, "  Talos workers:    talosctl -n <worker-ip> reboot")
		fmt.Fprintln(out, "  Kubeadm workers:  copy new CA to /etc/kubernetes/pki/ca.crt and restart kubelet")
		fmt.Fprintln(out)
		fmt.Fprintf(out, "Type the cluster name (%q) to confirm: ", name)
		line, err := reader.ReadString('\n')
		if err != nil {
			return fmt.Errorf("reading confirmation: %w", err)
		}
		if strings.TrimSpace(line) != name {
			return fmt.Errorf("confirmation did not match cluster name; rotation cancelled")
		}
		return nil
	}

	fmt.Fprintln(out)
	fmt.Fprintf(out, "Rotate %s certificates on cluster %q?\n", opts.rotationType, name)
	if opts.rotationType == rotateTypeKubeconfigs {
		fmt.Fprintln(out, "Existing downloaded kubeconfigs will become invalid and need to be re-downloaded.")
	} else {
		fmt.Fprintln(out, "The control plane apiserver will restart briefly.")
	}
	fmt.Fprint(out, "Proceed? [y/N]: ")
	line, err := reader.ReadString('\n')
	if err != nil {
		return fmt.Errorf("reading confirmation: %w", err)
	}
	ans := strings.ToLower(strings.TrimSpace(line))
	if ans != "y" && ans != "yes" {
		return fmt.Errorf("rotation cancelled by operator")
	}
	return nil
}

// translateServerError surfaces butler-server responses with a typed
// mapping so the operator sees actionable text rather than raw status
// codes. Pattern-match on the serverhttp.ServerError accessors; never
// string-match the message.
func translateServerError(err error) error {
	if errors.Is(err, serverhttp.ErrSessionExpired) {
		fmt.Fprintln(os.Stderr, "Butler session expired. Run 'butlerctl login' to re-authenticate.")
		return err
	}
	var se *serverhttp.ServerError
	if errors.As(err, &se) {
		switch {
		case se.IsConflict():
			return fmt.Errorf("rotation already in progress on this cluster (%s)", se.Message)
		case se.IsForbidden():
			return fmt.Errorf("forbidden: %s", se.Message)
		case se.IsNotFound():
			return fmt.Errorf("not found: %s", se.Message)
		case se.IsBadRequest():
			return fmt.Errorf("invalid rotation request: %s", se.Message)
		}
	}
	return err
}

// pollUntilDone polls /rotation-status every 5 seconds until the rotation
// reaches a terminal status (completed, failed) or the 5-minute timeout
// passes. Exits the process with code 2 on failed or 3 on timeout; returns
// nil on completed (cobra then exits 0). Context cancellation surfaces as
// the standard error path.
func pollUntilDone(ctx context.Context, sh *serverhttp.Client, path, outputFormat string) error {
	fmt.Fprintln(os.Stderr, "Waiting for rotation (poll every 5s, timeout 5m). Ctrl+C cancels the wait; rotation continues server-side.")

	deadline := time.Now().Add(rotationTimeoutMinutes * time.Minute)
	ticker := time.NewTicker(rotationPollInterval)
	defer ticker.Stop()

	for {
		var event rotationEvent
		if err := sh.Get(ctx, path, &event); err != nil {
			return translateServerError(err)
		}

		switch event.Status {
		case "completed":
			if err := printEvent(&event, outputFormat, "completed"); err != nil {
				return err
			}
			return nil
		case "failed":
			_ = printEvent(&event, outputFormat, "failed")
			fmt.Fprintf(os.Stderr, "\nRotation failed: %s\n", event.Message)
			os.Exit(exitRotationFailed)
		}

		// in_progress, unknown, empty: keep polling unless deadline passed.
		if time.Now().After(deadline) {
			_ = printEvent(&event, outputFormat, "timeout")
			fmt.Fprintf(os.Stderr, "\nRotation did not reach a terminal state within %d minutes. The server-side rotation continues; query rotation-status again later.\n", rotationTimeoutMinutes)
			os.Exit(exitRotationTimeout)
		}

		select {
		case <-ctx.Done():
			fmt.Fprintln(os.Stderr, "\nWait cancelled. Rotation continues server-side.")
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

// printEvent emits the rotation event in the requested format. The stage
// label is human-readable framing ("initiated", "completed", etc.); for
// json/yaml output the label is omitted so machines see the raw event.
func printEvent(event *rotationEvent, format, stage string) error {
	if format == "json" {
		return output.PrintJSON(os.Stdout, event)
	}
	if format == "yaml" {
		return output.PrintYAML(os.Stdout, event)
	}
	fmt.Printf("Rotation %s\n", stage)
	fmt.Printf("  Type:         %s\n", event.Type)
	fmt.Printf("  Status:       %s\n", event.Status)
	if !event.InitiatedAt.IsZero() {
		fmt.Printf("  Initiated:    %s\n", event.InitiatedAt.Format(time.RFC3339))
	}
	if event.InitiatedBy != "" {
		fmt.Printf("  Initiated by: %s\n", event.InitiatedBy)
	}
	if event.CompletedAt != nil && !event.CompletedAt.IsZero() {
		fmt.Printf("  Completed:    %s\n", event.CompletedAt.Format(time.RFC3339))
	}
	if len(event.AffectedSecrets) > 0 {
		fmt.Printf("  Affected secrets (%d):\n", len(event.AffectedSecrets))
		for _, s := range event.AffectedSecrets {
			fmt.Printf("    - %s\n", s)
		}
	}
	if event.Message != "" {
		fmt.Printf("  Message:      %s\n", event.Message)
	}
	return nil
}
