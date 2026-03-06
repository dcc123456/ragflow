package main

import (
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

// CloudProvider interface abstracts infrastructure deployment across different cloud providers
type CloudProvider interface {
	// DeployInfra deploys cloud infrastructure
	DeployInfra(ctx *pulumi.Context) (*InfraResult, error)

	// GetRegion returns the deployment region
	GetRegion() string

	// ValidateConfig validates configuration completeness
	ValidateConfig() error
}

// InfraResult holds infrastructure deployment results
type InfraResult struct {
	// Kubernetes cluster resource (for dependency tracking)
	ClusterResource pulumi.Resource

	// Kubernetes cluster configuration
	Kubeconfig      pulumi.StringOutput
	ClusterName     pulumi.StringOutput
	ClusterEndpoint pulumi.StringOutput

	// MySQL configuration
	MySqlEndpoint   pulumi.StringOutput
	MySqlPort       pulumi.IntOutput
	MySqlDatabase   pulumi.StringOutput
	MySqlUsername   pulumi.StringOutput
	MySqlPassword   pulumi.StringOutput

	// Elasticsearch configuration
	ESEndpoint      pulumi.StringOutput
	ESPort          pulumi.IntOutput
	ESProtocol      pulumi.StringOutput
	ESUsername      pulumi.StringOutput
	ESPassword      pulumi.StringOutput

	// Network configuration
	VPCID           pulumi.StringOutput
	VSwitchIDs      pulumi.StringArrayOutput
}
