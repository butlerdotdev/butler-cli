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

package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

// cloudLBResources tracks cloud LB resources created for cleanup on failure.
type cloudLBResources struct {
	provider    string
	clusterName string

	// GCP resources
	gcpProject  string
	gcpRegion   string
	staticIP    string
	healthCheck string
	targetPool  string
	fwdRule     string

	// AWS resources
	awsRegion      string
	nlbARN         string
	targetGroupARN string
	listenerARN    string

	// Azure resources
	azureResourceGroup string
	azureLocation      string
	publicIPName       string
	lbName             string
}

// isCloudProvider returns true for cloud providers that need a load balancer for HA.
func isCloudProvider(provider string) bool {
	switch provider {
	case "gcp", "aws", "azure":
		return true
	}
	return false
}

// createCloudLoadBalancer creates a cloud TCP load balancer for HA control plane.
// Returns the LB endpoint (IP or DNS name) and resources for cleanup.
func (o *Orchestrator) createCloudLoadBalancer(ctx context.Context, cfg *Config) (string, *cloudLBResources, error) {
	switch cfg.Provider {
	case "gcp":
		return o.createGCPLoadBalancer(ctx, cfg)
	case "aws":
		return o.createAWSLoadBalancer(ctx, cfg)
	case "azure":
		return o.createAzureLoadBalancer(ctx, cfg)
	default:
		return "", nil, fmt.Errorf("cloud LB not supported for provider %q", cfg.Provider)
	}
}

// cleanupCloudLoadBalancer removes cloud LB resources created during bootstrap.
func (o *Orchestrator) cleanupCloudLoadBalancer(ctx context.Context, res *cloudLBResources) {
	if res == nil {
		return
	}

	o.logger.Info("Cleaning up cloud load balancer resources")

	switch res.provider {
	case "gcp":
		o.cleanupGCPLoadBalancer(ctx, res)
	case "aws":
		o.cleanupAWSLoadBalancer(ctx, res)
	case "azure":
		o.cleanupAzureLoadBalancer(ctx, res)
	}
}

// --- GCP Load Balancer ---

func (o *Orchestrator) createGCPLoadBalancer(ctx context.Context, cfg *Config) (string, *cloudLBResources, error) {
	gcp := cfg.ProviderConfig.GCP
	if gcp == nil {
		return "", nil, fmt.Errorf("GCP provider config required for cloud LB")
	}

	res := &cloudLBResources{
		provider:    "gcp",
		clusterName: cfg.Cluster.Name,
		gcpProject:  gcp.ProjectID,
		gcpRegion:   gcp.Region,
	}

	saKeyPath := gcp.ServiceAccountKeyPath
	name := cfg.Cluster.Name

	// Authenticate gcloud with service account
	if err := o.runCloudCLI(ctx, "gcloud", "auth", "activate-service-account",
		"--key-file", saKeyPath, "--project", gcp.ProjectID); err != nil {
		return "", nil, fmt.Errorf("gcloud auth failed: %w", err)
	}

	// 1. Reserve static external IP
	ipName := name + "-cp-ip"
	o.logger.Info("Reserving static IP", "name", ipName)
	if err := o.runCloudCLI(ctx, "gcloud", "compute", "addresses", "create", ipName,
		"--region", gcp.Region, "--project", gcp.ProjectID); err != nil {
		return "", res, fmt.Errorf("reserving static IP: %w", err)
	}
	res.staticIP = ipName

	// Get the allocated IP address
	ip, err := o.getGCPStaticIP(ctx, ipName, gcp.Region, gcp.ProjectID)
	if err != nil {
		return "", res, fmt.Errorf("getting static IP address: %w", err)
	}
	o.logger.Info("Static IP reserved", "ip", ip)

	// 2. Create HTTP health check (TCP on port 6443)
	hcName := name + "-cp-hc"
	o.logger.Info("Creating health check", "name", hcName)
	if err := o.runCloudCLI(ctx, "gcloud", "compute", "http-health-checks", "create", hcName,
		"--port", "6443", "--request-path", "/healthz",
		"--project", gcp.ProjectID); err != nil {
		return "", res, fmt.Errorf("creating health check: %w", err)
	}
	res.healthCheck = hcName

	// 3. Create target pool
	tpName := name + "-cp-pool"
	o.logger.Info("Creating target pool", "name", tpName)
	if err := o.runCloudCLI(ctx, "gcloud", "compute", "target-pools", "create", tpName,
		"--region", gcp.Region, "--http-health-check", hcName,
		"--project", gcp.ProjectID); err != nil {
		return "", res, fmt.Errorf("creating target pool: %w", err)
	}
	res.targetPool = tpName

	// 4. Create forwarding rule
	fwdName := name + "-cp-fwd"
	o.logger.Info("Creating forwarding rule", "name", fwdName)
	if err := o.runCloudCLI(ctx, "gcloud", "compute", "forwarding-rules", "create", fwdName,
		"--region", gcp.Region, "--address", ipName,
		"--ports", "6443", "--target-pool", tpName,
		"--project", gcp.ProjectID); err != nil {
		return "", res, fmt.Errorf("creating forwarding rule: %w", err)
	}
	res.fwdRule = fwdName

	o.logger.Info("GCP load balancer created", "endpoint", ip)
	return ip, res, nil
}

func (o *Orchestrator) getGCPStaticIP(ctx context.Context, name, region, project string) (string, error) {
	out, err := exec.CommandContext(ctx, "gcloud", "compute", "addresses", "describe", name,
		"--region", region, "--project", project, "--format", "json").Output()
	if err != nil {
		return "", err
	}

	var addr struct {
		Address string `json:"address"`
	}
	if err := json.Unmarshal(out, &addr); err != nil {
		return "", fmt.Errorf("parsing address JSON: %w", err)
	}
	return addr.Address, nil
}

func (o *Orchestrator) cleanupGCPLoadBalancer(ctx context.Context, res *cloudLBResources) {
	// Delete in reverse order of creation
	if res.fwdRule != "" {
		o.logger.Info("Deleting forwarding rule", "name", res.fwdRule)
		_ = o.runCloudCLI(ctx, "gcloud", "compute", "forwarding-rules", "delete", res.fwdRule,
			"--region", res.gcpRegion, "--project", res.gcpProject, "--quiet")
	}
	if res.targetPool != "" {
		o.logger.Info("Deleting target pool", "name", res.targetPool)
		_ = o.runCloudCLI(ctx, "gcloud", "compute", "target-pools", "delete", res.targetPool,
			"--region", res.gcpRegion, "--project", res.gcpProject, "--quiet")
	}
	if res.healthCheck != "" {
		o.logger.Info("Deleting health check", "name", res.healthCheck)
		_ = o.runCloudCLI(ctx, "gcloud", "compute", "http-health-checks", "delete", res.healthCheck,
			"--project", res.gcpProject, "--quiet")
	}
	if res.staticIP != "" {
		o.logger.Info("Releasing static IP", "name", res.staticIP)
		_ = o.runCloudCLI(ctx, "gcloud", "compute", "addresses", "delete", res.staticIP,
			"--region", res.gcpRegion, "--project", res.gcpProject, "--quiet")
	}
}

// --- AWS Load Balancer ---

func (o *Orchestrator) createAWSLoadBalancer(ctx context.Context, cfg *Config) (string, *cloudLBResources, error) {
	aws := cfg.ProviderConfig.AWS
	if aws == nil {
		return "", nil, fmt.Errorf("AWS provider config required for cloud LB")
	}

	res := &cloudLBResources{
		provider:    "aws",
		clusterName: cfg.Cluster.Name,
		awsRegion:   aws.Region,
	}

	name := cfg.Cluster.Name

	// Set AWS credentials via environment
	envPrefix := []string{
		"AWS_ACCESS_KEY_ID=" + aws.AccessKeyID,
		"AWS_SECRET_ACCESS_KEY=" + aws.SecretAccessKey,
		"AWS_DEFAULT_REGION=" + aws.Region,
	}

	// 1. Create NLB
	nlbName := name + "-cp-nlb"
	o.logger.Info("Creating NLB", "name", nlbName)
	out, err := o.runCloudCLIWithEnvOutput(ctx, envPrefix,
		"aws", "elbv2", "create-load-balancer",
		"--name", nlbName, "--type", "network",
		"--scheme", "internet-facing",
		"--subnets", aws.SubnetID,
		"--output", "json")
	if err != nil {
		return "", res, fmt.Errorf("creating NLB: %w", err)
	}

	var nlbResult struct {
		LoadBalancers []struct {
			LoadBalancerArn string `json:"LoadBalancerArn"`
			DNSName         string `json:"DNSName"`
		} `json:"LoadBalancers"`
	}
	if err := json.Unmarshal(out, &nlbResult); err != nil {
		return "", res, fmt.Errorf("parsing NLB response: %w", err)
	}
	if len(nlbResult.LoadBalancers) == 0 {
		return "", res, fmt.Errorf("no load balancer returned")
	}
	res.nlbARN = nlbResult.LoadBalancers[0].LoadBalancerArn
	dnsName := nlbResult.LoadBalancers[0].DNSName

	// 2. Create target group
	tgName := name + "-cp-tg"
	o.logger.Info("Creating target group", "name", tgName)
	tgOut, err := o.runCloudCLIWithEnvOutput(ctx, envPrefix,
		"aws", "elbv2", "create-target-group",
		"--name", tgName, "--protocol", "TCP", "--port", "6443",
		"--target-type", "instance", "--vpc-id", aws.VPCID,
		"--output", "json")
	if err != nil {
		return "", res, fmt.Errorf("creating target group: %w", err)
	}

	var tgResult struct {
		TargetGroups []struct {
			TargetGroupArn string `json:"TargetGroupArn"`
		} `json:"TargetGroups"`
	}
	if err := json.Unmarshal(tgOut, &tgResult); err != nil {
		return "", res, fmt.Errorf("parsing target group response: %w", err)
	}
	if len(tgResult.TargetGroups) == 0 {
		return "", res, fmt.Errorf("no target group returned")
	}
	res.targetGroupARN = tgResult.TargetGroups[0].TargetGroupArn

	// 3. Create listener
	o.logger.Info("Creating listener")
	listenerOut, err := o.runCloudCLIWithEnvOutput(ctx, envPrefix,
		"aws", "elbv2", "create-listener",
		"--load-balancer-arn", res.nlbARN,
		"--protocol", "TCP", "--port", "6443",
		"--default-actions", fmt.Sprintf("Type=forward,TargetGroupArn=%s", res.targetGroupARN),
		"--output", "json")
	if err != nil {
		return "", res, fmt.Errorf("creating listener: %w", err)
	}

	var listenerResult struct {
		Listeners []struct {
			ListenerArn string `json:"ListenerArn"`
		} `json:"Listeners"`
	}
	if err := json.Unmarshal(listenerOut, &listenerResult); err == nil && len(listenerResult.Listeners) > 0 {
		res.listenerARN = listenerResult.Listeners[0].ListenerArn
	}

	o.logger.Info("AWS NLB created", "endpoint", dnsName)
	return dnsName, res, nil
}

func (o *Orchestrator) cleanupAWSLoadBalancer(ctx context.Context, res *cloudLBResources) {
	envPrefix := []string{
		"AWS_DEFAULT_REGION=" + res.awsRegion,
	}

	if res.listenerARN != "" {
		o.logger.Info("Deleting listener")
		_, _ = o.runCloudCLIWithEnvOutput(ctx, envPrefix,
			"aws", "elbv2", "delete-listener", "--listener-arn", res.listenerARN)
	}
	if res.targetGroupARN != "" {
		o.logger.Info("Deleting target group")
		_, _ = o.runCloudCLIWithEnvOutput(ctx, envPrefix,
			"aws", "elbv2", "delete-target-group", "--target-group-arn", res.targetGroupARN)
	}
	if res.nlbARN != "" {
		o.logger.Info("Deleting NLB")
		_, _ = o.runCloudCLIWithEnvOutput(ctx, envPrefix,
			"aws", "elbv2", "delete-load-balancer", "--load-balancer-arn", res.nlbARN)
	}
}

// --- Azure Load Balancer ---

func (o *Orchestrator) createAzureLoadBalancer(ctx context.Context, cfg *Config) (string, *cloudLBResources, error) {
	az := cfg.ProviderConfig.Azure
	if az == nil {
		return "", nil, fmt.Errorf("Azure provider config required for cloud LB")
	}

	res := &cloudLBResources{
		provider:           "azure",
		clusterName:        cfg.Cluster.Name,
		azureResourceGroup: az.ResourceGroup,
		azureLocation:      az.Location,
	}

	name := cfg.Cluster.Name

	// Login with service principal
	if err := o.runCloudCLI(ctx, "az", "login", "--service-principal",
		"-u", az.ClientID, "-p", az.ClientSecret, "--tenant", az.TenantID); err != nil {
		return "", nil, fmt.Errorf("az login failed: %w", err)
	}

	// 1. Create public IP
	pipName := name + "-cp-pip"
	o.logger.Info("Creating public IP", "name", pipName)
	pipOut, err := o.runCloudCLIOutput(ctx,
		"az", "network", "public-ip", "create",
		"--name", pipName, "--resource-group", az.ResourceGroup,
		"--location", az.Location, "--sku", "Standard",
		"--allocation-method", "Static", "--output", "json")
	if err != nil {
		return "", res, fmt.Errorf("creating public IP: %w", err)
	}
	res.publicIPName = pipName

	var pipResult struct {
		PublicIP struct {
			IPAddress string `json:"ipAddress"`
		} `json:"publicIp"`
	}
	if err := json.Unmarshal(pipOut, &pipResult); err != nil {
		return "", res, fmt.Errorf("parsing public IP response: %w", err)
	}
	ip := pipResult.PublicIP.IPAddress
	o.logger.Info("Public IP created", "ip", ip)

	// 2. Create load balancer
	lbName := name + "-cp-lb"
	o.logger.Info("Creating load balancer", "name", lbName)
	if err := o.runCloudCLI(ctx,
		"az", "network", "lb", "create",
		"--name", lbName, "--resource-group", az.ResourceGroup,
		"--location", az.Location, "--sku", "Standard",
		"--frontend-ip-name", "cp-frontend",
		"--public-ip-address", pipName,
		"--backend-pool-name", "cp-backend"); err != nil {
		return "", res, fmt.Errorf("creating load balancer: %w", err)
	}
	res.lbName = lbName

	// 3. Create health probe
	o.logger.Info("Creating health probe")
	if err := o.runCloudCLI(ctx,
		"az", "network", "lb", "probe", "create",
		"--name", "cp-probe", "--lb-name", lbName,
		"--resource-group", az.ResourceGroup,
		"--protocol", "Tcp", "--port", "6443"); err != nil {
		return "", res, fmt.Errorf("creating health probe: %w", err)
	}

	// 4. Create LB rule
	o.logger.Info("Creating LB rule")
	if err := o.runCloudCLI(ctx,
		"az", "network", "lb", "rule", "create",
		"--name", "cp-rule", "--lb-name", lbName,
		"--resource-group", az.ResourceGroup,
		"--frontend-ip-name", "cp-frontend",
		"--backend-pool-name", "cp-backend",
		"--probe-name", "cp-probe",
		"--protocol", "Tcp",
		"--frontend-port", "6443", "--backend-port", "6443"); err != nil {
		return "", res, fmt.Errorf("creating LB rule: %w", err)
	}

	o.logger.Info("Azure LB created", "endpoint", ip)
	return ip, res, nil
}

func (o *Orchestrator) cleanupAzureLoadBalancer(ctx context.Context, res *cloudLBResources) {
	if res.lbName != "" {
		o.logger.Info("Deleting load balancer", "name", res.lbName)
		_ = o.runCloudCLI(ctx, "az", "network", "lb", "delete",
			"--name", res.lbName, "--resource-group", res.azureResourceGroup, "--yes")
	}
	if res.publicIPName != "" {
		o.logger.Info("Deleting public IP", "name", res.publicIPName)
		_ = o.runCloudCLI(ctx, "az", "network", "public-ip", "delete",
			"--name", res.publicIPName, "--resource-group", res.azureResourceGroup, "--yes")
	}
}

// --- CLI Helpers ---

func (o *Orchestrator) runCloudCLI(ctx context.Context, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s %s failed: %w\n%s", name, strings.Join(args[:min(2, len(args))], " "), err, string(output))
	}
	return nil
}

func (o *Orchestrator) runCloudCLIOutput(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	output, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("%s failed: %w\n%s", name, err, string(exitErr.Stderr))
		}
		return nil, fmt.Errorf("%s failed: %w", name, err)
	}
	return output, nil
}

func (o *Orchestrator) runCloudCLIWithEnvOutput(ctx context.Context, envPrefix []string, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = append(cmd.Environ(), envPrefix...)
	output, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("%s failed: %w\n%s", name, err, string(exitErr.Stderr))
		}
		return nil, fmt.Errorf("%s failed: %w", name, err)
	}
	return output, nil
}
