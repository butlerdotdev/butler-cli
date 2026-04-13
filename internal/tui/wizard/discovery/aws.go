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

package discovery

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
)

func init() {
	Register("aws", func(creds CredentialProvider) (ProviderDiscovery, error) {
		return NewAWSDiscovery(creds)
	})
}

// AWSDiscovery discovers resources on Amazon Web Services using the
// EC2 API.
type AWSDiscovery struct {
	accessKeyID     string
	secretAccessKey string
	region          string
	cfg             aws.Config
	client          *ec2.Client
}

// NewAWSDiscovery creates an AWS discovery client from credentials.
func NewAWSDiscovery(creds CredentialProvider) (*AWSDiscovery, error) {
	accessKey := creds.Get("access_key")
	if accessKey == "" {
		return nil, fmt.Errorf("AWS access key ID is required")
	}
	secretKey := creds.Get("secret_key")
	if secretKey == "" {
		return nil, fmt.Errorf("AWS secret access key is required")
	}

	return &AWSDiscovery{
		accessKeyID:     accessKey,
		secretAccessKey: secretKey,
		region:          creds.Get("region"),
	}, nil
}

func (a *AWSDiscovery) Connect(ctx context.Context) error {
	region := a.region
	if region == "" {
		region = "us-east-1"
	}

	var err error
	a.cfg, err = awsconfig.LoadDefaultConfig(ctx,
		awsconfig.WithRegion(region),
		awsconfig.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(a.accessKeyID, a.secretAccessKey, ""),
		),
	)
	if err != nil {
		return fmt.Errorf("loading AWS config: %w", err)
	}

	a.client = ec2.NewFromConfig(a.cfg)

	// Validate connectivity by listing regions.
	_, err = a.client.DescribeRegions(ctx, &ec2.DescribeRegionsInput{
		AllRegions: aws.Bool(false),
	})
	if err != nil {
		return fmt.Errorf("connecting to AWS: %w", err)
	}

	return nil
}

func (a *AWSDiscovery) ResourceTypes() []ResourceTypeInfo {
	return []ResourceTypeInfo{
		{Type: ResourceRegions, Label: "Region", ParentType: ""},
		{Type: ResourceVPCs, Label: "VPC", ParentType: ResourceRegions},
		{Type: ResourceSubnets, Label: "Subnet", ParentType: ResourceVPCs},
		{Type: ResourceSecurityGroups, Label: "Security Group", ParentType: ResourceVPCs},
		{Type: ResourceImages, Label: "AMI", ParentType: ResourceRegions},
	}
}

func (a *AWSDiscovery) FetchResource(ctx context.Context, resourceType string, parentID string) ([]ProviderResource, error) {
	switch resourceType {
	case ResourceRegions:
		return a.listRegions(ctx)
	case ResourceVPCs:
		return a.listVPCs(ctx, parentID)
	case ResourceSubnets:
		return a.listSubnets(ctx, parentID)
	case ResourceSecurityGroups:
		return a.listSecurityGroups(ctx, parentID)
	case ResourceImages:
		return a.listAMIs(ctx, parentID)
	default:
		return nil, fmt.Errorf("unsupported resource type for AWS: %s", resourceType)
	}
}

func (a *AWSDiscovery) listRegions(ctx context.Context) ([]ProviderResource, error) {
	result, err := a.client.DescribeRegions(ctx, &ec2.DescribeRegionsInput{
		AllRegions: aws.Bool(false),
	})
	if err != nil {
		return nil, fmt.Errorf("listing regions: %w", err)
	}

	var resources []ProviderResource
	for _, r := range result.Regions {
		name := aws.ToString(r.RegionName)
		resources = append(resources, ProviderResource{
			Name: name,
			ID:   name,
		})
	}
	return resources, nil
}

func (a *AWSDiscovery) listVPCs(ctx context.Context, region string) ([]ProviderResource, error) {
	client := a.clientForRegion(region)

	result, err := client.DescribeVpcs(ctx, &ec2.DescribeVpcsInput{})
	if err != nil {
		return nil, fmt.Errorf("listing VPCs: %w", err)
	}

	var resources []ProviderResource
	for _, v := range result.Vpcs {
		vpcID := aws.ToString(v.VpcId)
		name := vpcID
		for _, tag := range v.Tags {
			if aws.ToString(tag.Key) == "Name" {
				name = aws.ToString(tag.Value)
				break
			}
		}

		desc := aws.ToString(v.CidrBlock)
		if aws.ToBool(v.IsDefault) {
			desc += " (default)"
		}

		resources = append(resources, ProviderResource{
			Name:        name,
			ID:          vpcID,
			Description: desc,
		})
	}
	return resources, nil
}

func (a *AWSDiscovery) listSubnets(ctx context.Context, vpcID string) ([]ProviderResource, error) {
	if vpcID == "" {
		return nil, fmt.Errorf("VPC ID is required for subnet listing")
	}

	result, err := a.client.DescribeSubnets(ctx, &ec2.DescribeSubnetsInput{
		Filters: []ec2types.Filter{
			{Name: aws.String("vpc-id"), Values: []string{vpcID}},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("listing subnets: %w", err)
	}

	var resources []ProviderResource
	for _, s := range result.Subnets {
		subnetID := aws.ToString(s.SubnetId)
		name := subnetID
		for _, tag := range s.Tags {
			if aws.ToString(tag.Key) == "Name" {
				name = aws.ToString(tag.Value)
				break
			}
		}

		desc := fmt.Sprintf("%s (%s)", aws.ToString(s.CidrBlock), aws.ToString(s.AvailabilityZone))

		resources = append(resources, ProviderResource{
			Name:        name,
			ID:          subnetID,
			Description: desc,
		})
	}
	return resources, nil
}

func (a *AWSDiscovery) listSecurityGroups(ctx context.Context, vpcID string) ([]ProviderResource, error) {
	if vpcID == "" {
		return nil, fmt.Errorf("VPC ID is required for security group listing")
	}

	result, err := a.client.DescribeSecurityGroups(ctx, &ec2.DescribeSecurityGroupsInput{
		Filters: []ec2types.Filter{
			{Name: aws.String("vpc-id"), Values: []string{vpcID}},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("listing security groups: %w", err)
	}

	var resources []ProviderResource
	for _, sg := range result.SecurityGroups {
		sgID := aws.ToString(sg.GroupId)
		name := aws.ToString(sg.GroupName)
		desc := aws.ToString(sg.Description)

		resources = append(resources, ProviderResource{
			Name:        name,
			ID:          sgID,
			Description: desc,
		})
	}
	return resources, nil
}

// clientForRegion returns an EC2 client configured for a specific region.
// If region is empty, it returns the default client.
func (a *AWSDiscovery) clientForRegion(region string) *ec2.Client {
	if region == "" || region == a.region {
		return a.client
	}
	regionCfg := a.cfg.Copy()
	regionCfg.Region = region
	return ec2.NewFromConfig(regionCfg)
}

func (a *AWSDiscovery) listAMIs(ctx context.Context, region string) ([]ProviderResource, error) {
	if region == "" {
		region = a.region
	}
	client := a.clientForRegion(region)
	result, err := client.DescribeImages(ctx, &ec2.DescribeImagesInput{
		Owners: []string{"self"},
	})
	if err != nil {
		return nil, fmt.Errorf("listing AMIs: %w", err)
	}
	var resources []ProviderResource
	for _, img := range result.Images {
		name := ""
		if img.Name != nil {
			name = *img.Name
		}
		id := ""
		if img.ImageId != nil {
			id = *img.ImageId
		}
		desc := ""
		if img.Description != nil {
			desc = *img.Description
		} else if img.Architecture != "" {
			desc = string(img.Architecture)
		}
		resources = append(resources, ProviderResource{
			Name:        name,
			ID:          id,
			Description: desc,
		})
	}
	return resources, nil
}

func (a *AWSDiscovery) SyncImage(_ context.Context, _, _ string) (string, error) {
	return "", ErrNotSupported
}

func (a *AWSDiscovery) PollImageSync(_ context.Context, _ string) (bool, string, error) {
	return false, "", ErrNotSupported
}
