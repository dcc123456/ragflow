package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	cs "github.com/alibabacloud-go/cs-20151215/client"
	"github.com/alibabacloud-go/tea-roa/client"
	"github.com/alibabacloud-go/tea-utils/service"
	"github.com/alibabacloud-go/tea/tea"
	"github.com/pulumi/pulumi-alicloud/sdk/v3/go/alicloud"
	"github.com/pulumi/pulumi-alicloud/sdk/v3/go/alicloud/ecs"
	"github.com/pulumi/pulumi-alicloud/sdk/v3/go/alicloud/vpc"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi/config"
)

// CreateClusterSecurityGroup creates an explicit enterprise security group for ACK/ASK clusters
//
// This function creates a security group that is managed by Pulumi state, ensuring
// proper deletion order (security group deleted before VPC).
//
// Security rules:
// - Ingress: Allow all traffic within the security group (for cluster communication)
// - Egress: Allow all outbound traffic
//
// Returns: Security group resource and error
func CreateClusterSecurityGroup(
	ctx *pulumi.Context,
	network *vpc.Network,
	aliCfg *alicloud.Provider,
) (*ecs.SecurityGroup, error) {
	ctx.Log.Info("Creating explicit enterprise security group for cluster", &pulumi.LogArgs{})

	securityGroup, err := ecs.NewSecurityGroup(ctx, "ragflow-cluster-security-group", &ecs.SecurityGroupArgs{
		VpcId:       network.ID(),
		Description: pulumi.String("Enterprise security group for RAGFlow ACK/ASK cluster"),
		// Set to true for enterprise security group
		// Note: This is the default behavior for ACK clusters
	}, pulumi.Provider(aliCfg))
	if err != nil {
		return nil, fmt.Errorf("failed to create security group: %w", err)
	}

	ctx.Log.Info("✓ Security group created successfully", &pulumi.LogArgs{})

	return securityGroup, nil
}

// CreateVSwitchesForCluster creates the requested number of VSwitches in different zones
//
// This function reads zones from the required aliyun.zones configuration.
// Zones must be manually specified to ensure ABSOLUTE stability across deployments.
// No automatic zone detection or stock querying is performed.
//
// IMPORTANT: Zones must be configured in Pulumi.<stack>.yaml:
//
//	aliyun.zones: cn-shanghai-b,cn-shanghai-e
//
// Parameters:
//   - numRequested: Number of vSwitches to create (typically 2 or 3 for multi-AZ)
//
// Returns: vswitches, vswitchIDs, error
func CreateVSwitchesForCluster(
	ctx *pulumi.Context,
	network *vpc.Network,
	aliCfg *alicloud.Provider,
	numRequested int,
) ([]*vpc.Switch, pulumi.StringArray, error) {

	if numRequested < 2 || numRequested > 3 {
		return nil, nil, fmt.Errorf("unsupported vSwitch count: %d (supported: 2-3)", numRequested)
	}

	ctx.Log.Info(fmt.Sprintf("Creating %d VSwitches in configured zones for high availability", numRequested), &pulumi.LogArgs{})

	// Read zones from config (required, no auto-detection)
	zoneOutputs, zoneStrings, err := readZonesFromConfig(ctx, numRequested)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to read zones configuration: %w", err)
	}

	// Create vSwitches in the configured zones
	var vswitches []*vpc.Switch
	var vswitchIDs pulumi.StringArray

	for i := 0; i < numRequested; i++ {
		vswitchName := fmt.Sprintf("ragflow-vswitch-%d", i+1)
		vswitchCidr := fmt.Sprintf("10.0.%d.0/24", 3+i) // 10.0.3.0/24, 10.0.4.0/24, 10.0.5.0/24

		ctx.Log.Info(fmt.Sprintf("Creating %s in zone: %s", vswitchName, zoneStrings[i]), &pulumi.LogArgs{})

		vswitch, err := vpc.NewSwitch(ctx, vswitchName, &vpc.SwitchArgs{
			VpcId:     network.ID(),
			CidrBlock: pulumi.String(vswitchCidr),
			ZoneId:    zoneOutputs[i], // Use pulumi.StringOutput directly
		}, pulumi.Provider(aliCfg))
		if err != nil {
			return nil, nil, fmt.Errorf("failed to create %s: %w", vswitchName, err)
		}

		vswitches = append(vswitches, vswitch)
		vswitchIDs = append(vswitchIDs, vswitch.ID())
	}

	ctx.Log.Info(fmt.Sprintf("Successfully created %d vSwitches in configured zones", numRequested), &pulumi.LogArgs{})

	return vswitches, vswitchIDs, nil
}

// readZonesFromConfig reads zones from the required aliyun.zones configuration
// and validates that the specified number of zones are available.
//
// This function performs NO automatic zone detection or stock querying.
// Zones must be manually specified in the configuration to ensure stability.
//
// Configuration example:
//
//	aliyun.zones: cn-shanghai-b,cn-shanghai-e
//
// Parameters:
//   - numRequested: Number of zones required (2 or 3)
//
// Returns: []pulumi.StringOutput, []string (zone IDs), error
func readZonesFromConfig(ctx *pulumi.Context, numRequested int) ([]pulumi.StringOutput, []string, error) {
	cfg := config.New(ctx, "")

	// Read zones from config (REQUIRED)
	zonesStr := cfg.Require("aliyun.zones")
	if zonesStr == "" {
		return nil, nil, fmt.Errorf("aliyun.zones is required but not set. "+
			"Please specify at least %d zones in your Pulumi.<stack>.yaml", numRequested)
	}

	// Parse comma-separated zones
	zones := strings.Split(zonesStr, ",")
	var configuredZones []string
	for _, zone := range zones {
		zone = strings.TrimSpace(zone)
		if zone != "" {
			configuredZones = append(configuredZones, zone)
		}
	}

	// Validate minimum zone count
	if len(configuredZones) < numRequested {
		return nil, nil, fmt.Errorf("zones requires at least %d zones, but only %d configured: %v",
			numRequested, len(configuredZones), configuredZones)
	}

	// Log configured zones
	ctx.Log.Info(fmt.Sprintf("Using %d manually configured zones: %v", numRequested, configuredZones[:numRequested]), &pulumi.LogArgs{})

	// Use the first N zones (in the order specified by user)
	selectedZones := configuredZones[:numRequested]

	// Convert to pulumi.StringOutput array
	zoneOutputs := make([]pulumi.StringOutput, numRequested)
	for i, zone := range selectedZones {
		zoneOutputs[i] = pulumi.String(zone).ToStringOutput()
	}

	return zoneOutputs, selectedZones, nil
}

// MapClusterTypeToSpec maps cluster type to Aliyun ClusterSpec
// - AckBasic -> ack.standard
// - AckPro, AckProAuto -> ack.pro.small
// - AskBasic -> ack.standard
// - AskPro -> ack.pro.small
func MapClusterTypeToSpec(clusterType string) string {
	switch clusterType {
	case "AckBasic", "AskBasic":
		return "ack.standard" // Basic Edition
	case "AckPro", "AckProAuto", "AskPro":
		return "ack.pro.small" // Pro Edition
	default:
		// Default to ack.pro.small for backward compatibility
		return "ack.pro.small"
	}
}

// IsAutoModeEnabled checks if Auto Mode (智能托管模式) should be enabled
// Auto Mode is only supported for AckProAuto
func IsAutoModeEnabled(clusterType string) bool {
	return clusterType == "AckProAuto"
}

// IsServerlessCluster checks if the cluster type is serverless (ASK)
func IsServerlessCluster(clusterType string) bool {
	return clusterType == "AskBasic" || clusterType == "AskPro"
}

// GetManagedAddons returns the list of managed addon names to install
// These addons are automatically managed by Aliyun
//
// For ACK (ManagedKubernetes): use names directly with ManagedKubernetesAddonArgs
// For ASK (ServerlessKubernetes): use names directly with ServerlessKubernetesAddonArgs
//
// Reference: https://help.aliyun.com/zh/ack/ack-managed-and-ack-dedicated/user-guide/component-overview
func GetManagedAddons() []string {
	// Common addons for both ACK and ASK
	return []string{
		"coredns",                // DNS service discovery (works for both)
		"alb-ingress-controller", // ALB Ingress routing (works for both)
		"gateway-api",            // Gateway API support (works for both)
		"csi-provisioner",        // Dynamic storage
		"knative",                // Serverless platform (optional)
	}
}

// GetClusterTypeName returns the human-readable cluster type name for logging
func GetClusterTypeName(clusterType string) string {
	switch clusterType {
	case "AckBasic":
		return "ACK托管集群基础版"
	case "AckPro":
		return "ACK托管集群Pro版"
	case "AckProAuto":
		return "ACK托管集群Pro版 (智能托管模式)"
	case "AskBasic":
		return "ACK Serverless集群基础版"
	case "AskPro":
		return "ACK Serverless集群Pro版"
	default:
		return "Unknown"
	}
}

// GetAliyunCredentials retrieves Aliyun credentials from Pulumi config
func GetAliyunCredentials(ctx *pulumi.Context) (region, accessKey, secretKey string, err error) {
	cfg := config.New(ctx, "")
	region = cfg.Require("aliyun.region")
	accessKey = cfg.Require("aliyun.accessKey")
	secretKey = cfg.Require("aliyun.secretKey")
	return region, accessKey, secretKey, nil
}

// FormatClusterConfig formats the cluster configuration for logging
func FormatClusterConfig(clusterType, version, serviceCidr, podCidr string) string {
	clusterTypeName := GetClusterTypeName(clusterType)
	autoModeInfo := ""
	if IsAutoModeEnabled(clusterType) {
		autoModeInfo = " [Auto Mode: enabled]"
	}

	return fmt.Sprintf("%s%s (version: %s, service_cidr: %s, pod_cidr: %s)",
		clusterTypeName, autoModeInfo, version, serviceCidr, podCidr)
}

// extractEndpointFromKubeconfig extracts the API server endpoint from kubeconfig YAML
func extractEndpointFromKubeconfig(kubeconfig string) (string, error) {
	if kubeconfig == "" {
		return "", fmt.Errorf("kubeconfig is empty")
	}

	lines := strings.Split(kubeconfig, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "server:") {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				server := strings.TrimSpace(parts[1])
				server = strings.Trim(server, `"`)
				return server, nil
			}
		}
	}

	return "", fmt.Errorf("could not find server endpoint in kubeconfig")
}

// GetClusterEndpoint retrieves the current cluster endpoint from Aliyun API
// This function queries the endpoint dynamically instead of storing it in Pulumi state,
// avoiding state sync issues when the cluster is recreated or endpoint changes.
//
// Usage pattern:
//
//	endpoint, err := GetClusterEndpoint(clusterID, region, accessKey, secretKey, false)
//	if err != nil {
//	    return fmt.Errorf("failed to get cluster endpoint: %w", err)
//	}
//	// Use endpoint for API calls or testing
//
// Parameters:
//   - clusterID: The Aliyun ACK cluster ID
//   - region: The Aliyun region (e.g., "cn-hangzhou")
//   - accessKey: Aliyun AccessKey ID
//   - secretKey: Aliyun AccessKey Secret
//   - usePrivateIP: If true, returns private endpoint; if false, returns public endpoint
//
// Returns:
//   - string: The cluster API server endpoint URL (e.g., "https://139.224.42.216:6443")
//   - error: Error if the query fails
func GetClusterEndpoint(clusterID, region, accessKey, secretKey string, usePrivateIP bool) (string, error) {
	// Create CS client
	config := &client.Config{
		AccessKeyId:     tea.String(accessKey),
		AccessKeySecret: tea.String(secretKey),
		RegionId:        tea.String(region),
		Endpoint:        tea.String("cs.aliyuncs.com"),
	}

	csClient, err := cs.NewClient(config)
	if err != nil {
		return "", fmt.Errorf("failed to create CS client: %w", err)
	}

	// Get kubeconfig with specified endpoint type
	request := &cs.DescribeClusterUserKubeconfigRequest{}
	query := &cs.DescribeClusterUserKubeconfigQuery{}
	query.SetPrivateIpAddress(usePrivateIP)
	request.SetQuery(query)

	runtime := &service.RuntimeOptions{}

	response, err := csClient.DescribeClusterUserKubeconfigWithOptions(tea.String(clusterID), request, runtime)
	if err != nil {
		return "", fmt.Errorf("failed to get kubeconfig: %w", err)
	}

	if response.Body == nil || response.Body.Config == nil {
		return "", fmt.Errorf("kubeconfig is empty")
	}

	kubeconfig := *response.Body.Config

	// Extract endpoint from kubeconfig
	endpoint, err := extractEndpointFromKubeconfig(kubeconfig)
	if err != nil {
		return "", fmt.Errorf("failed to extract endpoint from kubeconfig: %w", err)
	}

	return endpoint, nil
}

// GetClusterKubeconfig retrieves the current cluster kubeconfig from Aliyun API
// This function queries the kubeconfig dynamically instead of storing it in Pulumi state,
// avoiding state sync issues when the cluster is recreated or endpoint changes.
//
// Usage pattern:
//
//	kubeconfig, err := GetClusterKubeconfig(clusterID, region, accessKey, secretKey, false)
//	if err != nil {
//	    return fmt.Errorf("failed to get cluster kubeconfig: %w", err)
//	}
//	// Write kubeconfig to file for kubectl use
//	os.WriteFile("/tmp/kubeconfig", []byte(kubeconfig), 0600)
//
// Parameters:
//   - clusterID: The Aliyun ACK cluster ID
//   - region: The Aliyun region (e.g., "cn-hangzhou")
//   - accessKey: Aliyun AccessKey ID
//   - secretKey: Aliyun AccessKey Secret
//   - usePrivateIP: If true, returns kubeconfig with private endpoint; if false, public endpoint
//
// Returns:
//   - string: The cluster kubeconfig YAML
//   - error: Error if the query fails
func GetClusterKubeconfig(clusterID, region, accessKey, secretKey string, usePrivateIP bool) (string, error) {
	// Create CS client
	config := &client.Config{
		AccessKeyId:     tea.String(accessKey),
		AccessKeySecret: tea.String(secretKey),
		RegionId:        tea.String(region),
		Endpoint:        tea.String("cs.aliyuncs.com"),
	}

	csClient, err := cs.NewClient(config)
	if err != nil {
		return "", fmt.Errorf("failed to create CS client: %w", err)
	}

	// Get kubeconfig with specified endpoint type
	request := &cs.DescribeClusterUserKubeconfigRequest{}
	query := &cs.DescribeClusterUserKubeconfigQuery{}
	query.SetPrivateIpAddress(usePrivateIP)
	request.SetQuery(query)

	runtime := &service.RuntimeOptions{}

	response, err := csClient.DescribeClusterUserKubeconfigWithOptions(tea.String(clusterID), request, runtime)
	if err != nil {
		return "", fmt.Errorf("failed to get kubeconfig: %w", err)
	}

	if response.Body == nil || response.Body.Config == nil {
		return "", fmt.Errorf("kubeconfig is empty")
	}

	kubeconfig := *response.Body.Config
	return kubeconfig, nil
}

// InstallManagedAddons installs managed addons (CoreDNS, ALB Ingress Controller, Gateway API) for ACK and ASK clusters
// This is a unified function that works for both cluster types
//
// Reference: https://help.aliyun.com/zh/ack/ack-managed-and-ack-dedicated/developer-reference/api-cs-2015-12-15-installclusteraddons
func InstallManagedAddons(ctx *pulumi.Context, csClient *cs.Client, clusterID string, region string) error {
	// List available addons to get correct versions
	ctx.Log.Info(fmt.Sprintf("Querying available addons for cluster %s in region %s", clusterID, region), &pulumi.LogArgs{})

	addons, err := ListAvailableAddons(ctx, csClient, clusterID)
	if err != nil {
		return fmt.Errorf("failed to list available addons: %w", err)
	}

	// Define managed addons to install
	// Note: Addons that are not available will be automatically skipped (see loop below)
	managedAddons := []struct {
		name    string
		version string
		config  string
	}{
		{
			name:    "coredns",
			version: GetAddonVersion(addons, "coredns", ""),
			config:  "",
		},
		{
			name:    "alb-ingress-controller",
			version: GetAddonVersion(addons, "alb-ingress-controller", ""),
			config:  "",
		},
		{
			name:    "gateway-api",
			version: GetAddonVersion(addons, "gateway-api", ""),
			config:  "",
		},
		{
			name:    "csi-provisioner",
			version: GetAddonVersion(addons, "csi-provisioner", ""),
			config:  "",
		},
	}

	// Build request body
	var addonList []interface{}
	for _, addon := range managedAddons {
		// Skip if addon is not available
		if addon.version == "" {
			ctx.Log.Info(fmt.Sprintf("Addon %s is not available for this cluster, skipping", addon.name), &pulumi.LogArgs{})
			continue
		}

		addonMap := map[string]interface{}{
			"name":    addon.name,
			"version": addon.version,
		}
		if addon.config != "" {
			addonMap["config"] = addon.config
		}
		addonList = append(addonList, addonMap)
		ctx.Log.Info(fmt.Sprintf("Will install addon: %s (version: %s)", addon.name, addon.version), &pulumi.LogArgs{})
	}

	if len(addonList) == 0 {
		return fmt.Errorf("no managed addons available to install")
	}

	// Install addons using InstallClusterAddons API
	return InstallAddons(ctx, csClient, clusterID, addonList)
}

// ListAvailableAddons queries available addons for the cluster using ListAddons API
// This works for both ACK and ASK clusters
//
// Reference: https://help.aliyun.com/zh/ack/serverless-kubernetes/developer-reference/api-cs-2015-12-15-listaddons-serverless
func ListAvailableAddons(ctx *pulumi.Context, csClient *cs.Client, clusterID string) ([]map[string]interface{}, error) {
	runtime := &service.RuntimeOptions{}

	// Build query parameters - pass cluster_id as query parameter
	query := map[string]*string{
		"cluster_id": tea.String(clusterID),
	}

	// Use the correct API path: /api/v1/clusters (ListAddons action)
	respMap, err := csClient.Client.DoRequestWithAction(
		tea.String("ListAddons"),
		tea.String("2015-12-15"),
		tea.String("HTTPS"),
		tea.String("GET"),
		tea.String("AK"),
		tea.String("/api/v1/clusters"),
		query,
		map[string]*string{}, // headers
		nil,                  // body
		runtime,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to list addons: %w", err)
	}

	// Parse response
	respJSON, err := json.Marshal(respMap)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal response: %w", err)
	}

	var result struct {
		Addons []map[string]interface{} `json:"addons"`
	}
	err = json.Unmarshal(respJSON, &result)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal addons: %w", err)
	}

	ctx.Log.Info(fmt.Sprintf("Found %d available addons", len(result.Addons)), &pulumi.LogArgs{})
	return result.Addons, nil
}

// GetAddonVersion finds the version for a specific addon from the available addons list
func GetAddonVersion(addons []map[string]interface{}, addonName string, defaultVersion string) string {
	for _, addon := range addons {
		if name, ok := addon["name"].(string); ok && name == addonName {
			if version, ok := addon["version"].(string); ok {
				return version
			}
		}
	}
	return defaultVersion
}

// InstallAddons installs addons using InstallClusterAddons API
// This works for both ACK and ASK clusters
//
// Reference: https://help.aliyun.com/zh/ack/ack-managed-and-ack-dedicated/developer-reference/api-cs-2015-12-15-installclusteraddons
func InstallAddons(ctx *pulumi.Context, csClient *cs.Client, clusterID string, addons []interface{}) error {
	runtime := &service.RuntimeOptions{}

	// Build request body
	bodyStr, err := json.Marshal(addons)
	if err != nil {
		return fmt.Errorf("failed to marshal addons: %w", err)
	}

	// Use DoRequestWithAction method
	respMap, err := csClient.Client.DoRequestWithAction(
		tea.String("InstallClusterAddons"),
		tea.String("2015-12-15"),
		tea.String("HTTPS"),
		tea.String("POST"),
		tea.String("AK"),
		tea.String("/clusters/"+clusterID+"/components/install"),
		nil,                  // query
		map[string]*string{}, // headers
		bodyStr,              // body
		runtime,
	)
	if err != nil {
		return fmt.Errorf("failed to install addons: %w", err)
	}

	// Parse response to get task ID
	respJSON, err := json.Marshal(respMap)
	if err != nil {
		return err
	}

	var result struct {
		TaskID string `json:"task_id"`
	}
	err = json.Unmarshal(respJSON, &result)
	if err != nil {
		return err
	}

	if result.TaskID == "" {
		return fmt.Errorf("invalid response from InstallClusterAddons API: no task_id")
	}

	ctx.Log.Info(fmt.Sprintf("Addons installation task started: %s", result.TaskID), &pulumi.LogArgs{})

	// Poll for task completion
	_, err = PollClusterTask(ctx, csClient, result.TaskID)
	if err != nil {
		return fmt.Errorf("addons installation failed: %w", err)
	}

	ctx.Log.Info("✓ All managed addons installed successfully", &pulumi.LogArgs{})
	return nil
}

// PollClusterTask polls the task status until completion
// This works for both ACK and ASK clusters
func PollClusterTask(ctx *pulumi.Context, csClient *cs.Client, taskID string) (string, error) {
	ctx.Log.Info(fmt.Sprintf("Polling task %s (interval: 10s, timeout: 30m0s)", taskID), &pulumi.LogArgs{})

	timeout := 30 * time.Minute
	interval := 10 * time.Second
	startTime := time.Now()

	for time.Since(startTime) < timeout {
		request := &cs.DescribeTaskInfoRequest{}
		runtime := &service.RuntimeOptions{}

		response, err := csClient.DescribeTaskInfoWithOptions(tea.String(taskID), request, runtime)
		if err != nil {
			ctx.Log.Warn(fmt.Sprintf("Failed to describe task: %v, retrying...", err), &pulumi.LogArgs{})
			time.Sleep(interval)
			continue
		}

		if response.Body == nil || response.Body.State == nil {
			ctx.Log.Warn("Task response missing state, retrying...", &pulumi.LogArgs{})
			time.Sleep(interval)
			continue
		}

		taskState := *response.Body.State
		ctx.Log.Info(fmt.Sprintf("Task state: %s", taskState), &pulumi.LogArgs{})

		lowerState := strings.ToLower(taskState)
		if lowerState == "ok" || lowerState == "final" || lowerState == "success" || lowerState == "succeeded" {
			if response.Body.ClusterId != nil && *response.Body.ClusterId != "" {
				ctx.Log.Info(fmt.Sprintf("✓ Addons installation completed successfully! Cluster ID: %s", *response.Body.ClusterId), &pulumi.LogArgs{})
				return *response.Body.ClusterId, nil
			}
			return "", nil
		}

		if lowerState == "failed" || lowerState == "error" {
			return "", fmt.Errorf("task failed: %s", taskState)
		}

		if lowerState == "pending" || lowerState == "running" || lowerState == "initializing" {
			ctx.Log.Info(fmt.Sprintf("Task is %s, waiting...", taskState), &pulumi.LogArgs{})
			time.Sleep(interval)
			continue
		}

		ctx.Log.Warn(fmt.Sprintf("Unknown task state: %s, waiting...", taskState), &pulumi.LogArgs{})
		time.Sleep(interval)
	}

	return "", fmt.Errorf("task timeout after %v", timeout)
}
