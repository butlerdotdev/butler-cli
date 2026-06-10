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

package provider

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/butlerdotdev/butler/internal/common/log"
	"github.com/butlerdotdev/butler/internal/common/output"
	"github.com/butlerdotdev/butler/internal/common/serverhttp"
	"github.com/spf13/cobra"
)

// imageInfo mirrors butler-server's providers.ImageInfo
// (GET /api/providers/{ns}/{name}/images).
type imageInfo struct {
	Name        string `json:"name"`
	ID          string `json:"id"`
	Description string `json:"description,omitempty"`
	OS          string `json:"os,omitempty"`
}

type imageListResponse struct {
	Images []imageInfo `json:"images"`
}

// networkInfo mirrors butler-server's providers.NetworkInfo
// (GET /api/providers/{ns}/{name}/networks).
type networkInfo struct {
	Name        string `json:"name"`
	ID          string `json:"id"`
	VLAN        int    `json:"vlan,omitempty"`
	Description string `json:"description,omitempty"`
}

type networkListResponse struct {
	Networks []networkInfo `json:"networks"`
}

type discoveryOptions struct {
	namespace    string
	outputFormat string
}

// --- images ---

func newImagesCmd(_ *log.Logger) *cobra.Command {
	opts := &discoveryOptions{}

	cmd := &cobra.Command{
		Use:   "images NAME",
		Short: "List images available to a provider",
		Long: `List the VM images a provider can use, so you can find the image ID for
'cluster create'. The list is policy-filtered server-side, so it matches what
the console shows.

Image discovery is supported for on-premises providers (harvester, nutanix).
For cloud providers (aws, azure, gcp) and proxmox the image is set directly as
a field on the ProviderConfig (for example awsVpcId, gcpImage, azureImageUrn).

Examples:
  butleradm provider images nutanix
  butleradm provider images harvester -o json`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runImages(cmd.Context(), os.Stdout, args[0], opts)
		},
	}

	cmd.Flags().StringVarP(&opts.namespace, "namespace", "n", butlerSystem, "namespace of the ProviderConfig")
	cmd.Flags().StringVarP(&opts.outputFormat, "output", "o", "", "output format (table, json, yaml)")

	return cmd
}

func runImages(ctx context.Context, out io.Writer, name string, opts *discoveryOptions) error {
	sh, err := serverhttp.New()
	if err != nil {
		return err
	}
	path := fmt.Sprintf("/api/providers/%s/%s/images", opts.namespace, name)
	var res imageListResponse
	if err := sh.Get(ctx, path, &res); err != nil {
		return translateDiscoveryError(err, name)
	}

	switch opts.outputFormat {
	case "json":
		return output.PrintJSON(out, res)
	case "yaml":
		return output.PrintYAML(out, res)
	case "", "table":
		return printImages(out, res)
	default:
		return fmt.Errorf("unsupported output format %q (use table, json or yaml)", opts.outputFormat)
	}
}

func printImages(out io.Writer, res imageListResponse) error {
	if len(res.Images) == 0 {
		fmt.Fprintln(out, "No images found.")
		return nil
	}
	t := output.NewTable(out, "NAME", "ID", "OS", "DESCRIPTION")
	for _, img := range res.Images {
		t.AddRow(img.Name, img.ID, img.OS, img.Description)
	}
	return t.Flush()
}

// --- networks ---

func newNetworksCmd(_ *log.Logger) *cobra.Command {
	opts := &discoveryOptions{}

	cmd := &cobra.Command{
		Use:   "networks NAME",
		Short: "List networks available to a provider",
		Long: `List the networks a provider can use, so you can find the network ID for
'cluster create'. The list is policy-filtered server-side, so it matches what
the console shows.

Network discovery is supported for on-premises providers (harvester, nutanix).
For cloud providers (aws, azure, gcp) and proxmox the network is set directly
as a field on the ProviderConfig (for example awsVpcId, gcpNetwork,
azureVnetName).

Examples:
  butleradm provider networks nutanix
  butleradm provider networks harvester -o json`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runNetworks(cmd.Context(), os.Stdout, args[0], opts)
		},
	}

	cmd.Flags().StringVarP(&opts.namespace, "namespace", "n", butlerSystem, "namespace of the ProviderConfig")
	cmd.Flags().StringVarP(&opts.outputFormat, "output", "o", "", "output format (table, json, yaml)")

	return cmd
}

func runNetworks(ctx context.Context, out io.Writer, name string, opts *discoveryOptions) error {
	sh, err := serverhttp.New()
	if err != nil {
		return err
	}
	path := fmt.Sprintf("/api/providers/%s/%s/networks", opts.namespace, name)
	var res networkListResponse
	if err := sh.Get(ctx, path, &res); err != nil {
		return translateDiscoveryError(err, name)
	}

	switch opts.outputFormat {
	case "json":
		return output.PrintJSON(out, res)
	case "yaml":
		return output.PrintYAML(out, res)
	case "", "table":
		return printNetworks(out, res)
	default:
		return fmt.Errorf("unsupported output format %q (use table, json or yaml)", opts.outputFormat)
	}
}

func printNetworks(out io.Writer, res networkListResponse) error {
	if len(res.Networks) == 0 {
		fmt.Fprintln(out, "No networks found.")
		return nil
	}
	t := output.NewTable(out, "NAME", "ID", "VLAN", "DESCRIPTION")
	for _, n := range res.Networks {
		vlan := ""
		if n.VLAN != 0 {
			vlan = fmt.Sprintf("%d", n.VLAN)
		}
		t.AddRow(n.Name, n.ID, vlan, n.Description)
	}
	return t.Flush()
}

// translateDiscoveryError maps serverhttp errors to operator-facing messages.
// The load-bearing case is the 400 the server returns for cloud providers and
// proxmox ("listing not supported for provider: X"): image and network
// discovery only exists for harvester and nutanix, so the message must name
// the provider and point at the static ProviderConfig fields instead of
// surfacing a raw 400.
func translateDiscoveryError(err error, name string) error {
	if errors.Is(err, serverhttp.ErrSessionExpired) {
		fmt.Fprintln(os.Stderr, "Butler session expired. Run 'butleradm login' to re-authenticate.")
		return err
	}
	var se *serverhttp.ServerError
	if errors.As(err, &se) {
		switch {
		case se.IsBadRequest() && strings.Contains(se.Message, "not supported"):
			return fmt.Errorf("%s\nimage/network discovery is supported only for on-premises providers (harvester, nutanix). For cloud providers (aws, azure, gcp) and proxmox the image and network are set as fields on the ProviderConfig (for example awsVpcId, gcpImage, azureImageUrn)", se.Message)
		case se.IsNotFound():
			return fmt.Errorf("provider %q not found: %s", name, se.Message)
		case se.IsForbidden():
			return fmt.Errorf("forbidden: %s", se.Message)
		case se.IsBadRequest():
			return fmt.Errorf("invalid request: %s", se.Message)
		}
	}
	return err
}
