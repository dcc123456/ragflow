package main

// main.go implements RAGFlow deployment supporting two phases:
// Phase 1 (default): Creates infrastructure (VPC, VSwitches, ACK cluster)
// Phase 2 (phase: k8s): Creates Kubernetes resources only
//
// Usage:
//   pulumi up -s ali              # Phase 1: Infrastructure only
//   pulumi up -s ali_k8s          # Phase 2: K8s resources (set phase: k8s in config)

// https://www.pulumi.com/docs/iac/guides/building-extending/components/build-a-component/

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"

	k8smetav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes"
	apiextensions "github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/apiextensions"
	v1 "github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/apps/v1"
	corev1 "github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/core/v1"
	metav1 "github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/meta/v1"
	networkingv1 "github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/networking/v1"
	"github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/yaml"

	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
	pulumiconfig "github.com/pulumi/pulumi/sdk/v3/go/pulumi/config"
)

// RAGFlowConfig holds RAGFlow-specific configuration
type RAGFlowConfig struct {
	Image    string
	Replicas int
	Service  ServiceConfig
	API      APIConfig
}

// ImageConfig holds all container image URLs
type ImageConfig struct {
	MySQL         string
	Redis         string
	TEI           string
	RabbitMQ      string
	Curl          string
	AWSCLI        string
	Elasticsearch string // Optional: only used when kubernetes.use_public_registry is false
}

// ServiceConfig holds service configuration
type ServiceConfig struct {
	Type    string
	Enabled bool
}

// APIConfig holds API service configuration
type APIConfig struct {
	Service ServiceConfig
}

// GatewayConfig holds Gateway configuration
type GatewayConfig struct {
	ClassName   string
	Namespace   string
	Annotations map[string]string
	Hosts       []GatewayHost
	TLS         []GatewayTLS
}

// GatewayHost holds Gateway host configuration
type GatewayHost struct {
	Paths []GatewayPath
}

// GatewayPath holds Gateway path configuration
type GatewayPath struct {
	Path     string
	PathType string
}

// GatewayTLS holds Gateway TLS configuration
type GatewayTLS struct {
	Hosts      []string
	SecretName string
}

// GPUConfig holds GPU service configuration
type GPUConfig struct {
	UseGPU         bool // Use GPU (HAMi vGPU for sharing)
	Replicas       int
	Image          string
	VramMB         int    // VRAM to allocate in MB per pod (NOT USED in ACS - ACS auto-scales resources)
	GPUModelSeries string // GPU model series for Aliyun (e.g., T4, A10, L20, GU8TF, GU8TEF, G49E, P16EN), refers to https://help.aliyun.com/zh/cs/user-guide/gpu-families-supported-by-acs
	GPUCount       string // Number of GPUs for ACS (e.g., "1", "2", "4"), required for ACS GPU pods
	CPURequest     string // CPU request for ACS GPU pods (e.g., "2", "4", "8")
	MemoryRequest  string // Memory request for ACS GPU pods (e.g., "4Gi", "8Gi", "16Gi")
}

// Simple typed config for Pulumi stack values used in the PoC.
type StackConfig struct {
	Name                string
	Namespace           string
	StorageClass        string // REQUIRED: Kubernetes StorageClass for PVCs (MySQL, Elasticsearch, RabbitMQ)
	Env                 map[string]string
	RAGFlow             RAGFlowConfig
	Gateway             GatewayConfig
	Deepdoc             GPUConfig                // Unified DLA+OCR+TSR service
	Images              ImageConfig              // Container image URLs
	Registry            RegistryConfig           // Docker registry configuration
	MySQL               MySQLConfig              // MySQL configuration
	ES                  ESConfig                 // Elasticsearch configuration
	VSwitchIDs          pulumi.StringArrayOutput // vSwitch IDs for ALB configuration
	ESEndpointOutput    pulumi.StringOutput      // ES endpoint from StackReference (for external ES)
	ESPortOutput        pulumi.StringOutput      // ES port from StackReference (for external ES)
	ESProtocolOutput    pulumi.StringOutput      // ES protocol from StackReference (for external ES)
	ESUsernameOutput    pulumi.StringOutput      // ES username from StackReference (for external ES)
	ESPasswordOutput    pulumi.StringOutput      // ES password from StackReference (for external ES)
	MySQLEndpointOutput pulumi.StringOutput      // MySQL endpoint from StackReference (for external MySQL)
	MySQLPortOutput     pulumi.StringOutput      // MySQL port from StackReference (for external MySQL)
	MySQLPasswordOutput pulumi.StringOutput      // MySQL password from StackReference (for external MySQL)
}

// RegistryConfig holds Docker registry credentials
type RegistryConfig struct {
	Server   string
	Username string
	Password string
}

// MySQLConfig holds MySQL configuration
type MySQLConfig struct {
	External bool   // true means use external MySQL, false means create MySQL deployment in phase 1
	Host     string // Empty string means create MySQL deployment, non-empty means use external MySQL
	Port     string // Default: "3306"
	Password string // Default: "infiniflow@2023"
	DBName   string // Default: "ragflow"
}

// ESConfig holds Elasticsearch configuration
type ESConfig struct {
	External bool   // true means use external ES, false means create ES deployment in phase 1
	Host     string // Empty string means create ES deployment, non-empty means use external ES
	Port     string // Default: "9200"
	Protocol string // Default: "http"
	Username string // Default: "elastic"
	Password string // Default: "" (only used when ES is created by Pulumi)
}

// StackRefServices holds service connection details from StackReference
type StackRefServices struct {
	Endpoint pulumi.StringOutput
	Port     pulumi.StringOutput
	Username pulumi.StringOutput
	Password pulumi.StringOutput
	Database pulumi.StringOutput // Only for MySQL
}

// DESIGN CONSIDERATION: StorageClass Configuration
// ===============================================
// Problem: Different Kubernetes clusters have different StorageClass names
// - ACK/ASK clusters (Aliyun): alicloud-disk-alltype
// - ACS clusters (Aliyun): alicloud-disk-topology-alltype
// - Rook clusters: rook-ceph-block
// - Local path clusters: local-path
//
// Solution: Required configuration via kubernetes.storage_class
// - User MUST explicitly configure the StorageClass for their cluster
// - No default value - forces user to verify their cluster's StorageClass
// - Fail-fast validation during config loading prevents deployment failures
//
// Rationale for Required Configuration:
// - RabbitMQ PVC requires a valid StorageClass to provision storage
// - Auto-detection is unreliable (different clusters, different labels)
// - Shell calls to kubectl violate pure Go code requirement
// - Explicit configuration is clear and deterministic
//
// Benefits:
// - No guesswork - user knows exactly which StorageClass will be used
// - Fail-fast - errors caught during config loading, not at deployment time
// - Works across all cluster types (ACK, ASK, ACS, existing, on-prem)
// - Pure Go implementation with no external dependencies
//
// How to configure:
//   pulumi config set kubernetes.storage_class alicloud-disk-alltype
//
// How to find your cluster's StorageClass:
//   kubectl get storageclass

// getStorageClassPtr returns the configured StorageClass as a pulumi.StringPtrInput
//
// This is used in PVCs (MySQL, RabbitMQ) that expect a pulumi.StringPtrInput type
// Panics if config.StorageClass is empty (should never happen due to validation in LoadConfig)
func getStorageClassPtr(config *StackConfig) pulumi.StringPtrInput {
	if config.StorageClass == "" {
		panic("StorageClass is empty but this should have been validated in LoadConfig")
	}
	return pulumi.String(config.StorageClass)
}

// getStorageClassMapValue returns the configured StorageClass as a map value
//
// This is used in Elasticsearch volumeClaimTemplates where StorageClass is a map value
// Panics if config.StorageClass is empty (should never happen due to validation in LoadConfig)
func getStorageClassMapValue(config *StackConfig) interface{} {
	if config.StorageClass == "" {
		panic("StorageClass is empty but this should have been validated in LoadConfig")
	}
	return config.StorageClass
}

// LoadConfig reads config values from Pulumi configuration
func LoadConfig(ctx *pulumi.Context) (StackConfig, error) {
	// Read basic configuration
	namespace := getConfig(ctx, "kubernetes.namespace", "ragflow")

	// Read StorageClass configuration (REQUIRED for PVCs)
	// This must be explicitly set - no default value
	// All PVCs (MySQL, Elasticsearch, RabbitMQ) depend on this
	storageClass := getConfig(ctx, "kubernetes.storage_class", "")
	if storageClass == "" {
		return StackConfig{}, fmt.Errorf("kubernetes.storage_class is required but not set. Please configure it with: pulumi config set kubernetes.storage_class <your-storage-class-name>")
	}
	ctx.Log.Info(fmt.Sprintf("Using StorageClass: %s", storageClass), &pulumi.LogArgs{})

	// Enterprise registry configuration
	enterpriseRegistry := getConfig(ctx, "kubernetes.enterprise_registry", "192.168.1.51")
	// Use public registry or enterprise registry
	usePublicRegistryStr := getConfig(ctx, "kubernetes.use_public_registry", "true")
	usePublicRegistry, err := strconv.ParseBool(usePublicRegistryStr)
	if err != nil {
		usePublicRegistry = true // Default to true if parsing fails
	}

	// Helper function to convert public image URLs to enterprise registry URLs
	getImageURL := func(publicURL string) string {
		if usePublicRegistry {
			return publicURL
		}
		// Remove registry prefix if present
		parts := strings.Split(publicURL, "/")
		imageAndTag := parts[len(parts)-1]
		// Construct enterprise registry URL
		return fmt.Sprintf("%s/infiniflow/%s", enterpriseRegistry, imageAndTag)
	}

	// RAGFlow image configuration
	ragflowImageTag := getConfig(ctx, "ragflow.image_tag", "latest")
	ragflowImage := fmt.Sprintf("%s/infiniflow-ai/ragflow:%s", enterpriseRegistry, ragflowImageTag)
	ragflowReplicasStr := getConfig(ctx, "ragflow.replicas", "1")
	ragflowReplicas, _ := strconv.Atoi(ragflowReplicasStr)

	// Read S3 configuration
	s3Endpoint := getConfig(ctx, "s3.endpoint", "http://rook-ceph-rgw-my-store.rook-ceph.svc:80")
	s3Bucket := getConfig(ctx, "s3.bucket", "ragflow")
	s3Region := getConfig(ctx, "s3.region", "us-east-1")

	// Auto-detect storage type based on endpoint
	storageImplType := ""
	if strings.Contains(s3Endpoint, "aliyuncs.com") {
		storageImplType = "OSS"
	} else {
		storageImplType = "AWS_S3"
	}

	// Read S3 access credentials (sensitive information)
	s3AccessKey := getConfig(ctx, "s3.access_key", "")
	s3SecretKey := getConfig(ctx, "s3.secret_key", "")

	// Read RAGFlow secret key for session signing
	ragflowSecretKey := getConfig(ctx, "ragflow.secret_key", "DOnghtfiCeriTENdywhERlEtivOLicuL")
	if ragflowSecretKey == "DOnghtfiCeriTENdywhERlEtivOLicuL" {
		ctx.Log.Warn("Using default ragflow.secret_key — set a unique secret in production with: pulumi config set --secret ragflow.secret_key <your-key>", &pulumi.LogArgs{})
	}

	// Read Docker registry credentials (sensitive information)
	registryUsername := getConfig(ctx, "kubernetes.enterprise_registry_username", "")
	registryPassword := getConfig(ctx, "kubernetes.enterprise_registry_password", "")

	// Read MySQL configuration
	// If mysql_host is empty, we will create a MySQL deployment in the cluster
	// If mysql_host is set, we will use the external MySQL server
	externalMySQL := getConfig(ctx, "mysql.external", "false")
	mysqlHost := getConfig(ctx, "mysql.host", "mysql")
	mysqlPort := getConfig(ctx, "mysql.port", "3306")
	// MySQL always uses root user - no need for username configuration
	// Generate MySQL password using same method as aliyun.go createMySQL
	// Use fixed seed for consistency across deployments
	mysqlPassword := getConfig(ctx, "mysql.password", "infiniflow@2023")
	mysqlDBName := getConfig(ctx, "mysql.dbname", "ragflow")

	// Read Elasticsearch configuration
	// If es_host is empty, we will create an Elasticsearch deployment in the cluster
	// If es_host is set, we will use the external Elasticsearch server
	externalES := getConfig(ctx, "elasticsearch.external", "false")
	esHost := getConfig(ctx, "elasticsearch.host", "elasticsearch")
	esPort := getConfig(ctx, "elasticsearch.port", "9200")
	esProtocol := getConfig(ctx, "elasticsearch.protocol", "http")
	esUsername := getConfig(ctx, "elasticsearch.username", "elastic")
	esPassword := getConfig(ctx, "elasticsearch.password", "infiniflow@2023")

	// Read Unified DeepDoc service configuration
	// Note: DeepDoc is always enabled (replaces TSR/DLA/OCR services)
	deepdocReplicasStr := getConfig(ctx, "deepdoc.replicas", "1")
	deepdocReplicas, _ := strconv.Atoi(deepdocReplicasStr)
	// DeepDoc hardware type: "cpu" or "gpu" (default: "cpu")
	deepdocHardware := getConfig(ctx, "deepdoc.hardware", "cpu")
	deepdocImageTag := getConfig(ctx, "ragflow.image_tag", "latest")
	// Build image name based on hardware type and tag
	// CPU: <registry>/infiniflow-ai/deepdoc_cpu:<tag>
	// GPU: <registry>/infiniflow-ai/deepdoc_gpu:<tag>
	var deepdocImage string
	if deepdocHardware == "gpu" {
		deepdocImage = "deepdoc_gpu"
	} else {
		deepdocImage = "deepdoc_cpu"
	}
	deepdocImage = fmt.Sprintf("%s/infiniflow-ai/%s:%s", enterpriseRegistry, deepdocImage, deepdocImageTag)
	// DeepDoc GPU version uses HAMi vGPU for GPU sharing
	// CPU version does not need GPU resources
	deepdocVramStr := getConfig(ctx, "deepdoc.vram_mb", "2048") // Combined memory for all three models
	deepdocVram, _ := strconv.Atoi(deepdocVramStr)
	// Enable GPU for GPU version
	deepdocUseGPU := deepdocHardware == "gpu"
	// GPU configuration for Aliyun ACS (hardcoded fixed values)
	// See: https://help.aliyun.com/zh/cs/user-guide/gpu-families-supported-by-acs
	deepdocGPUModelSeries := "T4" // Fixed: T4 GPU model
	deepdocGPUCount := "1"        // Fixed: 1 GPU per pod
	deepdocCPURequest := "2"      // Fixed: 2 CPU cores
	deepdocMemoryRequest := "4Gi" // Fixed: 4GiB memory

	// Read Elasticsearch version for image configuration
	stackVersion := getConfig(ctx, "elasticsearch.version", "8.11.3")

	// Build all container image URLs
	images := ImageConfig{
		MySQL:         getImageURL("mysql:8.4"),
		Redis:         getImageURL("valkey/valkey:8"),
		TEI:           getImageURL("infiniflow/text-embeddings-inference:cpu-1.8"),
		RabbitMQ:      getImageURL("rabbitmq:4-management"),
		Curl:          getImageURL("curlimages/curl:latest"),
		AWSCLI:        getImageURL("amazon/aws-cli:latest"),
		Elasticsearch: getImageURL("elasticsearch:" + stackVersion),
	}

	env := map[string]string{
		"DOC_ENGINE":            "elasticsearch",
		"RAGFLOW_IMAGE":         ragflowImage,
		"STACK_VERSION":         "8.11.3",
		"MYSQL_HOST":            mysqlHost,
		"MYSQL_PORT":            mysqlPort,
		"MYSQL_DBNAME":          mysqlDBName,
		"MYSQL_USER":            "root", // Always use root user for MySQL
		"MYSQL_PASSWORD":        mysqlPassword,
		"REDIS_HOST":            "redis",
		"REDIS_PASSWORD":        "infini_rag_flow",
		"ES_HOST":               esHost,
		"ES_PROTOCOL":           esProtocol,
		"S3_ENDPOINT":           s3Endpoint,
		"S3_ACCESS_KEY":         s3AccessKey,
		"S3_SECRET_KEY":         s3SecretKey,
		"S3_BUCKET":             s3Bucket,
		"S3_REGION":             s3Region,
		"STORAGE_IMPL":          storageImplType,
		"PYTHONPATH":            "/ragflow",
		"TEI_HOST":              "tei",
		"TEI_MODEL":             "BAAI/bge-small-en-v1.5",
		"SVR_WEB_HTTP_PORT":     "80",
		"SVR_WEB_HTTPS_PORT":    "443",
		"SVR_HTTP_PORT":         "9380",
		"ADMIN_SVR_HTTP_PORT":   "9381",
		"SVR_MCP_PORT":          "9382",
		"COMPOSE_PROFILES":      "elasticsearch,cpu,tei-cpu",
		"RABBITMQ_HOST":         "rabbitmq",
		"RABBITMQ_PORT":         "5672",
		"RABBITMQ_API_PORT":     "15672",
		"RABBITMQ_DEFAULT_USER": "rag_flow",
		"RABBITMQ_DEFAULT_PASS": "infini_rag_flow",
		// Set a fixed secret key for session signing to avoid "Signature does not match" errors
		// This ensures all workers share the same key and it persists across restarts
		// Can be configured via Pulumi config (ragflow_secret_key), defaults to a secure random key
		"RAGFLOW_SECRET_KEY": ragflowSecretKey,
		// In Kubernetes, since K8s already provides process management (probes, restart policies),
		// many users choose to run single-process uvicorn and scale out by increasing Pod replicas.
		"UVICORN_WORKERS": "1",
	}

	ragflow := RAGFlowConfig{
		Image:    ragflowImage,
		Replicas: ragflowReplicas,
		Service: ServiceConfig{
			Type: "ClusterIP",
		},
		API: APIConfig{
			Service: ServiceConfig{
				Type:    "ClusterIP",
				Enabled: true,
			},
		},
	}

	// Gateway configuration
	gateway := GatewayConfig{
		ClassName:   "",
		Namespace:   namespace,           // Use the same namespace as other resources
		Annotations: map[string]string{}, // Can be extended later
		Hosts:       []GatewayHost{},     // Can be extended later
		TLS:         []GatewayTLS{},      // Can be extended later
	}

	return StackConfig{
		Name:         fmt.Sprintf("%s-%s", ctx.Project(), ctx.Stack()),
		Namespace:    namespace,
		StorageClass: storageClass,
		Env:          env,
		RAGFlow:      ragflow,
		Gateway:      gateway,
		Deepdoc: GPUConfig{
			UseGPU:         deepdocUseGPU,
			Replicas:       deepdocReplicas,
			Image:          deepdocImage,
			VramMB:         deepdocVram,
			GPUModelSeries: deepdocGPUModelSeries,
			GPUCount:       deepdocGPUCount,
			CPURequest:     deepdocCPURequest,
			MemoryRequest:  deepdocMemoryRequest,
		},
		Images: images,
		Registry: RegistryConfig{
			Server:   enterpriseRegistry,
			Username: registryUsername,
			Password: registryPassword,
		},
		MySQL: MySQLConfig{
			External: externalMySQL == "true",
			Host:     mysqlHost,
			Port:     mysqlPort,
			Password: mysqlPassword,
			DBName:   mysqlDBName,
		},
		ES: ESConfig{
			External: externalES == "true",
			Host:     esHost,
			Port:     esPort,
			Protocol: esProtocol,
			Username: esUsername,
			Password: esPassword,
		},
	}, nil
}

// Helper function to get configuration value with default
// If the key is missing or its value is empty (e.g. after sed strips "secure:" lines),
// returns defaultValue instead. This ensures secrets like mysql.password and
// elasticsearch.password fall back to their defaults when the encrypted value is removed.
func getConfig(ctx *pulumi.Context, key string, defaultValue string) string {
	if val, err := pulumiconfig.Try(ctx, key); err == nil && val != "" {
		return val
	}
	return defaultValue
}

// Helper function to get storage/disk size and auto-append "Gi" suffix
// Accepts both numeric values (e.g., "20") and values with suffix (e.g., "20Gi")
// Always returns value with "Gi" suffix for Kubernetes PVC compatibility
func getStorageSize(ctx *pulumi.Context, key string, defaultValue string) string {
	val := getConfig(ctx, key, defaultValue)
	// If value doesn't already have a suffix, append "Gi"
	if val != "" && !strings.HasSuffix(val, "Gi") && !strings.HasSuffix(val, "G") &&
		!strings.HasSuffix(val, "Mi") && !strings.HasSuffix(val, "M") &&
		!strings.HasSuffix(val, "Ki") && !strings.HasSuffix(val, "K") {
		return val + "Gi"
	}
	return val
}

// Helper function to convert string port to int with default value
func parsePortWithDefault(portStr string, defaultPort int) int {
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return defaultPort
	}
	return port
}

// Helper function to convert string port to int
func parsePort(portStr string) int {
	return parsePortWithDefault(portStr, 80)
}

// Helper function to read file content
func readFileContent(filePath string) (string, error) {
	content, err := os.ReadFile(filePath)
	if err != nil {
		return "", err
	}
	return string(content), nil
}

// deployK8sResources handles phase 2 deployment: Kubernetes resources only
// This function uses StackReference to read outputs from phase 1 infrastructure stack
func deployK8sResources(ctx *pulumi.Context, config StackConfig) error {
	ctx.Log.Info("=== Phase 2: Kubernetes Resources Deployment ===", &pulumi.LogArgs{})

	// Use the passed k8sProvider if provided (for onpremise/existing cluster case)
	// Otherwise create one from kubeconfig (for k8s phase from StackReference)
	var provider *kubernetes.Provider
	var err error

	cloud := getConfig(ctx, "cloud", "onpremise")
	// Create provider from kubeconfig (k8s phase mode)
	// Get infra stack name from config (e.g., "ali" or full "org/project/ali")
	infraStackName := getConfig(ctx, "depends_on_stack", "")
	// If not a full path (no "/"), prepend current org/project
	if infraStackName != "" && !strings.Contains(infraStackName, "/") {
		infraStackName = fmt.Sprintf("%s/%s/%s", ctx.Organization(), ctx.Project(), infraStackName)
	}
	useStackRef := infraStackName != ""

	var providerArgs kubernetes.ProviderArgs
	if useStackRef {
		// Use StackReference to read outputs from phase 1 infrastructure stack
		ctx.Log.Info(fmt.Sprintf("Using StackReference to read outputs from infra stack: %s", infraStackName), &pulumi.LogArgs{})

		infraStack, err := pulumi.NewStackReference(ctx, infraStackName, nil)
		if err != nil {
			return fmt.Errorf("failed to create stack reference for %s: %w", infraStackName, err)
		}

		load_resource := make([]string, 0)
		if cloud == "aliyun" {
			// Read vSwitch IDs from infra stack (for ALB configuration)
			// NOTE: ali stack now creates 2 vSwitches in different zones for ALB
			config.VSwitchIDs = infraStack.GetOutput(pulumi.String("vSwitchIds")).AsStringArrayOutput()
			load_resource = append(load_resource, "vSwitch IDs")
		}

		// Read kubeconfig from infra stack
		kubeconfig := infraStack.GetOutput(pulumi.String("kubeconfig")).AsStringOutput()
		providerArgs = kubernetes.ProviderArgs{
			Kubeconfig: kubeconfig,
		}
		load_resource = append(load_resource, "kubeconfig")

		// Read MySQL outputs from infra stack if using external MySQL
		if config.MySQL.External {
			config.MySQLEndpointOutput = infraStack.GetOutput(pulumi.String("mysql_endpoint")).AsStringOutput()
			config.MySQLPortOutput = infraStack.GetOutput(pulumi.String("mysql_port")).AsStringOutput()
			config.MySQLPasswordOutput = infraStack.GetOutput(pulumi.String("mysql_password")).AsStringOutput()
			load_resource = append(load_resource, "MySQL endpoint")
		}

		// Read ES outputs from infra stack if using external ES
		if config.ES.External {
			config.ESEndpointOutput = infraStack.GetOutput(pulumi.String("es_endpoint")).AsStringOutput()
			config.ESPortOutput = infraStack.GetOutput(pulumi.String("es_port")).AsStringOutput()
			config.ESProtocolOutput = infraStack.GetOutput(pulumi.String("es_protocol")).AsStringOutput()
			config.ESUsernameOutput = infraStack.GetOutput(pulumi.String("es_username")).AsStringOutput()
			config.ESPasswordOutput = infraStack.GetOutput(pulumi.String("es_password")).AsStringOutput()
			load_resource = append(load_resource, "ES endpoint")
		}

		ctx.Log.Info(fmt.Sprintf("Successfully loaded %s from Phase 1 (StackReference of %s)", strings.Join(load_resource, ", "), infraStackName), &pulumi.LogArgs{})
	} else {
		// in-cluster and onpremise mode:
		// Pulumi uses client-go's DefaultClientConfig which auto-detects in-cluster
		// environment via KUBERNETES_SERVICE_HOST and uses ServiceAccount token,
		// or falls back to KUBECONFIG or default ~/.kube/config for on-premise setups.
		providerArgs = kubernetes.ProviderArgs{}
		ctx.Log.Info(fmt.Sprintf("Using kubeconfig from in-cluster service account or default kubeconfig file"), &pulumi.LogArgs{})
	}

	// Load vSwitch IDs from config if not already set via StackReference
	// This supports the ROS template deployment flow where vSwitch IDs are passed
	// as environment variables and set via "pulumi config set aliyun.vswitch_ids"
	if cloud == "aliyun" && config.VSwitchIDs == (pulumi.StringArrayOutput{}) {
		vswitchIDsStr := getConfig(ctx, "aliyun.vswitch_ids", "")
		if vswitchIDsStr != "" {
			ids := strings.Split(vswitchIDsStr, ",")
			for i := range ids {
				ids[i] = strings.TrimSpace(ids[i])
			}
			config.VSwitchIDs = pulumi.ToStringArray(ids).ToStringArrayOutput()
			ctx.Log.Info(fmt.Sprintf("Loaded %d vSwitch IDs from config: %v", len(ids), ids), &pulumi.LogArgs{})
		}
	}

	// Create Kubernetes provider
	provider, err = kubernetes.NewProvider(ctx, "k8s-provider", &providerArgs)
	if err != nil {
		return fmt.Errorf("failed to create k8s provider: %w", err)
	}

	ctx.Log.Info("✓ Kubernetes provider configured", &pulumi.LogArgs{})

	// Create namespace
	namespace, err := corev1.NewNamespace(ctx, "ragflow-namespace", &corev1.NamespaceArgs{
		Metadata: &metav1.ObjectMetaArgs{
			Name: pulumi.String(config.Namespace),
		},
	}, pulumi.Provider(provider))
	if err != nil {
		return fmt.Errorf("failed to create namespace: %w", err)
	}

	// Create registry secret (if configured)
	registrySecret, err := createRegistrySecret(ctx, &config, namespace, provider)
	if err != nil {
		return fmt.Errorf("failed to create registry secret: %w", err)
	}

	// Create MySQL deployment if not using external MySQL
	if !config.MySQL.External {
		ctx.Log.Info("Creating MySQL deployment", &pulumi.LogArgs{})
		_, _, err = createMySQL(ctx, &config, namespace, provider)
		if err != nil {
			return fmt.Errorf("failed to create mysql: %w", err)
		}
	}

	// Create Elasticsearch deployment if not using external ES
	if !config.ES.External {
		// Deploy ECK Operator before creating Elasticsearch resource
		// This is required for the Elasticsearch CRD to exist
		ctx.Log.Info("Deploying ECK Operator for Elasticsearch CRD support...", &pulumi.LogArgs{})
		if err := deployECK(ctx, provider, nil); err != nil {
			return fmt.Errorf("failed to deploy ECK: %w", err)
		}

		ctx.Log.Info("Creating Elasticsearch deployment", &pulumi.LogArgs{})
		_, _, err = createElasticsearch(ctx, &config, namespace, provider)
		if err != nil {
			return fmt.Errorf("failed to create elasticsearch: %w", err)
		}
	}

	// Redis is always created internally (no external Redis option)
	ctx.Log.Info("Creating Redis deployment", &pulumi.LogArgs{})
	_, _, err = createRedis(ctx, &config, namespace, provider)
	if err != nil {
		return fmt.Errorf("failed to create redis: %w", err)
	}

	// Create TEI service
	ctx.Log.Info("Creating TEI deployment", &pulumi.LogArgs{})
	_, _, err = createTEI(ctx, &config, namespace, provider)
	if err != nil {
		return fmt.Errorf("failed to create TEI: %w", err)
	}

	// Create RabbitMQ
	ctx.Log.Info("Creating RabbitMQ deployment", &pulumi.LogArgs{})
	_, _, err = createRabbitMQ(ctx, &config, namespace, provider)
	if err != nil {
		return fmt.Errorf("failed to create rabbitmq: %w", err)
	}

	// Create DeepDoc deployment
	_, _, err = createDeepdocDeployment(ctx, &config, namespace, provider, registrySecret)
	if err != nil {
		return fmt.Errorf("failed to create deepdoc deployment: %w", err)
	}

	// Create RAGFlow deployments and services
	_, ragflowService, _, err := createRAGFlowDeployment(ctx, &config, namespace, provider, registrySecret)
	if err != nil {
		return fmt.Errorf("failed to create ragflow deployment: %w", err)
	}

	// Create Gateway API or Ingress resources (if enabled)
	if cloud == "aliyun" {
		// Create ALB Ingress for Aliyun ACK/ASK
		_, err = createIngress(ctx, &config, provider, ragflowService)
		if err != nil {
			return fmt.Errorf("failed to create ingress: %w", err)
		}
	} else {
		gatewayClass := getConfig(ctx, "kubernetes.gateway_class", "cilium")
		_, err = createGateway(ctx, &config, provider, ragflowService, gatewayClass)
		if err != nil {
			return fmt.Errorf("failed to create gateway: %w", err)
		}
	}

	ctx.Log.Info(fmt.Sprintf("✓ Successfully deployed all Kubernetes resources in namespace: %s", config.Namespace), &pulumi.LogArgs{})

	return nil
}

// createRegistrySecret creates a secret for registry authentication (if configured)
func createRegistrySecret(ctx *pulumi.Context, config *StackConfig, namespace *corev1.Namespace, provider *kubernetes.Provider) (*corev1.Secret, error) {
	registryServer := getConfig(ctx, "kubernetes.enterprise_registry", "")
	username := getConfig(ctx, "kubernetes.enterprise_registry_username", "")
	password := getConfig(ctx, "kubernetes.enterprise_registry_password", "")

	if registryServer == "" || username == "" || password == "" {
		ctx.Log.Info("No registry credentials configured, skipping registry secret", nil)
		return nil, nil
	}

	// Create Docker config JSON for registry authentication
	dockerConfigJSON := fmt.Sprintf(`{
		"auths": {
			"%s": {
				"username": "%s",
				"password": "%s",
				"auth": "%s"
			}
		}
	}`, registryServer, username, password, base64.StdEncoding.EncodeToString([]byte(username+":"+password)))

	// Kubernetes Secret .Data field requires base64-encoded values
	encodedDockerConfig := base64.StdEncoding.EncodeToString([]byte(dockerConfigJSON))

	secret, err := corev1.NewSecret(ctx, "registry-secret", &corev1.SecretArgs{
		Metadata: &metav1.ObjectMetaArgs{
			Name:      pulumi.String("regcred"),
			Namespace: namespace.Metadata.Name(),
		},
		Type: pulumi.String("kubernetes.io/dockerconfigjson"),
		Data: pulumi.StringMap{
			".dockerconfigjson": pulumi.String(encodedDockerConfig),
		},
	}, pulumi.Provider(provider))

	return secret, err
}

func main() {
	pulumi.Run(func(ctx *pulumi.Context) error {
		config, err := LoadConfig(ctx)
		if err != nil {
			return err
		}

		// Get phase configuration to determine what to deploy
		// phase: "infra" = deploy infrastructure only (ali stack) - MUST be set
		// phase: "k8s" = deploy k8s resources only (ali_k8s stack)
		phase := getConfig(ctx, "phase", "infra")
		ctx.Log.Info(fmt.Sprintf("Deployment phase: %s", phase), &pulumi.LogArgs{})

		// Phase 2: K8s resources only - skip infrastructure deployment
		if phase == "k8s" {
			return deployK8sResources(ctx, config)
		}

		// Phase 1: Infrastructure deployment (must set phase: infra)
		ctx.Log.Info("=== Phase 1: Infrastructure Deployment ===", &pulumi.LogArgs{})
		ctx.Log.Info(fmt.Sprintf("StorageClass: %s (will be used for all PVCs)", config.StorageClass), &pulumi.LogArgs{})
		ctx.Log.Info("", &pulumi.LogArgs{})

		// Get cloud provider configuration
		cloudProvider := getConfig(ctx, "cloud", "existing")
		ctx.Log.Info(fmt.Sprintf("Cloud provider mode: %s", cloudProvider), &pulumi.LogArgs{})

		var infra *InfraResult

		// Route to appropriate cloud provider
		switch cloudProvider {
		case "aliyun":
			// Deploy Aliyun infrastructure ONLY (Phase 1)
			cfg := pulumiconfig.New(ctx, "")
			provider, err := NewAliyunProvider(ctx, cfg)
			if err != nil {
				return fmt.Errorf("failed to create Aliyun provider: %w", err)
			}
			if err := provider.ValidateConfig(); err != nil {
				return fmt.Errorf("Aliyun config validation failed: %w", err)
			}
			infra, err = provider.DeployInfra(ctx)
			if err != nil {
				return fmt.Errorf("failed to deploy Aliyun infrastructure: %w", err)
			}

			// Export infrastructure outputs for Phase 2 (k8s stack)
			// MySQL complete configuration
			if config.MySQL.External {
				ctx.Export("mysql_endpoint", infra.MySqlEndpoint)
				// Convert IntOutput to StringOutput for consistent type handling
				ctx.Export("mysql_port", infra.MySqlPort.ApplyT(func(port int) string { return strconv.Itoa(port) }).(pulumi.StringOutput))
				ctx.Export("mysql_database", infra.MySqlDatabase)
				// Note: mysql_username is NOT exported because MySQL always uses 'root'
				ctx.Export("mysql_password", pulumi.ToSecret(infra.MySqlPassword))

			}
			// Elasticsearch complete configuration
			if config.ES.External {
				ctx.Export("es_endpoint", infra.ESEndpoint)
				// Convert IntOutput to StringOutput for consistent type handling
				ctx.Export("es_port", infra.ESPort.ApplyT(func(port int) string { return strconv.Itoa(port) }).(pulumi.StringOutput))
				ctx.Export("es_protocol", infra.ESProtocol)
				ctx.Export("es_username", infra.ESUsername)
				ctx.Export("es_password", pulumi.ToSecret(infra.ESPassword))
			}
			// Export kubeconfig for use by ali_k8s stack
			ctx.Export("kubeconfig", pulumi.ToSecret(infra.Kubeconfig))

			// Phase 1 complete - stop here, do NOT deploy K8s resources
			ctx.Log.Info("✓ Phase 1 complete: Infrastructure deployed successfully", &pulumi.LogArgs{})
			ctx.Log.Info("Kubernetes resources will be deployed in Phase 2", &pulumi.LogArgs{})
			return nil

		case "gcp":
			// TODO: Implement GCP provider
			return fmt.Errorf("GCP provider not yet implemented")

		case "onpremise":
			// Use existing K8s cluster - delegate to deployK8sResources
			fallthrough

		default:
			// For existing/on-premise clusters, create k8sProvider and delegate to deployK8sResources
			ctx.Log.Info("Creating Kubernetes provider for on-premise cluster", &pulumi.LogArgs{})
			// Delegate all K8s resource creation to deployK8sResources
			return deployK8sResources(ctx, config)
		}
	})
}

// deployECK deploys ECK (Elastic Cloud on Kubernetes) Operator
// This should only be called for cloud providers (aliyun/gcp) where we have admin privileges
// For existing clusters, ECK should be installed separately by cluster administrators
// Refers to https://www.elastic.co/docs/deploy-manage/deploy/cloud-on-k8s/install-using-yaml-manifest-quickstart
func deployECK(ctx *pulumi.Context, k8sProvider *kubernetes.Provider, clusterResource pulumi.Resource) error {
	ctx.Log.Info("Deploying ECK Operator...", &pulumi.LogArgs{})

	// Ignore changes for fields that may be managed by ECK itself or kubectl
	ignoreFields := []string{
		"*.metadata.annotations",
		"*.metadata.labels",
		"*.spec.versions",
	}

	// Build resource options with provider and optional cluster dependency
	var resourceOpts []pulumi.ResourceOption
	resourceOpts = append(resourceOpts, pulumi.Provider(k8sProvider), pulumi.IgnoreChanges(ignoreFields))
	if clusterResource != nil {
		resourceOpts = append(resourceOpts, pulumi.DependsOn([]pulumi.Resource{clusterResource}))
	}

	// 1. Deploy ECK CRDs
	_, err := yaml.NewConfigFile(ctx, "eck-crds", &yaml.ConfigFileArgs{
		File: "https://download.elastic.co/downloads/eck/3.2.0/crds.yaml",
	}, resourceOpts...)
	if err != nil {
		return fmt.Errorf("failed to deploy ECK CRDs: %w", err)
	}

	// 2. Deploy ECK Operator
	_, err = yaml.NewConfigFile(ctx, "eck-operator", &yaml.ConfigFileArgs{
		File: "https://download.elastic.co/downloads/eck/3.2.0/operator.yaml",
	}, resourceOpts...)
	if err != nil {
		return fmt.Errorf("failed to deploy ECK Operator: %w", err)
	}

	ctx.Log.Info("ECK Operator deployed successfully", &pulumi.LogArgs{})
	return nil
}

func createMySQL(ctx *pulumi.Context, config *StackConfig, namespace *corev1.Namespace, provider pulumi.ProviderResource) (*v1.Deployment, *corev1.Service, error) {
	// Read MySQL PVC size configuration
	mysqlStorage := getStorageSize(ctx, "mysql.disk_size", "1")

	// Use fixed password for internal MySQL deployment
	// The password must be known by ragflow containers, so we use config.MySQL.Password
	// instead of generating a random one (which would be inaccessible to ragflow)
	mysqlPassword := pulumi.String(config.MySQL.Password)

	// MySQL init.sql ConfigMap (fallback if MYSQL_DATABASE env var fails)
	// This SQL script creates the database on MySQL startup using configured dbname
	mysqlInitConfigMap, err := corev1.NewConfigMap(ctx, "mysql-init-configmap", &corev1.ConfigMapArgs{
		Metadata: &metav1.ObjectMetaArgs{
			Name:      pulumi.String("mysql-init"),
			Namespace: namespace.Metadata.Name(),
		},
		Data: pulumi.StringMap{
			"init.sql": pulumi.Sprintf("CREATE DATABASE IF NOT EXISTS %s;\nUSE %s;\n", config.MySQL.DBName, config.MySQL.DBName),
		},
	}, pulumi.Provider(provider))
	if err != nil {
		return nil, nil, err
	}

	// MySQL PVC
	mysqlPVC, err := corev1.NewPersistentVolumeClaim(ctx, "mysql-pvc", &corev1.PersistentVolumeClaimArgs{
		Metadata: &metav1.ObjectMetaArgs{
			Name:      pulumi.String("mysql-pvc"),
			Namespace: namespace.Metadata.Name(),
		},
		Spec: &corev1.PersistentVolumeClaimSpecArgs{
			AccessModes: pulumi.StringArray{
				pulumi.String("ReadWriteOnce"),
			},
			Resources: &corev1.VolumeResourceRequirementsArgs{
				Requests: pulumi.StringMap{
					"storage": pulumi.String(mysqlStorage),
				},
			},
			StorageClassName: getStorageClassPtr(config),
		},
	}, pulumi.Provider(provider))
	if err != nil {
		return nil, nil, err
	}

	// MySQL Deployment
	mysqlDeployment, err := v1.NewDeployment(ctx, "mysql-deployment", &v1.DeploymentArgs{
		Metadata: &metav1.ObjectMetaArgs{
			Name:      pulumi.String("mysql"),
			Namespace: namespace.Metadata.Name(),
		},
		Spec: &v1.DeploymentSpecArgs{
			Replicas: pulumi.Int(1),
			Selector: &metav1.LabelSelectorArgs{
				MatchLabels: pulumi.StringMap{
					"app": pulumi.String("mysql"),
				},
			},
			Strategy: &v1.DeploymentStrategyArgs{
				Type: pulumi.String("Recreate"), // Required for stateful singleton pod
			},
			Template: &corev1.PodTemplateSpecArgs{
				Metadata: &metav1.ObjectMetaArgs{
					Labels: pulumi.StringMap{
						"app": pulumi.String("mysql"),
					},
				},
				Spec: &corev1.PodSpecArgs{
					Containers: corev1.ContainerArray{
						&corev1.ContainerArgs{
							Name:  pulumi.String("mysql"),
							Image: pulumi.String(config.Images.MySQL),
							Ports: corev1.ContainerPortArray{
								&corev1.ContainerPortArgs{
									ContainerPort: pulumi.Int(parsePortWithDefault(config.MySQL.Port, 3306)),
								},
							},
							Env: corev1.EnvVarArray{
								&corev1.EnvVarArgs{
									Name:  pulumi.String("MYSQL_ROOT_PASSWORD"),
									Value: mysqlPassword,
								},
								&corev1.EnvVarArgs{
									Name:  pulumi.String("MYSQL_DATABASE"),
									Value: pulumi.String(config.MySQL.DBName),
								},
								// Note: MYSQL_USER and MYSQL_PASSWORD are intentionally NOT set
								// because we use the root user (same as Aliyun RDS).
								// The root user created by MYSQL_ROOT_PASSWORD automatically has
								// full privileges on MYSQL_DATABASE.
							},
							Args: pulumi.StringArray{
								pulumi.String("--max_connections=900"),
								pulumi.String("--character-set-server=utf8mb4"),
								pulumi.String("--max_allowed_packet=64505856"),
								pulumi.String("--collation-server=utf8mb4_general_ci"),
								pulumi.String("--tls_version=TLSv1.2,TLSv1.3"),
								pulumi.String("--server-id=3"),
								pulumi.String("--log-bin=mysql-bin"),
								pulumi.String("--binlog-format=row"),
								pulumi.String("--slave_skip_errors=all"),
								pulumi.String("--binlog_expire_logs_seconds=86400"),
								pulumi.String("--init-file=/data/application/init.sql"),
							},
							VolumeMounts: corev1.VolumeMountArray{
								&corev1.VolumeMountArgs{
									Name:      pulumi.String("mysql-storage"),
									MountPath: pulumi.String("/var/lib/mysql"),
								},
								&corev1.VolumeMountArgs{
									Name:      pulumi.String("mysql-init"),
									MountPath: pulumi.String("/data/application"),
								},
							},
							Resources: &corev1.ResourceRequirementsArgs{
								Requests: pulumi.StringMap{
									"memory": pulumi.String("8Gi"),
									"cpu":    pulumi.String("4000m"),
								},
								Limits: pulumi.StringMap{
									"memory": pulumi.String("8Gi"),
									"cpu":    pulumi.String("4000m"),
								},
							},
						},
					},
					Volumes: corev1.VolumeArray{
						&corev1.VolumeArgs{
							Name: pulumi.String("mysql-storage"),
							PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSourceArgs{
								ClaimName: mysqlPVC.Metadata.Name().Elem(),
							},
						},
						&corev1.VolumeArgs{
							Name: pulumi.String("mysql-init"),
							ConfigMap: &corev1.ConfigMapVolumeSourceArgs{
								Name: mysqlInitConfigMap.Metadata.Name().Elem(),
							},
						},
					},
				},
			},
		},
	}, pulumi.Provider(provider))
	if err != nil {
		return nil, nil, err
	}

	// MySQL Service
	mysqlService, err := corev1.NewService(ctx, "mysql-service", &corev1.ServiceArgs{
		Metadata: &metav1.ObjectMetaArgs{
			Name:      pulumi.String(config.MySQL.Host),
			Namespace: namespace.Metadata.Name(),
		},
		Spec: &corev1.ServiceSpecArgs{
			Selector: pulumi.StringMap{
				"app": pulumi.String("mysql"),
			},
			Ports: corev1.ServicePortArray{
				&corev1.ServicePortArgs{
					Port:       pulumi.Int(3306),
					TargetPort: pulumi.Int(3306),
				},
			},
		},
	}, pulumi.Provider(provider))
	if err != nil {
		return nil, nil, err
	}

	return mysqlDeployment, mysqlService, nil
}

func createRedis(ctx *pulumi.Context, config *StackConfig, namespace *corev1.Namespace, provider pulumi.ProviderResource) (*v1.Deployment, *corev1.Service, error) {
	// Redis Deployment
	redisDeployment, err := v1.NewDeployment(ctx, "redis-deployment", &v1.DeploymentArgs{
		Metadata: &metav1.ObjectMetaArgs{
			Name:      pulumi.String("redis"),
			Namespace: namespace.Metadata.Name(),
		},
		Spec: &v1.DeploymentSpecArgs{
			Replicas: pulumi.Int(1),
			Selector: &metav1.LabelSelectorArgs{
				MatchLabels: pulumi.StringMap{
					"app": pulumi.String("redis"),
				},
			},
			Strategy: &v1.DeploymentStrategyArgs{
				Type: pulumi.String("Recreate"), // Aggressive update strategy: kill all pods before creating new ones
			},
			Template: &corev1.PodTemplateSpecArgs{
				Metadata: &metav1.ObjectMetaArgs{
					Labels: pulumi.StringMap{
						"app": pulumi.String("redis"),
					},
				},
				Spec: &corev1.PodSpecArgs{
					Containers: corev1.ContainerArray{
						&corev1.ContainerArgs{
							Name:  pulumi.String("redis"),
							Image: pulumi.String(config.Images.Redis),
							Ports: corev1.ContainerPortArray{
								&corev1.ContainerPortArgs{
									ContainerPort: pulumi.Int(6379),
								},
							},
							Command: pulumi.StringArray{
								pulumi.String("valkey-server"),
								pulumi.String("--requirepass"),
								pulumi.String("infini_rag_flow"),
								pulumi.String("--maxmemory"),
								pulumi.String("128mb"),
								pulumi.String("--maxmemory-policy"),
								pulumi.String("allkeys-lru"),
							},
						},
					},
				},
			},
		},
	}, pulumi.Provider(provider))
	if err != nil {
		return nil, nil, err
	}

	// Redis Service
	redisService, err := corev1.NewService(ctx, "redis-service", &corev1.ServiceArgs{
		Metadata: &metav1.ObjectMetaArgs{
			Name:      pulumi.String("redis"),
			Namespace: namespace.Metadata.Name(),
		},
		Spec: &corev1.ServiceSpecArgs{
			Selector: pulumi.StringMap{
				"app": pulumi.String("redis"),
			},
			Ports: corev1.ServicePortArray{
				&corev1.ServicePortArgs{
					Port:       pulumi.Int(6379),
					TargetPort: pulumi.Int(6379),
				},
			},
		},
	}, pulumi.Provider(provider))
	if err != nil {
		return nil, nil, err
	}

	return redisDeployment, redisService, nil
}

func createElasticsearch(ctx *pulumi.Context, config *StackConfig, namespace *corev1.Namespace, provider pulumi.ProviderResource) (*apiextensions.CustomResource, *corev1.Service, error) {
	ctx.Log.Info("Creating Elasticsearch resources...", &pulumi.LogArgs{})
	// Elasticsearch custom resource using ECK
	// https://www.elastic.co/docs/deploy-manage/users-roles/cluster-or-deployment-auth/managed-credentials-eck
	// To access Elastic resources, the operator manages a default user named elastic with the superuser role.
	// Its password is stored in a Secret named <elasticsearch-name>-es-elastic-user.
	// For example, if the Elasticsearch resource is named "elasticsearch", the secret will be "elasticsearch-es-elastic-user".
	es_name := "elasticsearch"

	// Read Elasticsearch configuration
	es_replicas_str := getConfig(ctx, "elasticsearch.node_amount", "1")
	es_replicas, _ := strconv.Atoi(es_replicas_str)
	es_storage := getStorageSize(ctx, "elasticsearch.disk_size", "2")
	es_memory_request := getStorageSize(ctx, "elasticsearch.ram_size", "10")
	es_cpu_cores := getConfig(ctx, "elasticsearch.cpu_cores", "4")

	// Derive other resources from es_memory_request
	// Parse memory request (e.g., "2Gi", "4Gi")
	es_memory_limit := es_memory_request
	es_jvm_memory := es_memory_request
	es_cpu_request := es_cpu_cores + "000m"
	es_cpu_limit := es_cpu_request

	// Simple derivation: assume format "XGi"
	if strings.HasSuffix(es_memory_request, "Gi") {
		memStr := strings.TrimSuffix(es_memory_request, "Gi")
		if memVal, err := strconv.ParseFloat(memStr, 64); err == nil {
			// memory_limit = ram_size (Required for memory locking)
			// IMPORTANT: When bootstrap.memory_lock=true, memory limits must equal requests
			// This is necessary for memory locking to work properly in Kubernetes
			es_memory_limit = es_memory_request

			// JVM memory = 50% of ram_size is safer for ES to avoid OOM
			jvmMem := memVal * 0.5
			if jvmMem < 1 {
				jvmMem = 1
			}
			es_jvm_memory = fmt.Sprintf("%.0fg", jvmMem)
		}
	}
	ctx.Log.Info(fmt.Sprintf("Elasticsearch derived resources: ram_size=%s, memory_limit=%s, jvm_memory=%s, cpu=%s",
		es_memory_request, es_memory_limit, es_jvm_memory, es_cpu_limit), &pulumi.LogArgs{})
	esResource, err := apiextensions.NewCustomResource(ctx, "elasticsearch", &apiextensions.CustomResourceArgs{
		ApiVersion: pulumi.String("elasticsearch.k8s.elastic.co/v1"),
		Kind:       pulumi.String("Elasticsearch"),
		Metadata: &metav1.ObjectMetaArgs{
			Name:      pulumi.String(es_name),
			Namespace: namespace.Metadata.Name(),
		},
		OtherFields: kubernetes.UntypedArgs{
			"spec": map[string]interface{}{
				"version": config.Env["STACK_VERSION"],
				"nodeSets": []interface{}{
					map[string]interface{}{
						"name":  "default",
						"count": es_replicas,
						"config": map[string]interface{}{
							"node.store.allow_mmap": false,
							// Note: xpack.security.enabled is managed by ECK Operator and cannot be set by users.
							// Attempting to configure it will result in a validation error:
							// "spec.nodeSets[0].config.xpack.security.enabled: Forbidden: Configuration setting is reserved for internal use"
							// ECK automatically enables security and manages credentials.
							//
							// Note: bootstrap.memory_lock is disabled because it requires
							// additional ulimit configuration that's difficult to set in Kubernetes
							// without privileged containers. For production, consider using
							// a custom init container to set ulimit or run Elasticsearch as root.
							//"bootstrap.memory_lock":       true,
							"cluster.max_shards_per_node": 8192,
						},
						"podTemplate": map[string]interface{}{
							"spec": map[string]interface{}{
								// Note: Using sysctls for Aliyun ACS compatibility.
								// According to Aliyun docs, vm.max_map_count is a supported sysctl parameter.
								// See: https://help.aliyun.com/zh/cs/user-guide/acs-pod-instance-overview
								// This replaces the privileged init container approach.
								"sysctls": map[string]interface{}{
									"vm.max_map_count": "262144",
								},
								"containers": []interface{}{
									map[string]interface{}{
										"name": "elasticsearch",
										// Use custom image registry if configured (for Aliyun environments)
										"image": config.Images.Elasticsearch,
										// Note: IPC_LOCK capability removed for Aliyun compatibility.
										// Memory locking (bootstrap.memory_lock) is disabled in config below.
										"env": []interface{}{
											map[string]interface{}{
												"name":  "ES_JAVA_OPTS",
												"value": fmt.Sprintf("-Xms%s -Xmx%s", es_jvm_memory, es_jvm_memory),
											},
										},
										"resources": map[string]interface{}{
											// Note: For memory locking (bootstrap.memory_lock=true) to work properly,
											// memory limits must equal requests. This allows Kubernetes to allocate
											// locked memory that won't be swapped out by the kernel.
											"limits": map[string]interface{}{
												"memory": es_memory_limit,
												"cpu":    es_cpu_limit,
											},
											"requests": map[string]interface{}{
												"memory": es_memory_request,
												"cpu":    es_cpu_request,
											},
										},
									},
								},
							},
						},
						"volumeClaimTemplates": []interface{}{
							map[string]interface{}{
								"metadata": map[string]interface{}{
									"name": "elasticsearch-data",
								},
								"spec": map[string]interface{}{
									"accessModes": []interface{}{"ReadWriteOnce"},
									"resources": map[string]interface{}{
										"requests": map[string]interface{}{
											"storage": es_storage,
										},
									},
									"storageClassName": getStorageClassMapValue(config),
								},
							},
						},
					},
				},

				"http": map[string]interface{}{
					"service": map[string]interface{}{
						"spec": map[string]interface{}{
							"type": "ClusterIP",
						},
					},
					"tls": map[string]interface{}{
						"selfSignedCertificate": map[string]interface{}{
							"disabled": false,
						},
					},
				},
			},
		},
	}, pulumi.Provider(provider), pulumi.IgnoreChanges([]string{
		// ECK Operator manages these fields and may modify them dynamically.
		// Ignoring them prevents Server-Side Apply conflicts between Pulumi and the Operator.
		//
		// IMPORTANT: spec.nodeSets is an array type ([]interface{}), so we cannot ignore
		// nested fields like "spec.nodeSets.podTemplate.metadata.labels" because
		// Pulumi cannot traverse into array elements for ignore patterns.
		//
		// We DO NOT ignore "spec.nodeSets" because important configurations need
		// to be managed by Pulumi:
		// - StorageClass changes (e.g., alicloud-disk-alltype) need to be applied
		// - Resource limits/requests need to be configurable
		// - Config settings (cluster.max_shards_per_node) need to be applied
		//
		// If you encounter SSA conflicts with nodeSets fields, consider:
		// 1. Deleting the Elasticsearch resource and letting Pulumi recreate it
		// 2. Or temporarily using broader ignore patterns (not recommended)
		//
		// Fields managed by ECK that should be ignored:
		"spec.auth",                            // ECK manages authentication credentials
		"spec.monitoring",                      // ECK manages monitoring configuration
		"spec.transport",                       // ECK manages transport layer configuration
		"spec.updateStrategy",                  // ECK manages update strategies
		"spec.http.tls.certificate",            // ECK manages TLS certificates
		"spec.http.tls.certificateAuthorities", // ECK manages certificate authorities
		"spec.transport.tls",                   // ECK manages transport TLS
		"spec.transport.service",               // ECK manages transport service
	}))
	if err != nil {
		return nil, nil, err
	}

	// Create a Service named "elasticsearch" that points to the ECK Elasticsearch pods
	// ECK creates a service named "elasticsearch-es-http", we'll create an additional service
	// with the name "elasticsearch" for compatibility with RAGFlow.
	esService, err := corev1.NewService(ctx, "es-service", &corev1.ServiceArgs{
		Metadata: &metav1.ObjectMetaArgs{
			Name:      pulumi.String("elasticsearch"),
			Namespace: namespace.Metadata.Name(),
		},
		Spec: &corev1.ServiceSpecArgs{
			Selector: pulumi.StringMap{
				"elasticsearch.k8s.elastic.co/cluster-name":     pulumi.String("elasticsearch"),
				"elasticsearch.k8s.elastic.co/statefulset-name": pulumi.String("elasticsearch-es-default"),
			},
			Ports: corev1.ServicePortArray{
				&corev1.ServicePortArgs{
					Port:       pulumi.Int(9200),
					TargetPort: pulumi.String("http"),
				},
			},
		},
	}, pulumi.Provider(provider))
	if err != nil {
		return nil, nil, err
	}
	return esResource, esService, nil
}

// createMinIO function removed - using Ceph RGW S3-compatible object storage instead

func createTEI(ctx *pulumi.Context, config *StackConfig, namespace *corev1.Namespace, provider pulumi.ProviderResource) (*v1.Deployment, *corev1.Service, error) {
	// TEI Deployment
	tei_host := config.Env["TEI_HOST"]
	teiDeployment, err := v1.NewDeployment(ctx, "tei-deployment", &v1.DeploymentArgs{
		Metadata: &metav1.ObjectMetaArgs{
			Name:      pulumi.String(tei_host),
			Namespace: namespace.Metadata.Name(),
		},
		Spec: &v1.DeploymentSpecArgs{
			Replicas: pulumi.Int(1),
			Selector: &metav1.LabelSelectorArgs{
				MatchLabels: pulumi.StringMap{
					"app": pulumi.String(tei_host),
				},
			},
			Strategy: &v1.DeploymentStrategyArgs{
				Type: pulumi.String("Recreate"), // Aggressive update strategy: kill all pods before creating new ones
			},
			Template: &corev1.PodTemplateSpecArgs{
				Metadata: &metav1.ObjectMetaArgs{
					Labels: pulumi.StringMap{
						"app": pulumi.String("tei"),
					},
				},
				Spec: &corev1.PodSpecArgs{
					Containers: corev1.ContainerArray{
						&corev1.ContainerArgs{
							Name:  pulumi.String("tei"),
							Image: pulumi.String(config.Images.TEI),
							Ports: corev1.ContainerPortArray{
								&corev1.ContainerPortArgs{
									ContainerPort: pulumi.Int(80),
								},
							},
							Args: pulumi.StringArray{
								pulumi.String("--model-id"),
								pulumi.String("/data/" + config.Env["TEI_MODEL"]),
								pulumi.String("--auto-truncate"),
							},
						},
					},
				},
			},
		},
	}, pulumi.Provider(provider))
	if err != nil {
		return nil, nil, err
	}

	// TEI Service
	teiService, err := corev1.NewService(ctx, "tei-service", &corev1.ServiceArgs{
		Metadata: &metav1.ObjectMetaArgs{
			Name:      pulumi.String(tei_host),
			Namespace: namespace.Metadata.Name(),
		},
		Spec: &corev1.ServiceSpecArgs{
			Selector: pulumi.StringMap{
				"app": pulumi.String(tei_host),
			},
			Ports: corev1.ServicePortArray{
				&corev1.ServicePortArgs{
					Port:       pulumi.Int(80),
					TargetPort: pulumi.Int(80),
				},
			},
		},
	}, pulumi.Provider(provider))
	if err != nil {
		return nil, nil, err
	}

	return teiDeployment, teiService, nil
}

func createRabbitMQ(ctx *pulumi.Context, config *StackConfig, namespace *corev1.Namespace, provider pulumi.ProviderResource) (*v1.Deployment, *corev1.Service, error) {
	// Read RabbitMQ configuration files from ../docker/rabbitmq-conf/
	// Match docker-compose-base.yml volume mounts:
	// - ./rabbitmq-conf/definitions.json:/etc/rabbitmq/definitions.json:ro
	// - ./rabbitmq-conf/definitions.conf:/etc/rabbitmq/conf.d/10-definitions.conf:ro
	// Note: Pulumi runs from pulumi_ragflow/ directory, so we need ../docker/
	definitionsContent, err := readFileContent("../docker/rabbitmq-conf/definitions.json")
	if err != nil {
		return nil, nil, fmt.Errorf("failed to read definitions.json: %w", err)
	}

	definitionsConfContent, err := readFileContent("../docker/rabbitmq-conf/definitions.conf")
	if err != nil {
		return nil, nil, fmt.Errorf("failed to read definitions.conf: %w", err)
	}

	// Read RabbitMQ PVC size configuration
	rabbitmqStorage := getStorageSize(ctx, "rabbitmq.disk_size", "1")

	// RabbitMQ PVC
	rabbitmqPVC, err := corev1.NewPersistentVolumeClaim(ctx, "rabbitmq-pvc", &corev1.PersistentVolumeClaimArgs{
		Metadata: &metav1.ObjectMetaArgs{
			Name:      pulumi.String("rabbitmq-pvc"),
			Namespace: namespace.Metadata.Name(),
		},
		Spec: &corev1.PersistentVolumeClaimSpecArgs{
			AccessModes: pulumi.StringArray{
				pulumi.String("ReadWriteOnce"),
			},
			Resources: &corev1.VolumeResourceRequirementsArgs{
				Requests: pulumi.StringMap{
					"storage": pulumi.String(rabbitmqStorage),
				},
			},
			StorageClassName: getStorageClassPtr(config),
		},
	}, pulumi.Provider(provider))
	if err != nil {
		return nil, nil, err
	}

	// RabbitMQ ConfigMap
	// Match docker-compose-base.yml volume mounts:
	// - ./rabbitmq-conf/definitions.json:/etc/rabbitmq/definitions.json:ro
	// - ./rabbitmq-conf/definitions.conf:/etc/rabbitmq/conf.d/10-definitions.conf:ro
	rabbitmqConfigMap, err := corev1.NewConfigMap(ctx, "rabbitmq-config", &corev1.ConfigMapArgs{
		Metadata: &metav1.ObjectMetaArgs{
			Name:      pulumi.String("rabbitmq-config"),
			Namespace: namespace.Metadata.Name(),
		},
		Data: pulumi.StringMap{
			"definitions.json":    pulumi.String(definitionsContent),
			"10-definitions.conf": pulumi.String(definitionsConfContent),
		},
	}, pulumi.Provider(provider))
	if err != nil {
		return nil, nil, err
	}

	// RabbitMQ Deployment
	rabbitmqDeployment, err := v1.NewDeployment(ctx, "rabbitmq-deployment", &v1.DeploymentArgs{
		Metadata: &metav1.ObjectMetaArgs{
			Name:      pulumi.String("rabbitmq"),
			Namespace: namespace.Metadata.Name(),
		},
		Spec: &v1.DeploymentSpecArgs{
			Replicas: pulumi.Int(1),
			Selector: &metav1.LabelSelectorArgs{
				MatchLabels: pulumi.StringMap{
					"app": pulumi.String("rabbitmq"),
				},
			},
			Strategy: &v1.DeploymentStrategyArgs{
				Type: pulumi.String("Recreate"), // Required for stateful singleton pod
			},
			Template: &corev1.PodTemplateSpecArgs{
				Metadata: &metav1.ObjectMetaArgs{
					Labels: pulumi.StringMap{
						"app": pulumi.String("rabbitmq"),
					},
				},
				Spec: &corev1.PodSpecArgs{
					Containers: corev1.ContainerArray{
						&corev1.ContainerArgs{
							Name:  pulumi.String("rabbitmq"),
							Image: pulumi.String(config.Images.RabbitMQ),
							Ports: corev1.ContainerPortArray{
								&corev1.ContainerPortArgs{
									ContainerPort: pulumi.Int(5672),
									Name:          pulumi.String("amqp"),
								},
								&corev1.ContainerPortArgs{
									ContainerPort: pulumi.Int(15672),
									Name:          pulumi.String("management"),
								},
								&corev1.ContainerPortArgs{
									ContainerPort: pulumi.Int(15692),
									Name:          pulumi.String("prometheus"),
								},
							},
							Env: corev1.EnvVarArray{
								&corev1.EnvVarArgs{
									Name:  pulumi.String("RABBITMQ_DEFAULT_USER"),
									Value: pulumi.String("rag_flow"),
								},
								&corev1.EnvVarArgs{
									Name:  pulumi.String("RABBITMQ_DEFAULT_PASS"),
									Value: pulumi.String("infini_rag_flow"),
								},
							},
							VolumeMounts: corev1.VolumeMountArray{
								&corev1.VolumeMountArgs{
									Name:      pulumi.String("rabbitmq-storage"),
									MountPath: pulumi.String("/var/lib/rabbitmq"),
								},
								&corev1.VolumeMountArgs{
									Name:      pulumi.String("rabbitmq-definitions"),
									MountPath: pulumi.String("/etc/rabbitmq/definitions.json"),
									SubPath:   pulumi.String("definitions.json"),
								},
								&corev1.VolumeMountArgs{
									Name:      pulumi.String("rabbitmq-definitions-conf"),
									MountPath: pulumi.String("/etc/rabbitmq/conf.d/10-definitions.conf"),
									SubPath:   pulumi.String("10-definitions.conf"),
								},
							},
						},
					},
					Volumes: corev1.VolumeArray{
						&corev1.VolumeArgs{
							Name: pulumi.String("rabbitmq-storage"),
							PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSourceArgs{
								ClaimName: rabbitmqPVC.Metadata.Name().Elem(),
							},
						},
						&corev1.VolumeArgs{
							Name: pulumi.String("rabbitmq-definitions"),
							ConfigMap: &corev1.ConfigMapVolumeSourceArgs{
								Name: rabbitmqConfigMap.Metadata.Name().Elem(),
								Items: corev1.KeyToPathArray{
									&corev1.KeyToPathArgs{
										Key:  pulumi.String("definitions.json"),
										Path: pulumi.String("definitions.json"),
									},
								},
							},
						},
						&corev1.VolumeArgs{
							Name: pulumi.String("rabbitmq-definitions-conf"),
							ConfigMap: &corev1.ConfigMapVolumeSourceArgs{
								Name: rabbitmqConfigMap.Metadata.Name().Elem(),
								Items: corev1.KeyToPathArray{
									&corev1.KeyToPathArgs{
										Key:  pulumi.String("10-definitions.conf"),
										Path: pulumi.String("10-definitions.conf"),
									},
								},
							},
						},
					},
				},
			},
		},
	}, pulumi.Provider(provider))
	if err != nil {
		return nil, nil, err
	}

	// RabbitMQ Service
	rabbitmqService, err := corev1.NewService(ctx, "rabbitmq-service", &corev1.ServiceArgs{
		Metadata: &metav1.ObjectMetaArgs{
			Name:      pulumi.String("rabbitmq"),
			Namespace: namespace.Metadata.Name(),
		},
		Spec: &corev1.ServiceSpecArgs{
			Selector: pulumi.StringMap{
				"app": pulumi.String("rabbitmq"),
			},
			Ports: corev1.ServicePortArray{
				&corev1.ServicePortArgs{
					Name:       pulumi.String("amqp"),
					Port:       pulumi.Int(5672),
					TargetPort: pulumi.Int(5672),
				},
				&corev1.ServicePortArgs{
					Name:       pulumi.String("management"),
					Port:       pulumi.Int(15672),
					TargetPort: pulumi.Int(15672),
				},
				&corev1.ServicePortArgs{
					Name:       pulumi.String("prometheus"),
					Port:       pulumi.Int(15692),
					TargetPort: pulumi.Int(15692),
				},
			},
		},
	}, pulumi.Provider(provider))
	if err != nil {
		return nil, nil, err
	}

	return rabbitmqDeployment, rabbitmqService, nil
}

// buildCommonEnvVars builds common environment variables for RAGFlow and Parser pods
func buildCommonEnvVars(config *StackConfig) corev1.EnvVarArray {
	envVars := corev1.EnvVarArray{}

	// Add common environment variables (skip ES/MYSQL specific ones, handled below)
	skipKeys := map[string]bool{"ES_PROTOCOL": true, "ES_HOST": true, "ES_PORT": true, "ES_PASSWORD": true,
		"MYSQL_HOST": true, "MYSQL_PORT": true, "MYSQL_PASSWORD": true}

	keys := make([]string, 0, len(config.Env))
	for k := range config.Env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if skipKeys[k] {
			continue
		}
		envVars = append(envVars, &corev1.EnvVarArgs{Name: pulumi.String(k), Value: pulumi.String(config.Env[k])})
	}

	// ===== MySQL Configuration =====
	if config.MySQL.External && config.MySQLEndpointOutput != (pulumi.StringOutput{}) {
		// Case 1: MySQL from StackReference (external from infra stack)
		envVars = append(envVars,
			&corev1.EnvVarArgs{Name: pulumi.String("MYSQL_HOST"), Value: config.MySQLEndpointOutput},
			&corev1.EnvVarArgs{Name: pulumi.String("MYSQL_PORT"), Value: config.MySQLPortOutput},
			&corev1.EnvVarArgs{Name: pulumi.String("MYSQL_PASSWORD"), Value: config.MySQLPasswordOutput},
		)
	} else {
		// Case 2: MySQL from config (external, not from StackReference)
		// Case 3: Internal MySQL (K8s service)
		// Must explicitly add MYSQL_PASSWORD since it's in skipKeys
		envVars = append(envVars,
			&corev1.EnvVarArgs{Name: pulumi.String("MYSQL_HOST"), Value: pulumi.String(config.MySQL.Host)},
			&corev1.EnvVarArgs{Name: pulumi.String("MYSQL_PORT"), Value: pulumi.String(config.MySQL.Port)},
			&corev1.EnvVarArgs{Name: pulumi.String("MYSQL_PASSWORD"), Value: pulumi.String(config.MySQL.Password)},
		)
	}

	// ===== Elasticsearch Configuration =====
	useStackRefES := config.ES.External && config.ESEndpointOutput != (pulumi.StringOutput{})

	if useStackRefES {
		// Case 1: ES from StackReference (external from infra stack)
		envVars = append(envVars,
			&corev1.EnvVarArgs{Name: pulumi.String("ES_HOST"), Value: config.ESEndpointOutput},
			&corev1.EnvVarArgs{Name: pulumi.String("ES_PORT"), Value: config.ESPortOutput},
			&corev1.EnvVarArgs{Name: pulumi.String("ES_PROTOCOL"), Value: config.ESProtocolOutput},
			&corev1.EnvVarArgs{Name: pulumi.String("ES_USER"), Value: config.ESUsernameOutput},
			&corev1.EnvVarArgs{Name: pulumi.String("ELASTIC_PASSWORD"), Value: config.ESPasswordOutput},
		)
	} else if config.ES.External {
		// Case 2: ES from config (external, not from StackReference)
		envVars = append(envVars,
			&corev1.EnvVarArgs{Name: pulumi.String("ES_HOST"), Value: pulumi.String(config.ES.Host)},
			&corev1.EnvVarArgs{Name: pulumi.String("ES_PORT"), Value: pulumi.String(config.ES.Port)},
			&corev1.EnvVarArgs{Name: pulumi.String("ES_PROTOCOL"), Value: pulumi.String(config.ES.Protocol)},
			&corev1.EnvVarArgs{Name: pulumi.String("ES_USER"), Value: pulumi.String(config.ES.Username)},
			&corev1.EnvVarArgs{Name: pulumi.String("ELASTIC_PASSWORD"), Value: pulumi.String(config.ES.Password)},
		)
	} else {
		// Case 3: Internal ES (ECK managed)
		esProtocol, esHost := "https", "elasticsearch-es-http"
		esSecretName := "elasticsearch-es-elastic-user"

		envVars = append(envVars,
			&corev1.EnvVarArgs{Name: pulumi.String("ES_HOST"), Value: pulumi.String(esHost)},
			&corev1.EnvVarArgs{Name: pulumi.String("ES_PORT"), Value: pulumi.String(config.ES.Port)},
			&corev1.EnvVarArgs{Name: pulumi.String("ES_PROTOCOL"), Value: pulumi.String(esProtocol)},
			&corev1.EnvVarArgs{Name: pulumi.String("ES_USER"), Value: pulumi.String("elastic")},
			&corev1.EnvVarArgs{Name: pulumi.String("ELASTIC_PASSWORD"), ValueFrom: &corev1.EnvVarSourceArgs{SecretKeyRef: &corev1.SecretKeySelectorArgs{Name: pulumi.String(esSecretName), Key: pulumi.String("elastic")}}},
		)
	}

	return envVars
}

// createEntrypointConfigMap creates a ConfigMap with the entrypoint script that includes config generation
func createEntrypointConfigMap(ctx *pulumi.Context, namespace *corev1.Namespace, provider pulumi.ProviderResource,
	name string, entrypointPath string, addConfigGen bool) (*corev1.ConfigMap, error) {

	// Read entrypoint script
	entrypointContent, err := readFileContent(entrypointPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read %s: %w", entrypointPath, err)
	}

	// Prepend config generation logic if requested
	if addConfigGen {
		entrypointHeader := `#!/bin/bash

# -----------------------------------------------------------------------------
# Replace env variables in the service_conf.yaml file
# -----------------------------------------------------------------------------
CONF_DIR="/ragflow/conf"
TEMPLATE_FILE="${CONF_DIR}/service_conf.yaml.template"
CONF_FILE="${CONF_DIR}/service_conf.yaml"

rm -f "${CONF_FILE}"
while IFS= read -r line || [[ -n "$line" ]]; do
    eval "echo \"$line\"" >> "${CONF_FILE}"
done < "${TEMPLATE_FILE}"

`
		entrypointContent = entrypointHeader + entrypointContent
	}

	// Create ConfigMap
	configMap, err := corev1.NewConfigMap(ctx, name, &corev1.ConfigMapArgs{
		Metadata: &metav1.ObjectMetaArgs{
			Name:      pulumi.String(name),
			Namespace: namespace.Metadata.Name(),
		},
		Data: pulumi.StringMap{
			"entrypoint.sh": pulumi.String(entrypointContent),
		},
	}, pulumi.Provider(provider))
	if err != nil {
		return nil, err
	}

	return configMap, nil
}

// DeploymentConfig holds configuration differences between RAGFlow and Parser deployments
type DeploymentConfig struct {
	Name              string
	EntrypointFile    string
	Replicas          int
	ContainerName     string
	Command           pulumi.StringArray
	ContainerArgs     pulumi.StringArray
	AdditionalEnvVars corev1.EnvVarArray
	ContainerPorts    corev1.ContainerPortArray
	Resources         *corev1.ResourceRequirementsArgs
	InitContainers    corev1.ContainerArray
	VolumeMounts      corev1.VolumeMountArray
	Volumes           corev1.VolumeArray
	CreateService     bool
	ServicePorts      []ServicePortConfig
}

// ServicePortConfig holds service port configuration
type ServicePortConfig struct {
	Name       string
	Port       int
	TargetPort int
}

// createESWaitInitContainer creates an init container that waits for Elasticsearch to be ready
func createESWaitInitContainer(config *StackConfig) *corev1.ContainerArgs {
	// Build common environment variables to get ES configuration
	envVars := buildCommonEnvVars(config)

	// Unified ES wait command compatible with both internal and external ES
	// Uses curl with -k to support self-signed certificates (common in internal ECK)
	esWaitCommand := pulumi.String(`until curl -s -k -u "${ES_USER}:${ELASTIC_PASSWORD}" "${ES_PROTOCOL}://${ES_HOST}:${ES_PORT}/_cluster/health" | grep -q '"status":"green"\|"status":"yellow"'; do echo "Waiting for Elasticsearch at ${ES_HOST}..."; sleep 5; done; echo "Elasticsearch is ready."`)

	return &corev1.ContainerArgs{
		Name:  pulumi.String("wait-for-elasticsearch"),
		Image: pulumi.String(config.Images.Curl),
		Env:   envVars,
		Command: pulumi.StringArray{
			pulumi.String("sh"),
			pulumi.String("-c"),
			esWaitCommand,
		},
	}
}

func createRAGFlowDeployment(ctx *pulumi.Context, config *StackConfig, namespace *corev1.Namespace, provider pulumi.ProviderResource, registrySecret *corev1.Secret) (*v1.Deployment, *corev1.Service, *v1.Deployment, error) {
	// Read parser replicas from config, default to 1
	parserReplicasStr := getConfig(ctx, "parser.replicas", "1")
	parserReplicas, err := strconv.Atoi(parserReplicasStr)
	if err != nil {
		parserReplicas = 1
	}

	// Read worker counts from config to reduce fsnotify usage
	// Each worker process may use fsnotify watchers through RabbitMQ client library (pika)
	// Default values: WS=3, RAPTOR=3, GRAPHRAG=3, RESUME=1 (total 10 workers)
	// Reduced to: WS=1, RAPTOR=1, GRAPHRAG=1, RESUME=0 (total 3 workers)
	// This reduces the number of fsnotify instances and helps avoid "too many open files" error
	parserWSWorkers := getConfig(ctx, "parser.ws_workers", "1")
	parserRaptorWorkers := getConfig(ctx, "parser.raptor_workers", "1")
	parserGraphragWorkers := getConfig(ctx, "parser.graphrag_workers", "1")
	parserResumeWorkers := getConfig(ctx, "parser.resume_workers", "1")

	// RAGFlow Deployment
	ragflowDepCfg := DeploymentConfig{
		Name:           "ragflow",
		EntrypointFile: "../docker/entrypoint.sh",
		Replicas:       config.RAGFlow.Replicas,
		ContainerName:  "ragflow",
		Command:        nil, // Uses default container command
		ContainerArgs: pulumi.StringArray{
			pulumi.String("--enable-adminserver"),
		},
		AdditionalEnvVars: corev1.EnvVarArray{},
		ContainerPorts: corev1.ContainerPortArray{
			&corev1.ContainerPortArgs{
				ContainerPort: pulumi.Int(80), // static pages (standard)
			},
			&corev1.ContainerPortArgs{
				ContainerPort: pulumi.Int(9380), // web and api server
			},
			&corev1.ContainerPortArgs{
				ContainerPort: pulumi.Int(9381), // admin server
			},
			&corev1.ContainerPortArgs{
				ContainerPort: pulumi.Int(9382), // mcp server
			},
		},
		Resources: &corev1.ResourceRequirementsArgs{
			Requests: pulumi.StringMap{
				"memory": pulumi.String("16Gi"),
				"cpu":    pulumi.String("1000m"),
			},
			Limits: pulumi.StringMap{
				"memory": pulumi.String("24Gi"),
				"cpu":    pulumi.String("8000m"),
			},
		},
		VolumeMounts:  corev1.VolumeMountArray{},
		Volumes:       corev1.VolumeArray{},
		CreateService: true,
		ServicePorts: []ServicePortConfig{
			{Name: "api", Port: parsePort(config.Env["SVR_HTTP_PORT"]), TargetPort: 9380},
			{Name: "admin", Port: parsePort(config.Env["ADMIN_SVR_HTTP_PORT"]), TargetPort: 9381},
			{Name: "mcp", Port: parsePort(config.Env["SVR_MCP_PORT"]), TargetPort: 9382},
			{Name: "http", Port: 80, TargetPort: 80}, // Frontend nginx (standard)
		},
	}

	// Build init containers for RAGFlow
	var initContainers corev1.ContainerArray

	// MySQL database initialization (only for external MySQL)
	// If using external MySQL, create the database if it doesn't exist
	if config.MySQL.External {
		// Build MySQL host and password values - use StackReference output if available
		var mysqlHostValue pulumi.StringInput
		var mysqlPasswordValue pulumi.StringInput

		if config.MySQLEndpointOutput != (pulumi.StringOutput{}) {
			// Use MySQL endpoint from StackReference
			mysqlHostValue = config.MySQLEndpointOutput
		} else {
			// Use configured MYSQL_HOST from config
			mysqlHostValue = pulumi.String(config.Env["MYSQL_HOST"])
		}

		if config.MySQLPasswordOutput != (pulumi.StringOutput{}) {
			// Use MySQL password from StackReference
			mysqlPasswordValue = config.MySQLPasswordOutput
		} else {
			// Use configured MySQL password
			mysqlPasswordValue = pulumi.String(config.MySQL.Password)
		}

		initContainers = append(initContainers, &corev1.ContainerArgs{
			Name:  pulumi.String("init-mysql-database"),
			Image: pulumi.String(config.Images.MySQL),
			Env: corev1.EnvVarArray{
				&corev1.EnvVarArgs{
					Name:  pulumi.String("MYSQL_PWD"),
					Value: mysqlPasswordValue,
				},
			},
			Command: pulumi.StringArray{
				pulumi.String("sh"),
				pulumi.String("-c"),
				pulumi.Sprintf(
					"set +e && MYSQL_HOST=\"%s\" MYSQL_DBNAME=\"%s\" && "+
						"echo \"MySQL Init: Creating database ${MYSQL_DBNAME} if not exists...\" && "+
						"mysql -h \"${MYSQL_HOST}\" -u root -e \"CREATE DATABASE IF NOT EXISTS \\`${MYSQL_DBNAME}\\`;\" 2>/dev/null && "+
						"echo \"MySQL Init: Database ${MYSQL_DBNAME} created or already exists\" || "+
						"(echo \"MySQL Init: Error: Failed to create database\" && exit 1)",
					mysqlHostValue,
					pulumi.String(config.Env["MYSQL_DBNAME"]),
				),
			},
		})
	}

	// S3 bucket initialization
	// Uses AWS CLI commands compatible with both AWS S3 and Aliyun OSS
	if bucket, exists := config.Env["S3_BUCKET"]; exists && bucket != "" {
		initContainers = append(initContainers, &corev1.ContainerArgs{
			Name:  pulumi.String("init-s3-bucket"),
			Image: pulumi.String(config.Images.AWSCLI),
			Env: corev1.EnvVarArray{
				&corev1.EnvVarArgs{
					Name:  pulumi.String("AWS_ACCESS_KEY_ID"),
					Value: pulumi.String(config.Env["S3_ACCESS_KEY"]),
				},
				&corev1.EnvVarArgs{
					Name:  pulumi.String("AWS_SECRET_ACCESS_KEY"),
					Value: pulumi.String(config.Env["S3_SECRET_KEY"]),
				},
				&corev1.EnvVarArgs{
					Name:  pulumi.String("AWS_DEFAULT_REGION"),
					Value: pulumi.String(config.Env["S3_REGION"]),
				},
				&corev1.EnvVarArgs{
					Name:  pulumi.String("AWS_ENDPOINT_URL"),
					Value: pulumi.String(config.Env["S3_ENDPOINT"]),
				},
			},
			Command: pulumi.StringArray{
				pulumi.String("sh"),
				pulumi.String("-c"),
				pulumi.Sprintf(`
					set +e
					S3_ENDPOINT="%s"
					S3_BUCKET="%s"

					log() { echo "S3 Init: $*"; }

					# Configure AWS CLI for Aliyun OSS compatibility
					if echo "${S3_ENDPOINT}" | grep -q "aliyuncs.com"; then
						aws configure set default.s3.addressing_style virtual
					fi

					# Verify bucket exists and is accessible
					if aws s3 ls "s3://${S3_BUCKET}" --endpoint-url "${S3_ENDPOINT}" >/dev/null 2>&1; then
						log "Bucket verified"
						exit 0
					fi

					log "Bucket not found, attempting creation..."
					if aws s3 mb "s3://${S3_BUCKET}" --endpoint-url "${S3_ENDPOINT}"; then
						log "Bucket created successfully"
						exit 0
					fi

					# Bucket not accessible - provide guidance
					if echo "${S3_ENDPOINT}" | grep -q "aliyuncs.com"; then
						log "Error: Aliyun OSS bucket not accessible"
						log "Create bucket: https://oss.console.aliyun.com/"
						log "Name: ${S3_BUCKET}, Region: cn-shanghai"
					else
						log "Error: Bucket not accessible"
					fi
					exit 1
				`, config.Env["S3_ENDPOINT"], config.Env["S3_BUCKET"]),
			},
		})
	}

	// ES wait init container (added LAST to ensure it runs after all other init containers)
	// This ensures MySQL and S3 initialization complete before checking ES readiness
	esWaitContainer := createESWaitInitContainer(config)
	initContainers = append(initContainers, esWaitContainer)

	ragflowDepCfg.InitContainers = initContainers

	// Create RAGFlow deployment and service
	deployment, service, err := createRAGFlowAppDeployment(ctx, config, namespace, provider, ragflowDepCfg, registrySecret)
	if err != nil {
		return nil, nil, nil, err
	}

	// Parser Deployment
	parserDepCfg := DeploymentConfig{
		Name:           "parser",
		EntrypointFile: "../docker/entrypoint-parser.sh",
		Replicas:       parserReplicas,
		ContainerName:  "parser",
		Command: pulumi.StringArray{
			pulumi.String("/bin/bash"),
			pulumi.String("/ragflow/entrypoint-parser.sh"),
		},
		AdditionalEnvVars: corev1.EnvVarArray{
			&corev1.EnvVarArgs{
				Name:  pulumi.String("DEEPDOC_URL"),
				Value: pulumi.String("http://deepdoc:8000"),
			},
			&corev1.EnvVarArgs{
				Name:  pulumi.String("MAX_FILE_NUM_PER_USER"),
				Value: pulumi.String("100"),
			},
			&corev1.EnvVarArgs{
				Name:  pulumi.String("TZ"),
				Value: pulumi.String("Asia/Shanghai"),
			},
			&corev1.EnvVarArgs{
				Name:  pulumi.String("ENABLE_TIMEOUT_ASSERTION"),
				Value: pulumi.String("1"),
			},
			&corev1.EnvVarArgs{
				Name:  pulumi.String("WS_WORKERS"),
				Value: pulumi.String(parserWSWorkers),
			},
			&corev1.EnvVarArgs{
				Name:  pulumi.String("RAPTOR_WORKERS"),
				Value: pulumi.String(parserRaptorWorkers),
			},
			&corev1.EnvVarArgs{
				Name:  pulumi.String("GRAPHRAG_WORKERS"),
				Value: pulumi.String(parserGraphragWorkers),
			},
			&corev1.EnvVarArgs{
				Name:  pulumi.String("RESUME_WORKERS"),
				Value: pulumi.String(parserResumeWorkers),
			},
		},
		Resources: &corev1.ResourceRequirementsArgs{
			Requests: pulumi.StringMap{
				"memory": pulumi.String("4Gi"),
				"cpu":    pulumi.String("1000m"),
			},
			Limits: pulumi.StringMap{
				"memory": pulumi.String("8Gi"),
				"cpu":    pulumi.String("4000m"),
			},
		},
		VolumeMounts: corev1.VolumeMountArray{
			&corev1.VolumeMountArgs{
				Name:      pulumi.String("entrypoint"),
				MountPath: pulumi.String("/ragflow/entrypoint-parser.sh"),
				SubPath:   pulumi.String("entrypoint.sh"),
			},
			&corev1.VolumeMountArgs{
				Name:      pulumi.String("logs"),
				MountPath: pulumi.String("/ragflow/logs"),
			},
		},
		Volumes: corev1.VolumeArray{
			&corev1.VolumeArgs{
				Name:     pulumi.String("logs"),
				EmptyDir: &corev1.EmptyDirVolumeSourceArgs{},
			},
		},
		CreateService:  false,
		InitContainers: corev1.ContainerArray{esWaitContainer}, //parser also needs ES wait
	}

	parserDeployment, _, err := createRAGFlowAppDeployment(ctx, config, namespace, provider, parserDepCfg, registrySecret)
	if err != nil {
		return nil, nil, nil, err
	}

	return deployment, service, parserDeployment, nil
}

// createRAGFlowAppDeployment creates a RAGFlow or Parser deployment based on the provided configuration
func createRAGFlowAppDeployment(ctx *pulumi.Context, config *StackConfig, namespace *corev1.Namespace, provider pulumi.ProviderResource, depCfg DeploymentConfig, registrySecret *corev1.Secret) (*v1.Deployment, *corev1.Service, error) {
	// Build common environment variables using shared function
	commonEnvVars := buildCommonEnvVars(config)

	// Add additional environment variables
	envVars := commonEnvVars
	if len(depCfg.AdditionalEnvVars) > 0 {
		envVars = append(commonEnvVars, depCfg.AdditionalEnvVars...)
	}

	// Create entrypoint ConfigMap
	entrypointConfigMap, err := createEntrypointConfigMap(ctx, namespace, provider, depCfg.Name+"-config", depCfg.EntrypointFile, true)
	if err != nil {
		return nil, nil, err
	}

	// Prepare volumes - add entrypoint volume if Command is set (parser case)
	volumes := depCfg.Volumes
	if depCfg.Command != nil {
		entrypointVolume := &corev1.VolumeArgs{
			Name: pulumi.String("entrypoint"),
			ConfigMap: &corev1.ConfigMapVolumeSourceArgs{
				Name:        entrypointConfigMap.Metadata.Name().Elem(),
				DefaultMode: pulumi.Int(0755),
				Items: corev1.KeyToPathArray{
					&corev1.KeyToPathArgs{
						Key:  pulumi.String("entrypoint.sh"),
						Path: pulumi.String("entrypoint.sh"),
					},
				},
			},
		}
		volumes = append(volumes, entrypointVolume)
	}

	// Build container args
	containerArgs := &corev1.ContainerArgs{
		Name:  pulumi.String(depCfg.ContainerName),
		Image: pulumi.String(config.RAGFlow.Image),
		Env:   envVars,
	}

	// Set command if provided
	if depCfg.Command != nil {
		containerArgs.Command = depCfg.Command
	}

	// Set args if provided
	if depCfg.ContainerArgs != nil {
		containerArgs.Args = depCfg.ContainerArgs
	}

	// Set ports if provided
	if depCfg.ContainerPorts != nil {
		containerArgs.Ports = depCfg.ContainerPorts
	}

	// Set volume mounts
	containerArgs.VolumeMounts = depCfg.VolumeMounts

	// Set resources
	containerArgs.Resources = depCfg.Resources

	// Build imagePullSecrets array if registry secret is available
	var imagePullSecrets corev1.LocalObjectReferenceArray
	if registrySecret != nil {
		imagePullSecrets = corev1.LocalObjectReferenceArray{
			&corev1.LocalObjectReferenceArgs{
				Name: registrySecret.Metadata.Name(),
			},
		}
	}

	// Create deployment
	deploymentName := depCfg.Name + "-deployment"
	deployment, err := v1.NewDeployment(ctx, deploymentName, &v1.DeploymentArgs{
		Metadata: &metav1.ObjectMetaArgs{
			Name:      pulumi.String(depCfg.Name),
			Namespace: namespace.Metadata.Name(),
		},
		Spec: &v1.DeploymentSpecArgs{
			Replicas: pulumi.Int(depCfg.Replicas),
			Selector: &metav1.LabelSelectorArgs{
				MatchLabels: pulumi.StringMap{
					"app": pulumi.String(depCfg.Name),
				},
			},
			Strategy: &v1.DeploymentStrategyArgs{
				Type: pulumi.String("Recreate"), // Aggressive update strategy: kill all pods before creating new ones
			},
			Template: &corev1.PodTemplateSpecArgs{
				Metadata: &metav1.ObjectMetaArgs{
					Labels: pulumi.StringMap{
						"app": pulumi.String(depCfg.Name),
					},
				},
				Spec: &corev1.PodSpecArgs{
					InitContainers: depCfg.InitContainers,
					Containers: corev1.ContainerArray{
						containerArgs,
					},
					Volumes:          volumes,
					ImagePullSecrets: imagePullSecrets,
				},
			},
		},
	}, pulumi.Provider(provider))
	if err != nil {
		return nil, nil, err
	}

	// Create service if requested
	var service *corev1.Service
	if depCfg.CreateService {
		serviceName := depCfg.Name + "-service"
		servicePorts := corev1.ServicePortArray{}
		for _, sp := range depCfg.ServicePorts {
			servicePorts = append(servicePorts, &corev1.ServicePortArgs{
				Name:       pulumi.String(sp.Name),
				Port:       pulumi.Int(sp.Port),
				TargetPort: pulumi.Int(sp.TargetPort),
			})
		}

		service, err = corev1.NewService(ctx, serviceName, &corev1.ServiceArgs{
			Metadata: &metav1.ObjectMetaArgs{
				Name:      pulumi.String(depCfg.Name),
				Namespace: namespace.Metadata.Name(),
			},
			Spec: &corev1.ServiceSpecArgs{
				Selector: pulumi.StringMap{
					"app": pulumi.String(depCfg.Name),
				},
				Ports: servicePorts,
				Type:  pulumi.String("ClusterIP"),
			},
		}, pulumi.Provider(provider))
		if err != nil {
			return nil, nil, err
		}
	}

	return deployment, service, nil
}

func detectGatewayType(ctx *pulumi.Context, provider pulumi.ProviderResource) (string, error) {
	// Use client-go dynamic client to list GatewayClass resources in the cluster and pick by priority.

	// Build config using standard loading rules
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	configOverrides := &clientcmd.ConfigOverrides{}
	kubeConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, configOverrides)
	cfg, err := kubeConfig.ClientConfig()
	if err != nil {
		ctx.Log.Info("Failed to load kube config", &pulumi.LogArgs{})
		return "", fmt.Errorf("failed to load kube config: %w", err)
	}

	dyn, err := dynamic.NewForConfig(cfg)
	if err != nil {
		ctx.Log.Info("Failed to create dynamic client", &pulumi.LogArgs{})
		return "", fmt.Errorf("failed to create dynamic client: %w", err)
	}

	gvr := schema.GroupVersionResource{Group: "gateway.networking.k8s.io", Version: "v1", Resource: "gatewayclasses"}
	lst, err := dyn.Resource(gvr).List(context.Background(), k8smetav1.ListOptions{})
	if err != nil {
		ctx.Log.Info("Failed to list gatewayclasses", &pulumi.LogArgs{})
		return "", fmt.Errorf("failed to list gatewayclasses: %w", err)
	}

	var names []string
	for _, it := range lst.Items {
		n := it.GetName()
		if n == "" {
			continue
		}
		ctx.Log.Info(fmt.Sprintf("Found GatewayClass: %s", n), &pulumi.LogArgs{})
		// Prefer Cilium
		if strings.Contains(strings.ToLower(n), "cilium") {
			ctx.Log.Info(fmt.Sprintf("Selected GatewayClass (preferred Cilium): %s", n), &pulumi.LogArgs{})
			return n, nil
		}
		names = append(names, n)
	}

	// Otherwise, pick the first GatewayClass in sorted order for deterministic behavior.
	if len(names) > 0 {
		sort.Strings(names)
		ctx.Log.Info(fmt.Sprintf("Selected GatewayClass (first in sorted list): %s", names[0]), &pulumi.LogArgs{})
		return names[0], nil
	}

	ctx.Log.Info("No GatewayClass found in cluster", &pulumi.LogArgs{})
	return "", fmt.Errorf("no GatewayClass found in cluster")
}

func createCR(ctx *pulumi.Context, name, apiVersion, kind string, namespace pulumi.StringPtrInput, spec map[string]interface{}, provider pulumi.ProviderResource, annotations map[string]string) error {
	metadataArgs := &metav1.ObjectMetaArgs{
		Name:      pulumi.String(name),
		Namespace: namespace,
	}
	// Add annotations if provided
	if annotations != nil && len(annotations) > 0 {
		metadataArgs.Annotations = pulumi.ToStringMap(annotations)
	}

	_, err := apiextensions.NewCustomResource(ctx, name, &apiextensions.CustomResourceArgs{
		ApiVersion: pulumi.String(apiVersion),
		Kind:       pulumi.String(kind),
		Metadata:   metadataArgs,
		OtherFields: kubernetes.UntypedArgs{
			"spec": spec,
		},
	}, pulumi.Provider(provider))
	return err
}

// createHTTPRouteForPort creates a single HTTPRoute for a specific service port.
// This is a workaround for Aliyun ALB Gateway API bug where a single HTTPRoute
// referencing multiple ports of the same service causes server group registration failures.
// Each port requires its own HTTPRoute resource.
func createHTTPRouteForPort(
	ctx *pulumi.Context,
	routeName string,
	serviceName pulumi.StringOutput,
	gatewayNsName, httpRouteNsName string,
	provider pulumi.ProviderResource,
	pathPrefixes []string,
	port int,
) error {
	// Build parentRefs list (HTTP only)
	parentRefs := []interface{}{
		map[string]interface{}{
			"name":        "ragflow-gateway",
			"namespace":   gatewayNsName,
			"sectionName": "http", // Port 80
		},
	}

	// Build matches for all path prefixes
	matches := make([]interface{}, len(pathPrefixes))
	for i, prefix := range pathPrefixes {
		matches[i] = map[string]interface{}{
			"path": map[string]interface{}{
				"type":  "PathPrefix",
				"value": prefix,
			},
		}
	}

	routeSpec := map[string]interface{}{
		"parentRefs": parentRefs,
		"rules": []interface{}{
			map[string]interface{}{
				"matches": matches,
				"backendRefs": []interface{}{
					map[string]interface{}{
						"kind": "Service",
						"name": serviceName,
						"port": port,
					},
				},
			},
		},
	}

	ctx.Log.Info(fmt.Sprintf("Creating HTTPRoute '%s' for port %d with paths: %v", routeName, port, pathPrefixes), &pulumi.LogArgs{})
	if err := createCR(ctx, routeName, "gateway.networking.k8s.io/v1", "HTTPRoute", pulumi.String(httpRouteNsName), routeSpec, provider, nil); err != nil {
		ctx.Log.Error(fmt.Sprintf("Failed to create HTTPRoute '%s': %v", routeName, err), &pulumi.LogArgs{})
		return err
	}
	ctx.Log.Info(fmt.Sprintf("HTTPRoute '%s' created successfully", routeName), &pulumi.LogArgs{})
	return nil
}

// createPathBasedHTTPRoute creates multiple HTTPRoutes, one per service port.
// WORKAROUND for Aliyun ALB bug: Currently, an HTTPRoute that references multiple
// ports of the same service causes server group registration failures. The Aliyun
// team will fix this in a future release. The temporary workaround is to create
// separate HTTPRoutes for different ports of the same service.
func createPathBasedHTTPRoute(ctx *pulumi.Context, serviceName pulumi.StringOutput, gatewayNsName, httpRouteNsName string, provider pulumi.ProviderResource) error {
	// TLS Termination is handled by Gateway, traffic arrives here as HTTP.

	// HTTPRoute 1: /v1 and /api -> port 9380 (API service)
	if err := createHTTPRouteForPort(ctx, "ragflow-http-route-api", serviceName, gatewayNsName, httpRouteNsName, provider, []string{"/v1", "/api"}, 9380); err != nil {
		return err
	}

	// HTTPRoute 2: /api/v1/admin -> port 9381 (admin service)
	if err := createHTTPRouteForPort(ctx, "ragflow-http-route-admin", serviceName, gatewayNsName, httpRouteNsName, provider, []string{"/api/v1/admin"}, 9381); err != nil {
		return err
	}

	// HTTPRoute 3: / (root path) -> port 80 (frontend nginx - listening on 80 now)
	if err := createHTTPRouteForPort(ctx, "ragflow-http-route-frontend", serviceName, gatewayNsName, httpRouteNsName, provider, []string{"/"}, 80); err != nil {
		return err
	}

	ctx.Log.Info("All path-based HTTPRoutes created successfully", &pulumi.LogArgs{})
	return nil
}

// createGateway now expresses resources as compact specs and calls createCR to register them.
func createGateway(ctx *pulumi.Context, config *StackConfig, provider pulumi.ProviderResource, ragflowService *corev1.Service, gatewayClass string) (*apiextensions.CustomResource, error) {
	// Create Gateway in configured namespace (default: nginx-gateway)
	gatewayNsName := config.Gateway.Namespace

	ctx.Log.Info(fmt.Sprintf("Creating Gateway with GatewayClass: %s", gatewayClass), &pulumi.LogArgs{})

	// Build listeners list (HTTP only)
	listeners := []interface{}{
		// HTTP Listener (Port 80)
		map[string]interface{}{
			"name":     "http",
			"port":     80,
			"protocol": "HTTP",
			"allowedRoutes": map[string]interface{}{
				"namespaces": map[string]interface{}{"from": "All"},
			},
		},
	}

	gatewaySpec := map[string]interface{}{
		"gatewayClassName": gatewayClass,
		"listeners":        listeners,
	}
	ctx.Log.Info("Gateway hostname not set - will accept all incoming traffic", &pulumi.LogArgs{})

	ctx.Log.Info("Creating Gateway resource", &pulumi.LogArgs{})
	gateway, err := apiextensions.NewCustomResource(ctx, "ragflow-gateway", &apiextensions.CustomResourceArgs{
		ApiVersion: pulumi.String("gateway.networking.k8s.io/v1"),
		Kind:       pulumi.String("Gateway"),
		Metadata: &metav1.ObjectMetaArgs{
			Name:      pulumi.String("ragflow-gateway"),
			Namespace: pulumi.String(gatewayNsName),
		},
		OtherFields: kubernetes.UntypedArgs{
			"spec": gatewaySpec,
		},
	}, pulumi.Provider(provider))
	if err != nil {
		ctx.Log.Error(fmt.Sprintf("Failed to create Gateway: %v", err), &pulumi.LogArgs{})
		return nil, err
	}
	ctx.Log.Info("Gateway resource created successfully", &pulumi.LogArgs{})

	// Create a single HTTPRoute with path-based routing rules
	if err := createPathBasedHTTPRoute(ctx, ragflowService.Metadata.Name().Elem(), gatewayNsName, config.Namespace, provider); err != nil {
		return nil, err
	}

	// Export gateway address from Gateway status.addresses
	//
	// NOTE: Gateway API Specification (GEP) and Aliyun Implementation
	// ================================================================
	// According to Gateway API specification, status.addresses is an OPTIONAL field.
	// However, Aliyun (ACK) officially supports Gateway API (GA since late 2023) and
	// WILL populate status.addresses in production environments with:
	//   - type: IPAddress (for ALB public IP) or Hostname (for ALB DNS name)
	//   - value: the actual accessible address
	//
	// Reference: https://gateway-api.sigs.k8s.io/concepts/api-standards/gateway/#status-addresses
	//
	// For non-Aliyun clusters (local dev, other clouds), this may return empty string
	// depending on the Gateway Controller implementation (Cilium, NGINX Gateway, etc.)
	gatewayID := gateway.ID()
	gatewayAddress := gatewayID.ApplyT(func(id string) (string, error) {
		// Build config using standard loading rules
		loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
		configOverrides := &clientcmd.ConfigOverrides{}
		kubeConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, configOverrides)
		cfg, err := kubeConfig.ClientConfig()
		if err != nil {
			return "", fmt.Errorf("failed to load kube config: %w", err)
		}

		dyn, err := dynamic.NewForConfig(cfg)
		if err != nil {
			return "", fmt.Errorf("failed to create dynamic client: %w", err)
		}

		// Get Gateway resource by name and namespace
		gvr := schema.GroupVersionResource{Group: "gateway.networking.k8s.io", Version: "v1", Resource: "gateways"}
		gwObj, err := dyn.Resource(gvr).Namespace(gatewayNsName).Get(context.Background(), "ragflow-gateway", k8smetav1.GetOptions{})
		if err != nil {
			return "", fmt.Errorf("failed to get Gateway resource: %w", err)
		}

		// Extract address from status.addresses
		status, ok := gwObj.Object["status"].(map[string]interface{})
		if !ok {
			return "", fmt.Errorf("gateway status not available")
		}

		addresses, ok := status["addresses"].([]interface{})
		if !ok || len(addresses) == 0 {
			return "", fmt.Errorf("no addresses in Gateway status")
		}

		// Get first address (Gateway API supports multiple addresses, but we use the first one)
		firstAddr, ok := addresses[0].(map[string]interface{})
		if !ok {
			return "", fmt.Errorf("invalid address format")
		}

		// Extract value field (contains the actual IP or hostname)
		value, ok := firstAddr["value"].(string)
		if !ok || value == "" {
			return "", fmt.Errorf("address value is empty")
		}

		// Log address type for debugging
		if addrType, ok := firstAddr["type"].(string); ok {
			ctx.Log.Info(fmt.Sprintf("Gateway address (type=%s): %s", addrType, value), &pulumi.LogArgs{})
		} else {
			ctx.Log.Info(fmt.Sprintf("Gateway address: %s", value), &pulumi.LogArgs{})
		}

		return value, nil
	}).(pulumi.StringOutput)

	ctx.Export("gateway_address", gatewayAddress)

	// Create ConfigMap to expose gateway address in the same namespace as the Gateway
	_, err = corev1.NewConfigMap(ctx, "ragflow-gateway-address", &corev1.ConfigMapArgs{
		Metadata: &metav1.ObjectMetaArgs{
			Name:      pulumi.String("ragflow-gateway-address"),
			Namespace: pulumi.String(gatewayNsName),
		},
		Data: pulumi.StringMap{
			"gateway_address": gatewayAddress,
		},
	}, pulumi.Provider(provider))
	if err != nil {
		return nil, fmt.Errorf("failed to create gateway address ConfigMap: %w", err)
	}
	ctx.Log.Info(fmt.Sprintf("ConfigMap 'ragflow-gateway-address' created in namespace: %s", gatewayNsName), &pulumi.LogArgs{})

	return gateway, nil
}

// shouldUseIngress determines if we should use Ingress API instead of Gateway API
//
// Routing strategy:
// - Aliyun ACK (AckBasic, AckPro, AckProAuto) → Gateway API (has real nodes, can install Gateway Controller)
// - Aliyun ASK (AskBasic, AskPro) → Ingress API (Serverless with virtual nodes, use ALB Ingress Controller)
// - Other clouds/existing → Gateway API (default)
func shouldUseIngress(ctx *pulumi.Context, cloudProvider string) bool {
	if cloudProvider != "aliyun" {
		return false
	}

	// Read cluster type configuration
	// Valid values: AckBasic, AckPro, AckProAuto, AskBasic, AskPro
	clusterType := getConfig(ctx, "kubernetes.cluster_type", "AckPro")

	// Use Ingress for ASK types (starts with "Ask")
	// - AskBasic, AskPro → Ingress (Serverless clusters use ALB Ingress Controller)
	// - AckBasic, AckPro, AckProAuto → Gateway (has real nodes, can use Gateway Controller)
	isASK := strings.HasPrefix(clusterType, "Ask")

	if isASK {
		ctx.Log.Info(fmt.Sprintf("Detected Aliyun ASK (Serverless) cluster type: %s - will use Ingress API with ALB Ingress Controller", clusterType), &pulumi.LogArgs{})
		return true
	}

	ctx.Log.Info(fmt.Sprintf("Cluster type: %s (Aliyun ACK with real nodes) - will use Gateway API", clusterType), &pulumi.LogArgs{})
	return false
}

// createAlbConfig creates an AlbConfig resource for ALB Ingress Controller
// AlbConfig defines the ALB instance configuration (IP type, availability zones, listeners)
// This is required for Aliyun ALB Ingress Controller to provision an ALB instance
// Returns the AlbConfig resource for reference by IngressClass
//
// vSwitch IDs come from either StackReference (infra stack) or config key "aliyun.vswitch_ids".
func createAlbConfig(ctx *pulumi.Context, config *StackConfig, provider pulumi.ProviderResource) (*apiextensions.CustomResource, error) {
	// vSwitch IDs from StackReference or config
	vSwitchIdsOutput := config.VSwitchIDs

	// Convert StringArrayOutput to a format we can use in the resource
	// We need to apply the vSwitch IDs to build the zone mappings
	zoneMappingsOutput := vSwitchIdsOutput.ApplyT(func(ids []string) []interface{} {
		if len(ids) < 2 {
			return nil // Will cause error in ApplyT
		}

		ctx.Log.Info(fmt.Sprintf("Creating AlbConfig with %d vSwitches in different availability zones", len(ids)), &pulumi.LogArgs{})
		for i, vswId := range ids {
			ctx.Log.Info(fmt.Sprintf("  vSwitch[%d]: %s", i+1, vswId), &pulumi.LogArgs{})
		}

		// Build zone mappings from vSwitch IDs
		zoneMappings := []interface{}{}
		for _, vswId := range ids {
			zoneMappings = append(zoneMappings, map[string]interface{}{
				"vSwitchId": vswId,
			})
		}
		return zoneMappings
	})

	// Build AlbConfig spec
	albConfigSpec := map[string]interface{}{
		"config": map[string]interface{}{
			"name":         "ragflow-alb",
			"addressType":  "Internet", // Internet (public) or Intranet (private)
			"zoneMappings": zoneMappingsOutput,
		},
		"listeners": []interface{}{
			map[string]interface{}{
				"port":     80,
				"protocol": "HTTP",
			},
		},
	}

	albConfig, err := apiextensions.NewCustomResource(ctx, "alb-config", &apiextensions.CustomResourceArgs{
		ApiVersion: pulumi.String("alibabacloud.com/v1"),
		Kind:       pulumi.String("AlbConfig"),
		Metadata: &metav1.ObjectMetaArgs{
			Name: pulumi.String("alb"),
		},
		OtherFields: kubernetes.UntypedArgs{
			"spec": albConfigSpec,
		},
	}, pulumi.Provider(provider), pulumi.IgnoreChanges([]string{
		// ALB Ingress Controller manages these fields
		"status",
	}))
	if err != nil {
		return nil, fmt.Errorf("failed to create AlbConfig: %w", err)
	}

	ctx.Log.Info("AlbConfig created successfully - ALB instance will be provisioned by Aliyun controller", &pulumi.LogArgs{})
	ctx.Log.Info("Note: ALB instance provisioning takes ~20-30 seconds, monitored via: kubectl get albconfig alb -o yaml", &pulumi.LogArgs{})

	return albConfig, nil
}

// ensureALBIngressClass creates the ALB IngressClass and ensures AlbConfig exists
// This is required for ALB Ingress Controller to process Ingress resources
// The IngressClass references the AlbConfig via the parameters field
func ensureALBIngressClass(ctx *pulumi.Context, config *StackConfig, provider pulumi.ProviderResource) error {
	// Step 1: Create AlbConfig first (required by IngressClass)
	ctx.Log.Info("Creating AlbConfig for ALB Ingress Controller...", &pulumi.LogArgs{})
	albConfig, err := createAlbConfig(ctx, config, provider)
	if err != nil {
		return fmt.Errorf("failed to create AlbConfig: %w", err)
	}

	// Step 2: Create IngressClass that references the AlbConfig
	ingressClassSpec := map[string]interface{}{
		"controller": "ingress.k8s.alibabacloud/alb", // Correct controller name for Aliyun ALB
		"parameters": map[string]interface{}{
			"apiGroup": "alibabacloud.com",
			"kind":     "AlbConfig",
			"name":     albConfig.Metadata.Name(),
			"scope":    "Cluster",
		},
	}

	_, err = apiextensions.NewCustomResource(ctx, "alb-ingress-class", &apiextensions.CustomResourceArgs{
		ApiVersion: pulumi.String("networking.k8s.io/v1"),
		Kind:       pulumi.String("IngressClass"),
		Metadata: &metav1.ObjectMetaArgs{
			Name: pulumi.String("alb"),
		},
		OtherFields: kubernetes.UntypedArgs{
			"spec": ingressClassSpec,
		},
	}, pulumi.Provider(provider))
	if err != nil {
		return fmt.Errorf("failed to create ALB IngressClass: %w", err)
	}

	ctx.Log.Info("ALB IngressClass created/verified - Ingress resources can now use ingressClassName: alb", &pulumi.LogArgs{})
	return nil
}

// createIngress creates an Ingress resource for Aliyun ASK (Serverless) clusters
// Uses ALB Ingress Controller annotations for automatic ALB provisioning with public IP
// Returns the Ingress resource for extracting address from status
func createIngress(ctx *pulumi.Context, config *StackConfig, provider pulumi.ProviderResource, ragflowService *corev1.Service) (*networkingv1.Ingress, error) {
	// Step 1: Ensure ALB IngressClass exists (requires vSwitch IDs for AlbConfig)
	if config.VSwitchIDs != (pulumi.StringArrayOutput{}) {
		ctx.Log.Info("Ensuring ALB IngressClass exists...", &pulumi.LogArgs{})
		if err := ensureALBIngressClass(ctx, config, provider); err != nil {
			return nil, fmt.Errorf("failed to ensure ALB IngressClass: %w", err)
		}
	} else {
		ctx.Log.Info("Skipping AlbConfig creation (no vSwitch IDs configured) - using pre-installed ALB IngressClass", &pulumi.LogArgs{})
	}

	// Step 2: Create Ingress with minimal ALB annotations
	// Reference: alb_ingress.yaml
	// ALB Ingress Controller will auto-configure most settings
	annotations := map[string]string{
		// Address type: internet (public) or intranet (private)
		"alb.ingress.kubernetes.io/address-type": "internet",
	}

	ctx.Log.Info("ALB Ingress annotations:", &pulumi.LogArgs{})
	for k, v := range annotations {
		ctx.Log.Info(fmt.Sprintf("  %s: %s", k, v), &pulumi.LogArgs{})
	}

	// Build Ingress rules matching the existing Gateway/HTTPRoute configuration
	// Rule 1: /v1 and /api -> port 9380 (API service)
	// Rule 2: /api/v1/admin -> port 9381 (admin service)
	// Rule 3: / (root path) -> port 80 (frontend nginx)
	ingress, err := networkingv1.NewIngress(ctx, "ragflow-ingress", &networkingv1.IngressArgs{
		Metadata: &metav1.ObjectMetaArgs{
			Name:        pulumi.String("ragflow-ingress"),
			Namespace:   pulumi.String(config.Namespace),
			Annotations: pulumi.ToStringMap(annotations),
		},
		Spec: &networkingv1.IngressSpecArgs{
			IngressClassName: pulumi.String("alb"),
			Rules: networkingv1.IngressRuleArray{
				// Rule 1: API paths (/v1, /api) -> port 9380
				networkingv1.IngressRuleArgs{
					Http: &networkingv1.HTTPIngressRuleValueArgs{
						Paths: networkingv1.HTTPIngressPathArray{
							networkingv1.HTTPIngressPathArgs{
								Path:     pulumi.String("/v1"),
								PathType: pulumi.String("Prefix"),
								Backend: &networkingv1.IngressBackendArgs{
									Service: &networkingv1.IngressServiceBackendArgs{
										Name: ragflowService.Metadata.Name().ApplyT(func(name *string) string {
											if name == nil {
												return ""
											}
											return *name
										}).(pulumi.StringOutput),
										Port: &networkingv1.ServiceBackendPortArgs{
											Number: pulumi.Int(9380),
										},
									},
								},
							},
							networkingv1.HTTPIngressPathArgs{
								Path:     pulumi.String("/api"),
								PathType: pulumi.String("Prefix"),
								Backend: &networkingv1.IngressBackendArgs{
									Service: &networkingv1.IngressServiceBackendArgs{
										Name: ragflowService.Metadata.Name().ApplyT(func(name *string) string {
											if name == nil {
												return ""
											}
											return *name
										}).(pulumi.StringOutput),
										Port: &networkingv1.ServiceBackendPortArgs{
											Number: pulumi.Int(9380),
										},
									},
								},
							},
						},
					},
				},
				// Rule 2: Admin path (/api/v1/admin) -> port 9381
				networkingv1.IngressRuleArgs{
					Http: &networkingv1.HTTPIngressRuleValueArgs{
						Paths: networkingv1.HTTPIngressPathArray{
							networkingv1.HTTPIngressPathArgs{
								Path:     pulumi.String("/api/v1/admin"),
								PathType: pulumi.String("Prefix"),
								Backend: &networkingv1.IngressBackendArgs{
									Service: &networkingv1.IngressServiceBackendArgs{
										Name: ragflowService.Metadata.Name().ApplyT(func(name *string) string {
											if name == nil {
												return ""
											}
											return *name
										}).(pulumi.StringOutput),
										Port: &networkingv1.ServiceBackendPortArgs{
											Number: pulumi.Int(9381),
										},
									},
								},
							},
						},
					},
				},
				// Rule 3: Frontend (root path) -> port 80
				networkingv1.IngressRuleArgs{
					Http: &networkingv1.HTTPIngressRuleValueArgs{
						Paths: networkingv1.HTTPIngressPathArray{
							networkingv1.HTTPIngressPathArgs{
								Path:     pulumi.String("/"),
								PathType: pulumi.String("Prefix"),
								Backend: &networkingv1.IngressBackendArgs{
									Service: &networkingv1.IngressServiceBackendArgs{
										Name: ragflowService.Metadata.Name().ApplyT(func(name *string) string {
											if name == nil {
												return ""
											}
											return *name
										}).(pulumi.StringOutput),
										Port: &networkingv1.ServiceBackendPortArgs{
											Number: pulumi.Int(80),
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}, pulumi.Provider(provider))
	if err != nil {
		ctx.Log.Error(fmt.Sprintf("Failed to create Ingress: %v", err), &pulumi.LogArgs{})
		return nil, err
	}
	ctx.Log.Info("Ingress resource created successfully - ALB will be automatically provisioned", &pulumi.LogArgs{})

	// Export ingress address (IP or hostname)
	// Pulumi will automatically wait for the ALB to be provisioned and the status to be populated
	gatewayAddress := ingress.Status.ApplyT(func(status *networkingv1.IngressStatus) (string, error) {
		if status == nil || status.LoadBalancer == nil || len(status.LoadBalancer.Ingress) == 0 {
			ctx.Log.Warn("No ingress address in status - ALB may still be provisioning", &pulumi.LogArgs{})
			return "", nil
		}

		firstIngress := status.LoadBalancer.Ingress[0]
		// Try to get IP first, then hostname
		if firstIngress.Ip != nil && *firstIngress.Ip != "" {
			ctx.Log.Info(fmt.Sprintf("Ingress IP address: %s", *firstIngress.Ip), &pulumi.LogArgs{})
			return *firstIngress.Ip, nil
		}
		if firstIngress.Hostname != nil && *firstIngress.Hostname != "" {
			ctx.Log.Info(fmt.Sprintf("Ingress hostname: %s", *firstIngress.Hostname), &pulumi.LogArgs{})
			return *firstIngress.Hostname, nil
		}

		ctx.Log.Warn("No IP or hostname found in ingress status", &pulumi.LogArgs{})
		return "", nil
	}).(pulumi.StringOutput)

	ctx.Export("gateway_address", gatewayAddress)

	// Create ConfigMap to expose gateway address in the same namespace as the Ingress
	_, err = corev1.NewConfigMap(ctx, "ragflow-gateway-address", &corev1.ConfigMapArgs{
		Metadata: &metav1.ObjectMetaArgs{
			Name:      pulumi.String("ragflow-gateway-address"),
			Namespace: pulumi.String(config.Namespace),
		},
		Data: pulumi.StringMap{
			"gateway_address": gatewayAddress,
		},
	}, pulumi.Provider(provider))
	if err != nil {
		ctx.Log.Error(fmt.Sprintf("Failed to create gateway address ConfigMap: %v", err), &pulumi.LogArgs{})
		return nil, fmt.Errorf("failed to create gateway address ConfigMap: %w", err)
	}
	ctx.Log.Info(fmt.Sprintf("ConfigMap 'ragflow-gateway-address' created in namespace: %s", config.Namespace), &pulumi.LogArgs{})

	return ingress, nil
}

// isAliyunCluster detects if the current cluster is running on Aliyun
// by checking if the S3 endpoint is Aliyun OSS
func isAliyunCluster(config *StackConfig) bool {
	s3Endpoint := config.Env["S3_ENDPOINT"]
	return strings.Contains(s3Endpoint, "aliyuncs.com")
}

// hasKnativeService checks if Knative Serving is installed in the cluster
// by checking for the serving.knative.dev API group
func hasKnativeService(ctx *pulumi.Context, provider pulumi.ProviderResource) bool {
	// Build config using standard loading rules
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	configOverrides := &clientcmd.ConfigOverrides{}
	kubeConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, configOverrides)
	cfg, err := kubeConfig.ClientConfig()
	if err != nil {
		ctx.Log.Info("Failed to load kube config for Knative detection", &pulumi.LogArgs{})
		return false
	}

	dyn, err := dynamic.NewForConfig(cfg)
	if err != nil {
		ctx.Log.Info("Failed to create dynamic client for Knative detection", &pulumi.LogArgs{})
		return false
	}

	// Try to list Knative Services in the default namespace
	// This will fail if Knative Serving is not installed
	gvr := schema.GroupVersionResource{Group: "serving.knative.dev", Version: "v1", Resource: "services"}
	_, err = dyn.Resource(gvr).List(context.Background(), k8smetav1.ListOptions{
		Limit: 1,
	})

	if err != nil {
		ctx.Log.Info("Knative Serving not detected in cluster", &pulumi.LogArgs{})
		return false
	}

	ctx.Log.Info("Knative Serving detected in cluster", &pulumi.LogArgs{})
	return true
}

// buildGPUResources creates GPU resource requirements based on configuration
//
// GPU Access Strategy:
// - Use NVIDIA runtime class (runtimeClassName: nvidia) to provide GPU access
// - Use NVIDIA_VISIBLE_DEVICES=all environment variable to expose GPU
// - Do NOT request nvidia.com/gpu resource to allow multiple pods to share the GPU
//
// Why this approach works:
// - NVIDIA runtime class ensures GPU devices are properly mounted in the container
// - Environment variable NVIDIA_VISIBLE_DEVICES=all tells the runtime which GPU to use
// - Without requesting nvidia.com/gpu resource, Kubernetes scheduler won't limit GPU usage
// - Multiple pods can be scheduled on the same node and share the GPU
//
// Trade-offs:
// - No GPU resource tracking in Kubernetes (pods can oversubscribe GPU)
// - No memory isolation between pods (all share full GPU memory)
// - Relies on users to not oversubscribe GPU resources
//
// PREREQUISITES:
// - NVIDIA runtime must be configured in containerd: /etc/containerd/config.toml
// - NVIDIA device plugin must be installed: kubectl get pods -n kube-system | grep nvidia-device-plugin
// - Node must be labeled with gpu=true: kubectl label nodes <node-name> gpu=true
func buildGPUResources(vramMB int) *corev1.ResourceRequirementsArgs {
	// Do NOT request nvidia.com/gpu resource
	// GPU access is provided through NVIDIA runtime class and environment variables
	// This allows multiple pods to share the same GPU
	return &corev1.ResourceRequirementsArgs{}
}

// createKnativeServiceForDeepdocGPU creates a Knative Service for deepdoc_gpu on Aliyun ACS
// This enables scale-to-zero and autoscaling capabilities for GPU workloads
//
// Knative Service configuration for Aliyun ACS GPU:
// - Uses nvidia.com/gpu resource request to specify GPU count
// - Uses alibabacloud.com/compute-class: gpu label to specify compute class
// - Enables autoscaling with scale-to-zero capability
// - Sets appropriate CPU and memory requests/limits according to ACS specs
//
// Reference: https://help.aliyun.com/zh/cs/user-guide/acs-pod-instance-overview
func createKnativeServiceForDeepdocGPU(ctx *pulumi.Context, config *StackConfig, namespace *corev1.Namespace, provider pulumi.ProviderResource, registrySecret *corev1.Secret) (*apiextensions.CustomResource, *corev1.Service, error) {
	ctx.Log.Info("Creating Knative Service for deepdoc_gpu with ACS-compliant GPU configuration", &pulumi.LogArgs{})

	// Build pod template spec with ACS-compliant GPU configuration
	// According to ACS documentation, GPU pods must request nvidia.com/gpu resource
	podTemplateSpec := map[string]interface{}{
		"metadata": map[string]interface{}{
			"annotations": map[string]interface{}{
				// Autoscaling annotations
				// Pulling deepdoc_gpu image (25GB) is very very slow (costs ~22 minutes on Aliyun ECS). Scaling to zero is meanless!
				"autoscaling.knative.dev/minScale": "1",  // Scale to zero when idle
				"autoscaling.knative.dev/maxScale": "10", // Maximum 10 replicas
				// Target 1 concurrent request per pod for GPU workloads
				// GPU workloads typically benefit from fewer concurrent requests per pod
				"autoscaling.knative.dev/target": "1",
				// Use concurrency-based autoscaling (default)
				"autoscaling.knative.dev/metric": "concurrency",
				// Initial scale: start with 1 pod when service is created
				"autoscaling.knative.dev/initialScale": "1",
				// Scale to zero after 30 seconds of inactivity (reduces GPU cost when idle)
				"autoscaling.knative.dev/scaleToZeroGracePeriodSeconds": "30",
				// Window for autoscaling metrics
				"autoscaling.knative.dev/window": "60s",
			},
			"labels": map[string]interface{}{
				"app": "deepdoc",
				// CRITICAL: This label enables GPU on Aliyun ACS
				// It tells Aliyun's autoscaler to allocate GPU resources
				"alibabacloud.com/compute-class": "gpu",
				// CRITICAL: This label specifies GPU model series for Aliyun ACS
				// Available options: T4, A10, L20, GU8TF, GU8TEF, G49E, P16EN, etc.
				// See: https://help.aliyun.com/zh/cs/user-guide/gpu-families-supported-by-acs
				"alibabacloud.com/gpu-model-series": config.Deepdoc.GPUModelSeries,
			},
		},
		"spec": map[string]interface{}{
			// Tolerations for GPU nodes
			"tolerations": []interface{}{
				map[string]interface{}{
					"key":      "nvidia.com/gpu",
					"operator": "Equal",
					"value":    "true",
					"effect":   "NoSchedule",
				},
			},
			// Resource requests for Aliyun ACS GPU pods
			// CRITICAL: ACS requires both requests and limits for GPU resources
			// ACS will auto-adjust (规整) resources to match supported specs
			// Reference: https://help.aliyun.com/zh/cs/user-guide/acs-pod-instance-overview
			"containers": []interface{}{
				map[string]interface{}{
					"name":  "deepdoc",
					"image": config.Deepdoc.Image,
					"ports": []interface{}{
						map[string]interface{}{
							"containerPort": 8000,
							"protocol":      "TCP",
						},
					},
					// GPU environment variables
					"env": []interface{}{
						map[string]interface{}{
							"name":  "NVIDIA_VISIBLE_DEVICES",
							"value": "all",
						},
						map[string]interface{}{
							"name":  "NVIDIA_DRIVER_CAPABILITIES",
							"value": "compute,utility",
						},
					},
					// CRITICAL: ACS requires GPU resource requests in BOTH requests and limits
					// ACS will automatically adjust (规整) to supported specs if needed
					"resources": map[string]interface{}{
						"requests": map[string]interface{}{
							"cpu":            config.Deepdoc.CPURequest,    // e.g., "2" or "4"
							"memory":         config.Deepdoc.MemoryRequest, // e.g., "4Gi" or "8Gi"
							"nvidia.com/gpu": config.Deepdoc.GPUCount,      // e.g., "1" or "2"
						},
						"limits": map[string]interface{}{
							"cpu":            config.Deepdoc.CPURequest,    // MUST equal requests for ACS GPU pods
							"memory":         config.Deepdoc.MemoryRequest, // MUST equal requests for ACS GPU pods
							"nvidia.com/gpu": config.Deepdoc.GPUCount,      // MUST equal requests for ACS GPU pods
						},
					},
				},
			},
		},
	}

	// If registry secret is available, add imagePullSecrets
	if registrySecret != nil {
		podTemplateSpec["spec"].(map[string]interface{})["imagePullSecrets"] = []interface{}{
			map[string]interface{}{
				"name": registrySecret.Metadata.Name(),
			},
		}
	}

	// Build Knative Service spec
	knativeServiceSpec := map[string]interface{}{
		"template": podTemplateSpec,
	}

	// Create Knative Service as a CustomResource
	knativeService, err := apiextensions.NewCustomResource(ctx, "deepdoc-knative-service", &apiextensions.CustomResourceArgs{
		ApiVersion: pulumi.String("serving.knative.dev/v1"),
		Kind:       pulumi.String("Service"),
		Metadata: &metav1.ObjectMetaArgs{
			Name:      pulumi.String("deepdoc"),
			Namespace: namespace.Metadata.Name(),
			Annotations: pulumi.StringMap{
				// Add annotation that changes when hardware config changes
				"ragflow/deepdoc-hardware": pulumi.Sprintf("gpu/%s-gpus%s", config.Deepdoc.GPUModelSeries, config.Deepdoc.GPUCount),
			},
		},
		OtherFields: kubernetes.UntypedArgs{
			"spec": knativeServiceSpec,
		},
	}, pulumi.Provider(provider))
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create Knative Service: %w", err)
	}

	ctx.Log.Info("Knative Service created successfully with ACS-compliant GPU configuration", &pulumi.LogArgs{})

	// Knative automatically creates a Route and a K8s Service
	// The service name matches the Knative Service name
	// We return nil for the service reference to avoid conflict with the Knative-managed service
	return knativeService, nil, nil
}

// createDeepdocDeployment creates a unified deployment for DLA+OCR+TSR models
//
// Deployment Strategy:
// - Aliyun + GPU + Knative + use_knative_gpu=true: Use Knative Service with autoscaling (scale-to-zero)
// - Other cases: Use standard Kubernetes Deployment
//
// NOTE: Knative does NOT support runtimeClassName for GPU access.
// When using Knative for GPU on Aliyun, GPU access must be provided through
// Aliyun-specific labels (alibabacloud.com/compute-class: gpu) and the GPU node
// must have NVIDIA drivers properly configured at the host level.
func createDeepdocDeployment(ctx *pulumi.Context, config *StackConfig, namespace *corev1.Namespace, provider pulumi.ProviderResource, registrySecret *corev1.Secret) (*v1.Deployment, *corev1.Service, error) {
	// Read configuration for whether to use Knative for GPU
	// Default is false (use standard Deployment with runtimeClassName support)
	useKnativeGPUStr := getConfig(ctx, "deepdoc.use_knative_gpu", "false")
	useKnativeGPU, _ := strconv.ParseBool(useKnativeGPUStr)

	// Check if we should use Knative Service for GPU on Aliyun
	// Conditions:
	// 1. Cluster is on Aliyun (detected by S3 endpoint)
	// 2. Deepdoc hardware is GPU
	// 3. Knative Serving is installed
	// 4. use_knative_gpu config is true (user must explicitly enable)
	useKnative := isAliyunCluster(config) && config.Deepdoc.UseGPU && hasKnativeService(ctx, provider) && useKnativeGPU

	if useKnative {
		ctx.Log.Info("Using Knative Service for deepdoc_gpu on Aliyun with autoscaling", &pulumi.LogArgs{})

		// Create Knative Service instead of Deployment
		knativeService, serviceRef, err := createKnativeServiceForDeepdocGPU(ctx, config, namespace, provider, registrySecret)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to create Knative Service: %w", err)
		}

		// Export the Knative Service name for reference
		ctx.Export("deepdocKnativeService", knativeService.Metadata.Name())
		ctx.Log.Info("Knative Service 'deepdoc' created with scale-to-zero and autoscaling enabled", &pulumi.LogArgs{})

		// Return nil for deployment since Knative manages pods
		// Return service reference for compatibility
		return nil, serviceRef, nil
	}

	// Use standard Kubernetes Deployment for non-GPU or non-Aliyun deployments
	ctx.Log.Info("Using standard Kubernetes Deployment for deepdoc", &pulumi.LogArgs{})

	// Build container args
	containerArgs := &corev1.ContainerArgs{
		Name:  pulumi.String("deepdoc"),
		Image: pulumi.String(config.Deepdoc.Image),
		Ports: corev1.ContainerPortArray{
			&corev1.ContainerPortArgs{
				ContainerPort: pulumi.Int(8000), // LitServe default port
			},
		},
	}

	// Build pod spec
	podSpec := &corev1.PodSpecArgs{
		Containers: corev1.ContainerArray{
			containerArgs,
		},
	}

	// Add imagePullSecrets if registry secret is available
	if registrySecret != nil {
		podSpec.ImagePullSecrets = corev1.LocalObjectReferenceArray{
			&corev1.LocalObjectReferenceArgs{
				Name: registrySecret.Metadata.Name(),
			},
		}
	}

	// Add GPU-specific configuration only if GPU is enabled
	if config.Deepdoc.UseGPU {
		// GPU version: Build GPU resources based on configuration
		// Unified service needs more VRAM as it runs all three models
		gpuResources := buildGPUResources(config.Deepdoc.VramMB)
		containerArgs.Resources = gpuResources

		// Add environment variables to expose GPU to container
		// Since we're not requesting nvidia.com/gpu resource, we need these env vars
		containerArgs.Env = corev1.EnvVarArray{
			&corev1.EnvVarArgs{
				Name:  pulumi.String("NVIDIA_VISIBLE_DEVICES"),
				Value: pulumi.String("all"),
			},
			&corev1.EnvVarArgs{
				Name:  pulumi.String("NVIDIA_DRIVER_CAPABILITIES"),
				Value: pulumi.String("compute,utility"),
			},
		}

		containerArgs.Command = pulumi.StringArray{
			//pulumi.String("--disable-ocr"),
			//pulumi.String("--disable-tsr"),
		}

		// Add GPU-specific scheduling configuration
		podSpec.Tolerations = corev1.TolerationArray{
			&corev1.TolerationArgs{
				Key:      pulumi.String("nvidia.com/gpu"),
				Operator: pulumi.String("Equal"),
				Value:    pulumi.String("true"),
				Effect:   pulumi.String("NoSchedule"),
			},
		}
		podSpec.NodeSelector = pulumi.StringMap{
			"gpu": pulumi.String("true"),
		}
		// Use NVIDIA runtime to ensure GPU devices are properly mounted
		// This is required for GPU access without requesting nvidia.com/gpu resource
		podSpec.RuntimeClassName = pulumi.String("nvidia")

		// GPU Access without Resource Requesting
		//
		// WHY we don't request nvidia.com/gpu resource:
		// - Allows multiple pods to be scheduled on the same GPU node
		// - Bypasses Kubernetes scheduler's GPU tracking
		// - Simpler setup than Time-sharing or MIG
		//
		// HOW GPU access is provided:
		// - NVIDIA runtime class (runtimeClassName: nvidia) handles GPU device mounting
		// - Environment variable NVIDIA_VISIBLE_DEVICES=all exposes GPU to container
		// - NVIDIA container toolkit provides driver libraries
		//
		// Trade-offs:
		// - No GPU resource tracking/limiting in Kubernetes
		// - No memory isolation between pods
		// - Users must manually manage GPU utilization to avoid oversubscription
		//
		// PREREQUISITES:
		// - NVIDIA runtime configured in containerd: /etc/containerd/config.toml
		// - NVIDIA device plugin installed: kubectl get pods -n kube-system | grep nvidia-device-plugin
		// - Node labeled with gpu=true
	}

	// Build hardware type string for annotation
	hardwareType := "cpu"
	if config.Deepdoc.UseGPU {
		hardwareType = "gpu"
	}

	// DeepDoc Deployment
	deepdocDeployment, err := v1.NewDeployment(ctx, "deepdoc-deployment", &v1.DeploymentArgs{
		Metadata: &metav1.ObjectMetaArgs{
			Name:      pulumi.String("deepdoc"),
			Namespace: namespace.Metadata.Name(),
			Annotations: pulumi.StringMap{
				// Add annotation that changes when hardware type changes
				// This forces a rolling update when deepdoc.hardware config changes
				"ragflow/deepdoc-hardware": pulumi.Sprintf("%s/vram-%d", hardwareType, config.Deepdoc.VramMB),
			},
		},
		Spec: &v1.DeploymentSpecArgs{
			Replicas: pulumi.Int(config.Deepdoc.Replicas),
			Selector: &metav1.LabelSelectorArgs{
				MatchLabels: pulumi.StringMap{
					"app": pulumi.String("deepdoc"),
				},
			},
			Strategy: &v1.DeploymentStrategyArgs{
				Type: pulumi.String("Recreate"), // Aggressive update strategy: kill all pods before creating new ones
			},
			Template: &corev1.PodTemplateSpecArgs{
				Metadata: &metav1.ObjectMetaArgs{
					Labels: pulumi.StringMap{
						"app": pulumi.String("deepdoc"),
					},
					Annotations: pulumi.StringMap{
						// Pod template annotation also changes to force rolling update
						"ragflow/deepdoc-hardware": pulumi.Sprintf("%s/vram-%d", hardwareType, config.Deepdoc.VramMB),
					},
				},
				Spec: podSpec,
			},
		},
	}, pulumi.Provider(provider))
	if err != nil {
		return nil, nil, err
	}

	// DeepDoc Service - single service for all three endpoints
	deepdocService, err := corev1.NewService(ctx, "deepdoc-service", &corev1.ServiceArgs{
		Metadata: &metav1.ObjectMetaArgs{
			Name:      pulumi.String("deepdoc"),
			Namespace: namespace.Metadata.Name(),
		},
		Spec: &corev1.ServiceSpecArgs{
			Selector: pulumi.StringMap{
				"app": pulumi.String("deepdoc"),
			},
			Ports: corev1.ServicePortArray{
				&corev1.ServicePortArgs{
					Name:       pulumi.String("http"),
					Port:       pulumi.Int(8000),
					TargetPort: pulumi.Int(8000),
				},
			},
		},
	}, pulumi.Provider(provider))
	if err != nil {
		return nil, nil, err
	}

	return deepdocDeployment, deepdocService, nil
}
