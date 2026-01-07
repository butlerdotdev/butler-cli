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

// Package provider implements butleradm provider commands.
package provider

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/butlerdotdev/butler/internal/common/client"
	"github.com/butlerdotdev/butler/internal/common/log"
	"github.com/butlerdotdev/butler/internal/common/output"
	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

const (
	butlerSystem = "butler-system"
)

// NewProviderCmd creates the provider parent command
func NewProviderCmd(logger *log.Logger) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "provider",
		Short: "Manage infrastructure provider configurations",
		Long: `Manage infrastructure provider configurations for Butler.

Provider configurations define how Butler connects to infrastructure
providers like Nutanix, Harvester, Proxmox, or cloud platforms.

Commands:
  list      List all provider configurations
  validate  Test connectivity to a provider

Examples:
  # List all providers
  butleradm provider list

  # Validate a provider configuration
  butleradm provider validate nutanix`,
	}

	cmd.AddCommand(newListCmd(logger))
	cmd.AddCommand(newValidateCmd(logger))

	return cmd
}

type listOptions struct {
	kubeconfig   string
	outputFormat string
}

func newListCmd(logger *log.Logger) *cobra.Command {
	opts := &listOptions{}

	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List provider configurations",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runList(cmd.Context(), logger, opts)
		},
	}

	cmd.Flags().StringVar(&opts.kubeconfig, "kubeconfig", "", "path to kubeconfig")
	cmd.Flags().StringVarP(&opts.outputFormat, "output", "o", "table", "output format (table, json, yaml)")

	return cmd
}

func runList(ctx context.Context, logger *log.Logger, opts *listOptions) error {
	c, err := getClient(opts.kubeconfig)
	if err != nil {
		return err
	}

	list, err := c.Dynamic.Resource(client.ProviderConfigGVR).Namespace(butlerSystem).List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("listing ProviderConfigs: %w", err)
	}

	format, err := output.ParseFormat(opts.outputFormat)
	if err != nil {
		return err
	}

	if format == output.FormatJSON || format == output.FormatYAML {
		printer := output.NewPrinter(format, os.Stdout)
		return printer.Print(list.Items, nil)
	}

	// Table output
	table := output.NewTable(os.Stdout, "NAME", "PROVIDER", "VALIDATED", "ENDPOINT", "AGE")

	for _, pc := range list.Items {
		name := pc.GetName()
		provider := getNestedString(pc.Object, "spec", "provider")
		validated := getNestedBool(pc.Object, "status", "validated")

		var endpoint string
		switch provider {
		case "nutanix":
			endpoint = getNestedString(pc.Object, "spec", "nutanix", "endpoint")
		case "harvester":
			endpoint = "(in-cluster)"
		case "proxmox":
			endpoint = getNestedString(pc.Object, "spec", "proxmox", "endpoint")
		default:
			endpoint = "-"
		}

		validatedStr := "No"
		if validated {
			validatedStr = output.ColorizePhase("Ready") // Use green
		} else {
			validatedStr = output.ColorizePhase("Pending")
		}

		age := output.FormatAge(pc.GetCreationTimestamp().Time)

		table.AddRow(name, provider, validatedStr, endpoint, age)
	}

	return table.Flush()
}

type validateOptions struct {
	kubeconfig string
	timeout    time.Duration
	insecure   bool
}

func newValidateCmd(logger *log.Logger) *cobra.Command {
	opts := &validateOptions{}

	cmd := &cobra.Command{
		Use:   "validate NAME",
		Short: "Validate connectivity to a provider",
		Long: `Test connectivity to an infrastructure provider.

This command attempts to connect to the provider's API using the
configured credentials and updates the ProviderConfig status.

For Nutanix: Tests Prism Central API connectivity
For Harvester: Tests in-cluster Harvester API
For Proxmox: Tests Proxmox VE API connectivity

Examples:
  # Validate the nutanix provider config
  butleradm provider validate nutanix

  # Validate with longer timeout
  butleradm provider validate nutanix --timeout 60s

  # Skip TLS verification (not recommended for production)
  butleradm provider validate nutanix --insecure`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runValidate(cmd.Context(), logger, args[0], opts)
		},
	}

	cmd.Flags().StringVar(&opts.kubeconfig, "kubeconfig", "", "path to kubeconfig")
	cmd.Flags().DurationVar(&opts.timeout, "timeout", 30*time.Second, "connection timeout")
	cmd.Flags().BoolVar(&opts.insecure, "insecure", false, "skip TLS certificate verification")

	return cmd
}

func runValidate(ctx context.Context, logger *log.Logger, name string, opts *validateOptions) error {
	c, err := getClient(opts.kubeconfig)
	if err != nil {
		return err
	}

	// Get the ProviderConfig
	pc, err := c.Dynamic.Resource(client.ProviderConfigGVR).Namespace(butlerSystem).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("getting ProviderConfig %s: %w", name, err)
	}

	provider := getNestedString(pc.Object, "spec", "provider")
	logger.Info("validating provider", "name", name, "type", provider)

	var validationErr error
	switch provider {
	case "nutanix":
		validationErr = validateNutanix(ctx, c, pc, opts, logger)
	case "harvester":
		validationErr = validateHarvester(ctx, c, pc, opts, logger)
	case "proxmox":
		validationErr = validateProxmox(ctx, c, pc, opts, logger)
	default:
		return fmt.Errorf("unknown provider type: %s", provider)
	}

	// Update ProviderConfig status
	if err := updateProviderConfigStatus(ctx, c, pc, validationErr); err != nil {
		logger.Warn("failed to update ProviderConfig status", "error", err)
	}

	if validationErr != nil {
		logger.Error("validation failed", "error", validationErr)
		return validationErr
	}

	logger.Success("provider validated successfully", "name", name)
	return nil
}

func validateNutanix(ctx context.Context, c *client.Client, pc *unstructured.Unstructured, opts *validateOptions, logger *log.Logger) error {
	endpoint := getNestedString(pc.Object, "spec", "nutanix", "endpoint")
	if endpoint == "" {
		return fmt.Errorf("nutanix endpoint not configured")
	}

	// Get port from spec (default 9440 for Prism Central)
	port := getNestedInt64(pc.Object, "spec", "nutanix", "port")
	if port == 0 {
		port = 9440
	}

	// Get insecure flag from provider config
	insecure := getNestedBool(pc.Object, "spec", "nutanix", "insecure")
	if opts.insecure {
		insecure = true
	}

	// Get credentials from secret - credentialsRef is at spec level, not nested under nutanix
	secretName := getNestedString(pc.Object, "spec", "credentialsRef", "name")
	if secretName == "" {
		return fmt.Errorf("credentials secret not configured (spec.credentialsRef.name)")
	}

	secret, err := c.Clientset.CoreV1().Secrets(butlerSystem).Get(ctx, secretName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("getting credentials secret %s: %w", secretName, err)
	}

	// Keys are "username" and "password" per the CRD docs
	username := string(secret.Data["username"])
	password := string(secret.Data["password"])
	if username == "" || password == "" {
		// Try alternate key names used by CAPX
		username = string(secret.Data["NUTANIX_USER"])
		password = string(secret.Data["NUTANIX_PASSWORD"])
	}
	if username == "" || password == "" {
		return fmt.Errorf("credentials secret %s missing username/password (or NUTANIX_USER/NUTANIX_PASSWORD)", secretName)
	}

	// Build the full API URL with port
	// Strip trailing slash from endpoint
	endpoint = strings.TrimSuffix(endpoint, "/")

	// Check if endpoint already has a port
	apiURL := endpoint
	if !strings.Contains(strings.TrimPrefix(strings.TrimPrefix(endpoint, "https://"), "http://"), ":") {
		// No port in endpoint, add it
		apiURL = fmt.Sprintf("%s:%d", endpoint, port)
	}

	logger.Info("testing Prism Central connectivity", "endpoint", apiURL, "insecure", insecure)

	// Test API connectivity
	httpClient := &http.Client{
		Timeout: opts.timeout,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: insecure,
			},
		},
	}

	// Try to hit the clusters API endpoint
	fullURL := fmt.Sprintf("%s/api/nutanix/v3/clusters/list", apiURL)

	// Create request with empty JSON body (required by Nutanix API)
	reqBody := strings.NewReader("{}")
	req, err := http.NewRequestWithContext(ctx, "POST", fullURL, reqBody)
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}
	req.SetBasicAuth(username, password)
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("connecting to Prism Central at %s: %w", apiURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == 401 {
		return fmt.Errorf("authentication failed - check credentials")
	}
	if resp.StatusCode >= 400 {
		return fmt.Errorf("API returned status %d", resp.StatusCode)
	}

	logger.Success("Prism Central API accessible", "status", resp.StatusCode)
	return nil
}

func validateHarvester(ctx context.Context, c *client.Client, pc *unstructured.Unstructured, opts *validateOptions, logger *log.Logger) error {
	// For Harvester, we check if the Harvester CRDs are available
	// and if we can list VirtualMachines
	logger.Info("testing Harvester in-cluster connectivity")

	// Check if Harvester VirtualMachine CRD exists
	_, err := c.Clientset.Discovery().ServerResourcesForGroupVersion("kubevirt.io/v1")
	if err != nil {
		return fmt.Errorf("Harvester/KubeVirt API not available: %w", err)
	}

	logger.Success("Harvester API accessible")
	return nil
}

func validateProxmox(ctx context.Context, c *client.Client, pc *unstructured.Unstructured, opts *validateOptions, logger *log.Logger) error {
	endpoint := getNestedString(pc.Object, "spec", "proxmox", "endpoint")
	if endpoint == "" {
		return fmt.Errorf("proxmox endpoint not configured")
	}

	// Get insecure flag from provider config
	insecure := getNestedBool(pc.Object, "spec", "proxmox", "insecure")
	if opts.insecure {
		insecure = true
	}

	// Get credentials from secret - credentialsRef is at spec level
	secretName := getNestedString(pc.Object, "spec", "credentialsRef", "name")
	if secretName == "" {
		return fmt.Errorf("credentials secret not configured (spec.credentialsRef.name)")
	}

	secret, err := c.Clientset.CoreV1().Secrets(butlerSystem).Get(ctx, secretName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("getting credentials secret %s: %w", secretName, err)
	}

	// Try token-based auth first
	tokenID := string(secret.Data["token"])
	tokenSecret := string(secret.Data["tokenSecret"])

	// Fallback to alternate key names
	if tokenID == "" {
		tokenID = string(secret.Data["PROXMOX_TOKEN_ID"])
		tokenSecret = string(secret.Data["PROXMOX_TOKEN_SECRET"])
	}

	// Or username/password
	username := string(secret.Data["username"])
	password := string(secret.Data["password"])

	if tokenID == "" && username == "" {
		return fmt.Errorf("credentials secret %s missing token or username/password", secretName)
	}

	logger.Info("testing Proxmox API connectivity", "endpoint", endpoint)

	httpClient := &http.Client{
		Timeout: opts.timeout,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: insecure,
			},
		},
	}

	// Test API connectivity - get version
	apiURL := fmt.Sprintf("%s/api2/json/version", endpoint)
	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}

	if tokenID != "" {
		req.Header.Set("Authorization", fmt.Sprintf("PVEAPIToken=%s=%s", tokenID, tokenSecret))
	} else {
		req.SetBasicAuth(username, password)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("connecting to Proxmox: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == 401 {
		return fmt.Errorf("authentication failed - check credentials")
	}
	if resp.StatusCode >= 400 {
		return fmt.Errorf("API returned status %d", resp.StatusCode)
	}

	logger.Success("Proxmox API accessible")
	return nil
}

func updateProviderConfigStatus(ctx context.Context, c *client.Client, pc *unstructured.Unstructured, validationErr error) error {
	// Get current status or create new
	currentStatus, _, _ := unstructured.NestedMap(pc.Object, "status")
	if currentStatus == nil {
		currentStatus = make(map[string]interface{})
	}

	// Update validated flag
	currentStatus["validated"] = validationErr == nil

	// Update lastValidationTime (this is a metav1.Time in the CRD)
	currentStatus["lastValidationTime"] = time.Now().UTC().Format(time.RFC3339)

	// Update conditions
	conditions, _, _ := unstructured.NestedSlice(pc.Object, "status", "conditions")
	if conditions == nil {
		conditions = []interface{}{}
	}

	// Find or create the Ready condition
	readyCondition := map[string]interface{}{
		"type":               "Ready",
		"lastTransitionTime": time.Now().UTC().Format(time.RFC3339),
		"observedGeneration": pc.GetGeneration(),
	}

	if validationErr == nil {
		readyCondition["status"] = "True"
		readyCondition["reason"] = "ValidationSucceeded"
		readyCondition["message"] = "Provider connectivity validated successfully"
	} else {
		readyCondition["status"] = "False"
		readyCondition["reason"] = "ValidationFailed"
		readyCondition["message"] = validationErr.Error()
	}

	// Replace or add the Ready condition
	found := false
	for i, c := range conditions {
		cond, ok := c.(map[string]interface{})
		if ok && cond["type"] == "Ready" {
			conditions[i] = readyCondition
			found = true
			break
		}
	}
	if !found {
		conditions = append(conditions, readyCondition)
	}
	currentStatus["conditions"] = conditions

	// Set the status
	if err := unstructured.SetNestedMap(pc.Object, currentStatus, "status"); err != nil {
		return fmt.Errorf("setting status: %w", err)
	}

	_, err := c.Dynamic.Resource(client.ProviderConfigGVR).Namespace(butlerSystem).UpdateStatus(ctx, pc, metav1.UpdateOptions{})
	return err
}

func getClient(kubeconfigPath string) (*client.Client, error) {
	if kubeconfigPath != "" {
		return client.NewFromKubeconfig(kubeconfigPath)
	}
	return client.NewFromDefault()
}

func getNestedString(obj map[string]interface{}, fields ...string) string {
	val, _, _ := unstructured.NestedString(obj, fields...)
	return val
}

func getNestedBool(obj map[string]interface{}, fields ...string) bool {
	val, _, _ := unstructured.NestedBool(obj, fields...)
	return val
}

func getNestedInt64(obj map[string]interface{}, fields ...string) int64 {
	val, _, _ := unstructured.NestedInt64(obj, fields...)
	return val
}

// ProviderInfo is used for JSON/YAML output
type ProviderInfo struct {
	Name            string `json:"name"`
	Provider        string `json:"provider"`
	Validated       bool   `json:"validated"`
	Endpoint        string `json:"endpoint,omitempty"`
	ValidationError string `json:"validationError,omitempty"`
	LastValidatedAt string `json:"lastValidatedAt,omitempty"`
}

func extractProviderInfo(pc *unstructured.Unstructured) ProviderInfo {
	provider := getNestedString(pc.Object, "spec", "provider")

	var endpoint string
	switch provider {
	case "nutanix":
		endpoint = getNestedString(pc.Object, "spec", "nutanix", "endpoint")
	case "harvester":
		endpoint = "(in-cluster)"
	case "proxmox":
		endpoint = getNestedString(pc.Object, "spec", "proxmox", "endpoint")
	}

	return ProviderInfo{
		Name:            pc.GetName(),
		Provider:        provider,
		Validated:       getNestedBool(pc.Object, "status", "validated"),
		Endpoint:        endpoint,
		ValidationError: getNestedString(pc.Object, "status", "validationError"),
		LastValidatedAt: getNestedString(pc.Object, "status", "lastValidatedAt"),
	}
}

// MarshalJSON implements custom JSON marshaling for provider list
func marshalProviders(providers []unstructured.Unstructured) ([]byte, error) {
	infos := make([]ProviderInfo, len(providers))
	for i, p := range providers {
		infos[i] = extractProviderInfo(&p)
	}
	return json.MarshalIndent(infos, "", "  ")
}
