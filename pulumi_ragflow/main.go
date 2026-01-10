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
	"k8s.io/client-go/tools/clientcmd"

	"github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes"
	apiextensions "github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/apiextensions"
	v1 "github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/apps/v1"
	corev1 "github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/core/v1"
	metav1 "github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/meta/v1"
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
	Host        string
}

// GatewayHost holds Gateway host configuration
type GatewayHost struct {
	Host  string
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
	UseGPU   bool // Use GPU (HAMi vGPU for sharing)
	Replicas int
	Image    string
	VramMB   int // VRAM to allocate in MB per pod
}

// Simple typed config for Pulumi stack values used in the PoC.
type StackConfig struct {
	Name      string
	Namespace string
	Env       map[string]string
	RAGFlow   RAGFlowConfig
	Gateway   GatewayConfig
	Deepdoc   GPUConfig // Unified DLA+OCR+TSR service
}

// LoadConfig reads config values from Pulumi configuration
func LoadConfig(ctx *pulumi.Context) (StackConfig, error) {
	// Read basic configuration
	namespace := getConfig(ctx, "namespace", "ragflow")
	// Enterprise registry configuration
	enterpriseRegistry := getConfig(ctx, "enterprise_registry", "192.168.1.51")
	// RAGFlow image configuration
	ragflowImageTag := getConfig(ctx, "ragflow_image_tag", "latest")
	ragflowImage := fmt.Sprintf("%s/infiniflow-ai/ragflow:%s", enterpriseRegistry, ragflowImageTag)
	ragflowReplicasStr := getConfig(ctx, "ragflow_replicas", "1")
	ragflowReplicas, _ := strconv.Atoi(ragflowReplicasStr)
	gatewayHost := getConfig(ctx, "ragflow_gateway", "ragflow.local")

	// Read S3 configuration
	s3Endpoint := getConfig(ctx, "s3_endpoint", "http://rook-ceph-rgw-my-store.rook-ceph.svc:80")
	s3Bucket := getConfig(ctx, "s3_bucket", "ragflow")
	s3Region := getConfig(ctx, "s3_region", "us-east-1")
	storageImplType := getConfig(ctx, "storage_impl_type", "AWS_S3")

	// Read S3 access credentials (sensitive information)
	s3AccessKey := getConfig(ctx, "s3_access_key", "")
	s3SecretKey := getConfig(ctx, "s3_secret_key", "")

	// Debug: Print the secret values to verify they are being read
	ctx.Log.Info(fmt.Sprintf("S3 Credentials: s3AccessKey='%s', s3SecretKey='%s'", s3AccessKey, s3SecretKey), &pulumi.LogArgs{})

	// Read RAGFlow secret key for session signing
	ragflowSecretKey := getConfig(ctx, "ragflow_secret_key", "DOnghtfiCeriTENdywhERlEtivOLicuL")

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
		deepdocImage = fmt.Sprintf("%s/infiniflow-ai/deepdoc_gpu:%s", enterpriseRegistry, deepdocImageTag)
	} else {
		deepdocImage = fmt.Sprintf("%s/infiniflow-ai/deepdoc_cpu:%s", enterpriseRegistry, deepdocImageTag)
	}
	// DeepDoc GPU version uses HAMi vGPU for GPU sharing
	// CPU version does not need GPU resources
	deepdocVramStr := getConfig(ctx, "deepdoc_vram_mb", "10240") // Combined memory for all three models
	deepdocVram, _ := strconv.Atoi(deepdocVramStr)
	// Enable GPU for GPU version
	deepdocUseGPU := deepdocHardware == "gpu"

	// Validate required S3 configuration
	// Note: In preview mode, secret values might not be available
	// We should only validate during actual deployment
	if storageImplType == "AWS_S3" {
		if s3AccessKey == "" || s3SecretKey == "" {
			ctx.Log.Warn("S3 access credentials are empty, but continuing for preview", &pulumi.LogArgs{})
			// Don't fail during preview, as secret values might not be available
		}
		if s3Bucket == "" {
			return StackConfig{}, fmt.Errorf("S3 bucket is required when storage_impl_type is AWS_S3")
		}
	}

	env := map[string]string{
		"DOC_ENGINE":            "elasticsearch",
		"RAGFLOW_IMAGE":         ragflowImage,
		"STACK_VERSION":         "8.11.3",
		"MYSQL_HOST":            "mysql",
		"MYSQL_PORT":            "3306",
		"MYSQL_DBNAME":          "ragflow",
		"MYSQL_USER":            "root",
		"MYSQL_PASSWORD":        "root",
		"REDIS_HOST":            "redis",
		"REDIS_PASSWORD":        "infini_rag_flow",
		"ES_HOST":               "elasticsearch-es-http",
		"ES_PROTOCOL":           "https",
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
		Image:    env["RAGFLOW_IMAGE"],
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

	gateway := GatewayConfig{
		ClassName:   "",
		Namespace:   namespace,           // Use the same namespace as other resources
		Annotations: map[string]string{}, // Can be extended later
		Hosts:       []GatewayHost{},     // Can be extended later
		TLS:         []GatewayTLS{},      // Can be extended later
		Host:        gatewayHost,
	}

	return StackConfig{
		Name:      fmt.Sprintf("%s-%s", ctx.Project(), ctx.Stack()),
		Namespace: namespace,
		Env:       env,
		RAGFlow:   ragflow,
		Gateway:   gateway,
		Deepdoc: GPUConfig{
			UseGPU:   deepdocUseGPU,
			Replicas: deepdocReplicas,
			Image:    deepdocImage,
			VramMB:   deepdocVram,
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

		// Create MySQL
		mysqlDeployment, mysqlService, err := createMySQL(ctx, &config, namespace, k8sProvider)
		if err != nil {
			return err
		}

		// Create Redis
		redisDeployment, redisService, err := createRedis(ctx, &config, namespace, k8sProvider)
		if err != nil {
			return err
		}

		// Create Elasticsearch/Infinity
		esDeployment, esService, err := createElasticsearch(ctx, &config, namespace, k8sProvider)
		if err != nil {
			return err
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

		// Get Elasticsearch password (shared by RAGFlow and Parser deployments)
		// Note: This secret is created by the ECK operator after the Elasticsearch custom resource is created
		//
		// We need to handle two scenarios:
		// 1. ES cluster doesn't exist: ECK will create both the cluster and the secret
		// 2. ES cluster already exists: The secret already exists
		//
		// Solution: Create a placeholder secret with IgnoreChanges, which works in both cases:
		// - Scenario 1: Pulumi creates placeholder, ECK updates it (ignored by Pulumi)
		// - Scenario 2: Pulumi creates placeholder, ECK updates it (ignored by Pulumi)
		//
		// IMPORTANT: We CANNOT use pulumi.Import here because it fails during preview if the
		// resource doesn't exist yet. Instead, we create a placeholder and use IgnoreChanges
		// to allow ECK to manage it.
		es_name := "elasticsearch"
		secretName := fmt.Sprintf("%s-es-elastic-user", es_name)

		ctx.Log.Info("Creating placeholder Elasticsearch secret (will be managed by ECK)", &pulumi.LogArgs{})
		esElasticUserSecret, err := corev1.NewSecret(ctx, "es-elastic-user-ref", &corev1.SecretArgs{
			Metadata: &metav1.ObjectMetaArgs{
				Name:      pulumi.String(secretName),
				Namespace: namespace.Metadata.Name(),
			},
			// Use string data for the secret (will be base64 encoded automatically)
			StringData: pulumi.StringMap{
				"elastic": pulumi.String("changeme"), // Placeholder, will be replaced by ECK
			},
		}, pulumi.Provider(k8sProvider), pulumi.IgnoreChanges([]string{
			// Ignore all fields since this secret is managed by ECK
			// This prevents Pulumi from trying to manage/overwrite ECK's changes
			"data",
			"metadata",
		}))
		if err != nil {
			return fmt.Errorf("failed to create Elasticsearch secret reference: %w", err)
		}

		// Create RAGFlow deployment (includes parser deployment)
		ragflowDeployment, ragflowService, parserDeployment, err := createRAGFlowDeployment(ctx, &config, namespace, k8sProvider, esElasticUserSecret)
		if err != nil {
			return err
		}

		// Create Unified DeepDoc Service (DLA+OCR+TSR)
		deepdocDeployment, deepdocService, err := createDeepdocDeployment(ctx, &config, namespace, k8sProvider)
		if err != nil {
			return err
		}

		// Create Gateway (always enabled)
		gatewayClass, err := detectGatewayType(ctx, k8sProvider)
		if err != nil {
			return fmt.Errorf("failed to detect gateway class: %w", err)
		}

		gateway, err := createGateway(ctx, &config, k8sProvider, ragflowService, gatewayClass)
		if err != nil {
			return err
		}

		// Export outputs
		ctx.Export("namespace", namespace.Metadata.Name())
		ctx.Export("mysqlDeployment", mysqlDeployment.Metadata.Name())
		ctx.Export("mysqlService", mysqlService.Metadata.Name())
		ctx.Export("redisDeployment", redisDeployment.Metadata.Name())
		ctx.Export("redisService", redisService.Metadata.Name())
		ctx.Export("esDeployment", esDeployment.Metadata.Name())
		ctx.Export("esService", esService.Metadata.Name())
		ctx.Export("teiDeployment", teiDeployment.Metadata.Name())
		ctx.Export("teiService", teiService.Metadata.Name())
		ctx.Export("rabbitmqDeployment", rabbitmqDeployment.Metadata.Name())
		ctx.Export("rabbitmqService", rabbitmqService.Metadata.Name())
		ctx.Export("ragflowDeployment", ragflowDeployment.Metadata.Name())
		ctx.Export("ragflowService", ragflowService.Metadata.Name())
		ctx.Export("parserDeployment", parserDeployment.Metadata.Name())
		ctx.Export("gatewayClass", pulumi.String(gatewayClass))
		ctx.Export("gatewayName", gateway.Metadata.Name())

		// Export unified DeepDoc service
		ctx.Export("deepdocDeployment", deepdocDeployment.Metadata.Name())
		ctx.Export("deepdocService", deepdocService.Metadata.Name())

		return nil
	})
}

func createMySQL(ctx *pulumi.Context, config *StackConfig, namespace *corev1.Namespace, provider pulumi.ProviderResource) (*v1.Deployment, *corev1.Service, error) {
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
					"storage": pulumi.String("1Gi"),
				},
			},
			StorageClassName: pulumi.String("rook-ceph-block"),
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
							Image: pulumi.String("mysql:8.4"),
							Ports: corev1.ContainerPortArray{
								&corev1.ContainerPortArgs{
									ContainerPort: pulumi.Int(3306),
								},
							},
							Env: corev1.EnvVarArray{
								&corev1.EnvVarArgs{
									Name:  pulumi.String("MYSQL_ROOT_PASSWORD"),
									Value: pulumi.String("root"),
								},
								&corev1.EnvVarArgs{
									Name:  pulumi.String("MYSQL_DATABASE"),
									Value: pulumi.String("ragflow"),
								},
							},
							VolumeMounts: corev1.VolumeMountArray{
								&corev1.VolumeMountArgs{
									Name:      pulumi.String("mysql-storage"),
									MountPath: pulumi.String("/var/lib/mysql"),
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
							Image: pulumi.String("valkey/valkey:8"),
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
									"storageClassName": "rook-ceph-block",
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
							Image: pulumi.String("infiniflow/text-embeddings-inference:cpu-1.8"),
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
	// Read RabbitMQ configuration files
	rabbitmqConfContent, err := readFileContent("../docker/rabbitmq-conf/rabbitmq.conf")
	if err != nil {
		return nil, nil, fmt.Errorf("failed to read rabbitmq.conf: %w", err)
	}

	definitionsContent, err := readFileContent("../docker/rabbitmq-conf/definitions.json")
	if err != nil {
		return nil, nil, fmt.Errorf("failed to read definitions.json: %w", err)
	}

	enabledPluginsContent, err := readFileContent("../docker/rabbitmq-conf/enabled_plugins")
	if err != nil {
		return nil, nil, fmt.Errorf("failed to read enabled_plugins: %w", err)
	}

	erlangCookieContent, err := readFileContent("../docker/rabbitmq-conf/rabbitmq.erlang.cookie")
	if err != nil {
		return nil, nil, fmt.Errorf("failed to read .erlang.cookie: %w", err)
	}

	entrypointContent, err := readFileContent("../docker/rabbitmq-conf/rabbitmq-entrypoint.sh")
	if err != nil {
		return nil, nil, fmt.Errorf("failed to read cluster-entrypoint.sh: %w", err)
	}

	// Modify entrypoint content to remove erlang cookie copying since it's handled by initContainer
	modifiedEntrypoint := strings.Replace(entrypointContent, `# Copy .erlang.cookie to the correct location and set permissions
cp /etc/rabbitmq/.erlang.cookie /var/lib/rabbitmq/.erlang.cookie
chmod 400 /var/lib/rabbitmq/.erlang.cookie

`, "", 1)

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
					"storage": pulumi.String("1Gi"),
				},
			},
			StorageClassName: pulumi.String("rook-ceph-block"),
		},
	}, pulumi.Provider(provider))
	if err != nil {
		return nil, nil, err
	}

	// RabbitMQ ConfigMap
	rabbitmqConfigMap, err := corev1.NewConfigMap(ctx, "rabbitmq-config", &corev1.ConfigMapArgs{
		Metadata: &metav1.ObjectMetaArgs{
			Name:      pulumi.String("rabbitmq-config"),
			Namespace: namespace.Metadata.Name(),
		},
		Data: pulumi.StringMap{
			"rabbitmq.conf":         pulumi.String(rabbitmqConfContent),
			"definitions.json":      pulumi.String(definitionsContent),
			"enabled_plugins":       pulumi.String(enabledPluginsContent),
			"cluster-entrypoint.sh": pulumi.String(modifiedEntrypoint),
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
					InitContainers: corev1.ContainerArray{
						&corev1.ContainerArgs{
							Name:  pulumi.String("init-erlang-cookie"),
							Image: pulumi.String("busybox"),
							Command: pulumi.StringArray{
								pulumi.String("sh"),
								pulumi.String("-c"),
								pulumi.Sprintf("if [ ! -f /var/lib/rabbitmq/.erlang.cookie ]; then echo -n '%s' > /var/lib/rabbitmq/.erlang.cookie && chmod 400 /var/lib/rabbitmq/.erlang.cookie; fi", erlangCookieContent),
							},
							VolumeMounts: corev1.VolumeMountArray{
								&corev1.VolumeMountArgs{
									Name:      pulumi.String("rabbitmq-storage"),
									MountPath: pulumi.String("/var/lib/rabbitmq"),
								},
							},
						},
					},
					Containers: corev1.ContainerArray{
						&corev1.ContainerArgs{
							Name:  pulumi.String("rabbitmq"),
							Image: pulumi.String("rabbitmq:4-management"),
							Command: pulumi.StringArray{
								pulumi.String("/usr/local/bin/cluster-entrypoint.sh"),
							},
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
									Name:      pulumi.String("rabbitmq-config"),
									MountPath: pulumi.String("/etc/rabbitmq"),
								},
								&corev1.VolumeMountArgs{
									Name:      pulumi.String("rabbitmq-entrypoint"),
									MountPath: pulumi.String("/usr/local/bin/cluster-entrypoint.sh"),
									SubPath:   pulumi.String("cluster-entrypoint.sh"),
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
							Name: pulumi.String("rabbitmq-config"),
							ConfigMap: &corev1.ConfigMapVolumeSourceArgs{
								Name: rabbitmqConfigMap.Metadata.Name().Elem(),
							},
						},
						&corev1.VolumeArgs{
							Name: pulumi.String("rabbitmq-entrypoint"),
							ConfigMap: &corev1.ConfigMapVolumeSourceArgs{
								Name:        rabbitmqConfigMap.Metadata.Name().Elem(),
								DefaultMode: pulumi.Int(0755),
								Items: corev1.KeyToPathArray{
									&corev1.KeyToPathArgs{
										Key:  pulumi.String("cluster-entrypoint.sh"),
										Path: pulumi.String("cluster-entrypoint.sh"),
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
func buildCommonEnvVars(config *StackConfig, esPassword pulumi.StringOutput) corev1.EnvVarArray {
	envVars := corev1.EnvVarArray{}

	// Add all environment variables from config (includes PYTHONPATH, MYSQL_HOST, etc.)
	keys := make([]string, 0, len(config.Env))
	for k := range config.Env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		envVars = append(envVars, &corev1.EnvVarArgs{
			Name:  pulumi.String(k),
			Value: pulumi.String(config.Env[k]),
		})
	}

	// Add Elasticsearch password
	envVars = append(envVars, &corev1.EnvVarArgs{
		Name:  pulumi.String("ELASTIC_PASSWORD"),
		Value: esPassword,
	})

	// Add ES configuration JSON, using HTTPS protocol
	envVars = append(envVars, &corev1.EnvVarArgs{
		Name:  pulumi.String("ES"),
		Value: pulumi.Sprintf(`{"hosts": "https://%s:9200", "username": "elastic", "password": "%s"}`, config.Env["ES_HOST"], esPassword),
	})

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

func createRAGFlowDeployment(ctx *pulumi.Context, config *StackConfig, namespace *corev1.Namespace, provider pulumi.ProviderResource, esSecret *corev1.Secret) (*v1.Deployment, *corev1.Service, *v1.Deployment, error) {
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
				ContainerPort: pulumi.Int(9380),
			},
			&corev1.ContainerPortArgs{
				ContainerPort: pulumi.Int(9381),
			},
			&corev1.ContainerPortArgs{
				ContainerPort: pulumi.Int(9382),
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
			{Name: "http", Port: 80, TargetPort: 80}, // Frontend nginx server
		},
	}

	// Build init containers for RAGFlow
	var initContainers corev1.ContainerArray
	// Use the shared Elasticsearch secret passed as parameter
	esPassword := esSecret.Data.ApplyT(func(m map[string]string) (string, error) {
		b64, ok := m["elastic"]
		if !ok {
			return "", nil
		}
		decoded, err := base64.StdEncoding.DecodeString(b64)
		if err != nil {
			return "", err
		}
		return string(decoded), nil
	}).(pulumi.StringOutput)

	// Wait for Elasticsearch to be ready
	initContainers = append(initContainers, &corev1.ContainerArgs{
		Name:  pulumi.String("wait-for-elasticsearch"),
		Image: pulumi.String("curlimages/curl:latest"),
		Command: pulumi.StringArray{
			pulumi.String("sh"),
			pulumi.String("-c"),
			pulumi.Sprintf("until curl -k -u elastic:%s https://elasticsearch-es-http:9200/_cluster/health | grep -q '\"status\":\"green\"\\|\"status\":\"yellow\"'; do echo 'Waiting for Elasticsearch...'; sleep 5; done; echo 'Elasticsearch is ready.'", esPassword),
		},
	})
	if bucket, exists := config.Env["S3_BUCKET"]; exists && bucket != "" {
		initContainers = append(initContainers, &corev1.ContainerArgs{
			Name:  pulumi.String("init-s3-bucket"),
			Image: pulumi.String("amazon/aws-cli:latest"),
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
					set -e
					echo "Waiting for S3 endpoint to be ready..."
					# Try to connect to S3 endpoint
					for i in $(seq 1 30); do
						if curl -s -f %s >/dev/null 2>&1; then
							echo "S3 endpoint is ready."
							break
						fi
						if [ $i -eq 30 ]; then
							echo "S3 endpoint not ready after 150 seconds, continuing anyway..."
						fi
						echo "S3 endpoint not ready, waiting... (attempt $i/30)"
						sleep 5
					done
					echo "Creating S3 bucket '%s'..."
					# Use AWS CLI with S3 compatibility mode
					aws --endpoint-url=%s s3api create-bucket --bucket %s --region %s || \
					aws --endpoint-url=%s s3 mb s3://%s || \
					echo "Bucket may already exist or creation failed, continuing..."
					echo "Bucket creation attempt completed."
				`, config.Env["S3_ENDPOINT"], config.Env["S3_BUCKET"], config.Env["S3_ENDPOINT"], config.Env["S3_BUCKET"], config.Env["S3_REGION"], config.Env["S3_ENDPOINT"], config.Env["S3_BUCKET"]),
			},
		})
	}
	ragflowDepCfg.InitContainers = initContainers

	// Create RAGFlow deployment and service
	deployment, service, err := createRAGFlowAppDeployment(ctx, config, namespace, provider, esSecret, ragflowDepCfg)
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
		CreateService: false,
	}

	parserDeployment, _, err := createRAGFlowAppDeployment(ctx, config, namespace, provider, esSecret, parserDepCfg)
	if err != nil {
		return nil, nil, nil, err
	}

	return deployment, service, parserDeployment, nil
}

// createRAGFlowAppDeployment creates a RAGFlow or Parser deployment based on the provided configuration
func createRAGFlowAppDeployment(ctx *pulumi.Context, config *StackConfig, namespace *corev1.Namespace, provider pulumi.ProviderResource, esSecret *corev1.Secret, depCfg DeploymentConfig) (*v1.Deployment, *corev1.Service, error) {
	// Use the shared Elasticsearch secret passed as parameter
	esPassword := esSecret.Data.ApplyT(func(m map[string]string) (string, error) {
		b64, ok := m["elastic"]
		if !ok {
			return "", nil
		}
		decoded, err := base64.StdEncoding.DecodeString(b64)
		if err != nil {
			return "", err
		}
		return string(decoded), nil
	}).(pulumi.StringOutput)

	// Build common environment variables using shared function
	commonEnvVars := buildCommonEnvVars(config, esPassword)

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
					Volumes: volumes,
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

func createCR(ctx *pulumi.Context, name, apiVersion, kind string, namespace pulumi.StringPtrInput, spec map[string]interface{}, provider pulumi.ProviderResource) error {
	_, err := apiextensions.NewCustomResource(ctx, name, &apiextensions.CustomResourceArgs{
		ApiVersion: pulumi.String(apiVersion),
		Kind:       pulumi.String(kind),
		Metadata: &metav1.ObjectMetaArgs{
			Name:      pulumi.String(name),
			Namespace: namespace,
		},
		OtherFields: kubernetes.UntypedArgs{
			"spec": spec,
		},
	}, pulumi.Provider(provider))
	return err
}

func createPathBasedHTTPRoute(ctx *pulumi.Context, gatewayHost string, serviceName pulumi.StringOutput, gatewayNsName, httpRouteNsName string, provider pulumi.ProviderResource) error {
	// Create HTTPRoute with path-based routing rules based on docker/nginx/ragflow.conf
	routeSpec := map[string]interface{}{
		"parentRefs": []interface{}{
			map[string]interface{}{
				"name":        "ragflow-gateway",
				"namespace":   gatewayNsName,
				"sectionName": "http",
			},
		},
		"rules": []interface{}{
			// Rule 1: /v1 or /api -> port 9380 (API service)
			map[string]interface{}{
				"matches": []interface{}{
					map[string]interface{}{
						"path": map[string]interface{}{
							"type":  "PathPrefix",
							"value": "/v1",
						},
					},
					map[string]interface{}{
						"path": map[string]interface{}{
							"type":  "PathPrefix",
							"value": "/api",
						},
					},
				},
				"backendRefs": []interface{}{
					map[string]interface{}{
						"kind": "Service",
						"name": serviceName,
						"port": 9380,
					},
				},
			},
			// Rule 2: /api/v1/admin -> port 9381 (admin service)
			map[string]interface{}{
				"matches": []interface{}{
					map[string]interface{}{
						"path": map[string]interface{}{
							"type":  "PathPrefix",
							"value": "/api/v1/admin",
						},
					},
				},
				"backendRefs": []interface{}{
					map[string]interface{}{
						"kind": "Service",
						"name": serviceName,
						"port": 9381,
					},
				},
			},
			// Rule 3: / (root path) -> port 80 (frontend nginx)
			map[string]interface{}{
				"matches": []interface{}{
					map[string]interface{}{
						"path": map[string]interface{}{
							"type":  "PathPrefix",
							"value": "/",
						},
					},
				},
				"backendRefs": []interface{}{
					map[string]interface{}{
						"kind": "Service",
						"name": serviceName,
						"port": 80,
					},
				},
			},
		},
	}

	// Add hostname if gateway has one
	if gatewayHost != "" {
		routeSpec["hostnames"] = []string{gatewayHost}
	}

	ctx.Log.Info("Creating path-based HTTPRoute resource", &pulumi.LogArgs{})
	if err := createCR(ctx, "ragflow-http-route", "gateway.networking.k8s.io/v1", "HTTPRoute", pulumi.String(httpRouteNsName), routeSpec, provider); err != nil {
		ctx.Log.Error(fmt.Sprintf("Failed to create path-based HTTPRoute: %v", err), &pulumi.LogArgs{})
		return err
	}
	ctx.Log.Info("Path-based HTTPRoute resource created successfully", &pulumi.LogArgs{})
	return nil
}

// createGateway now expresses resources as compact specs and calls createCR to register them.
func createGateway(ctx *pulumi.Context, config *StackConfig, provider pulumi.ProviderResource, ragflowService *corev1.Service, gatewayClass string) (*apiextensions.CustomResource, error) {
	// Create Gateway in configured namespace (default: nginx-gateway)
	gatewayNsName := config.Gateway.Namespace

	ctx.Log.Info(fmt.Sprintf("Creating Gateway with GatewayClass: %s", gatewayClass), &pulumi.LogArgs{})

	gatewaySpec := map[string]interface{}{
		"gatewayClassName": gatewayClass,
		"listeners": []interface{}{
			map[string]interface{}{
				"name":     "http",
				"port":     80,
				"protocol": "HTTP",
				"allowedRoutes": map[string]interface{}{
					"namespaces": map[string]interface{}{
						"from": "All",
					},
				},
			},
		},
	}
	if config.Gateway.Host != "" {
		listeners := gatewaySpec["listeners"].([]interface{})
		for i := range listeners {
			listeners[i].(map[string]interface{})["hostname"] = config.Gateway.Host
		}
		ctx.Log.Info(fmt.Sprintf("Gateway hostname set to: %s for all listeners", config.Gateway.Host), &pulumi.LogArgs{})
	}

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
	if err := createPathBasedHTTPRoute(ctx, config.Gateway.Host, ragflowService.Metadata.Name().Elem(), gatewayNsName, config.Namespace, provider); err != nil {
		return nil, err
	}

	return gateway, nil
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

// createDeepdocDeployment creates a unified deployment for DLA+OCR+TSR models
func createDeepdocDeployment(ctx *pulumi.Context, config *StackConfig, namespace *corev1.Namespace, provider pulumi.ProviderResource) (*v1.Deployment, *corev1.Service, error) {
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
