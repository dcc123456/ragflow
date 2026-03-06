# =============================================================================
# RAGFlow Aliyun Cloud Deployment - Variables
# =============================================================================
# Shared variables for infrastructure and kubernetes stages
#
# This file contains all variables used by both infrastructure.tf and kubernetes.tf
# =============================================================================
# Variables
# =============================================================================

# =============================================================================
# Network Configuration
# =============================================================================

variable "vpc_option" {
  type        = string
  default     = "new"
  description = <<EOT
  {
    "AllowedValues": ["new", "existing"],
    "AssociationPropertyMetadata": {
      "ValueLabelMapping": {
        "new": {
          "zh-cn": "新建专有网络",
          "en": "New VPC"
        },
        "existing": {
          "zh-cn": "已有专有网络",
          "en": "Existing VPC"
        }
      }
    },
    "Description": {
      "en": "Choose to create new VPC or use existing VPC",
      "zh-cn": "选择创建新VPC或使用现有VPC"
    },
    "Label": {
      "en": "VPC Option",
      "zh-cn": "VPC选项"
    }
  }
  EOT

  validation {
    condition     = contains(["new", "existing"], var.vpc_option)
    error_message = "vpc_option must be 'new' or 'existing'."
  }
}

variable "region" {
  type        = string
  default     = "cn-shanghai"
  description = <<EOT
  {
    "AssociationProperty": "ALIYUN::ECS::RegionId",
    "Description": {
      "en": "Select the region where the resources will be deployed",
      "zh-cn": "选择资源部署的阿里云地域"
    },
    "Label": {
      "en": "Region",
      "zh-cn": "地域"
    }
  }
  EOT

  validation {
    condition     = can(regex("^cn-[a-z]+$", var.region))
    error_message = "Region must be a valid Aliyun region format (e.g., cn-hangzhou, cn-shanghai)."
  }
}

variable "zone_id" {
  type        = string
  default     = "cn-shanghai-b"
  description = <<EOT
  {
    "AssociationProperty": "ALIYUN::ECS::ZoneId",
    "Description": {
      "en": "Availability zone for first VSwitch. Use 'aliyun ecs DescribeZones' to list all zones",
      "zh-cn": "第一个交换机的可用区。使用 'aliyun ecs DescribeZones' 列出所有可用区"
    },
    "Label": {
      "en": "Primary Availability Zone",
      "zh-cn": "主可用区"
    }
  }
  EOT

  validation {
    condition     = can(regex("^cn-[a-z]+-[a-z]$", var.zone_id))
    error_message = "Zone ID must be a valid Aliyun zone format (e.g., cn-hangzhou-i, cn-shanghai-b). Use 'aliyun ecs DescribeZones' to list available zones."
  }
}

variable "zone_id_2" {
  type        = string
  default     = "cn-shanghai-e"
  description = <<EOT
  {
    "AssociationProperty": "ALIYUN::ECS::ZoneId",
    "AssociationPropertyMetadata": {
      "ExclusiveTo": ["zone_id"]
    },
    "Description": {
      "en": "Availability zone for second VSwitch (optional). Use a different zone from zone_id for HA",
      "zh-cn": "第二个交换机的可用区（可选）。使用与 zone_id 不同的可用区以实现高可用"
    },
    "Label": {
      "en": "Secondary Availability Zone",
      "zh-cn": "备用可用区"
    }
  }
  EOT

  validation {
    condition     = var.zone_id_2 == "" || can(regex("^cn-[a-z]+-[a-z]$", var.zone_id_2))
    error_message = "Zone ID must be a valid Aliyun zone format (e.g., cn-hangzhou-i, cn-shanghai-b), or empty to skip."
  }
}

variable "vpc_id" {
  type        = string
  default     = ""
  description = <<EOT
  {
    "AssociationProperty": "ALIYUN::ECS::VPC::VPCId",
    "AssociationPropertyMetadata": {
      "RegionId": "region",
      "Visible": {
        "Condition": {
          "Fn::Equals": ["existing", "vpc_option"]
        }
      }
    },
    "Description": {
      "en": "Existing VPC ID (required when using existing VPC)",
      "zh-cn": "现有专有网络ID（使用现有VPC时必填）"
    },
    "Label": {
      "en": "Existing VPC ID",
      "zh-cn": "现有专有网络ID"
    },
    "Required": {
      "Fn::Equals": ["existing", "vpc_option"]
    }
  }
  EOT
}

variable "vpc_cidr" {
  type        = string
  default     = "10.0.0.0/16"
  description = <<EOT
  {
    "AssociationProperty": "ALIYUN::ECS::VPC::CidrBlock",
    "AssociationPropertyMetadata": {
      "Visible": {
        "Condition": {
          "Fn::Equals": ["new", "vpc_option"]
        }
      }
    },
    "Description": {
      "en": "The ip address range of the VPC in the CidrBlock form. <br>You can use the following ip address ranges and their subnets: <br><font color='green'>[10.0.0.0/8]</font><br><font color='green'>[172.16.0.0/12]</font><br><font color='green'>[192.168.0.0/16]</font>",
      "zh-cn": "VPC的ip地址段范围，<br>您可以使用以下的ip地址段或其子网:<br><font color='green'>[10.0.0.0/8]</font><br><font color='green'>[172.16.0.0/12]</font><br><font color='green'>[192.168.0.0/16]</font>"
    },
    "Label": {
      "en": "VPC CIDR Block",
      "zh-cn": "VPC CIDR地址块"
    }
  }
  EOT
}

variable "vswitch_ids" {
  type        = list(string)
  default     = []
  description = <<EOT
  {
    "AssociationProperty": "ALIYUN::ECS::VSwitch::VSwitchId",
    "AssociationPropertyMetadata": {
      "VpcId": "vpc_id",
      "Visible": {
        "Condition": {
          "Fn::Equals": ["existing", "vpc_option"]
        }
      }
    },
    "Description": {
      "en": "Existing VSwitch IDs (required when using existing VPC)",
      "zh-cn": "现有交换机ID列表（使用现有VPC时必填）"
    },
    "Label": {
      "en": "Existing VSwitch IDs",
      "zh-cn": "现有交换机ID列表"
    },
    "Required": {
      "Fn::Equals": ["existing", "vpc_option"]
    }
  }
  EOT
}

variable "vswitch_cidrs" {
  type        = list(string)
  default     = ["10.0.1.0/24", "10.0.2.0/24"]
  description = <<EOT
  {
    "AssociationProperty": "ALIYUN::ECS::VSwitch::CidrBlock",
    "AssociationPropertyMetadata": {
      "VpcCidrBlock": "vpc_cidr",
      "Visible": {
        "Condition": {
          "Fn::Equals": ["new", "vpc_option"]
        }
      }
    },
    "Description": {
      "en": "VSwitch CIDR blocks (for new VSwitch)",
      "zh-cn": "交换机CIDR地址块（新建交换机）"
    },
    "Label": {
      "en": "VSwitch CIDR Blocks",
      "zh-cn": "交换机CIDR地址块"
    }
  }
  EOT
}

variable "pod_cidr" {
  type        = string
  default     = "10.16.0.0/16"
  description = <<EOT
  {
    "AssociationProperty": "ALIYUN::CS::ManagedKubernetes::PodCidr",
    "Description": {
      "en": "Kubernetes pod CIDR block",
      "zh-cn": "Kubernetes Pod CIDR地址块"
    },
    "Label": {
      "en": "Pod CIDR",
      "zh-cn": "Pod CIDR地址块"
    }
  }
  EOT
}

variable "service_cidr" {
  type        = string
  default     = "10.17.0.0/16"
  description = <<EOT
  {
    "AssociationProperty": "ALIYUN::CS::ManagedKubernetes::ServiceCidr",
    "Description": {
      "en": "Kubernetes service CIDR block",
      "zh-cn": "Kubernetes Service CIDR地址块"
    },
    "Label": {
      "en": "Service CIDR",
      "zh-cn": "Service CIDR地址块"
    }
  }
  EOT
}

# =============================================================================
# Kubernetes Configuration
# =============================================================================

variable "storage_class" {
  type        = string
  default     = "alicloud-disk-alltype"
  description = <<EOT
  {
    "AssociationProperty": "ALIYUN::CS::Kubernetes::StorageClass",
    "AllowedValues": ["alicloud-disk-ssd", "alicloud-disk-essd", "alicloud-disk-available"],
    "AssociationPropertyMetadata": {
      "ValueLabelMapping": {
        "alicloud-disk-ssd": {
          "zh-cn": "SSD云盘",
          "en": "SSD Cloud Disk"
        },
        "alicloud-disk-essd": {
          "zh-cn": "ESSD云盘",
          "en": "ESSD Cloud Disk"
        },
        "alicloud-disk-available": {
          "zh-cn": "可用区盘",
          "en": "Available Zone Disk"
        }
      }
    },
    "Description": {
      "en": "Kubernetes StorageClass for PVCs (MySQL, Elasticsearch, RabbitMQ)",
      "zh-cn": "Kubernetes存储类用于PVC（MySQL、Elasticsearch、RabbitMQ）"
    },
    "Label": {
      "en": "Storage Class",
      "zh-cn": "存储类"
    }
  }
  EOT
}

variable "namespace" {
  type        = string
  default     = "ragflow"
  description = <<EOT
  {
    "Description": {
      "en": "Kubernetes namespace for RAGFlow deployment",
      "zh-cn": "RAGFlow部署的Kubernetes命名空间"
    },
    "Label": {
      "en": "Kubernetes Namespace",
      "zh-cn": "Kubernetes命名空间"
    }
  }
  EOT
}

# =============================================================================
# Storage Configuration
# =============================================================================

variable "existing_bucket_name" {
  type        = string
  default     = ""
  description = <<EOT
  {
    "AssociationProperty": "ALIYUN::OSS::Bucket::BucketName",
    "Description": {
      "en": "Existing OSS bucket name. Leave empty to create a new bucket",
      "zh-cn": "现有OSS存储桶名称。留空则创建新的存储桶"
    },
    "Label": {
      "en": "Existing OSS Bucket",
      "zh-cn": "现有OSS存储桶"
    }
  }
  EOT
}

variable "storage_location" {
  type        = string
  default     = "cn-hangzhou"
  description = <<EOT
  {
    "AssociationProperty": "ALIYUN::OSS::Bucket::RegionId",
    "Description": {
      "en": "OSS bucket location (region)",
      "zh-cn": "OSS存储桶所在地域"
    },
    "Label": {
      "en": "OSS Bucket Region",
      "zh-cn": "OSS存储桶地域"
    }
  }
  EOT
}

# =============================================================================
# MySQL Configuration
# =============================================================================

variable "mysql_deployment_mode" {
  type        = string
  default     = "k8s"
  description = <<EOT
  {
    "AllowedValues": ["k8s", "cloud"],
    "AssociationPropertyMetadata": {
      "ValueLabelMapping": {
        "k8s": {
          "zh-cn": "在Kubernetes中部署",
          "en": "Deploy in Kubernetes"
        },
        "cloud": {
          "zh-cn": "购买阿里云RDS<font color='red'><b>使用付费配置</b></font>",
          "en": "Purchase Aliyun RDS"
        }
      }
    },
    "Description": {
      "en": "MySQL deployment mode: k8s (deploy in Kubernetes) or cloud (use Aliyun RDS)",
      "zh-cn": "MySQL部署模式：k8s（在Kubernetes中部署）或 cloud（使用阿里云RDS）"
    },
    "Label": {
      "en": "MySQL Deployment Mode",
      "zh-cn": "MySQL部署模式"
    }
  }
  EOT

  validation {
    condition     = contains(["k8s", "cloud"], var.mysql_deployment_mode)
    error_message = "mysql_deployment_mode must be 'k8s' or 'cloud'."
  }
}


variable "mysql_instance_class" {
  type        = string
  default     = "mysql.n2.large.2c"
  description = <<EOT
  {
    "AssociationProperty": "ALIYUN::RDS::Instance::InstanceType",
    "AssociationPropertyMetadata": {
      "Visible": {
        "Condition": {
          "Fn::Equals": ["cloud", "mysql_deployment_mode"]
        }
      }
    },
    "Description": {
      "en": "RDS MySQL instance class (cloud mode) - High Availability edition. See: https://rdsbuy.console.aliyun.com/newCreate/rds/mysql",
      "zh-cn": "RDS MySQL实例规格（云模式 - 高可用版）。参考文档：https://rdsbuy.console.aliyun.com/newCreate/rds/mysql"
    },
    "Label": {
      "en": "MySQL Instance Class (HA)",
      "zh-cn": "MySQL实例规格（高可用）"
    }
  }
  EOT
}

variable "mysql_storage" {
  type        = number
  default     = 100
  description = <<EOT
  {
    "AssociationProperty": "ALIYUN::RDS::Instance::InstanceStorage",
    "MinValue": 20,
    "MaxValue": 3000,
    "AssociationPropertyMetadata": {
      "Visible": {
        "Condition": {
          "Fn::Equals": ["cloud", "mysql_deployment_mode"]
        }
      }
    },
    "Description": {
      "en": "MySQL storage size in GB (cloud mode)",
      "zh-cn": "MySQL存储大小（GB）（云模式）"
    },
    "Label": {
      "en": "MySQL Storage (GB)",
      "zh-cn": "MySQL存储（GB）"
    }
  }
  EOT
}

variable "mysql_password" {
  type        = string
  sensitive   = true
  default     = ""
  description = <<EOT
  {
    "AssociationProperty": "ALIYUN::RDS::Instance::MasterAccountPassword",
    "AssociationPropertyMetadata": {
      "Visible": {
        "Condition": {
          "Fn::Equals": ["cloud", "mysql_deployment_mode"]
        }
      },
      "Placeholder": {
        "zh-cn": "长度为8~32个字符，必须同时包含大写英文字母、小写英文字母、数字和特殊字符中的三项",
        "en": "8~32 characters, must contain three of: uppercase letters, lowercase letters, numbers, special characters"
      }
    },
    "ConstraintDescription": {
      "zh-cn": "长度为8~32个字符，必须同时包含大写英文字母、小写英文字母、数字和特殊字符中的三项。支持的特殊字符为：!@#$%&*()_+-=",
      "en": "8~32 characters, must contain three of: uppercase letters, lowercase letters, numbers, special characters (!@#$%&*()_+-=)"
    },
    "Description": {
      "en": "MySQL root password. Leave empty to auto-generate a secure password",
      "zh-cn": "MySQL root密码。留空则自动生成安全密码"
    },
    "Label": {
      "en": "MySQL Password",
      "zh-cn": "MySQL密码"
    },
    "NoEcho": "true"
  }
  EOT
}

variable "mysql_k8s_storage" {
  type        = number
  default     = 200
  description = <<EOT
  {
    "MinValue": 20,
    "MaxValue": 500,
    "AssociationPropertyMetadata": {
      "Visible": {
        "Condition": {
          "Fn::Equals": ["k8s", "mysql_deployment_mode"]
        }
      }
    },
    "Description": {
      "en": "MySQL storage size in GB (k8s mode). Aliyun PVC shall be no less than 20Gi",
      "zh-cn": "MySQL存储大小（GB）（K8s模式）。阿里云PVC最小为20Gi"
    },
    "Label": {
      "en": "MySQL K8s Storage (GB)",
      "zh-cn": "MySQL K8s存储（GB）"
    }
  }
  EOT
}

variable "mysql_cpu_request" {
  type        = string
  default     = "4"
  description = <<EOT
  {
    "AssociationPropertyMetadata": {
      "Visible": {
        "Condition": {
          "Fn::Equals": ["k8s", "mysql_deployment_mode"]
        }
      }
    },
    "Description": {
      "en": "MySQL CPU request (k8s mode)",
      "zh-cn": "MySQL CPU请求（K8s模式）"
    },
    "Label": {
      "en": "MySQL CPU Request",
      "zh-cn": "MySQL CPU请求"
    }
  }
  EOT
}

variable "mysql_cpu_limit" {
  type        = string
  default     = "8"
  description = <<EOT
  {
    "AssociationPropertyMetadata": {
      "Visible": {
        "Condition": {
          "Fn::Equals": ["k8s", "mysql_deployment_mode"]
        }
      }
    },
    "Description": {
      "en": "MySQL CPU limit (k8s mode)",
      "zh-cn": "MySQL CPU限制（K8s模式）"
    },
    "Label": {
      "en": "MySQL CPU Limit",
      "zh-cn": "MySQL CPU限制"
    }
  }
  EOT
}

variable "mysql_memory_request" {
  type        = string
  default     = "8Gi"
  description = <<EOT
  {
    "AssociationPropertyMetadata": {
      "Visible": {
        "Condition": {
          "Fn::Equals": ["k8s", "mysql_deployment_mode"]
        }
      }
    },
    "Description": {
      "en": "MySQL memory request (k8s mode)",
      "zh-cn": "MySQL内存请求（K8s模式）"
    },
    "Label": {
      "en": "MySQL Memory Request",
      "zh-cn": "MySQL内存请求"
    }
  }
  EOT
}

variable "mysql_memory_limit" {
  type        = string
  default     = "16Gi"
  description = <<EOT
  {
    "AssociationPropertyMetadata": {
      "Visible": {
        "Condition": {
          "Fn::Equals": ["k8s", "mysql_deployment_mode"]
        }
      }
    },
    "Description": {
      "en": "MySQL memory limit (k8s mode)",
      "zh-cn": "MySQL内存限制（K8s模式）"
    },
    "Label": {
      "en": "MySQL Memory Limit",
      "zh-cn": "MySQL内存限制"
    }
  }
  EOT
}

# =============================================================================
# Elasticsearch Configuration
# =============================================================================

variable "es_deployment_mode" {
  type        = string
  default     = "k8s"
  description = <<EOT
  {
    "AllowedValues": ["k8s", "cloud"],
    "AssociationPropertyMetadata": {
      "ValueLabelMapping": {
        "k8s": {
          "zh-cn": "在Kubernetes中部署",
          "en": "Deploy in Kubernetes"
        },
        "cloud": {
          "zh-cn": "购买阿里云ES<font color='red'><b>使用付费配置</b></font>",
          "en": "Purchase Aliyun ES"
        }
      }
    },
    "Description": {
      "en": "Elasticsearch deployment mode: k8s (deploy in Kubernetes) or cloud (use Aliyun ES)",
      "zh-cn": "Elasticsearch部署模式：k8s（在Kubernetes中部署）或 cloud（使用阿里云ES）"
    },
    "Label": {
      "en": "Elasticsearch Deployment Mode",
      "zh-cn": "Elasticsearch部署模式"
    }
  }
  EOT

  validation {
    condition     = contains(["k8s", "cloud"], var.es_deployment_mode)
    error_message = "es_deployment_mode must be 'k8s' or 'cloud'."
  }
}

variable "es_version" {
  type        = string
  default     = "8.17_with_X-Pack"
  description = <<EOT
  {
    "AssociationProperty": "ALIYUN::Elasticsearch::Instance::ElasticsearchVersion",
    "AllowedValues": [
      "8.17_with_X-Pack",
      "8.15_with_X-Pack",
    ],
    "AssociationPropertyMetadata": {
      "ValueLabelMapping": {
        "8.17_with_X-Pack": {
          "zh-cn": "8.17 向量增强版（推荐）",
          "en": "8.17 Vector Enhanced (Recommended)"
        },
        "8.15_with_X-Pack": {
          "zh-cn": "8.15 向量增强版",
          "en": "8.15 Vector Enhanced"
        },
      },
      "Visible": {
        "Condition": {
          "Fn::Equals": ["cloud", "es_deployment_mode"]
        }
      }
    },
    "Description": {
      "en": "Elasticsearch version (cloud mode). 8.17/8.15 with X-Pack support vector search. RAGFlow requires ES 8.x. See: https://help.aliyun.com/zh/es/product-overview/overview-6",
      "zh-cn": "Elasticsearch版本（云模式）。8.17/8.15 with X-Pack支持向量搜索。RAGFlow要求ES 8.x版本。参考文档：https://help.aliyun.com/zh/es/product-overview/overview-6"
    },
    "Label": {
      "en": "Elasticsearch Version (Vector Enhanced, RAGFlow requires 8.x)",
      "zh-cn": "Elasticsearch版本（向量增强版，RAGFlow要求8.x）"
    }
  }
  EOT
}

variable "es_node_count" {
  type        = number
  default     = 4 # Zones=2, integer multiple is 4
  description = <<EOT
  {
    "MinValue": 1,
    "MaxValue": 50,
    "AssociationPropertyMetadata": {
      "Visible": {
        "Condition": {
          "Fn::Equals": ["cloud", "es_deployment_mode"]
        }
      }
    },
    "Description": {
      "en": "Number of Elasticsearch data nodes (cloud mode), shall be multiple times of zones",
      "zh-cn": "Elasticsearch数据节点数量（云模式），必须是可用区数目的整数倍"
    },
    "Label": {
      "en": "Elasticsearch Node Count",
      "zh-cn": "Elasticsearch节点数量"
    }
  }
  EOT
}

variable "es_node_spec" {
  type        = string
  default     = "elasticsearch.sn2ne.xlarge.new" # Match existing instance: 4C 16GB
  description = <<EOT
  {
    "AssociationProperty": "ALIYUN::Elasticsearch::Instance::NodeSpec",
    "AllowedValues": [
      "elasticsearch.sn1ne.large.new",
      "elasticsearch.sn1ne.xlarge.new",
      "elasticsearch.sn1ne.2xlarge.new",
      "elasticsearch.sn1ne.4xlarge.new",
      "elasticsearch.sn2ne.large.new",
      "elasticsearch.sn2ne.xlarge.new",
      "elasticsearch.sn2ne.2xlarge.new",
      "elasticsearch.sn2ne.4xlarge.new",
      "elasticsearch.sn2ne.8xlarge.new",
      "elasticsearch.r7a.2xlarge.new",
      "elasticsearch.r7a.4xlarge.new",
      "elasticsearch.r7a.8xlarge.new"
    ],
    "AssociationPropertyMetadata": {
      "ValueLabelMapping": {
        "elasticsearch.sn1ne.large.new": {
          "zh-cn": "sn1ne.large.new (2 vCPU 4GB) - 计算型/入门",
          "en": "sn1ne.large.new (2 vCPU 4GB) - Compute/Entry"
        },
        "elasticsearch.sn1ne.xlarge.new": {
          "zh-cn": "sn1ne.xlarge.new (4 vCPU 8GB) - 计算型",
          "en": "sn1ne.xlarge.new (4 vCPU 8GB) - Compute"
        },
        "elasticsearch.sn1ne.2xlarge.new": {
          "zh-cn": "sn1ne.2xlarge.new (8 vCPU 16GB) - 计算型/高并发",
          "en": "sn1ne.2xlarge.new (8 vCPU 16GB) - Compute/High Concurrency"
        },
        "elasticsearch.sn1ne.4xlarge.new": {
          "zh-cn": "sn1ne.4xlarge.new (16 vCPU 32GB) - 计算型/高性能",
          "en": "sn1ne.4xlarge.new (16 vCPU 32GB) - Compute/High Performance"
        },
        "elasticsearch.sn2ne.large.new": {
          "zh-cn": "sn2ne.large.new (2 vCPU 8GB) - 通用型/测试",
          "en": "sn2ne.large.new (2 vCPU 8GB) - General/Test"
        },
        "elasticsearch.sn2ne.xlarge.new": {
          "zh-cn": "sn2ne.xlarge.new (4 vCPU 16GB) - 通用型/向量搜索推荐",
          "en": "sn2ne.xlarge.new (4 vCPU 16GB) - General/Vector Search Rec."
        },
        "elasticsearch.sn2ne.2xlarge.new": {
          "zh-cn": "sn2ne.2xlarge.new (8 vCPU 32GB) - 通用型/生产主力",
          "en": "sn2ne.2xlarge.new (8 vCPU 32GB) - General/Production Standard"
        },
        "elasticsearch.sn2ne.4xlarge.new": {
          "zh-cn": "sn2ne.4xlarge.new (16 vCPU 64GB) - 通用型/大规模",
          "en": "sn2ne.4xlarge.new (16 vCPU 64GB) - General/Large Scale"
        },
        "elasticsearch.sn2ne.8xlarge.new": {
          "zh-cn": "sn2ne.8xlarge.new (32 vCPU 128GB) - 通用型/超大规模",
          "en": "sn2ne.8xlarge.new (32 vCPU 128GB) - General/Extra Large"
        },
        "elasticsearch.r7a.2xlarge.new": {
          "zh-cn": "r7a.2xlarge.new (8 vCPU 64GB) - 内存型/海量数据",
          "en": "r7a.2xlarge.new (8 vCPU 64GB) - Memory Optimized/Large Data"
        },
        "elasticsearch.r7a.4xlarge.new": {
          "zh-cn": "r7a.4xlarge.new (16 vCPU 128GB) - 内存型/高性能聚合",
          "en": "r7a.4xlarge.new (16 vCPU 128GB) - Memory Optimized/High Aggregation"
        },
        "elasticsearch.r7a.8xlarge.new": {
          "zh-cn": "r7a.8xlarge.new (32 vCPU 256GB) - 内存型/极大规模",
          "en": "r7a.8xlarge.new (32 vCPU 256GB) - Memory Optimized/Extreme Scale"
        }
      },
      "Visible": {
        "Condition": {
          "Fn::Equals": [
            "cloud",
            "es_deployment_mode"
          ]
        }
      }
    },
    "Description": {
      "en": "Select the node specification. 'sn2ne' (1:4) is recommended for general vector search; 'sn1ne' (1:2) for high write/compute; 'r7a' (1:8) for large memory requirements.",
      "zh-cn": "选择节点规格。'sn2ne' (1:4) 系列推荐用于通用向量搜索；'sn1ne' (1:2) 用于高写入计算场景；'r7a' (1:8) 用于大内存需求场景。"
    },
    "Label": {
      "en": "Elasticsearch Node Spec",
      "zh-cn": "Elasticsearch 节点规格"
    }
  }
  EOT
}

variable "es_disk_size" {
  type        = number
  default     = 1000 # Match existing instance data node: 1000GB
  description = <<EOT
  {
    "MinValue": 20,
    "MaxValue": 12288,
    "AssociationPropertyMetadata": {
      "Visible": {
        "Condition": {
          "Fn::Equals": ["cloud", "es_deployment_mode"]
        }
      }
    },
    "Description": {
      "en": "Elasticsearch disk size per node in GB (cloud mode). ESSD supports up to 12TB for PL2/PL3",
      "zh-cn": "Elasticsearch每节点磁盘大小（GB）（云模式）。ESSD在PL2/PL3下支持最大12TB"
    },
    "Label": {
      "en": "Elasticsearch Disk Size (GB)",
      "zh-cn": "Elasticsearch磁盘大小（GB）"
    }
  }
  EOT
}

variable "es_disk_type" {
  type        = string
  default     = "cloud_essd" # ESSD required for ES 8.x versions
  description = <<EOT
  {
    "AllowedValues": ["cloud_ssd", "cloud_essd"],
    "AssociationPropertyMetadata": {
      "ValueLabelMapping": {
        "cloud_ssd": {
          "zh-cn": "SSD云盘",
          "en": "SSD Cloud Disk"
        },
        "cloud_essd": {
          "zh-cn": "ESSD云盘（推荐）",
          "en": "ESSD Cloud Disk (Recommended)"
        }
      },
      "Visible": {
        "Condition": {
          "Fn::Equals": ["cloud", "es_deployment_mode"]
        }
      }
    },
    "Description": {
      "en": "Elasticsearch disk type: cloud_ssd or cloud_essd. ESSD recommended for vector search",
      "zh-cn": "Elasticsearch磁盘类型：cloud_ssd 或 cloud_essd。向量搜索推荐使用ESSD"
    },
    "Label": {
      "en": "Elasticsearch Disk Type",
      "zh-cn": "Elasticsearch磁盘类型"
    }
  }
  EOT
}

variable "es_disk_performance_level" {
  type        = string
  default     = "PL1" # Changed from PL2 to PL1 for better compatibility
  description = <<EOT
  {
    "AllowedValues": ["PL1", "PL2", "PL3", "PL4"],
    "AssociationPropertyMetadata": {
      "ValueLabelMapping": {
        "PL1": {
          "zh-cn": "PL1 (标准)",
          "en": "PL1 (Standard)"
        },
        "PL2": {
          "zh-cn": "PL2 (推荐)",
          "en": "PL2 (Recommended)"
        },
        "PL3": {
          "zh-cn": "PL3 (高性能)",
          "en": "PL3 (High Performance)"
        },
        "PL4": {
          "zh-cn": "PL4 (超高性能，需1261GB+)",
          "en": "PL4 (Ultra High Performance, 1261GB+)"
        }
      },
      "Visible": {
        "Condition": {
          "Fn::And": [
            {"Fn::Equals": ["cloud", "es_deployment_mode"]},
            {"Fn::Equals": ["cloud_essd", "es_disk_type"]}
          ]
        }
      }
    },
    "Description": {
      "en": "ESSD performance level. PL2 recommended for vector search. PL3/PL4 require minimum disk sizes.",
      "zh-cn": "ESSD性能级别。向量搜索推荐PL2。PL3/PL4需要最小磁盘容量。"
    },
    "Label": {
      "en": "ESSD Performance Level",
      "zh-cn": "ESSD性能级别"
    }
  }
  EOT
}

variable "es_password" {
  type        = string
  sensitive   = true
  default     = ""
  description = <<EOT
  {
    "AssociationProperty": "ALIYUN::Elasticsearch::Instance::Password",
    "AssociationPropertyMetadata": {
      "Visible": {
        "Condition": {
          "Fn::Equals": ["cloud", "es_deployment_mode"]
        }
      },
      "Placeholder": {
        "zh-cn": "长度为8~32个字符，必须同时包含大写英文字母、小写英文字母、数字和特殊字符中的三项",
        "en": "8~32 characters, must contain three of: uppercase letters, lowercase letters, numbers, special characters"
      }
    },
    "ConstraintDescription": {
      "zh-cn": "长度为8~32个字符，必须同时包含大写英文字母、小写英文字母、数字和特殊字符中的三项。支持的特殊字符为：!@#$%&*()_+-=",
      "en": "8~32 characters, must contain three of: uppercase letters, lowercase letters, numbers, special characters (!@#$%&*()_+-=)"
    },
    "Description": {
      "en": "Elasticsearch password. Leave empty to auto-generate a secure password",
      "zh-cn": "Elasticsearch密码。留空则自动生成安全密码"
    },
    "Label": {
      "en": "Elasticsearch Password",
      "zh-cn": "Elasticsearch密码"
    },
    "NoEcho": "true"
  }
  EOT
}

variable "es_k8s_node_count" {
  type        = number
  default     = 3
  description = <<EOT
  {
    "MinValue": 1,
    "MaxValue": 10,
    "AssociationPropertyMetadata": {
      "Visible": {
        "Condition": {
          "Fn::Equals": ["k8s", "es_deployment_mode"]
        }
      }
    },
    "Description": {
      "en": "Number of Elasticsearch nodes (k8s mode)",
      "zh-cn": "Elasticsearch节点数量（K8s模式）"
    },
    "Label": {
      "en": "Elasticsearch K8s Node Count",
      "zh-cn": "Elasticsearch K8s节点数量"
    }
  }
  EOT
}

variable "es_k8s_storage" {
  type        = number
  default     = 500
  description = <<EOT
  {
    "MinValue": 20,
    "MaxValue": 500,
    "AssociationPropertyMetadata": {
      "Visible": {
        "Condition": {
          "Fn::Equals": ["k8s", "es_deployment_mode"]
        }
      }
    },
    "Description": {
      "en": "Elasticsearch storage size per node in GB (k8s mode). Aliyun PVC shall be no less than 20Gi",
      "zh-cn": "Elasticsearch每节点存储大小（GB）（K8s模式）。阿里云PVC最小为20Gi"
    },
    "Label": {
      "en": "Elasticsearch K8s Storage (GB)",
      "zh-cn": "Elasticsearch K8s存储（GB）"
    }
  }
  EOT
}

variable "es_cpu_request" {
  type        = string
  default     = "4"
  description = <<EOT
  {
    "AssociationPropertyMetadata": {
      "Visible": {
        "Condition": {
          "Fn::Equals": ["k8s", "es_deployment_mode"]
        }
      }
    },
    "Description": {
      "en": "Elasticsearch CPU request (k8s mode)",
      "zh-cn": "Elasticsearch CPU请求（K8s模式）"
    },
    "Label": {
      "en": "Elasticsearch CPU Request",
      "zh-cn": "Elasticsearch CPU请求"
    }
  }
  EOT
}

variable "es_cpu_limit" {
  type        = string
  default     = "8"
  description = <<EOT
  {
    "AssociationPropertyMetadata": {
      "Visible": {
        "Condition": {
          "Fn::Equals": ["k8s", "es_deployment_mode"]
        }
      }
    },
    "Description": {
      "en": "Elasticsearch CPU limit (k8s mode)",
      "zh-cn": "Elasticsearch CPU限制（K8s模式）"
    },
    "Label": {
      "en": "Elasticsearch CPU Limit",
      "zh-cn": "Elasticsearch CPU限制"
    }
  }
  EOT
}

variable "es_memory_request" {
  type        = string
  default     = "16Gi"
  description = <<EOT
  {
    "AssociationPropertyMetadata": {
      "Visible": {
        "Condition": {
          "Fn::Equals": ["k8s", "es_deployment_mode"]
        }
      }
    },
    "Description": {
      "en": "Elasticsearch memory request (k8s mode)",
      "zh-cn": "Elasticsearch内存请求（K8s模式）"
    },
    "Label": {
      "en": "Elasticsearch Memory Request",
      "zh-cn": "Elasticsearch内存请求"
    }
  }
  EOT
}

variable "es_memory_limit" {
  type        = string
  default     = "32Gi"
  description = <<EOT
  {
    "AssociationPropertyMetadata": {
      "Visible": {
        "Condition": {
          "Fn::Equals": ["k8s", "es_deployment_mode"]
        }
      }
    },
    "Description": {
      "en": "Elasticsearch memory limit (k8s mode)",
      "zh-cn": "Elasticsearch内存限制（K8s模式）"
    },
    "Label": {
      "en": "Elasticsearch Memory Limit",
      "zh-cn": "Elasticsearch内存限制"
    }
  }
  EOT
}

variable "es_heap_size" {
  type        = string
  default     = "16g"
  description = <<EOT
  {
    "AssociationPropertyMetadata": {
      "Visible": {
        "Condition": {
          "Fn::Equals": ["k8s", "es_deployment_mode"]
        }
      }
    },
    "Description": {
      "en": "Elasticsearch JVM heap size (~50% of memory limit)",
      "zh-cn": "Elasticsearch JVM堆大小（内存限制的约50%）"
    },
    "Label": {
      "en": "Elasticsearch Heap Size",
      "zh-cn": "Elasticsearch堆大小"
    }
  }
  EOT
}

# =============================================================================
# TEI (Text Embeddings) Configuration
# =============================================================================

variable "tei_model" {
  type        = string
  default     = "BAAI/bge-small-en-v1.5"
  description = <<EOT
  {
    "AllowedValues": ["BAAI/bge-small-en-v1.5", "BAAI/bge-base-en-v1.5", "BAAI/bge-large-en-v1.5"],
    "Description": {
      "en": "TEI model to use for text embeddings",
      "zh-cn": "用于文本嵌入的TEI模型"
    },
    "Label": {
      "en": "TEI Model",
      "zh-cn": "TEI模型"
    }
  }
  EOT
}

variable "tei_replicas" {
  type        = number
  default     = 0
  description = <<EOT
  {
    "MinValue": 0,
    "MaxValue": 10,
    "Description": {
      "en": "Number of TEI replicas for text embeddings (set to 0 to disable)",
      "zh-cn": "用于文本嵌入的TEI副本数（设为0以禁用）"
    },
    "Label": {
      "en": "TEI Replicas",
      "zh-cn": "TEI副本数"
    }
  }
  EOT
}

variable "tei_cpu_request" {
  type        = string
  default     = "4"
  description = <<EOT
  {
    "Description": {
      "en": "TEI CPU request (e.g., 2, 4, 8)",
      "zh-cn": "TEI CPU请求（例如：2, 4, 8）"
    },
    "Label": {
      "en": "TEI CPU Request",
      "zh-cn": "TEI CPU请求"
    }
  }
  EOT
}

variable "tei_cpu_limit" {
  type        = string
  default     = "8"
  description = <<EOT
  {
    "Description": {
      "en": "TEI CPU limit (e.g., 2, 4, 8)",
      "zh-cn": "TEI CPU限制（例如：2, 4, 8）"
    },
    "Label": {
      "en": "TEI CPU Limit",
      "zh-cn": "TEI CPU限制"
    }
  }
  EOT
}

variable "tei_memory_request" {
  type        = string
  default     = "8Gi"
  description = <<EOT
  {
    "Description": {
      "en": "TEI memory request (e.g., 4Gi, 8Gi, 16Gi)",
      "zh-cn": "TEI内存请求（例如：4Gi, 8Gi, 16Gi）"
    },
    "Label": {
      "en": "TEI Memory Request",
      "zh-cn": "TEI内存请求"
    }
  }
  EOT
}

variable "tei_memory_limit" {
  type        = string
  default     = "16Gi"
  description = <<EOT
  {
    "Description": {
      "en": "TEI memory limit (e.g., 4Gi, 8Gi, 16Gi)",
      "zh-cn": "TEI内存限制（例如：4Gi, 8Gi, 16Gi）"
    },
    "Label": {
      "en": "TEI Memory Limit",
      "zh-cn": "TEI内存限制"
    }
  }
  EOT
}

# =============================================================================
# Redis Configuration
# =============================================================================


variable "redis_cpu_request" {
  type        = string
  default     = "2"
  description = <<EOT
  {
    "Description": {
      "en": "Redis CPU request (e.g., 2, 4, 8)",
      "zh-cn": "Redis CPU请求（例如：2, 4, 8）"
    },
    "Label": {
      "en": "Redis CPU Request",
      "zh-cn": "Redis CPU请求"
    }
  }
  EOT
}

variable "redis_cpu_limit" {
  type        = string
  default     = "4"
  description = <<EOT
  {
    "Description": {
      "en": "Redis CPU limit (e.g., 2, 4, 8)",
      "zh-cn": "Redis CPU限制（例如：2, 4, 8）"
    },
    "Label": {
      "en": "Redis CPU Limit",
      "zh-cn": "Redis CPU限制"
    }
  }
  EOT
}

variable "redis_memory_request" {
  type        = string
  default     = "4Gi"
  description = <<EOT
  {
    "Description": {
      "en": "Redis memory request (e.g., 2Gi, 4Gi, 8Gi)",
      "zh-cn": "Redis内存请求（例如：2Gi, 4Gi, 8Gi）"
    },
    "Label": {
      "en": "Redis Memory Request",
      "zh-cn": "Redis内存请求"
    }
  }
  EOT
}

variable "redis_memory_limit" {
  type        = string
  default     = "8Gi"
  description = <<EOT
  {
    "Description": {
      "en": "Redis memory limit (e.g., 4Gi, 8Gi, 16Gi)",
      "zh-cn": "Redis内存限制（例如：4Gi, 8Gi, 16Gi）"
    },
    "Label": {
      "en": "Redis Memory Limit",
      "zh-cn": "Redis内存限制"
    }
  }
  EOT
}

# =============================================================================
# RabbitMQ Configuration
# =============================================================================

variable "rabbitmq_storage" {
  type        = number
  default     = 20
  description = <<EOT
  {
    "MinValue": 20,
    "MaxValue": 100,
    "Description": {
      "en": "RabbitMQ storage size in GB. Aliyun PVC shall be no less than 20Gi",
      "zh-cn": "RabbitMQ存储大小（GB）。阿里云PVC最小为20Gi"
    },
    "Label": {
      "en": "RabbitMQ Storage (GB)",
      "zh-cn": "RabbitMQ存储（GB）"
    }
  }
  EOT
}

variable "rabbitmq_cpu_request" {
  type        = string
  default     = "1"
  description = <<EOT
  {
    "Description": {
      "en": "RabbitMQ CPU request",
      "zh-cn": "RabbitMQ CPU请求"
    },
    "Label": {
      "en": "RabbitMQ CPU Request",
      "zh-cn": "RabbitMQ CPU请求"
    }
  }
  EOT
}

variable "rabbitmq_cpu_limit" {
  type        = string
  default     = "2"
  description = <<EOT
  {
    "Description": {
      "en": "RabbitMQ CPU limit",
      "zh-cn": "RabbitMQ CPU限制"
    },
    "Label": {
      "en": "RabbitMQ CPU Limit",
      "zh-cn": "RabbitMQ CPU限制"
    }
  }
  EOT
}

variable "rabbitmq_memory_request" {
  type        = string
  default     = "2Gi"
  description = <<EOT
  {
    "Description": {
      "en": "RabbitMQ memory request",
      "zh-cn": "RabbitMQ内存请求"
    },
    "Label": {
      "en": "RabbitMQ Memory Request",
      "zh-cn": "RabbitMQ内存请求"
    }
  }
  EOT
}

variable "rabbitmq_memory_limit" {
  type        = string
  default     = "4Gi"
  description = <<EOT
  {
    "Description": {
      "en": "RabbitMQ memory limit",
      "zh-cn": "RabbitMQ内存限制"
    },
    "Label": {
      "en": "RabbitMQ Memory Limit",
      "zh-cn": "RabbitMQ内存限制"
    }
  }
  EOT
}


# =============================================================================
# Container Registry Configuration
# =============================================================================

variable "private_registry" {
  type        = string
  default     = "infiniflow-registry.cn-shanghai.cr.aliyuncs.com/infiniflow-ai"
  description = <<EOT
  {
    "Description": {
      "en": "Private container registry URL for RAGFlow and DeepDoc images (e.g., 'gcr.io/ragflow-462809' or 'infiniflow-registry.cn-shanghai.cr.aliyuncs.com/infiniflow-ai')",
      "zh-cn": "RAGFlow和DeepDoc镜像的私有仓库URL"
    },
    "Label": {
      "en": "Private Registry",
      "zh-cn": "私有镜像仓库"
    }
  }
  EOT
}

variable "public_registry" {
  type        = string
  default     = "infiniflow-registry.cn-shanghai.cr.aliyuncs.com/infiniflow"
  description = <<EOT
  {
    "Description": {
      "en": "Public container registry URL for third-party images (MySQL, Redis, TEI, RabbitMQ, etc.). If empty, uses default registries (docker.io, quay.io, etc.)",
      "zh-cn": "第三方镜像（MySQL、Redis、TEI、RabbitMQ等）的公共仓库URL。为空时使用默认仓库"
    },
    "Label": {
      "en": "Public Registry",
      "zh-cn": "公共镜像仓库"
    }
  }
  EOT
}

# =============================================================================
# RAGFlow Application Configuration
# =============================================================================

variable "ragflow_image" {
  type        = string
  default     = "ragflow:latest"
  description = <<EOT
  {
    "Description": {
      "en": "RAGFlow container image (including tag)",
      "zh-cn": "RAGFlow容器镜像（含标签）"
    },
    "Label": {
      "en": "RAGFlow Image",
      "zh-cn": "RAGFlow镜像"
    }
  }
  EOT
}

variable "es_image" {
  type        = string
  default     = "elasticsearch:9.3.1"
  description = <<EOT
  {
    "Description": {
      "en": "Elasticsearch container image (including tag)",
      "zh-cn": "Elasticsearch容器镜像（含标签）"
    },
    "Label": {
      "en": "Elasticsearch Image",
      "zh-cn": "Elasticsearch镜像"
    }
  }
  EOT
}

variable "ragflow_replicas" {
  type        = number
  default     = 3
  description = <<EOT
  {
    "MinValue": 1,
    "MaxValue": 10,
    "Description": {
      "en": "Number of RAGFlow replicas for high availability",
      "zh-cn": "RAGFlow副本数用于高可用"
    },
    "Label": {
      "en": "RAGFlow Replicas",
      "zh-cn": "RAGFlow副本数"
    }
  }
  EOT
}

variable "ragflow_cpu_request" {
  type        = string
  default     = "2"
  description = <<EOT
  {
    "Description": {
      "en": "RAGFlow CPU request (e.g., 2, 4, 8)",
      "zh-cn": "RAGFlow CPU请求（例如：2, 4, 8）"
    },
    "Label": {
      "en": "RAGFlow CPU Request",
      "zh-cn": "RAGFlow CPU请求"
    }
  }
  EOT
}

variable "ragflow_cpu_limit" {
  type        = string
  default     = "4"
  description = <<EOT
  {
    "Description": {
      "en": "RAGFlow CPU limit (e.g., 2, 4, 8)",
      "zh-cn": "RAGFlow CPU限制（例如：2, 4, 8）"
    },
    "Label": {
      "en": "RAGFlow CPU Limit",
      "zh-cn": "RAGFlow CPU限制"
    }
  }
  EOT
}

variable "ragflow_memory_request" {
  type        = string
  default     = "8Gi"
  description = <<EOT
  {
    "Description": {
      "en": "RAGFlow memory request (e.g., 4Gi, 8Gi, 16Gi)",
      "zh-cn": "RAGFlow内存请求（例如：4Gi, 8Gi, 16Gi）"
    },
    "Label": {
      "en": "RAGFlow Memory Request",
      "zh-cn": "RAGFlow内存请求"
    }
  }
  EOT
}

variable "ragflow_memory_limit" {
  type        = string
  default     = "16Gi"
  description = <<EOT
  {
    "Description": {
      "en": "RAGFlow memory limit (e.g., 4Gi, 8Gi, 16Gi)",
      "zh-cn": "RAGFlow内存限制（例如：4Gi, 8Gi, 16Gi）"
    },
    "Label": {
      "en": "RAGFlow Memory Limit",
      "zh-cn": "RAGFlow内存限制"
    }
  }
  EOT
}

# =============================================================================
# Parser Configuration
# =============================================================================

variable "parser_replicas" {
  type        = number
  default     = 3
  description = <<EOT
  {
    "MinValue": 1,
    "MaxValue": 10,
    "Description": {
      "en": "Number of Parser replicas for document processing",
      "zh-cn": "用于文档处理的Parser副本数"
    },
    "Label": {
      "en": "Parser Replicas",
      "zh-cn": "Parser副本数"
    }
  }
  EOT
}

variable "parser_cpu_request" {
  type        = string
  default     = "2"
  description = <<EOT
  {
    "Description": {
      "en": "Parser CPU request (e.g., 2, 4, 8)",
      "zh-cn": "Parser CPU请求（例如：2, 4, 8）"
    },
    "Label": {
      "en": "Parser CPU Request",
      "zh-cn": "Parser CPU请求"
    }
  }
  EOT
}

variable "parser_cpu_limit" {
  type        = string
  default     = "4"
  description = <<EOT
  {
    "Description": {
      "en": "Parser CPU limit (e.g., 2, 4, 8)",
      "zh-cn": "Parser CPU限制（例如：2, 4, 8）"
    },
    "Label": {
      "en": "Parser CPU Limit",
      "zh-cn": "Parser CPU限制"
    }
  }
  EOT
}

variable "parser_memory_request" {
  type        = string
  default     = "8Gi"
  description = <<EOT
  {
    "Description": {
      "en": "Parser memory request (e.g., 8Gi, 16Gi)",
      "zh-cn": "Parser内存请求（例如：8Gi, 16Gi）"
    },
    "Label": {
      "en": "Parser Memory Request",
      "zh-cn": "Parser内存请求"
    }
  }
  EOT
}

variable "parser_memory_limit" {
  type        = string
  default     = "16Gi"
  description = <<EOT
  {
    "Description": {
      "en": "Parser memory limit (e.g., 8Gi, 16Gi)",
      "zh-cn": "Parser内存限制（例如：8Gi, 16Gi）"
    },
    "Label": {
      "en": "Parser Memory Limit",
      "zh-cn": "Parser内存限制"
    }
  }
  EOT
}

# =============================================================================
# DeepDoc Configuration
# =============================================================================

variable "deepdoc_image" {
  type        = string
  default     = "deepdoc_cpu:latest"
  description = <<EOT
  {
    "Description": {
      "en": "DeepDoc container image (including tag)",
      "zh-cn": "DeepDoc容器镜像（含标签）"
    },
    "Label": {
      "en": "DeepDoc Image",
      "zh-cn": "DeepDoc镜像"
    }
  }
  EOT
}

variable "deepdoc_replicas" {
  type        = number
  default     = 3
  description = <<EOT
  {
    "MinValue": 1,
    "MaxValue": 10,
    "Description": {
      "en": "Number of DeepDoc replicas for OCR and document analysis",
      "zh-cn": "用于OCR和文档分析的DeepDoc副本数"
    },
    "Label": {
      "en": "DeepDoc Replicas",
      "zh-cn": "DeepDoc副本数"
    }
  }
  EOT
}

variable "deepdoc_cpu_request" {
  type        = string
  default     = "8"
  description = <<EOT
  {
    "Description": {
      "en": "DeepDoc CPU request (e.g., 4, 8, 16)",
      "zh-cn": "DeepDoc CPU请求（例如：4, 8, 16）"
    },
    "Label": {
      "en": "DeepDoc CPU Request",
      "zh-cn": "DeepDoc CPU请求"
    }
  }
  EOT
}

variable "deepdoc_cpu_limit" {
  type        = string
  default     = "16"
  description = <<EOT
  {
    "Description": {
      "en": "DeepDoc CPU limit (e.g., 4, 8, 16)",
      "zh-cn": "DeepDoc CPU限制（例如：4, 8, 16）"
    },
    "Label": {
      "en": "DeepDoc CPU Limit",
      "zh-cn": "DeepDoc CPU限制"
    }
  }
  EOT
}

variable "deepdoc_memory_request" {
  type        = string
  default     = "32Gi"
  description = <<EOT
  {
    "Description": {
      "en": "DeepDoc memory request (e.g., 16Gi, 32Gi, 64Gi)",
      "zh-cn": "DeepDoc内存请求（例如：16Gi, 32Gi, 64Gi）"
    },
    "Label": {
      "en": "DeepDoc Memory Request",
      "zh-cn": "DeepDoc内存请求"
    }
  }
  EOT
}

variable "deepdoc_memory_limit" {
  type        = string
  default     = "64Gi"
  description = <<EOT
  {
    "Description": {
      "en": "DeepDoc memory limit (e.g., 16Gi, 32Gi, 64Gi)",
      "zh-cn": "DeepDoc内存限制（例如：16Gi, 32Gi, 64Gi）"
    },
    "Label": {
      "en": "DeepDoc Memory Limit",
      "zh-cn": "DeepDoc内存限制"
    }
  }
  EOT
}

variable "deepdoc_use_gpu" {
  type        = bool
  default     = false
  description = <<EOT
  {
    "Description": {
      "en": "Enable GPU for DeepDoc (requires GPU nodes and NVIDIA runtime)",
      "zh-cn": "为DeepDoc启用GPU（需要GPU节点和NVIDIA运行时）"
    },
    "Label": {
      "en": "Enable GPU for DeepDoc",
      "zh-cn": "为DeepDoc启用GPU"
    }
  }
  EOT
}

# =============================================================================
# Gateway Configuration
# =============================================================================

variable "gateway_host" {
  type        = string
  default     = "ragflow.aliyun.com"
  description = <<EOT
  {
    "AssociationProperty": "ALIYUN::ECS::Instance::HostName",
    "Description": {
      "en": "Gateway hostname for accessing RAGFlow (should resolve to gateway IP)",
      "zh-cn": "访问RAGFlow的网关主机名（应解析到网关IP）"
    },
    "Label": {
      "en": "Gateway Hostname",
      "zh-cn": "网关主机名"
    }
  }
  EOT
}

variable "enable_tls" {
  type        = bool
  default     = false
  description = <<EOT
  {
    "Description": {
      "en": "Enable TLS/HTTPS for gateway (requires certificate configuration)",
      "zh-cn": "为网关启用TLS/HTTPS（需要证书配置）"
    },
    "Label": {
      "en": "Enable TLS",
      "zh-cn": "启用TLS"
    }
  }
  EOT
}

# =============================================================================
# Payment Configuration
# =============================================================================

variable "payment_type" {
  type        = string
  default     = "PostPaid"
  description = <<EOT
  {
    "AllowedValues": ["PostPaid", "PrePaid"],
    "AssociationProperty": "ChargeType",
    "AssociationPropertyMetadata": {
      "LocaleKey": "InstanceChargeType",
      "ValueLabelMapping": {
        "PostPaid": {
          "zh-cn": "按量付费",
          "en": "Pay-As-You-Go"
        },
        "PrePaid": {
          "zh-cn": "包年包月",
          "en": "Subscription"
        }
      }
    },
    "Description": {
      "en": "Payment type: PostPaid (pay-as-you-go) or PrePaid (subscription)",
      "zh-cn": "付费类型：PostPaid（按量付费）或 PrePaid（包年包月）"
    },
    "Label": {
      "en": "Payment Type",
      "zh-cn": "付费类型"
    }
  }
  EOT

  validation {
    condition     = contains(["PostPaid", "PrePaid"], var.payment_type)
    error_message = "payment_type must be 'PostPaid' or 'PrePaid'."
  }
}

variable "payment_period" {
  type        = number
  default     = 1
  description = <<EOT
  {
    "MinValue": 1,
    "MaxValue": 9,
    "AssociationProperty": "PayPeriod",
    "AssociationPropertyMetadata": {
      "Visible": {
        "Condition": {
          "Fn::Not": {
            "Fn::Equals": ["PostPaid", "payment_type"]
          }
        }
      }
    },
    "Description": {
      "en": "Payment period duration for PrePaid (1-9)",
      "zh-cn": "预付费付费周期时长（1-9）"
    },
    "Label": {
      "en": "Payment Period",
      "zh-cn": "付费周期"
    }
  }
  EOT
}

variable "payment_period_unit" {
  type        = string
  default     = "Month"
  description = <<EOT
  {
    "AllowedValues": ["Month", "Year"],
    "AssociationPropertyMetadata": {
      "ValueLabelMapping": {
        "Month": {
          "zh-cn": "月",
          "en": "Month"
        },
        "Year": {
          "zh-cn": "年",
          "en": "Year"
        }
      },
      "Visible": {
        "Condition": {
          "Fn::Not": {
            "Fn::Equals": ["PostPaid", "payment_type"]
          }
        }
      }
    },
    "Description": {
      "en": "Payment period unit: Month or Year",
      "zh-cn": "付费周期单位：月或年"
    },
    "Label": {
      "en": "Payment Period Unit",
      "zh-cn": "付费周期单位"
    }
  }
  EOT
}

# =============================================================================
# Tags
# =============================================================================

variable "tags" {
  type = map(string)
  default = {
    Project   = "RAGFlow"
    ManagedBy = "Terraform"
  }
  description = <<EOT
  {
    "Description": {
      "en": "Common tags for all resources (key-value pairs)",
      "zh-cn": "所有资源的通用标签（键值对）"
    },
    "Label": {
      "en": "Resource Tags",
      "zh-cn": "资源标签"
    }
  }
  EOT
}

# =============================================================================
# VPC and Network
# =============================================================================
# See Aliyun ROS documentation: https://help.aliyun.com/zh/ros/developer-reference/aliyun-ecs-vpc

