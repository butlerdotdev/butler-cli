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
	"context"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"io"
	"math"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/butlerdotdev/butler/internal/common/client"
	"github.com/butlerdotdev/butler/internal/common/log"
	"github.com/butlerdotdev/butler/internal/common/output"
	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// certInfo holds parsed certificate information for display.
type certInfo struct {
	SecretName string    `json:"secretName"`
	Subject    string    `json:"subject"`
	Issuer     string    `json:"issuer"`
	Expires    time.Time `json:"expires"`
	DaysLeft   int       `json:"daysLeft"`
	Status     string    `json:"status"`
}

type certsOptions struct {
	namespace      string
	outputFormat   string
	kubeconfigPath string
	kubeContext    string
}

// newCertsCmd creates the cluster certs command.
func newCertsCmd(logger *log.Logger) *cobra.Command {
	opts := &certsOptions{}

	cmd := &cobra.Command{
		Use:   "certs NAME",
		Short: "Show certificate status for a tenant cluster",
		Long: `Display TLS certificate details for a tenant cluster's control plane.

Lists all certificates from Secrets in the cluster's Steward
TenantControlPlane namespace, showing subject, issuer, expiry,
and health status.

Health status:
  Healthy   More than 30 days until expiry
  Warning   7-30 days until expiry
  Critical  Less than 7 days until expiry
  Expired   Certificate has expired

Examples:
  # Show certificate status
  butlerctl cluster certs my-cluster

  # Show certs for a cluster in a specific namespace
  butlerctl cluster certs my-cluster -n team-payments

  # Output as JSON
  butlerctl cluster certs my-cluster -o json`,
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeClusterNames,
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.kubeContext, _ = cmd.Flags().GetString("context")
			return runCerts(cmd.Context(), logger, args[0], opts)
		},
	}

	cmd.Flags().StringVarP(&opts.namespace, "namespace", "n", DefaultTenantNamespace, "namespace of the TenantCluster")
	cmd.Flags().StringVarP(&opts.outputFormat, "output", "o", "", "output format (json, yaml)")
	cmd.Flags().StringVar(&opts.kubeconfigPath, "kubeconfig", "", "path to management cluster kubeconfig")

	return cmd
}

func runCerts(ctx context.Context, logger *log.Logger, clusterName string, opts *certsOptions) error {
	// Connect to management cluster
	c, err := client.New(opts.kubeconfigPath, opts.kubeContext)
	if err != nil {
		return fmt.Errorf("connecting to management cluster: %w", err)
	}

	// Get the TenantCluster to find the TCP namespace
	tc, err := c.GetTenantCluster(ctx, opts.namespace, clusterName)
	if err != nil {
		return fmt.Errorf("getting TenantCluster %s/%s: %w", opts.namespace, clusterName, err)
	}

	// Read the tenant namespace from status
	tenantNS := client.GetNestedString(tc.Object, "status", "tenantNamespace")
	if tenantNS == "" {
		return fmt.Errorf("TenantCluster %s does not have a tenant namespace yet (phase: %s)",
			clusterName, client.GetNestedString(tc.Object, "status", "phase"))
	}

	// List all secrets in the TCP namespace
	secrets, err := c.Clientset.CoreV1().Secrets(tenantNS).List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("listing secrets in namespace %s: %w", tenantNS, err)
	}

	// Parse certificates from secrets
	now := time.Now()
	var certs []certInfo

	for i := range secrets.Items {
		secret := &secrets.Items[i]
		certKeys := findCertDataKeys(secret.Data)
		for _, key := range certKeys {
			data := secret.Data[key]
			parsed := parseCertificates(data)
			for _, cert := range parsed {
				daysLeft := int(math.Floor(time.Until(cert.NotAfter).Hours() / 24))
				certs = append(certs, certInfo{
					SecretName: secret.Name,
					Subject:    cert.Subject.CommonName,
					Issuer:     cert.Issuer.CommonName,
					Expires:    cert.NotAfter,
					DaysLeft:   daysLeft,
					Status:     certHealthStatus(now, cert.NotAfter),
				})
			}
		}
	}

	if len(certs) == 0 {
		fmt.Printf("No certificates found in namespace %s for cluster %s.\n", tenantNS, clusterName)
		return nil
	}

	// Sort by days left ascending so critical certs appear first
	sort.Slice(certs, func(i, j int) bool {
		return certs[i].DaysLeft < certs[j].DaysLeft
	})

	// Handle structured output
	if opts.outputFormat == "json" {
		return output.PrintJSON(os.Stdout, certs)
	}
	if opts.outputFormat == "yaml" {
		return output.PrintYAML(os.Stdout, certs)
	}

	// Table output
	return printCertsTable(os.Stdout, certs)
}

// printCertsTable renders certificate info as a table.
func printCertsTable(w io.Writer, certs []certInfo) error {
	table := output.NewTable(w, "SECRET", "SUBJECT", "ISSUER", "EXPIRES", "DAYS", "STATUS")

	for _, ci := range certs {
		status := colorizeCertStatus(ci.Status)
		table.AddRow(
			ci.SecretName,
			ci.Subject,
			ci.Issuer,
			ci.Expires.Format("2006-01-02"),
			fmt.Sprintf("%d", ci.DaysLeft),
			status,
		)
	}

	return table.Flush()
}

// findCertDataKeys returns keys from a Secret's data map that are likely
// to contain PEM-encoded X.509 certificates. It checks standard TLS
// secret keys, common certificate file extensions, and raw PEM headers.
func findCertDataKeys(data map[string][]byte) []string {
	var keys []string
	for k, v := range data {
		if len(v) == 0 {
			continue
		}
		// Standard TLS secret keys
		if k == "tls.crt" || k == "ca.crt" {
			keys = append(keys, k)
			continue
		}
		// Keys ending in common cert extensions
		lower := strings.ToLower(k)
		if strings.HasSuffix(lower, ".crt") || strings.HasSuffix(lower, ".pem") {
			keys = append(keys, k)
			continue
		}
		// Check if the value starts with a PEM certificate header
		if len(v) > 27 && string(v[:27]) == "-----BEGIN CERTIFICATE-----" {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	return keys
}

// parseCertificates decodes PEM blocks and parses X.509 certificates.
// Non-certificate PEM blocks and malformed certificates are silently skipped.
func parseCertificates(data []byte) []*x509.Certificate {
	var certs []*x509.Certificate
	rest := data
	for {
		var block *pem.Block
		block, rest = pem.Decode(rest)
		if block == nil {
			break
		}
		if block.Type != "CERTIFICATE" {
			continue
		}
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			continue
		}
		certs = append(certs, cert)
	}
	return certs
}

// certHealthStatus returns a human-readable health label based on how
// many days remain until the certificate expires.
func certHealthStatus(now, notAfter time.Time) string {
	daysLeft := notAfter.Sub(now).Hours() / 24
	switch {
	case daysLeft < 0:
		return "Expired"
	case daysLeft < 7:
		return "Critical"
	case daysLeft < 30:
		return "Warning"
	default:
		return "Healthy"
	}
}

// colorizeCertStatus returns a colorized status string when the terminal
// supports it, falling back to plain text otherwise.
func colorizeCertStatus(status string) string {
	if !output.ColorEnabled() {
		return status
	}
	switch status {
	case "Healthy":
		return output.Success(status)
	case "Warning":
		return output.Warning(status)
	case "Critical", "Expired":
		return output.Danger(status)
	default:
		return status
	}
}
