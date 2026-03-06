package main

import (
	"fmt"

	"github.com/pulumi/pulumi-alicloud/sdk/v3/go/alicloud"
	pulumics "github.com/pulumi/pulumi-alicloud/sdk/v3/go/alicloud/cs"
	"github.com/pulumi/pulumi-alicloud/sdk/v3/go/alicloud/elasticsearch"
	"github.com/pulumi/pulumi-alicloud/sdk/v3/go/alicloud/rds"
	"github.com/pulumi/pulumi-alicloud/sdk/v3/go/alicloud/vpc"
	"github.com/pulumi/pulumi-random/sdk/v4/go/random"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi/config"
)

// AliyunProvider implements Aliyun (Alibaba Cloud) infrastructure
type AliyunProvider struct {
	cfg    *config.Config
	aliCfg *alicloud.Provider
}

// Payment type constants (unified configuration values)
const (
	PaymentTypePostPaid = "PostPaid" // Pay-as-you-go
	PaymentTypePrePaid  = "PrePaid"  // Subscription/Prepaid
)

// mapMySQLPaymentType converts unified payment type to MySQL-specific value
func mapMySQLPaymentType(paymentType string) string {
	switch paymentType {
	case PaymentTypePostPaid:
		return "Postpaid" // MySQL uses lowercase
	case PaymentTypePrePaid:
		return "Prepaid" // MySQL uses lowercase
	default:
		// Default to Postpaid if unknown
		return "Postpaid"
	}
}

// mapESPaymentType converts unified payment type to Elasticsearch-specific value
// The new PaymentType field uses: PayAsYouGo, Subscription
func mapESPaymentType(paymentType string) string {
	switch paymentType {
	case PaymentTypePostPaid:
		return "PayAsYouGo"
	case PaymentTypePrePaid:
		return "Subscription"
	default:
		// Default to PayAsYouGo if unknown
		return "PayAsYouGo"
	}
}

// NewAliyunProvider creates an Aliyun (Alibaba Cloud) provider
// Credentials are configured via Pulumi.yaml or environment variables:
// - ALIBABA_CLOUD_ACCESS_KEY_ID / ALIBABA_CLOUD_ACCESS_KEY_SECRET (since v1.228.0)
// - aliyun.accessKey / aliyun.secretKey in Pulumi stack config
func NewAliyunProvider(ctx *pulumi.Context, cfg *config.Config) (*AliyunProvider, error) {
	region := cfg.Require("aliyun.region")
	// Get Aliyun provider credentials from aliyun namespace
	accessKey := cfg.Require("aliyun.accessKey")
	secretKey := cfg.Require("aliyun.secretKey")

	// Create provider with explicit credentials from Pulumi config
	aliCfg, err := alicloud.NewProvider(ctx, "aliyun", &alicloud.ProviderArgs{
		Region:    pulumi.String(region),
		AccessKey: pulumi.String(accessKey),
		SecretKey: pulumi.String(secretKey),
	})
	if err != nil {
		return nil, err
	}

	ctx.Log.Info(fmt.Sprintf("Aliyun provider configured for region: %s", region), &pulumi.LogArgs{})

	return &AliyunProvider{
		cfg:    cfg,
		aliCfg: aliCfg,
	}, nil
}

// DeployInfra deploys Aliyun (Alibaba Cloud) infrastructure
func (p *AliyunProvider) DeployInfra(ctx *pulumi.Context) (*InfraResult, error) {
	result := &InfraResult{}

	// 1. Create VPC
	// VSwitches will be created later based on cluster type requirements
	network, err := p.createNetwork(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to create network: %w", err)
	}
	result.VPCID = network.ID().ToStringOutput()

	// 2. Create Kubernetes cluster (including VSwitches)
	// CreateVSwitchesForCluster will create the required vSwitches based on cluster type
	cluster, clusterEndpoint, kubeconfig, vswitch1, vswitchIDs, clusterName, err := p.createKubernetesCluster(ctx, network)
	if err != nil {
		return nil, fmt.Errorf("failed to create Kubernetes cluster: %w", err)
	}
	// For custom provider (ACK Pro), cluster is nil
	if cluster != nil {
		result.ClusterResource = cluster
		result.ClusterName = pulumi.String(clusterName).ToStringOutput()
	} else {
		// Custom provider was used, set cluster name from config
		result.ClusterName = pulumi.String("ragflow-k8s").ToStringOutput()
	}
	result.ClusterEndpoint = clusterEndpoint
	result.Kubeconfig = kubeconfig
	result.VSwitchIDs = vswitchIDs

	// 3. Create MySQL (if using external MySQL)
	// mysql.external: "false" means create MySQL in k8s (default)
	// mysql.external: "true" means use create MySQL outside k8s
	if p.cfg.GetBool("mysql.external") {
		mysqlEndpoint, mysqlPort, mysqlUser, mysqlPass, err := p.createMySQL(ctx, network, vswitch1)
		if err != nil {
			return nil, fmt.Errorf("failed to create MySQL: %w", err)
		}
		result.MySqlEndpoint = mysqlEndpoint
		result.MySqlPort = mysqlPort
		result.MySqlUsername = mysqlUser
		result.MySqlPassword = mysqlPass
		result.MySqlDatabase = pulumi.String("ragflow").ToStringOutput()
	}

	// 4. Create Elasticsearch (if using external ES)
	// elasticsearch.external: "false" means create Elasticsearch in k8s (default)
	// elasticsearch.external: "true" means create Elasticsearch outside k8s
	if p.cfg.GetBool("elasticsearch.external") {
		esEndpoint, esPort, esProtocol, esUser, esPass, err := p.createElasticsearch(ctx, vswitch1)
		if err != nil {
			return nil, fmt.Errorf("failed to create Elasticsearch: %w", err)
		}
		result.ESEndpoint = esEndpoint
		result.ESPort = esPort
		result.ESProtocol = esProtocol
		result.ESUsername = esUser
		result.ESPassword = esPass
	}

	return result, nil
}

// createNetwork creates VPC only
// VSwitches will be created by CreateVSwitchesForCluster based on cluster type requirements
// Returns: network, error
func (p *AliyunProvider) createNetwork(ctx *pulumi.Context) (*vpc.Network, error) {
	cidr := p.cfg.Get("vpc.cidr")
	if cidr == "" {
		cidr = "10.0.0.0/16" // Default CIDR
	}

	// Create VPC
	network, err := vpc.NewNetwork(ctx, "ragflow-vpc", &vpc.NetworkArgs{
		CidrBlock: pulumi.String(cidr),
	}, pulumi.Provider(p.aliCfg))
	if err != nil {
		return nil, err
	}

	ctx.Log.Info("Successfully created VPC", &pulumi.LogArgs{})
	return network, nil
}

// createKubernetesCluster creates ACK托管集群 or ACK Serverless集群 using official pulumi-alicloud Provider
//
// Supports five cluster types:
// - AckBasic:   cluster.type = "AckBasic"   → ManagedKubernetes + cluster_spec:ack.standard
// - AckPro:     cluster.type = "AckPro"     → ManagedKubernetes + cluster_spec:ack.pro.small
// - AckProAuto: cluster.type = "AckProAuto" → ManagedKubernetes + cluster_spec:ack.pro.small + auto_mode enabled
// - AskBasic:   cluster.type = "AskBasic"   → ServerlessKubernetes + cluster_spec:ack.standard
// - AskPro:     cluster.type = "AskPro"     → ServerlessKubernetes + cluster_spec:ack.pro.small
//
// NOTE: Auto Mode (智能托管模式) is only supported for ACK托管集群 Pro Edition, not ASK Serverless
//
// This function internally calls CreateVSwitchesForCluster to create all required VSwitches
// based on the cluster type.
//
// Returns: cluster (nil), clusterEndpoint, kubeconfig, vswitch1 (for MySQL/ES), vswitchIDs, clusterName, error
func (p *AliyunProvider) createKubernetesCluster(ctx *pulumi.Context, network *vpc.Network) (pulumi.Resource, pulumi.StringOutput, pulumi.StringOutput, *vpc.Switch, pulumi.StringArrayOutput, string, error) {
	// Get cluster type configuration
	clusterType := p.cfg.Get("kubernetes.cluster_type")
	if clusterType == "" {
		clusterType = "AckPro" // Default to ACK Pro
	}

	// Validate cluster type
	supportedTypes := []string{"AckBasic", "AckPro", "AckProAuto", "AskBasic", "AskPro"}
	isSupported := false
	for _, t := range supportedTypes {
		if clusterType == t {
			isSupported = true
			break
		}
	}

	if !isSupported {
		return nil, pulumi.StringOutput{}, pulumi.StringOutput{}, nil, pulumi.StringArrayOutput{}, "", fmt.Errorf("unsupported cluster type: %s (supported types: AckBasic, AckPro, AckProAuto, AskBasic, AskPro)", clusterType)
	}

	version := p.cfg.Get("kubernetes.version")
	if version == "" {
		version = "1.34.3-aliyun.1" // Default stable version
	}

	serviceCidr := p.cfg.Get("kubernetes.service_cidr")
	if serviceCidr == "" {
		serviceCidr = "172.21.0.0/20" // Default Service CIDR
	}

	podCidr := p.cfg.Get("kubernetes.pod_cidr")
	if podCidr == "" {
		podCidr = "10.1.0.0/16" // Default Pod CIDR
	}

	// Create VSwitches based on cluster type (multi-AZ support)
	// Determine number of vSwitches needed based on cluster type
	// AckBasic, AskBasic, AskPro use 2 vSwitches
	// AckPro, AckProAuto needs 3 vSwitches for HA
	numRequested := 2
	if clusterType == "AckPro" || clusterType == "AckProAuto" {
		numRequested = 3
	}

	vswitches, vswitchIDs, err := CreateVSwitchesForCluster(ctx, network, p.aliCfg, numRequested)
	if err != nil {
		return nil, pulumi.StringOutput{}, pulumi.StringOutput{}, nil, pulumi.StringArrayOutput{}, "", fmt.Errorf("failed to create VSwitches: %w", err)
	}

	// Get the first vSwitch for MySQL/ES deployment
	vswitch1 := vswitches[0]

	// Convert vswitchIDs to StringArrayOutput (works for any count: 2, 3, or more)
	outputs := make([]interface{}, len(vswitchIDs))
	for i, vsID := range vswitchIDs {
		outputs[i] = vsID.ToStringOutput()
	}
	vswitchIDsOutput := pulumi.All(outputs...).ApplyT(func(values []interface{}) []string {
		result := make([]string, len(values))
		for i, v := range values {
			result[i] = v.(string)
		}
		return result
	}).(pulumi.StringArrayOutput)

	// Get Aliyun credentials for dynamic queries
	region, accessKey, secretKey, err := GetAliyunCredentials(ctx)
	if err != nil {
		return nil, pulumi.StringOutput{}, pulumi.StringOutput{}, nil, pulumi.StringArrayOutput{}, "", fmt.Errorf("failed to get Aliyun credentials: %w", err)
	}

	// Generate login password (fixed for now - in production use a secret)
	loginPassword := "Ragflow@123456"

	// Stack-based resource naming
	stackName := ctx.Stack()
	pulumiResourceName := fmt.Sprintf("%s-ragflow-k8s", stackName)
	clusterName := pulumiResourceName

	// Log cluster configuration
	ctx.Log.Info(FormatClusterConfig(clusterType, version, serviceCidr, podCidr), &pulumi.LogArgs{})
	ctx.Log.Info(fmt.Sprintf("Stack: %s, cluster name: %s, Pulumi resource: %s", stackName, clusterName, pulumiResourceName), &pulumi.LogArgs{})

	// Create cluster based on type (ACK vs ASK)
	if IsServerlessCluster(clusterType) {
		// ACK Serverless (AskBasic, AskPro) - use cs.ServerlessKubernetes
		cluster, endpoint, kubeconfig, _, err := p.createServerlessCluster(ctx, network, vswitches, vswitchIDs, clusterName, clusterType, version, serviceCidr, region, accessKey, secretKey)
		return cluster, endpoint, kubeconfig, vswitch1, vswitchIDsOutput, clusterName, err
	} else {
		// ACK托管集群 (AckBasic, AckPro, AckProAuto) - use cs.ManagedKubernetes
		cluster, endpoint, kubeconfig, _, err := p.createManagedCluster(ctx, network, vswitches, vswitchIDs, clusterName, clusterType, version, serviceCidr, podCidr, loginPassword, region, accessKey, secretKey)
		return cluster, endpoint, kubeconfig, vswitch1, vswitchIDsOutput, clusterName, err
	}
}

// createManagedOrServerlessCluster creates ACK托管集群 or ACK Serverless集群 using official pulumi-alicloud Provider
// Supports five cluster types: AckBasic, AckPro, AckProAuto, AskBasic, AskPro
//
// Migration to official Provider (2025-02-02):
// - ACK clusters (AckBasic, AckPro, AckProAuto) use cs.ManagedKubernetes
// - ASK clusters (AskBasic, AskPro) use cs.ServerlessKubernetes
// - Custom logic (multi-AZ VSwitch creation) is preserved in helper functions

// createManagedCluster creates ACK托管集群 using official cs.ManagedKubernetes resource
func (p *AliyunProvider) createManagedCluster(
	ctx *pulumi.Context,
	network *vpc.Network,
	vswitches []*vpc.Switch,
	vswitchIDs pulumi.StringArray,
	clusterName string,
	clusterType string,
	version string,
	serviceCidr string,
	podCidr string,
	loginPassword string,
	region string,
	accessKey string,
	secretKey string,
) (pulumi.Resource, pulumi.StringOutput, pulumi.StringOutput, string, error) {

	clusterSpec := MapClusterTypeToSpec(clusterType)
	enableAutoMode := IsAutoModeEnabled(clusterType)

	// Build AutoMode config if enabled
	var autoMode *pulumics.ManagedKubernetesAutoModeArgs
	if enableAutoMode {
		autoMode = &pulumics.ManagedKubernetesAutoModeArgs{
			Enabled: pulumi.Bool(true),
		}
		ctx.Log.Warn("Auto Mode is enabled - kubernetes.version will be managed by Aliyun", &pulumi.LogArgs{})
	}

	// Create explicit security group (managed by Pulumi state)
	// This ensures proper deletion order: security group deleted before VPC
	securityGroup, err := CreateClusterSecurityGroup(ctx, network, p.aliCfg)
	if err != nil {
		return nil, pulumi.StringOutput{}, pulumi.StringOutput{}, "", fmt.Errorf("failed to create security group: %w", err)
	}

	// Create managed cluster using official Provider
	// Note: Official Provider automatically infers VPC from VswitchIds
	addonNames := GetManagedAddons()
	// Convert addon names to ManagedKubernetesAddonArray
	var addons pulumics.ManagedKubernetesAddonArray
	for _, name := range addonNames {
		addons = append(addons, &pulumics.ManagedKubernetesAddonArgs{
			Name: pulumi.String(name),
		})
	}

	cluster, err := pulumics.NewManagedKubernetes(ctx, "ragflow-k8s", &pulumics.ManagedKubernetesArgs{
		Name:               pulumi.String(clusterName),
		VswitchIds:         vswitchIDs,
		ClusterSpec:        pulumi.String(clusterSpec),
		Version:            pulumi.String(version),
		ServiceCidr:        pulumi.String(serviceCidr),
		PodCidr:            pulumi.String(podCidr),
		AutoMode:           autoMode,
		Addons:             addons,
		SecurityGroupId:    securityGroup.ID(), // Use explicit security group
		DeletionProtection: pulumi.Bool(false), // Allow cluster deletion during pulumi destroy
	}, pulumi.Provider(p.aliCfg))

	if err != nil {
		return nil, pulumi.StringOutput{}, pulumi.StringOutput{}, "", fmt.Errorf("failed to create managed cluster: %w", err)
	}

	// Create worker node pool (only for ACK, not ASK)
	err = p.createWorkerNodePool(ctx, cluster, vswitchIDs, loginPassword)
	if err != nil {
		ctx.Log.Warn(fmt.Sprintf("Failed to create worker node pool: %v. Cluster created successfully without worker nodes.", err), &pulumi.LogArgs{})
	}

	// Export cluster ID
	ctx.Export("ragflow-k8s-cluster-id", cluster.ID())

	// Export vSwitch IDs (for ALB configuration in ali_k8s stack)
	ctx.Export("vSwitchIds", vswitchIDs)

	// Dynamically query endpoint and kubeconfig from Aliyun API (NOT stored in Pulumi state)
	clusterID := cluster.ID().ToStringOutput()
	clusterEndpoint := clusterID.ApplyT(func(id string) (string, error) {
		if id == "" {
			return "", nil // Cluster ID is empty during preview - this is expected
		}
		return GetClusterEndpoint(id, region, accessKey, secretKey, false)
	}).(pulumi.StringOutput)

	// Fetch kubeconfig dynamically from Aliyun API
	// Note: Pulumi will automatically handle the dependency and resolve this after cluster creation
	kubeconfig := clusterID.ApplyT(func(id string) (string, error) {
		if id == "" {
			// Cluster ID is not yet available (preview or pending)
			// Return a valid empty kubeconfig YAML - Pulumi will re-run this after cluster is created
			return "", nil
		}
		return GetClusterKubeconfig(id, region, accessKey, secretKey, false)
	}).(pulumi.StringOutput)

	// Return cluster resource for dependency tracking
	return cluster, clusterEndpoint, kubeconfig, clusterName, nil
}

// createServerlessCluster creates ACK Serverless集群 using official cs.ServerlessKubernetes resource
func (p *AliyunProvider) createServerlessCluster(
	ctx *pulumi.Context,
	network *vpc.Network,
	vswitches []*vpc.Switch,
	vswitchIDs pulumi.StringArray,
	clusterName string,
	clusterType string,
	version string,
	serviceCidr string,
	region string,
	accessKey string,
	secretKey string,
) (pulumi.Resource, pulumi.StringOutput, pulumi.StringOutput, string, error) {

	clusterSpec := MapClusterTypeToSpec(clusterType)

	// Create explicit security group (managed by Pulumi state)
	// This ensures proper deletion order: security group deleted before VPC
	securityGroup, err := CreateClusterSecurityGroup(ctx, network, p.aliCfg)
	if err != nil {
		return nil, pulumi.StringOutput{}, pulumi.StringOutput{}, "", fmt.Errorf("failed to create security group: %w", err)
	}

	addonNames := GetManagedAddons()
	// Convert addon names to ManagedKubernetesAddonArray
	var addons pulumics.ServerlessKubernetesAddonArray
	for _, name := range addonNames {
		addons = append(addons, &pulumics.ServerlessKubernetesAddonArgs{
			Name: pulumi.String(name),
		})
	}

	// Create serverless cluster using official Provider with Addons
	// Addons are installed during cluster creation (only works for Create operation)
	cluster, err := pulumics.NewServerlessKubernetes(ctx, "ragflow-k8s", &pulumics.ServerlessKubernetesArgs{
		Name:                        pulumi.String(clusterName),
		VpcId:                       network.ID(),
		VswitchIds:                  vswitchIDs,
		ClusterSpec:                 pulumi.String(clusterSpec),
		Version:                     pulumi.String(version),
		ServiceCidr:                 pulumi.String(serviceCidr),
		SecurityGroupId:             securityGroup.ID(), // Use explicit security group
		NewNatGateway:               pulumi.Bool(true),
		EndpointPublicAccessEnabled: pulumi.Bool(true),
		DeletionProtection:          pulumi.Bool(false), // Allow cluster deletion during pulumi destroy
		Addons:                      addons,
	}, pulumi.Provider(p.aliCfg))

	if err != nil {
		return nil, pulumi.StringOutput{}, pulumi.StringOutput{}, "", fmt.Errorf("failed to create serverless cluster: %w", err)
	}

	// Export cluster ID
	ctx.Export("ragflow-k8s-cluster-id", cluster.ID())

	// Export vSwitch IDs (for ALB configuration in ali_k8s stack)
	ctx.Export("vSwitchIds", vswitchIDs)

	// Dynamically query endpoint and kubeconfig from Aliyun API (NOT stored in Pulumi state)
	clusterID := cluster.ID().ToStringOutput()
	clusterEndpoint := clusterID.ApplyT(func(id string) (string, error) {
		if id == "" {
			return "", nil // Cluster ID is empty during preview - this is expected
		}
		return GetClusterEndpoint(id, region, accessKey, secretKey, false)
	}).(pulumi.StringOutput)

	// Fetch kubeconfig dynamically from Aliyun API
	// Note: Pulumi will automatically handle the dependency and resolve this after cluster creation
	kubeconfig := clusterID.ApplyT(func(id string) (string, error) {
		if id == "" {
			// Cluster ID is not yet available (preview or pending)
			// Return a valid empty kubeconfig YAML - Pulumi will re-run this after cluster is created
			return "", nil
		}
		return GetClusterKubeconfig(id, region, accessKey, secretKey, false)
	}).(pulumi.StringOutput)

	// Return cluster resource for dependency tracking
	return cluster, clusterEndpoint, kubeconfig, clusterName, nil
}

// createWorkerNodePool creates worker nodes for ACK托管集群 using official cs.NodePool resource
func (p *AliyunProvider) createWorkerNodePool(ctx *pulumi.Context, cluster *pulumics.ManagedKubernetes, vswitchIDs pulumi.StringArray, loginPassword string) error {
	stackName := ctx.Stack()
	nodePoolName := fmt.Sprintf("%s-ragflow-nodepool", stackName)

	ctx.Log.Info(fmt.Sprintf("Creating worker node pool '%s' with 3 nodes (ecs.c6.xlarge)", nodePoolName), &pulumi.LogArgs{})

	// Create node pool using official Provider
	_, err := pulumics.NewNodePool(ctx, "ragflow-nodepool", &pulumics.NodePoolArgs{
		ClusterId:  cluster.ID(),
		Name:       pulumi.String("default-pool"),
		VswitchIds: vswitchIDs,
		InstanceTypes: pulumi.StringArray{
			pulumi.String("ecs.c6.xlarge"),
		},
		NodeCount:          pulumi.Int(3),
		Password:           pulumi.String(loginPassword),
		SystemDiskCategory: pulumi.String("cloud_essd"),
		SystemDiskSize:     pulumi.Int(120),
		DataDisks: pulumics.NodePoolDataDiskArray{
			&pulumics.NodePoolDataDiskArgs{
				Category: pulumi.String("cloud_essd"),
				Size:     pulumi.Int(120),
			},
		},
		KeyName: pulumi.String(""),
	}, pulumi.Provider(p.aliCfg))

	if err != nil {
		return fmt.Errorf("failed to create node pool: %w", err)
	}

	ctx.Log.Info("Worker node pool created successfully", &pulumi.LogArgs{})

	// Export node pool ID (for checking if it already exists)
	ctx.Export("ragflow-k8s-nodepool-id", pulumi.String(""))

	return nil
}

// createMySQL creates Aliyun RDS MySQL instance
//
// Args:
//   - network: VPC network
//   - vswitch: vSwitch for RDS deployment
//
// Note: SecurityIps is set to 0.0.0.0/0 to allow all IPs.
// For production, this should be restricted to specific CIDR ranges.
func (p *AliyunProvider) createMySQL(ctx *pulumi.Context, network *vpc.Network, vswitch *vpc.Switch) (
	pulumi.StringOutput, pulumi.IntOutput, pulumi.StringOutput, pulumi.StringOutput, error) {

	// Get configuration
	instanceClass := p.cfg.Get("mysql.node_spec")
	if instanceClass == "" {
		instanceClass = "mysql.x2.large.2c" // Default instance type (see https://help.aliyun.com/zh/rds/apsaradb-rds-for-mysql/primary-apsaradb-rds-for-mysql-instance-types)
	}
	storage := p.cfg.GetInt("mysql.disk_size")
	if storage == 0 {
		storage = 20 // Default: 20GB (min value for Aliyun RDS)
	}
	engine := p.cfg.Get("mysql.engine")
	if engine == "" {
		engine = "MySQL" // Use MySQL instead of mysql8.0
	}
	engineVersion := p.cfg.Get("mysql.engine_version")
	if engineVersion == "" {
		engineVersion = "8.0"
	}

	// Generate random password using ragflow@ + random number
	randomInt, err := random.NewRandomInteger(ctx, fmt.Sprintf("%s-mysql-password", ctx.Stack()), &random.RandomIntegerArgs{
		Min: pulumi.Int(100000),
		Max: pulumi.Int(999999),
	})
	if err != nil {
		return pulumi.StringOutput{}, pulumi.IntOutput{}, pulumi.StringOutput{}, pulumi.StringOutput{}, err
	}
	password := pulumi.Sprintf("ragflow@%d", randomInt.Result)

	// Get payment configuration and map to MySQL-specific value
	paymentType := p.cfg.Get("payment.type")
	if paymentType == "" {
		paymentType = PaymentTypePostPaid // Default: pay-as-you-go
	}
	mysqlPaymentType := mapMySQLPaymentType(paymentType)

	// Get Pod CIDR for RDS SecurityIps (for Pod access)
	// In ACK (Aliyun Container Service for Kubernetes), Pod IPs are allocated from Pod CIDR,
	// which is different from VPC CIDR. We need to add both to RDS SecurityIps.
	podCidr := p.cfg.Get("kubernetes.pod_cidr")
	if podCidr == "" {
		podCidr = "10.1.0.0/16" // Default Pod CIDR (must match cluster configuration)
	}

	// SecurityIps: Allow VPC CIDR + Pod CIDR only
	// - VPC CIDR: for VPC resources (nodes, VSwitches)
	// - Pod CIDR: for K8s Pods (critical! Pods use separate CIDR from VPC)
	// This is secure - only allows traffic from within the VPC and Pod network
	securityIps := pulumi.StringArray{
		network.CidrBlock,      // VPC CIDR for node/VSwitch access
		pulumi.String(podCidr), // Pod CIDR for Pod access (CRITICAL!)
	}
	ctx.Log.Info(fmt.Sprintf("MySQL RDS SecurityIps: VPC CIDR + Pod CIDR (%s)", podCidr), &pulumi.LogArgs{})

	// SSL Configuration: Disable SSL enforcement
	// SslAction: "Close" turns off SSL encryption for RDS
	// This allows clients to connect without SSL using get_server_public_key for caching_sha2_password
	sslAction := pulumi.String("Close") // Disable SSL at RDS instance level
	ctx.Log.Info("MySQL RDS SSL encryption disabled (SslAction=Close). Clients can use get_server_public_key for authentication.", &pulumi.LogArgs{})

	// Build RDS instance args
	instanceArgs := &rds.InstanceArgs{
		Category:              pulumi.String("HighAvailability"),
		Engine:                pulumi.String(engine),
		EngineVersion:         pulumi.String(engineVersion),
		InstanceType:          pulumi.String(instanceClass),
		InstanceStorage:       pulumi.Int(storage),
		InstanceChargeType:    pulumi.String(mysqlPaymentType),
		DbInstanceStorageType: pulumi.String("cloud_ssd"), // Use cloud SSD storage
		VswitchId:             vswitch.ID(),
		SecurityIps:           securityIps,
		SslAction:             sslAction, // Disable SSL encryption
	}

	// For PrePaid subscriptions, add period and period unit
	// IMPORTANT: 关于Aliyun API是否支持PeriodType的说明
	//
	// 证据来源：本地Pulumi SDK代码仓库
	// ================================================
	// 1. RDS Instance (rds.Instance) - 没有PeriodUnit字段
	//    文件: /home/zhichyu/github.com/pulumi/pulumi-alicloud/sdk/go/alicloud/rds/instance.go
	//    代码: Period pulumi.IntPtrInput (line 1664)
	//    注释: "The duration that you will buy DB instance (in month)"
	//    验证: grep -n "PeriodUnit" instance.go -> No matches found
	//    底层API: CreateDBInstance (从注释中的文档链接可以看出)
	//
	// 2. RDS Custom (rds.Custom) - 有PeriodUnit字段
	//    文件: /home/zhichyu/github.com/pulumi/pulumi-alicloud/sdk/go/alicloud/rds/custom.go
	//    代码: PeriodUnit pulumi.StringPtrInput (lines 227, 344, 426, 512, 593)
	//    注释: "The unit of duration of the year-to-month billing method. Value range: Year | Month"
	//    示例: PeriodUnit: pulumi.String("Month") (line 141 in example)
	//    底层API: RunRCInstances (custom.go:19的文档链接)
	//
	// 重要说明:
	// - RDS Instance和RDS Custom是两种不同的资源类型，调用不同的API
	// - RDS Custom有PeriodUnit只能证明RunRCInstances API支持PeriodType
	// - 不能证明CreateDBInstance API支持PeriodType
	// - 因此，对于RDS Instance，必须在应用层将年转换为月
	// 因此必须在应用层将年转换为月
	if paymentType == PaymentTypePrePaid {
		period := p.cfg.GetInt("payment_period")
		if period == 0 {
			period = 1 // Default: 1
		}
		periodUnit := p.cfg.Get("payment_period_unit")
		if periodUnit == "" {
			periodUnit = "Month" // Default: Month
		}

		// Convert to months if unit is Year (Pulumi SDK limitation: no PeriodType field)
		if periodUnit == "Year" {
			period = period * 12
			ctx.Log.Info(fmt.Sprintf("MySQL PrePaid: %d year(s) = %d months (Pulumi SDK limitation: converted to months because SDK doesn't expose PeriodType field)", period/12, period), &pulumi.LogArgs{})
		} else {
			ctx.Log.Info(fmt.Sprintf("MySQL PrePaid: %d month(s)", period), &pulumi.LogArgs{})
		}

		instanceArgs.Period = pulumi.Int(period)
	}

	// Create RDS instance
	instance, err := rds.NewInstance(ctx, fmt.Sprintf("%s-ragflow-mysql", ctx.Stack()), instanceArgs, pulumi.Provider(p.aliCfg))
	if err != nil {
		return pulumi.StringOutput{}, pulumi.IntOutput{}, pulumi.StringOutput{}, pulumi.StringOutput{}, err
	}

	// Create database
	_, err = rds.NewDatabase(ctx, fmt.Sprintf("%s-ragflow-db", ctx.Stack()), &rds.DatabaseArgs{
		InstanceId:   instance.ID(),
		DataBaseName: pulumi.String("ragflow"),
		CharacterSet: pulumi.String("utf8mb4"),
	}, pulumi.Provider(p.aliCfg))
	if err != nil {
		return pulumi.StringOutput{}, pulumi.IntOutput{}, pulumi.StringOutput{}, pulumi.StringOutput{}, err
	}

	// Create account (without AccountPrivilege field - use separate privilege resource)
	account, err := rds.NewAccount(ctx, fmt.Sprintf("%s-ragflow-user", ctx.Stack()), &rds.AccountArgs{
		DbInstanceId:    instance.ID(),
		AccountName:     pulumi.String("root"),
		AccountPassword: password,
	}, pulumi.Provider(p.aliCfg))
	if err != nil {
		return pulumi.StringOutput{}, pulumi.IntOutput{}, pulumi.StringOutput{}, pulumi.StringOutput{}, err
	}

	// Grant database privileges to account
	_, err = rds.NewAccountPrivilege(ctx, fmt.Sprintf("%s-ragflow-user-priv", ctx.Stack()), &rds.AccountPrivilegeArgs{
		InstanceId:  instance.ID(),
		AccountName: account.AccountName,
		Privilege:   pulumi.String("ReadWrite"),
		DbNames:     pulumi.StringArray{pulumi.String("ragflow")},
	}, pulumi.Provider(p.aliCfg))
	if err != nil {
		return pulumi.StringOutput{}, pulumi.IntOutput{}, pulumi.StringOutput{}, pulumi.StringOutput{}, err
	}

	// Use default MySQL port 3306
	port := pulumi.Int(3306).ToIntOutput()

	return instance.ConnectionString, port, account.AccountName, password.ToStringOutput(), nil
}

// createElasticsearch creates Aliyun Elasticsearch instance
//
// Design decision: Elasticsearch uses the same VSwitch as other infrastructure
// resources (VPC, RDS MySQL). The VSwitch zone is explicitly configured in
// Pulumi.ali.yaml via aliyun.zones.
//
// Zone fallback logic is intentionally NOT implemented because:
//  1. All resources should be in the same availability zone for simplified network topology
//  2. Pulumi's deployment model doesn't support runtime retry logic
//  3. If the specified zone has insufficient resources, the user should manually
//     change aliyun.zones to another available zone
//
// This approach follows the same pattern as ROS templates, where resources
// reference VSwitchId rather than specifying zones independently.
func (p *AliyunProvider) createElasticsearch(ctx *pulumi.Context, vswitch *vpc.Switch) (
	pulumi.StringOutput, pulumi.IntOutput, pulumi.StringOutput, pulumi.StringOutput, pulumi.StringOutput, error) {

	// Get configuration
	version := p.cfg.Get("elasticsearch.version")
	if version == "" {
		version = "8.13_with_X-Pack" // Use 8.13 for better .new spec support
	}

	// ES configuration from config
	// IMPORTANT: Use .new suffix specs for better PostPaid availability
	dataNodeSpec := p.cfg.Get("elasticsearch.node_spec")
	if dataNodeSpec == "" {
		dataNodeSpec = "elasticsearch.sn1ne.large.new" // .new suffix for better availability
	}
	dataNodeAmount := p.cfg.GetInt("elasticsearch.node_amount")
	if dataNodeAmount == 0 {
		dataNodeAmount = 3 // Minimum 3 nodes recommended for production
	}
	dataNodeDisk := p.cfg.GetInt("elasticsearch.disk_size")
	if dataNodeDisk == 0 {
		dataNodeDisk = 20 // GB per data node
	}
	dataNodeDiskType := p.cfg.Get("elasticsearch.disk_type")
	if dataNodeDiskType == "" {
		dataNodeDiskType = "cloud_essd" // Default to ESSD for ES
	}

	// Get performance level for ESSD (required when using cloud_essd)
	dataNodePerformanceLevel := p.cfg.Get("elasticsearch.disk_performance_level")
	if dataNodePerformanceLevel == "" {
		dataNodePerformanceLevel = "PL1" // Default: PL1 (performance level 1)
	}

	kibanaSpec := "elasticsearch.sn1ne.large" // Use sn1ne.large for better availability
	kibanaAmount := 1
	kibanaDisk := 0 // 0 (no dedicated disk)

	// Generate random password using random number
	randomInt, err := random.NewRandomInteger(ctx, fmt.Sprintf("%s-es-password", ctx.Stack()), &random.RandomIntegerArgs{
		Min: pulumi.Int(100000),
		Max: pulumi.Int(999999),
	})
	if err != nil {
		return pulumi.StringOutput{}, pulumi.IntOutput{}, pulumi.StringOutput{}, pulumi.StringOutput{}, pulumi.StringOutput{}, err
	}
	password := pulumi.Sprintf("ES@%d", randomInt.Result)

	// Get payment configuration and map to ES-specific value
	paymentType := p.cfg.Get("payment.type")
	if paymentType == "" {
		paymentType = PaymentTypePostPaid // Default: pay-as-you-go
	}
	esPaymentType := mapESPaymentType(paymentType)

	ctx.Log.Info(fmt.Sprintf("Creating Elasticsearch with payment config: %s -> %s",
		paymentType, esPaymentType), &pulumi.LogArgs{})
	ctx.Log.Info(fmt.Sprintf("Spec: %s, %d data nodes, %d GB disk, %s/%s",
		dataNodeSpec, dataNodeAmount, dataNodeDisk, dataNodeDiskType, dataNodePerformanceLevel), &pulumi.LogArgs{})
	ctx.Log.Info(fmt.Sprintf("Kibana: spec=%s, amount=%d, disk=%d GB",
		kibanaSpec, kibanaAmount, kibanaDisk), &pulumi.LogArgs{})

	// Create ES instance using the provided VSwitch
	esArgs := &elasticsearch.InstanceArgs{
		Description: pulumi.String("RAGFlow Elasticsearch Cluster"),
		VswitchId:   vswitch.ID(), // Use the same VSwitch as other resources
		Version:     pulumi.String(version),
		Password:    password,
		PaymentType: pulumi.String(esPaymentType), // Use new field replacing deprecated InstanceChargeType
		// Data node configuration
		DataNodeConfiguration: &elasticsearch.InstanceDataNodeConfigurationArgs{
			Amount:           pulumi.Int(dataNodeAmount),
			Disk:             pulumi.Int(dataNodeDisk),
			DiskType:         pulumi.String(dataNodeDiskType),
			Spec:             pulumi.String(dataNodeSpec),
			PerformanceLevel: pulumi.String(dataNodePerformanceLevel), // ESSD performance level (PL0-PL3)
		},
		// Kibana configuration
		KibanaConfiguration: &elasticsearch.InstanceKibanaConfigurationArgs{
			Amount: pulumi.Int(kibanaAmount),
			Disk:   pulumi.Int(kibanaDisk),
			Spec:   pulumi.String(kibanaSpec),
		},
		EnablePublic: pulumi.Bool(false),
	}

	// For PrePaid/Subscription payment, add period
	// IMPORTANT: Aliyun底层API支持PeriodType (Month/Year)，但Pulumi Elasticsearch SDK没有暴露此字段
	//
	// 证据来源：本地Pulumi SDK代码仓库
	// ================================================
	// Elasticsearch Instance (elasticsearch.InstanceArgs) - 没有PeriodUnit字段
	// 文件: /home/zhichyu/github.com/pulumi/pulumi-alicloud/sdk/go/alicloud/elasticsearch/instance.go
	// 代码: Period *int (line 409)
	// 注释: "The duration that you will buy Elasticsearch instance (in month)"
	// 验证: grep -n "PeriodUnit" instance.go -> No matches found
	//
	// 结论: Pulumi Elasticsearch SDK只暴露了Period字段（单位：月），没有PeriodUnit字段
	//       必须在应用层将年转换为月
	// 因此必须在应用层将年转换为月
	if paymentType == PaymentTypePrePaid {
		period := p.cfg.GetInt("payment_period")
		if period == 0 {
			period = 1 // Default: 1
		}
		periodUnit := p.cfg.Get("payment_period_unit")
		if periodUnit == "" {
			periodUnit = "Month" // Default: Month
		}

		// Convert to months if unit is Year (Pulumi SDK limitation: no PeriodType field)
		if periodUnit == "Year" {
			period = period * 12
			ctx.Log.Info(fmt.Sprintf("Elasticsearch PrePaid: %d year(s) = %d months (Pulumi SDK limitation: converted to months because SDK doesn't expose PeriodType field)", period/12, period), &pulumi.LogArgs{})
		} else {
			ctx.Log.Info(fmt.Sprintf("Elasticsearch PrePaid: %d month(s)", period), &pulumi.LogArgs{})
		}

		esArgs.Period = pulumi.Int(period)
	}

	esInstance, err := elasticsearch.NewInstance(ctx, "ragflow-es", esArgs,
		pulumi.Provider(p.aliCfg))

	if err != nil {
		return pulumi.StringOutput{}, pulumi.IntOutput{}, pulumi.StringOutput{}, pulumi.StringOutput{}, pulumi.StringOutput{},
			fmt.Errorf("failed to create Elasticsearch: %w\n\n"+
				"TIP: If the error indicates insufficient resources in the zone,\n"+
				"change aliyun.zones in Pulumi.ali.yaml to another available zone.", err)
	}

	ctx.Log.Info("✓ Elasticsearch created successfully", &pulumi.LogArgs{})

	port := pulumi.Int(9200).ToIntOutput()
	protocol := pulumi.String("http").ToStringOutput() // Aliyun ES uses HTTP by default
	return esInstance.Domain, port, protocol, pulumi.String("elastic").ToStringOutput(), password.ToStringOutput(), nil
}

// GetRegion returns the deployment region
func (p *AliyunProvider) GetRegion() string {
	return p.cfg.Require("aliyun.region")
}

// ValidateConfig validates configuration completeness
func (p *AliyunProvider) ValidateConfig() error {
	region := p.cfg.Get("aliyun.region")
	if region == "" {
		return fmt.Errorf("missing required configuration: aliyun.region")
	}
	return nil
}
