package main

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
	k8sclientset "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes"
	apiextensions "github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/apiextensions"
	v1 "github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/apps/v1"
	corev1 "github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/core/v1"
	metav1 "github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/meta/v1"

	"github.com/pulumi/pulumi-alicloud/sdk/v3/go/alicloud"
	"github.com/pulumi/pulumi-alicloud/sdk/v3/go/alicloud/alb"
	"github.com/pulumi/pulumi-alicloud/sdk/v3/go/alicloud/vpc"
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
	Elasticsearch string // Optional: only used when use_public_registry is false
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
	EnableHTTPS bool   // Enable HTTPS listener (default: false)
	TLSCertPEM  string // TLS certificate in PEM format
	TLSKeyPEM   string // TLS private key in PEM format
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
	Name         string
	Namespace    string
	StorageClass string // Storage class for PVCs
	Env          map[string]string
	RAGFlow      RAGFlowConfig
	Gateway      GatewayConfig
	Deepdoc      GPUConfig      // Unified DLA+OCR+TSR service
	Images       ImageConfig    // Container image URLs
	Registry     RegistryConfig // Docker registry configuration
	MySQL        MySQLConfig    // MySQL configuration
	ES           ESConfig       // Elasticsearch configuration
}

// RegistryConfig holds Docker registry credentials
type RegistryConfig struct {
	Server   string
	Username string
	Password string
}

// MySQLConfig holds MySQL configuration
type MySQLConfig struct {
	Host     string // Empty string means create MySQL deployment, non-empty means use external MySQL
	Port     string // Default: "3306"
	Password string // Default: "infiniflow@2023"
	DBName   string // Default: "ragflow"
}

// ESConfig holds Elasticsearch configuration
type ESConfig struct {
	Host     string // Empty string means create ES deployment, non-empty means use external ES
	Port     string // Default: "9200"
	Protocol string // Default: "http"
	Password string // Default: "" (only used when ES is created by Pulumi)
}

// LoadConfig reads config values from Pulumi configuration
func LoadConfig(ctx *pulumi.Context) (StackConfig, error) {
	// Read basic configuration
	namespace := getConfig(ctx, "namespace", "ragflow")
	// Storage class configuration
	storageClass := getConfig(ctx, "storage_class", "rook-ceph-block")
	// Enterprise registry configuration
	enterpriseRegistry := getConfig(ctx, "enterprise_registry", "192.168.1.51")
	// Use public registry or enterprise registry
	usePublicRegistryStr := getConfig(ctx, "use_public_registry", "true")
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
	ragflowImageTag := getConfig(ctx, "ragflow_image_tag", "latest")
	ragflowImage := fmt.Sprintf("%s/infiniflow-ai/ragflow:%s", enterpriseRegistry, ragflowImageTag)
	ragflowReplicasStr := getConfig(ctx, "ragflow_replicas", "1")
	ragflowReplicas, _ := strconv.Atoi(ragflowReplicasStr)

	// Read S3 configuration
	s3Endpoint := getConfig(ctx, "s3_endpoint", "http://rook-ceph-rgw-my-store.rook-ceph.svc:80")
	s3Bucket := getConfig(ctx, "s3_bucket", "ragflow")
	s3Region := getConfig(ctx, "s3_region", "us-east-1")

	// Auto-detect storage type based on endpoint
	storageImplType := ""
	if strings.Contains(s3Endpoint, "aliyuncs.com") {
		storageImplType = "OSS"
	} else {
		storageImplType = "AWS_S3"
	}

	// Read S3 access credentials (sensitive information)
	s3AccessKey := getConfig(ctx, "s3_access_key", "")
	s3SecretKey := getConfig(ctx, "s3_secret_key", "")

	// Debug: Print the secret values to verify they are being read
	ctx.Log.Info(fmt.Sprintf("S3 Credentials: s3AccessKey='%s', s3SecretKey='%s'", s3AccessKey, s3SecretKey), &pulumi.LogArgs{})

	// Read RAGFlow secret key for session signing
	ragflowSecretKey := getConfig(ctx, "ragflow_secret_key", "DOnghtfiCeriTENdywhERlEtivOLicuL")

	// Read Docker registry credentials (sensitive information)
	registryUsername := getConfig(ctx, "enterprise_registry_username", "")
	registryPassword := getConfig(ctx, "enterprise_registry_password", "")

	// Read MySQL configuration
	// If mysql_host is empty, we will create a MySQL deployment in the cluster
	// If mysql_host is set, we will use the external MySQL server
	mysqlHost := getConfig(ctx, "mysql_host", "")
	mysqlPort := getConfig(ctx, "mysql_port", "3306")
	mysqlPassword := getConfig(ctx, "mysql_password", "infiniflow@2023")
	mysqlDBName := getConfig(ctx, "mysql_dbname", "rag_flow")

	// Determine MySQL host for RAGFlow environment variables
	// If using external MySQL, use the configured host; otherwise use the K8s service name
	mysqlHostForEnv := mysqlHost
	if mysqlHostForEnv == "" {
		mysqlHostForEnv = "mysql" // Default K8s service name
	}

	// Read Elasticsearch configuration
	// If es_host is empty, we will create an Elasticsearch deployment in the cluster
	// If es_host is set, we will use the external Elasticsearch server
	esHost := getConfig(ctx, "es_host", "")
	esPort := getConfig(ctx, "es_port", "9200")
	esProtocol := getConfig(ctx, "es_protocol", "")
	esPassword := getConfig(ctx, "es_password", "infiniflow@2023")

	// Determine Elasticsearch host, port, and protocol for RAGFlow environment variables
	// For internal ES (ECK): Force HTTPS and use default ECK service name
	// For external ES: Use configured values (default to HTTP if not specified)
	var esHostForEnv, esProtocolForEnv string
	if esHost == "" {
		// Internal ES (ECK managed)
		esHostForEnv = "elasticsearch-es-http"
		esProtocolForEnv = "https" // ECK always uses HTTPS
	} else {
		// External ES
		esHostForEnv = esHost
		if esProtocol == "" {
			esProtocolForEnv = "http" // Default to HTTP for external ES
		} else {
			esProtocolForEnv = esProtocol
		}
	}

	// Read Unified DeepDoc service configuration
	// Note: DeepDoc is always enabled (replaces TSR/DLA/OCR services)
	deepdocReplicasStr := getConfig(ctx, "deepdoc_replicas", "1")
	deepdocReplicas, _ := strconv.Atoi(deepdocReplicasStr)
	// DeepDoc hardware type: "cpu" or "gpu" (default: "cpu")
	deepdocHardware := getConfig(ctx, "deepdoc_hardware", "cpu")
	deepdocImageTag := getConfig(ctx, "deepdoc_image_tag", "latest")
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
	deepdocVramStr := getConfig(ctx, "deepdoc_vram_mb", "2048") // Combined memory for all three models
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
	stackVersion := getConfig(ctx, "stack_version", "8.11.3")

	// Build all container image URLs
	images := ImageConfig{
		MySQL:         getImageURL("mysql:8.4"),
		Redis:         getImageURL("valkey/valkey:8"),
		TEI:           getImageURL("infiniflow/text-embeddings-inference:cpu-1.8"),
		RabbitMQ:      getImageURL("rabbitmq:4-management"),
		Curl:          getImageURL("curlimages/curl:latest"),
		AWSCLI:        getImageURL("amazon/aws-cli:latest"),
		Elasticsearch: getImageURL("docker.elastic.co/elasticsearch/elasticsearch:" + stackVersion),
	}

	env := map[string]string{
		"DOC_ENGINE":            "elasticsearch",
		"RAGFLOW_IMAGE":         ragflowImage,
		"STACK_VERSION":         "8.11.3",
		"MYSQL_HOST":            mysqlHostForEnv,
		"MYSQL_PORT":            mysqlPort,
		"MYSQL_DBNAME":          mysqlDBName,
		"MYSQL_USER":            "root",
		"MYSQL_PASSWORD":        mysqlPassword,
		"REDIS_HOST":            "redis",
		"REDIS_PASSWORD":        "infini_rag_flow",
		"ES_HOST":               esHostForEnv,
		"ES_PROTOCOL":           esProtocolForEnv,
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

	// Gateway HTTPS/TLS configuration
	// EnableHTTPS controls whether to create HTTPS listener (default: false)
	// When EnableHTTPS is true, both TLSCertPEM and TLSKeyPEM must be provided
	gatewayEnableHTTPSStr := getConfig(ctx, "gateway_enable_https", "false")
	gatewayEnableHTTPS, _ := strconv.ParseBool(gatewayEnableHTTPSStr)
	gatewayTLSCert := getConfig(ctx, "gateway_tls_cert", "")
	gatewayTLSKey := getConfig(ctx, "gateway_tls_key", "")

	// Validate: if HTTPS is enabled, certificate and key must be provided
	if gatewayEnableHTTPS && (gatewayTLSCert == "" || gatewayTLSKey == "") {
		return StackConfig{}, fmt.Errorf("gateway_enable_https is true, but gateway_tls_cert and/or gateway_tls_key are not provided. Both must be set when HTTPS is enabled")
	}

	gateway := GatewayConfig{
		ClassName:   "",
		Namespace:   namespace,           // Use the same namespace as other resources
		Annotations: map[string]string{}, // Can be extended later
		Hosts:       []GatewayHost{},     // Can be extended later
		TLS:         []GatewayTLS{},      // Can be extended later
		EnableHTTPS: gatewayEnableHTTPS,
		TLSCertPEM:  gatewayTLSCert,
		TLSKeyPEM:   gatewayTLSKey,
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
			Host:     mysqlHost,
			Port:     mysqlPort,
			Password: mysqlPassword,
			DBName:   mysqlDBName,
		},
		ES: ESConfig{
			Host:     esHost,
			Port:     esPort,
			Protocol: esProtocol,
			Password: esPassword,
		},
	}, nil
}

// Helper function to get configuration value with default
func getConfig(ctx *pulumi.Context, key string, defaultValue string) string {
	if val, err := pulumiconfig.Try(ctx, key); err == nil {
		return val
	}
	return defaultValue
}

// Helper function to convert string port to int
func parsePort(portStr string) int {
	port, err := strconv.Atoi(portStr)
	if err != nil {
		// Default to 80 if parsing fails
		return 80
	}
	return port
}

// Helper function to read file content
func readFileContent(filePath string) (string, error) {
	content, err := os.ReadFile(filePath)
	if err != nil {
		return "", err
	}
	return string(content), nil
}

func main() {
	pulumi.Run(func(ctx *pulumi.Context) error {
		config, err := LoadConfig(ctx)
		if err != nil {
			return err
		}

		// Create Kubernetes provider
		k8sProvider, err := kubernetes.NewProvider(ctx, "k8s-provider", &kubernetes.ProviderArgs{})
		if err != nil {
			return err
		}

		// Create namespace
		namespace, err := corev1.NewNamespace(ctx, "ragflow-namespace", &corev1.NamespaceArgs{
			Metadata: &metav1.ObjectMetaArgs{
				Name: pulumi.String(config.Namespace),
			},
		}, pulumi.Provider(k8sProvider))
		if err != nil {
			return err
		}

		// Create Docker registry secret for private registry authentication
		// This secret will be used by pods that need to pull images from private registry
		var registrySecret *corev1.Secret
		if config.Registry.Username != "" && config.Registry.Password != "" {
			// Build dockerconfigjson with base64 encoded auth
			dockerConfigJSON := fmt.Sprintf(`{
				"auths": {
					"%s": {
						"username": "%s",
						"password": "%s",
						"auth": "%s"
					}
				}
			}`, config.Registry.Server, config.Registry.Username, config.Registry.Password,
				base64.StdEncoding.EncodeToString([]byte(config.Registry.Username+":"+config.Registry.Password)))

			// Base64 encode the entire JSON config for Kubernetes Secret data field
			encodedConfig := base64.StdEncoding.EncodeToString([]byte(dockerConfigJSON))

			registrySecret, err = corev1.NewSecret(ctx, "registry-secret", &corev1.SecretArgs{
				Metadata: &metav1.ObjectMetaArgs{
					Name:      pulumi.String("ragflow-registry-secret"),
					Namespace: namespace.Metadata.Name(),
				},
				Type: pulumi.String("kubernetes.io/dockerconfigjson"),
				Data: pulumi.StringMap{
					".dockerconfigjson": pulumi.String(encodedConfig),
				},
			}, pulumi.Provider(k8sProvider))
			if err != nil {
				return fmt.Errorf("failed to create registry secret: %w", err)
			}
			ctx.Log.Info("Created registry secret for enterprise registry authentication", &pulumi.LogArgs{})
		} else {
			ctx.Log.Warn("Registry credentials not configured, skipping registry secret creation", &pulumi.LogArgs{})
		}

		// Create MySQL deployment or use external MySQL
		// If mysql_host is empty, create MySQL deployment in the cluster
		// If mysql_host is set, use the external MySQL server
		var mysqlDeployment *v1.Deployment
		var mysqlService *corev1.Service
		if config.MySQL.Host == "" {
			ctx.Log.Info("Creating MySQL deployment in the cluster", &pulumi.LogArgs{})
			mysqlDeployment, mysqlService, err = createMySQL(ctx, &config, namespace, k8sProvider)
			if err != nil {
				return err
			}
		} else {
			ctx.Log.Info(fmt.Sprintf("Using external MySQL at %s:%s", config.MySQL.Host, config.MySQL.Port), &pulumi.LogArgs{})
		}

		// Create Redis
		redisDeployment, redisService, err := createRedis(ctx, &config, namespace, k8sProvider)
		if err != nil {
			return err
		}

		// Create Elasticsearch deployment or use external Elasticsearch
		// If es_host is empty, create Elasticsearch deployment in the cluster
		// If es_host is set, use the external Elasticsearch server
		var esDeployment interface{}
		var esService *corev1.Service
		if config.ES.Host == "" {
			ctx.Log.Info("Creating Elasticsearch deployment in the cluster", &pulumi.LogArgs{})
			esDeployment, esService, err = createElasticsearch(ctx, &config, namespace, k8sProvider)
			if err != nil {
				return err
			}
		} else {
			ctx.Log.Info(fmt.Sprintf("Using external Elasticsearch at %s:%s", config.ES.Host, config.ES.Port), &pulumi.LogArgs{})
		}

		// MinIO is replaced by Ceph RGW S3-compatible object storage

		// Create TEI
		teiDeployment, teiService, err := createTEI(ctx, &config, namespace, k8sProvider)
		if err != nil {
			return err
		}

		// Create RabbitMQ
		rabbitmqDeployment, rabbitmqService, err := createRabbitMQ(ctx, &config, namespace, k8sProvider)
		if err != nil {
			return err
		}

		// Get Elasticsearch secret name (shared by RAGFlow and Parser deployments)
		//
		// Two scenarios:
		// 1. Internal ES (es_host is empty): Use ECK-managed secret name
		// 2. External ES (es_host is set): Create secret with configured password
		var esElasticUserSecret *corev1.Secret
		es_name := "elasticsearch"
		secretName := fmt.Sprintf("%s-es-elastic-user", es_name)

		if config.ES.Host == "" {
			// Internal ES: Create a reference to ECK-managed secret (don't manage it)
			ctx.Log.Info("Using ECK-managed Elasticsearch secret", &pulumi.LogArgs{})
			// Create a dummy secret resource that we will ignore completely
			// This allows us to reference the secret in deployments without managing it
			esElasticUserSecret, err = corev1.NewSecret(ctx, "es-elastic-user-ref", &corev1.SecretArgs{
				Metadata: &metav1.ObjectMetaArgs{
					Name:      pulumi.String(secretName),
					Namespace: namespace.Metadata.Name(),
				},
			}, pulumi.Provider(k8sProvider), pulumi.IgnoreChanges([]string{
				// Ignore all fields - this secret is entirely managed by ECK
				"data", "stringData", "metadata", "type",
			}))
			if err != nil {
				return fmt.Errorf("failed to create reference to ECK Elasticsearch secret: %w", err)
			}
		} else {
			// External ES: Create secret with configured password
			ctx.Log.Info("Creating Elasticsearch secret for external ES", &pulumi.LogArgs{})
			// If es_password is empty, use a default password
			esPassword := config.ES.Password
			if esPassword == "" {
				esPassword = "changeme" // Default password for external ES
			}
			// Use a different secret name for external ES to avoid conflicts
			externalSecretName := "elasticsearch-external-credentials"
			esElasticUserSecret, err = corev1.NewSecret(ctx, "es-elastic-user-ref", &corev1.SecretArgs{
				Metadata: &metav1.ObjectMetaArgs{
					Name:      pulumi.String(externalSecretName),
					Namespace: namespace.Metadata.Name(),
				},
				StringData: pulumi.StringMap{
					"elastic": pulumi.String(esPassword),
				},
			}, pulumi.Provider(k8sProvider))
			if err != nil {
				return fmt.Errorf("failed to create Elasticsearch secret for external ES: %w", err)
			}
		}

		// Create RAGFlow deployment (includes parser deployment)
		ragflowDeployment, ragflowService, parserDeployment, err := createRAGFlowDeployment(ctx, &config, namespace, k8sProvider, esElasticUserSecret, registrySecret)
		if err != nil {
			return err
		}

		// Create Unified DeepDoc Service (DLA+OCR+TSR)
		deepdocDeployment, deepdocService, err := createDeepdocDeployment(ctx, &config, namespace, k8sProvider, registrySecret)
		if err != nil {
			return err
		}

		// Create Gateway/Ingress (always enabled)
		//
		// PLATFORM-SPECIFIC ROUTING STRATEGY:
		// - Aliyun clusters: Use Ingress API (traditional, better controller support)
		// - Other clusters: Use Gateway API (modern standard)
		//
		// WHY DIFFERENT APIS FOR ALIYUN:
		// Aliyun ALB Gateway API implementation has a known bug where HTTPRoute forwarding
		// rules fail when pods are recreated. The root cause is that the ALB Gateway API
		// implementation doesn't properly watch Service Endpoints changes.
		//
		// Workaround: Use Ingress API for Aliyun clusters, which has proven Endpoints
		// watching support through the ALB Ingress Controller.
		//
		// BROKEN STATE ON ALIYUN ACS:
		// - ACS (Aliyun Container Service Serverless) doesn't support LoadBalancer creation
		// - Ingress resources won't get LoadBalancer IP assigned
		// - This code is left in broken state as requested for next person to handle
		var gateway *apiextensions.CustomResource
		var gatewayClass string
		var routingAPI pulumi.StringInput

		// if isAliyunCluster(&config) {
		if false {
			// Use Ingress API for Aliyun ALB
			ctx.Log.Info("Detected Aliyun cluster, using Ingress API (automatically creates IngressClass)", &pulumi.LogArgs{})
			if err := createIngress(ctx, &config, k8sProvider, ragflowService); err != nil {
				return err
			}
			gatewayClass = "alb"
			routingAPI = pulumi.String("Ingress")
		} else {
			// Use Gateway API for non-Aliyun clusters (modern standard)
			ctx.Log.Info("Non-Aliyun cluster, using Gateway API (modern standard)", &pulumi.LogArgs{})
			gatewayClass, err = detectGatewayType(ctx, k8sProvider)
			if err != nil {
				return fmt.Errorf("failed to detect gateway class: %w", err)
			}

			gateway, err = createGateway(ctx, &config, k8sProvider, ragflowService, gatewayClass)
			if err != nil {
				return err
			}
			routingAPI = pulumi.String("Gateway")
		}

		// Export outputs
		ctx.Export("namespace", namespace.Metadata.Name())
		if mysqlDeployment != nil {
			ctx.Export("mysqlDeployment", mysqlDeployment.Metadata.Name())
		} else {
			ctx.Export("mysqlDeployment", pulumi.String("external"))
		}
		if mysqlService != nil {
			ctx.Export("mysqlService", mysqlService.Metadata.Name())
		} else {
			ctx.Export("mysqlService", pulumi.String("external"))
		}
		ctx.Export("redisDeployment", redisDeployment.Metadata.Name())
		ctx.Export("redisService", redisService.Metadata.Name())
		if esDeployment != nil {
			ctx.Export("esDeployment", esDeployment.(*apiextensions.CustomResource).Metadata.Name())
		} else {
			ctx.Export("esDeployment", pulumi.String("external"))
		}
		if esService != nil {
			ctx.Export("esService", esService.Metadata.Name())
		} else {
			ctx.Export("esService", pulumi.String("external"))
		}
		ctx.Export("teiDeployment", teiDeployment.Metadata.Name())
		ctx.Export("teiService", teiService.Metadata.Name())
		ctx.Export("rabbitmqDeployment", rabbitmqDeployment.Metadata.Name())
		ctx.Export("rabbitmqService", rabbitmqService.Metadata.Name())
		ctx.Export("ragflowDeployment", ragflowDeployment.Metadata.Name())
		ctx.Export("ragflowService", ragflowService.Metadata.Name())
		ctx.Export("parserDeployment", parserDeployment.Metadata.Name())
		ctx.Export("gatewayClass", pulumi.String(gatewayClass))
		if gateway != nil {
			ctx.Export("gatewayName", gateway.Metadata.Name())
		} else {
			ctx.Export("gatewayName", pulumi.String("none"))
		}
		ctx.Export("routingAPI", routingAPI)

		// Export unified DeepDoc service
		// Note: deepdocDeployment may be nil if using Knative Service
		if deepdocDeployment != nil {
			ctx.Export("deepdocDeployment", deepdocDeployment.Metadata.Name())
		} else {
			ctx.Export("deepdocDeployment", pulumi.String("knative"))
		}

		if deepdocService != nil {
			ctx.Export("deepdocService", deepdocService.Metadata.Name())
		} else {
			ctx.Export("deepdocService", pulumi.String("deepdoc"))
		}

		return nil
	})
}

func createMySQL(ctx *pulumi.Context, config *StackConfig, namespace *corev1.Namespace, provider pulumi.ProviderResource) (*v1.Deployment, *corev1.Service, error) {
	// Read MySQL PVC size configuration
	mysqlStorage := getConfig(ctx, "mysql_storage", "1Gi")

	// MySQL init.sql ConfigMap
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
			StorageClassName: pulumi.String(config.StorageClass),
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
									ContainerPort: pulumi.Int(3306),
								},
							},
							Env: corev1.EnvVarArray{
								&corev1.EnvVarArgs{
									Name:  pulumi.String("MYSQL_ROOT_PASSWORD"),
									Value: pulumi.String(config.MySQL.Password),
								},
							},
							Args: pulumi.StringArray{
								pulumi.String("--max_connections=1000"),
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
			Name:      pulumi.String("mysql"),
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
	es_name := "elasticsearch"

	// Read Elasticsearch configuration
	es_replicas_str := getConfig(ctx, "es_replicas", "1")
	es_replicas, _ := strconv.Atoi(es_replicas_str)
	es_storage := getConfig(ctx, "es_storage", "2Gi")
	es_memory_request := getConfig(ctx, "es_memory_request", "10Gi")

	// Derive other resources from es_memory_request
	// Parse memory request (e.g., "2Gi", "4Gi")
	es_memory_limit := es_memory_request
	es_jvm_memory := es_memory_request
	es_cpu_request := "1000m"
	es_cpu_limit := "2000m"

	// Simple derivation: assume format "XGi"
	if strings.HasSuffix(es_memory_request, "Gi") {
		memStr := strings.TrimSuffix(es_memory_request, "Gi")
		if memVal, err := strconv.ParseFloat(memStr, 64); err == nil {
			// memory_limit = memory_request (Required for memory locking)
			// IMPORTANT: When bootstrap.memory_lock=true, memory limits must equal requests
			// This is necessary for memory locking to work properly in Kubernetes
			es_memory_limit = es_memory_request

			// JVM memory = 50% of memory_request is safer for ES to avoid OOM
			jvmMem := memVal * 0.5
			if jvmMem < 1 {
				jvmMem = 1
			}
			es_jvm_memory = fmt.Sprintf("%.0fg", jvmMem)

			// CPU request: 1000m per 4Gi, minimum 1000m
			cpuVal := int((memVal / 4) * 1000)
			if cpuVal < 1000 {
				cpuVal = 1000
			}
			es_cpu_request = fmt.Sprintf("%dm", cpuVal)

			// CPU limit = CPU request * 2
			es_cpu_limit = fmt.Sprintf("%dm", cpuVal*2)
		}
	}
	ctx.Log.Info(fmt.Sprintf("Elasticsearch derived resources: memory_request=%s, memory_limit=%s, jvm_memory=%s, cpu_request=%s, cpu_limit=%s",
		es_memory_request, es_memory_limit, es_jvm_memory, es_cpu_request, es_cpu_limit), &pulumi.LogArgs{})
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
							"node.store.allow_mmap":  false,
							"xpack.security.enabled": true,
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
									"storageClassName": config.StorageClass,
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
		// ECK Operator manages these fields and may modify them dynamically
		// Ignoring them prevents Server-Side Apply conflicts between Pulumi and the Operator
		// WARNING: This prevents configuration of Elasticsearch parameters (e.g., bootstrap.memory_lock)
		// Consider more granular ignore patterns for production use
		"spec.nodeSets",
		"spec.auth",
		"spec.monitoring",
		"spec.transport",
		"spec.updateStrategy",
		"spec.http.tls.certificate",
		"spec.http.tls.certificateAuthorities",
		"spec.transport.tls",
		"spec.transport.service",
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
	rabbitmqStorage := getConfig(ctx, "rabbitmq_storage", "1Gi")

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
			StorageClassName: pulumi.String(config.StorageClass),
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
// For internal ES (ECK managed), uses secret references to always get the current password
// For external ES, uses the configured password directly
func buildCommonEnvVars(config *StackConfig, esSecretName pulumi.StringPtrOutput, useInternalES bool) corev1.EnvVarArray {
	envVars := corev1.EnvVarArray{}

	// Add all environment variables from config (includes PYTHONPATH, MYSQL_HOST, etc.)
	// For internal ES, skip ES_PROTOCOL and ES_HOST as they will be set to correct values below
	keys := make([]string, 0, len(config.Env))
	for k := range config.Env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		// For internal ES, skip ES_PROTOCOL and ES_HOST - they will be set correctly below
		if useInternalES && (k == "ES_PROTOCOL" || k == "ES_HOST") {
			continue
		}
		envVars = append(envVars, &corev1.EnvVarArgs{
			Name:  pulumi.String(k),
			Value: pulumi.String(config.Env[k]),
		})
	}

	// Add Elasticsearch password
	// For internal ES: reference the ECK-managed secret directly (runtime lookup)
	// For external ES: use the configured password value
	if useInternalES {
		envVars = append(envVars, &corev1.EnvVarArgs{
			Name: pulumi.String("ELASTIC_PASSWORD"),
			ValueFrom: &corev1.EnvVarSourceArgs{
				SecretKeyRef: &corev1.SecretKeySelectorArgs{
					Name: esSecretName,
					Key:  pulumi.String("elastic"),
				},
			},
		})
	} else {
		// External ES: use configured password
		esPassword := config.ES.Password
		if esPassword == "" {
			esPassword = "changeme"
		}
		envVars = append(envVars, &corev1.EnvVarArgs{
			Name:  pulumi.String("ELASTIC_PASSWORD"),
			Value: pulumi.String(esPassword),
		})
	}

	// Add ES configuration JSON
	// For internal ES: use entrypoint script to build JSON at runtime from ELASTIC_PASSWORD env var
	// For external ES: include password directly in the JSON
	var esProtocol, esHost string
	esPort := config.ES.Port

	if useInternalES {
		// Internal ES (ECK): Force HTTPS and use default ECK service name
		// Ignore user-configured ES_PROTOCOL and ES_HOST for internal ES
		esProtocol = "https"
		esHost = "elasticsearch-es-http"
	} else {
		// External ES: use configured values
		esProtocol = config.Env["ES_PROTOCOL"]
		if esProtocol == "" {
			esProtocol = "http" // Default for external ES
		}
		esHost = config.Env["ES_HOST"]
		if esHost == "" {
			esHost = "elasticsearch"
		}
	}

	if useInternalES {
		// Internal ES: password will be substituted from ELASTIC_PASSWORD env var at runtime
		// The entrypoint script handles this substitution
		envVars = append(envVars, &corev1.EnvVarArgs{
			Name:  pulumi.String("ES_HOST_JSON"),
			Value: pulumi.Sprintf(`"%s://%s:%s"`, esProtocol, esHost, esPort),
		})
		envVars = append(envVars, &corev1.EnvVarArgs{
			Name:  pulumi.String("ES_PROTOCOL"),
			Value: pulumi.String(esProtocol),
		})
		envVars = append(envVars, &corev1.EnvVarArgs{
			Name:  pulumi.String("ES_HOST"),
			Value: pulumi.String(esHost),
		})
		envVars = append(envVars, &corev1.EnvVarArgs{
			Name:  pulumi.String("ES_USERNAME"),
			Value: pulumi.String("elastic"),
		})
		// ES_JSON_TEMPLATE will be used by entrypoint to build final ES config
		envVars = append(envVars, &corev1.EnvVarArgs{
			Name:  pulumi.String("ES_JSON_TEMPLATE"),
			Value: pulumi.Sprintf(`{"hosts": "%s://%s:%s", "username": "elastic", "password": "${ELASTIC_PASSWORD}"}`, esProtocol, esHost, esPort),
		})
	} else {
		// External ES: include password directly in JSON
		esPassword := config.ES.Password
		if esPassword == "" {
			esPassword = "changeme"
		}
		envVars = append(envVars, &corev1.EnvVarArgs{
			Name:  pulumi.String("ES"),
			Value: pulumi.Sprintf(`{"hosts": "%s://%s:%s", "username": "elastic", "password": "%s"}`, esProtocol, esHost, esPort, esPassword),
		})
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
func createESWaitInitContainer(config *StackConfig, esSecret *corev1.Secret) *corev1.ContainerArgs {
	var esWaitCommand pulumi.StringInput
	var esPasswordEnvVar *corev1.EnvVarArgs

	if config.ES.Host == "" {
		// Internal ES - use SecretKeyRef to read password from ECK secret at runtime
		esWaitCommand = pulumi.String("until curl -k -u elastic:${ES_PASSWORD} https://elasticsearch-es-http:9200/_cluster/health | grep -q '\"status\":\"green\"\\|\"status\":\"yellow\"'; do echo 'Waiting for Elasticsearch...'; sleep 5; done; echo 'Elasticsearch is ready.'")
		esPasswordEnvVar = &corev1.EnvVarArgs{
			Name: pulumi.String("ES_PASSWORD"),
			ValueFrom: &corev1.EnvVarSourceArgs{
				SecretKeyRef: &corev1.SecretKeySelectorArgs{
					Name: esSecret.Metadata.Name(),
					Key:  pulumi.String("elastic"),
				},
			},
		}
		return &corev1.ContainerArgs{
			Name:  pulumi.String("wait-for-elasticsearch"),
			Image: pulumi.String(config.Images.Curl),
			Env: corev1.EnvVarArray{
				esPasswordEnvVar,
			},
			Command: pulumi.StringArray{
				pulumi.String("sh"),
				pulumi.String("-c"),
				esWaitCommand,
			},
		}
	}

	// External ES - use configured password and connection details
	esWaitCommand = pulumi.String("until curl -u elastic:${ES_PASSWORD} ${ES_PROTOCOL}://${ES_HOST}:${ES_PORT}/_cluster/health | grep -q '\"status\":\"green\"\\|\"status\":\"yellow\"'; do echo 'Waiting for external Elasticsearch at ${ES_HOST}...'; sleep 5; done; echo 'Elasticsearch is ready.'")
	// Get password from external ES secret (created at stack level)
	esPassword := config.ES.Password
	if esPassword == "" {
		esPassword = "changeme"
	}
	esPasswordEnvVar = &corev1.EnvVarArgs{
		Name:  pulumi.String("ES_PASSWORD"),
		Value: pulumi.String(esPassword),
	}

	return &corev1.ContainerArgs{
		Name:  pulumi.String("wait-for-elasticsearch"),
		Image: pulumi.String(config.Images.Curl),
		Env: corev1.EnvVarArray{
			esPasswordEnvVar,
			&corev1.EnvVarArgs{
				Name:  pulumi.String("ES_HOST"),
				Value: pulumi.String(config.Env["ES_HOST"]),
			},
			&corev1.EnvVarArgs{
				Name:  pulumi.String("ES_PROTOCOL"),
				Value: pulumi.String(config.Env["ES_PROTOCOL"]),
			},
			&corev1.EnvVarArgs{
				Name:  pulumi.String("ES_PORT"),
				Value: pulumi.String(config.ES.Port),
			},
		},
		Command: pulumi.StringArray{
			pulumi.String("sh"),
			pulumi.String("-c"),
			esWaitCommand,
		},
	}
}

func createRAGFlowDeployment(ctx *pulumi.Context, config *StackConfig, namespace *corev1.Namespace, provider pulumi.ProviderResource, esSecret *corev1.Secret, registrySecret *corev1.Secret) (*v1.Deployment, *corev1.Service, *v1.Deployment, error) {
	// Read parser replicas from config, default to 1
	parserReplicasStr := getConfig(ctx, "parser_replicas", "1")
	parserReplicas, err := strconv.Atoi(parserReplicasStr)
	if err != nil {
		parserReplicas = 1
	}

	// Read worker counts from config to reduce fsnotify usage
	// Each worker process may use fsnotify watchers through RabbitMQ client library (pika)
	// Default values: WS=3, RAPTOR=3, GRAPHRAG=3, RESUME=1 (total 10 workers)
	// Reduced to: WS=1, RAPTOR=1, GRAPHRAG=1, RESUME=0 (total 3 workers)
	// This reduces the number of fsnotify instances and helps avoid "too many open files" error
	parserWSWorkers := getConfig(ctx, "parser_ws_workers", "1")
	parserRaptorWorkers := getConfig(ctx, "parser_raptor_workers", "1")
	parserGraphragWorkers := getConfig(ctx, "parser_graphrag_workers", "1")
	parserResumeWorkers := getConfig(ctx, "parser_resume_workers", "0")

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
	if config.MySQL.Host != "" {
		initContainers = append(initContainers, &corev1.ContainerArgs{
			Name:  pulumi.String("init-mysql-database"),
			Image: pulumi.String(config.Images.MySQL),
			Env: corev1.EnvVarArray{
				&corev1.EnvVarArgs{
					Name:  pulumi.String("MYSQL_PWD"),
					Value: pulumi.String(config.MySQL.Password),
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
					config.Env["MYSQL_HOST"],
					config.Env["MYSQL_DBNAME"],
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
	initContainers = append(initContainers, createESWaitInitContainer(config, esSecret))

	ragflowDepCfg.InitContainers = initContainers

	// Create RAGFlow deployment and service
	deployment, service, err := createRAGFlowAppDeployment(ctx, config, namespace, provider, esSecret, ragflowDepCfg, registrySecret)
	if err != nil {
		return nil, nil, nil, err
	}

	// Build init containers for parser (parser also needs ES wait)
	var parserInitContainers corev1.ContainerArray
	// Add ES wait init container using the same shared function
	parserInitContainers = append(parserInitContainers, createESWaitInitContainer(config, esSecret))

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
		CreateService: false,
	}
	parserDepCfg.InitContainers = parserInitContainers

	parserDeployment, _, err := createRAGFlowAppDeployment(ctx, config, namespace, provider, esSecret, parserDepCfg, registrySecret)
	if err != nil {
		return nil, nil, nil, err
	}

	return deployment, service, parserDeployment, nil
}

// createRAGFlowAppDeployment creates a RAGFlow or Parser deployment based on the provided configuration
func createRAGFlowAppDeployment(ctx *pulumi.Context, config *StackConfig, namespace *corev1.Namespace, provider pulumi.ProviderResource, esSecret *corev1.Secret, depCfg DeploymentConfig, registrySecret *corev1.Secret) (*v1.Deployment, *corev1.Service, error) {
	// Determine if using internal ES (ECK managed) or external ES
	useInternalES := config.ES.Host == ""

	// Build common environment variables using shared function
	// Pass the secret metadata name directly for internal ES, nil for external ES
	commonEnvVars := buildCommonEnvVars(config, esSecret.Metadata.Name(), useInternalES)

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

// Helper struct for HTTPRoute configuration
type HTTPRouteConfig struct {
	Name        string
	SectionName string
	Port        int
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
	enableHTTPS bool,
	provider pulumi.ProviderResource,
	pathPrefixes []string,
	port int,
) error {
	// Build parentRefs list (always bind to HTTP, conditionally to HTTPS)
	parentRefs := []interface{}{
		map[string]interface{}{
			"name":        "ragflow-gateway",
			"namespace":   gatewayNsName,
			"sectionName": "http", // Port 80
		},
	}

	// Add HTTPS parent ref if enabled
	if enableHTTPS {
		parentRefs = append(parentRefs, map[string]interface{}{
			"name":        "ragflow-gateway",
			"namespace":   gatewayNsName,
			"sectionName": "https", // Port 443
		})
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
func createPathBasedHTTPRoute(ctx *pulumi.Context, serviceName pulumi.StringOutput, gatewayNsName, httpRouteNsName string, enableHTTPS bool, provider pulumi.ProviderResource) error {
	// TLS Termination is handled by Gateway, traffic arrives here as HTTP.

	// HTTPRoute 1: /v1 and /api -> port 9380 (API service)
	if err := createHTTPRouteForPort(ctx, "ragflow-http-route-api", serviceName, gatewayNsName, httpRouteNsName, enableHTTPS, provider, []string{"/v1", "/api"}, 9380); err != nil {
		return err
	}

	// HTTPRoute 2: /api/v1/admin -> port 9381 (admin service)
	if err := createHTTPRouteForPort(ctx, "ragflow-http-route-admin", serviceName, gatewayNsName, httpRouteNsName, enableHTTPS, provider, []string{"/api/v1/admin"}, 9381); err != nil {
		return err
	}

	// HTTPRoute 3: / (root path) -> port 80 (frontend nginx - listening on 80 now)
	if err := createHTTPRouteForPort(ctx, "ragflow-http-route-frontend", serviceName, gatewayNsName, httpRouteNsName, enableHTTPS, provider, []string{"/"}, 80); err != nil {
		return err
	}

	ctx.Log.Info("All path-based HTTPRoutes created successfully", &pulumi.LogArgs{})
	return nil
}

// createGateway now expresses resources as compact specs and calls createCR to register them.
func createGateway(ctx *pulumi.Context, config *StackConfig, provider pulumi.ProviderResource, ragflowService *corev1.Service, gatewayClass string) (*apiextensions.CustomResource, error) {
	// Create Gateway in configured namespace (default: nginx-gateway)
	gatewayNsName := config.Gateway.Namespace

	// Handle TLS certificate for HTTPS listener (if enabled)
	var secretName string
	if config.Gateway.EnableHTTPS {
		// Use user-provided certificate (validation in LoadConfig ensures cert and key are present)
		certPEM := config.Gateway.TLSCertPEM
		keyPEM := config.Gateway.TLSKeyPEM
		ctx.Log.Info("Using user-provided TLS certificate for Gateway HTTPS listener", &pulumi.LogArgs{})

		// Create Secret for Gateway TLS
		secretName = "ragflow-gateway-cert"
		_, err := corev1.NewSecret(ctx, secretName, &corev1.SecretArgs{
			Metadata: &metav1.ObjectMetaArgs{
				Name:      pulumi.String(secretName),
				Namespace: pulumi.String(gatewayNsName),
			},
			Type: pulumi.String("kubernetes.io/tls"),
			StringData: pulumi.StringMap{
				"tls.crt": pulumi.String(certPEM),
				"tls.key": pulumi.String(keyPEM),
			},
		}, pulumi.Provider(provider))
		if err != nil {
			return nil, fmt.Errorf("failed to create TLS secret: %w", err)
		}
	}

	ctx.Log.Info(fmt.Sprintf("Creating Gateway with GatewayClass: %s", gatewayClass), &pulumi.LogArgs{})

	// Build listeners list (always include HTTP, conditionally include HTTPS)
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

	// Add HTTPS listener if enabled
	if config.Gateway.EnableHTTPS {
		listeners = append(listeners, map[string]interface{}{
			"name":     "https",
			"port":     443,
			"protocol": "HTTPS",
			"tls": map[string]interface{}{
				"mode": "Terminate",
				"certificateRefs": []interface{}{
					map[string]interface{}{
						"kind": "Secret",
						"name": secretName,
					},
				},
			},
			"allowedRoutes": map[string]interface{}{
				"namespaces": map[string]interface{}{"from": "All"},
			},
		})
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
	if err := createPathBasedHTTPRoute(ctx, ragflowService.Metadata.Name().Elem(), gatewayNsName, config.Namespace, config.Gateway.EnableHTTPS, provider); err != nil {
		return nil, err
	}

	return gateway, nil
}

// isAliyunCluster detects if the current cluster is running on Aliyun
// by checking if the S3 endpoint is Aliyun OSS
func isAliyunCluster(config *StackConfig) bool {
	s3Endpoint := config.Env["S3_ENDPOINT"]
	return strings.Contains(s3Endpoint, "aliyuncs.com")
}

func getClusterEnvConfig() (string, string, string, error) {
	// Use client-go to fetch config map
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	configOverrides := &clientcmd.ConfigOverrides{}
	kubeConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, configOverrides)
	config, err := kubeConfig.ClientConfig()
	if err != nil {
		return "", "", "", err
	}
	clientset, err := k8sclientset.NewForConfig(config)
	if err != nil {
		return "", "", "", err
	}

	ctx := context.TODO()
	// Try ack-cluster-profile
	cm, err := clientset.CoreV1().ConfigMaps("kube-system").Get(ctx, "ack-cluster-profile", k8smetav1.GetOptions{})
	if err == nil && cm != nil {
		region := ""
		if val, ok := cm.Data["vsw-zone"]; ok {
			parts := strings.Split(val, ":")
			if len(parts) > 1 {
				zone := parts[1]
				// Basic heuristic to get region from zone (e.g. cn-shanghai-l -> cn-shanghai)
				lastDash := strings.LastIndex(zone, "-")
				if lastDash > 0 {
					region = zone[:lastDash]
				}
			}
		}
		return cm.Data["vpcid"], cm.Data["vswitch"], region, nil
	}

	// Try acs-profile as fallback
	cm, err = clientset.CoreV1().ConfigMaps("kube-system").Get(ctx, "acs-profile", k8smetav1.GetOptions{})
	if err == nil && cm != nil {
		// acs-profile might not easy way to get region, return empty
		return cm.Data["vpcId"], cm.Data["vSwitchIds"], "", nil
	}

	return "", "", "", fmt.Errorf("could not find cluster profile configmaps")
}

// createIngress creates an Ingress resource for Aliyun ALB
//
// BROKEN STATE - KNOWN ISSUES:
// 1. ACS clusters don't support automatic LoadBalancer creation
// 2. Ingress resources won't get LoadBalancer IP assigned
// 3. ALB Ingress Controller on ACS has limited/broken functionality
// 4. Original Gateway API bug remains: HTTPRoute forwarding rules fail when pods are recreated
//
// WHY USE INGRESS FOR ALIYUN:
// - Aliyun ALB Ingress Controller has proven Endpoints watching support
// - Gateway API implementation on Aliyun ALB has buggy Endpoints watching
// - Ingress is the traditional Kubernetes API with better controller support
//
// This code is left in broken state as requested by user for next person to handle.
func createIngress(ctx *pulumi.Context, config *StackConfig, provider pulumi.ProviderResource, ragflowService *corev1.Service) error {
	ingressNsName := config.Namespace
	var albID pulumi.StringOutput // Use StringOutput to hold the ID
	var albIDSet bool             // Flag to check if ALB was created manually

	// Check if explicit ALB configuration is present
	vpcID := getConfig(ctx, "alb_vpc_id", "")
	subnetIDs := getConfig(ctx, "alb_vswitch_ids", "")
	aliRegion := ""

	// Always try to read from cluster environment to get defaults and region
	cVpc, cSubnets, cRegion, err := getClusterEnvConfig()
	if err == nil {
		aliRegion = cRegion
		// Use discovered values if config is missing
		// Or override config? User said "Must runtime read".
		// But usually manual config overrides auto-discovery.
		// However, I need region anyway.
		if vpcID == "" {
			vpcID = cVpc
		}
		if subnetIDs == "" {
			subnetIDs = cSubnets
		}
		ctx.Log.Info(fmt.Sprintf("Discovered ALB config from cluster: vpc=%s, vswitches=%s, region=%s", cVpc, cSubnets, cRegion), &pulumi.LogArgs{})
	} else {
		ctx.Log.Warn(fmt.Sprintf("Failed to discover cluster config: %v", err), &pulumi.LogArgs{})
	}

	if vpcID != "" && subnetIDs != "" {
		ctx.Log.Info("Found ALB config (vpc_id, vswitch_ids). Provisioning ALB via pulumi-alicloud...", &pulumi.LogArgs{})

		// Configure Aliyun Provider dynamically if S3 creds are available
		var aliProvider pulumi.ProviderResource
		if config.Env["S3_ACCESS_KEY"] != "" && config.Env["S3_SECRET_KEY"] != "" && isAliyunCluster(config) {
			ctx.Log.Info("Using S3 credentials for Aliyun Provider (derived from S3 config)", &pulumi.LogArgs{})
			p, err := alicloud.NewProvider(ctx, "aliyun-dynamic", &alicloud.ProviderArgs{
				AccessKey: pulumi.String(config.Env["S3_ACCESS_KEY"]),
				SecretKey: pulumi.String(config.Env["S3_SECRET_KEY"]),
				Region:    pulumi.String(aliRegion),
			})
			if err != nil {
				return fmt.Errorf("failed to create alicloud provider: %w", err)
			}
			aliProvider = p
		}

		// Split subsystem IDs (preferred but might be insufficient zones)
		vswitchIDList := strings.Split(subnetIDs, ",")
		var cleanVswitchIDs []string
		for _, s := range vswitchIDList {
			cleanVswitchIDs = append(cleanVswitchIDs, strings.TrimSpace(s))
		}

		// Initialize opts and resOpts
		opts := []pulumi.InvokeOption{}
		resOpts := []pulumi.ResourceOption{}
		if aliProvider != nil {
			opts = append(opts, pulumi.Provider(aliProvider))
			resOpts = append(resOpts, pulumi.Provider(aliProvider))
		}

		// Get ALL VSwitches in the VPC to ensure we can find enough zones (ALB requires >= 2 zones)
		// We prioritize the configured ones, but fallback/augment with others if needed.
		// Actually, just fetching all and picking unique zones is safest for "Manual Mode" in ACS where profile might be incomplete.
		switches, err := vpc.GetSwitches(ctx, &vpc.GetSwitchesArgs{
			VpcId: &vpcID,
		}, opts...)
		if err != nil {
			return fmt.Errorf("failed to get vswitches info: %w", err)
		}

		// Select one switch per zone
		zoneMap := make(map[string]string) // ZoneID -> VswitchID
		// First pass: prefer specified switches
		for _, s := range switches.Vswitches {
			for _, preferred := range cleanVswitchIDs {
				if s.Id == preferred {
					zoneMap[s.ZoneId] = s.Id
					break
				}
			}
		}
		// Second pass: fill other zones if needed (ALB needs >= 2 zones usually)
		if len(zoneMap) < 2 {
			for _, s := range switches.Vswitches {
				if _, exists := zoneMap[s.ZoneId]; !exists {
					zoneMap[s.ZoneId] = s.Id
				}
			}
		}

		var zoneMappings alb.LoadBalancerZoneMappingArray
		for z, v := range zoneMap {
			zoneMappings = append(zoneMappings, &alb.LoadBalancerZoneMappingArgs{
				VswitchId: pulumi.String(v),
				ZoneId:    pulumi.String(z),
			})
		}

		if len(zoneMappings) < 2 {
			ctx.Log.Warn(fmt.Sprintf("Only found %d zones for ALB. ALB creation might fail if >= 2 zones are required. (Found zones in VPC: %v)", len(zoneMappings), zoneMap), &pulumi.LogArgs{})
		}

		// Create ALB Load Balancer using Alibaba Cloud provider
		albInstance, err := alb.NewLoadBalancer(ctx, "ragflow-alb", &alb.LoadBalancerArgs{
			LoadBalancerName:     pulumi.String("ragflow-alb"),
			LoadBalancerEdition:  pulumi.String("Basic"),
			AddressType:          pulumi.String("Internet"),
			AddressAllocatedMode: pulumi.String("Dynamic"),
			VpcId:                pulumi.String(vpcID),
			ZoneMappings:         zoneMappings,
			LoadBalancerBillingConfig: &alb.LoadBalancerLoadBalancerBillingConfigArgs{
				PayType: pulumi.String("PayAsYouGo"),
			},
		}, append(resOpts, pulumi.IgnoreChanges([]string{"zoneMappings"}))...)
		if err != nil {
			return fmt.Errorf("failed to create ALB instance: %w", err)
		}

		albID = albInstance.ID().ToStringOutput()
		albIDSet = true
		ctx.Log.Info("ALB Load Balancer created successfully via Pulumi", &pulumi.LogArgs{})

		// Create a placeholder AlbConfig 'alb' because IngressClass references it.
		// Even in manual mode (reuse ALB), the controller often expects the AlbConfig to exist
		// to pick up default settings or simply to validate the IngressClass parameters.
		ctx.Log.Info("Creating AlbConfig 'alb' (Manual Mode)", &pulumi.LogArgs{})
		albConfigSpec := map[string]interface{}{
			"config": map[string]interface{}{
				"name":                 "ragflow-alb-managed", // Use different name to avoid conflict? Or same?
				"addressType":          "Internet",
				"addressAllocatedMode": "Dynamic",
			},
		}
		// We use same name 'alb' for the CR resource to match IngressClass reference
		if err := createCR(ctx, "alb", "alibabacloud.com/v1", "AlbConfig", nil, albConfigSpec, provider, nil); err != nil {
			ctx.Log.Warn("Failed to create AlbConfig 'alb': "+err.Error(), &pulumi.LogArgs{})
		}

	} else {
		// Existing fallback logic...
		ctx.Log.Info("No ALB config found (alb_vpc_id, alb_vswitch_ids). Falling back to Ingress Controller provisioning.", &pulumi.LogArgs{})

		// Step 1: Create AlbConfig for Aliyun ALB
		// This defines the ALB instance configuration (Internet-facing, Dynamic IP)
		ctx.Log.Info("Creating AlbConfig 'alb'", &pulumi.LogArgs{})
		albConfigSpec := map[string]interface{}{
			"config": map[string]interface{}{
				"name":                 "ragflow-alb",
				"addressType":          "Internet",
				"addressAllocatedMode": "Dynamic",
			},
		}
		if err := createCR(ctx, "alb", "alibabacloud.com/v1", "AlbConfig", nil, albConfigSpec, provider, nil); err != nil {
			ctx.Log.Warn("Failed to create AlbConfig 'alb' (might already exist or CRD unsupported): "+err.Error(), &pulumi.LogArgs{})
			// Do not return error, proceed to try IngressClass
		} else {
			ctx.Log.Info("AlbConfig 'alb' created successfully", &pulumi.LogArgs{})
		}
	}

	// Step 2: Create IngressClass for Aliyun ALB Ingress Controller
	// The IngressClass tells Kubernetes which Ingress controller should handle this Ingress
	ctx.Log.Info("Creating IngressClass 'alb' for Aliyun ALB Ingress Controller", &pulumi.LogArgs{})
	ingressClassSpec := map[string]interface{}{
		"controller": "alb.k8s.aliyun.com/alb-ingress-controller",
		"parameters": map[string]interface{}{
			"apiGroup": "alibabacloud.com",
			"kind":     "AlbConfig",
			"name":     "alb",
			"scope":    "Cluster",
		},
	}
	if err := createCR(ctx, "alb", "networking.k8s.io/v1", "IngressClass", nil, ingressClassSpec, provider, nil); err != nil {
		return fmt.Errorf("failed to create IngressClass: %w", err)
	}
	ctx.Log.Info("IngressClass 'alb' created successfully", &pulumi.LogArgs{})

	// Step 3: Build annotations map for Aliyun ALB Ingress
	// These annotations tell the ALB Ingress Controller how to configure the load balancer
	annotations := map[string]string{
		"alb.ingress.kubernetes.io/ingress-class": "alb",
		"pulumi.com/skipAwait":                    "true", // Avoid hanging if ALB provisioning takes too long
	}
	if config.Gateway.EnableHTTPS {
		// Configure both HTTP (80) and HTTPS (443) listeners
		annotations["alb.ingress.kubernetes.io/listen-ports"] = `[{"HTTP": 80}, {"HTTPS": 443}]`
	} else {
		// Configure only HTTP (80) listener
		annotations["alb.ingress.kubernetes.io/listen-ports"] = `[{"HTTP": 80}]`
	}

	// Step 4: Build ingress rules with path-based routing
	// Based on docker/nginx/ragflow.conf routing rules:
	// - /api/v1/admin -> port 9381 (admin service)
	// - /api or /v1 -> port 9380 (API service)
	// - / -> port 80 (frontend nginx)
	ingressRules := []interface{}{
		map[string]interface{}{
			"http": map[string]interface{}{
				"paths": []interface{}{
					// Rule 1: /api/v1/admin -> port 9381 (admin service)
					map[string]interface{}{
						"path":     "/api/v1/admin",
						"pathType": "Prefix",
						"backend": map[string]interface{}{
							"service": map[string]interface{}{
								"name": ragflowService.Metadata.Name().Elem(),
								"port": map[string]interface{}{
									"number": 9381,
								},
							},
						},
					},
					// Rule 2: /api -> port 9380 (API service)
					map[string]interface{}{
						"path":     "/api",
						"pathType": "Prefix",
						"backend": map[string]interface{}{
							"service": map[string]interface{}{
								"name": ragflowService.Metadata.Name().Elem(),
								"port": map[string]interface{}{
									"number": 9380,
								},
							},
						},
					},
					// Rule 3: /v1 -> port 9380 (API service)
					map[string]interface{}{
						"path":     "/v1",
						"pathType": "Prefix",
						"backend": map[string]interface{}{
							"service": map[string]interface{}{
								"name": ragflowService.Metadata.Name().Elem(),
								"port": map[string]interface{}{
									"number": 9380,
								},
							},
						},
					},
					// Rule 4: / -> port 80 (frontend nginx)
					map[string]interface{}{
						"path":     "/",
						"pathType": "Prefix",
						"backend": map[string]interface{}{
							"service": map[string]interface{}{
								"name": ragflowService.Metadata.Name().Elem(),
								"port": map[string]interface{}{
									"number": 80,
								},
							},
						},
					},
				},
			},
		},
	}

	ingressSpec := map[string]interface{}{
		"ingressClassName": "alb",
		"rules":            ingressRules,
	}

	ctx.Log.Info("Creating Ingress resource for Aliyun ALB", &pulumi.LogArgs{})

	// Create Ingress resource
	if albIDSet {
		// Manual ALB flow: Use Pulumi Output for the ID
		ctx.Log.Info("Using Manual ALB ID for Ingress...", &pulumi.LogArgs{})

		// Convert plain annotations map to pulumi.StringMap to support Output values
		pulumiAnnotations := pulumi.StringMap{}
		for k, v := range annotations {
			pulumiAnnotations[k] = pulumi.String(v)
		}
		// Add ALB ID to annotations
		pulumiAnnotations["alb.ingress.kubernetes.io/load-balancer-id"] = albID

		// Create Ingress using generic CustomResource to avoid complex typed struct construction for now
		// but using proper Metadata with Output-capable Annotations
		_, err := apiextensions.NewCustomResource(ctx, "ragflow-ingress", &apiextensions.CustomResourceArgs{
			ApiVersion: pulumi.String("networking.k8s.io/v1"),
			Kind:       pulumi.String("Ingress"),
			Metadata: &metav1.ObjectMetaArgs{
				Name:        pulumi.String("ragflow-ingress"),
				Namespace:   pulumi.String(ingressNsName),
				Annotations: pulumiAnnotations,
			},
			OtherFields: kubernetes.UntypedArgs{
				"spec": ingressSpec,
			},
		}, pulumi.Provider(provider))
		if err != nil {
			return err
		}
		ctx.Log.Info("Ingress resource (manual ALB) created successfully", &pulumi.LogArgs{})
	} else {
		// Fallback/Standard flow
		if err := createCR(ctx, "ragflow-ingress", "networking.k8s.io/v1", "Ingress", pulumi.String(ingressNsName), ingressSpec, provider, annotations); err != nil {
			return err
		}
	}
	ctx.Log.Info("Ingress resource created successfully for Aliyun ALB", &pulumi.LogArgs{})

	return nil
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
	useKnativeGPUStr := getConfig(ctx, "use_knative_gpu", "false")
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
				// This forces a rolling update when deepdoc_hardware config changes
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
