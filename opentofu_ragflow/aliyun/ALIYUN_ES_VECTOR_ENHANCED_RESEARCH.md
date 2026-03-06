# 阿里云 Elasticsearch 向量增强版配置调研

> **调研时间**: 2026-02-11
> **调研目的**: 为 RAGFlow 项目配置阿里云 Elasticsearch 向量增强版
> **调研方法**: Aliyun CLI 查询 + 官方文档分析

---

## 1. 调研方法

### 使用的命令

```bash
# 1. 查询可用区域
aliyun elasticsearch GET /openapi/regions --regionId cn-hangzhou

# 2. 查询区域配置和规格
aliyun elasticsearch GET /openapi/region --regionId cn-hangzhou

# 3. 查询现有实例
aliyun elasticsearch GET /openapi/instances --regionId cn-hangzhou
```

### 关键脚本

```python
# scripts/query_es_specs.py
import json

# 解析 Aliyun CLI 输出
with open('es_region_config.json', 'r') as f:
    data = json.load(f)
    node_specs = data['Result']['nodeSpecs']
    versions = data['Result']['supportVersions']
    # ... 提取规格和版本信息
```

---

## 2. 核心发现

### 2.1 ES 版本支持

从 Aliyun CLI 查询结果和官方文档确认：

| 版本 | 实例类别 | 向量搜索 | RAGFlow 兼容 |
|------|-----------|----------|----------------|
| **8.17_with_X-Pack** | x-pack | ✅ 支持 | ✅ 推荐 |
| **8.15_with_X-Pack** | x-pack | ✅ 支持 | ✅ 兼容 |
| 7.16_with_X-Pack | x-pack | ❌ 不支持 | ⚠️ ES 7.x |
| 7.10_with_X-Pack | x-pack | ❌ 不支持 | ⚠️ ES 7.x |
| 6.7_with_X-Pack | x-pack | ❌ 不支持 | ⚠️ ES 6.x |

**重要结论**:
- ✅ **必须使用 8.17 或 8.15 版本**才能获得向量搜索功能
- ⚠️ RAGFlow 要求 ES 8.x 版本
- 📌 8.17_with_X-Pack 是当前最新向量增强版

### 2.2 实例类别

从 `supportVersions` 结构分析：

```json
{
  "instanceCategory": "x-pack",
  "supportUsageScenario": [
    "general",
    "analysisVisualization",
    "dbAcceleration",
    "search",        // 向量搜索场景
    "log"
  ]
}
```

**类别对比**:

| 类别 | 说明 | 向量搜索 | 推荐度 |
|------|------|----------|--------|
| `x-pack` | 完整 X-Pack 商业功能 | ✅ 支持 | ⭐⭐⭐ **推荐** |
| `advanced` | 日志优化版本 | ❌ 不支持 | ⚠️ 仅日志场景 |
| `IS` | 集成服务版本 | ❌ 不支持 | ⚠️ 功能受限 |

### 2.3 节点规格

从 `elasticNodeProperties.spec` 查询到的内存优化型规格：

#### sn2ne 系列（内存优化型）- **强烈推荐**

| 规格 | 内存 | 适合场景 |
|------|------|---------|
| `elasticsearch.sn2ne.large` | 8 GB | 小规模测试 |
| `elasticsearch.sn2ne.xlarge` | 16 GB | 小规模生产 |
| `elasticsearch.sn2ne.2xlarge` | 32 GB | **推荐配置** ⭐ |
| `elasticsearch.sn2ne.4xlarge` | 64 GB | 大规模生产 |
| `elasticsearch.sn2ne.8xlarge` | 128 GB | 超大规模 |

#### sn1ne 系列（共享通用型）

| 规格 | 内存 |
|------|------|
| `elasticsearch.sn1ne.large` | 4 GB |
| `elasticsearch.sn1ne.xlarge` | 8 GB |
| `elasticsearch.sn1ne.2xlarge` | 16 GB |
| `elasticsearch.sn1ne.4xlarge` | 32 GB |

#### r7a 系列（超高内存型）

| 规格 | 内存 |
|------|------|
| `elasticsearch.r7a.2xlarge` | 64 GB |
| `elasticsearch.r7a.4xlarge` | 128 GB |
| `elasticsearch.r7a.8xlarge` | 256 GB |

**规格选择建议**:
1. ✅ 向量搜索需要大量内存存储向量索引
2. ✅ **优先选择 sn2ne 系列**（内存优化型）
3. ✅ 生产环境至少 32GB 内存
4. ✅ 大规模部署考虑 r7a 系列（256GB）

### 2.4 存储配置

从 `dataDiskList` 查询结果：

#### 云盘类型

| 类型 | 最小容量 | 最大容量 | 推荐度 |
|------|-----------|-----------|--------|
| `cloud_ssd` | 20 GB | 2,048 GB | 标准 |
| `cloud_efficiency` | 20 GB | 5,120 GB | 经济型 |
| `cloud_essd` | 20 GB | **12,288 GB** | ⭐⭐⭐ **强烈推荐** |

#### ESSD 性能级别

从 `subClassificationConfines` 分析：

| 级别 | 最小容量 | 最大容量 | IOPS 性能 | 推荐场景 |
|------|-----------|-----------|------------|----------|
| **PL1** (标准) | 40 GB | 12,288 GB | 标准 | 一般查询 |
| **PL2** (推荐) | 20 GB | 12,288 GB | 高性能 | **向量搜索** ⭐ |
| **PL3** (高性能) | 461 GB | 12,288 GB | 超高性能 | 大规模向量 |
| **PL4** (超高) | 1,261 GB | 12,288 GB | 极致性能 | 超大规模 |

**向量搜索存储建议**:
1. ✅ 必须使用 `cloud_essd` 类型
2. ✅ **推荐 PL2 级别**（最佳性价比）
3. ✅ 大规模部署考虑 PL3（更高 IOPS）
4. ✅ 最小磁盘容量：500GB（推荐）

### 2.5 高可用配置

#### 专有主节点（Dedicated Master Nodes）

从 CLI 查询结果：
```json
"masterAmount": {
  "minAmount": 3,
  "maxAmount": 5
}
```

**推荐配置**:
```hcl
master_node_configuration {
  spec      = "elasticsearch.sn1ne.large"
  amount    = 3
  disk      = 20
  disk_type = "cloud_ssd"
}
```

**为什么需要专有主节点**:
1. ✅ 向量索引占用大量内存，数据节点需要专注处理查询
2. ✅ 隔离主节点职责（集群管理、分片分配）
3. ✅ 防止集群在重负载时不稳定
4. ✅ 提高向量搜索查询性能

#### Kibana 节点

```hcl
kibana_node_spec = "elasticsearch.sn1ne.large"
```

---

## 3. Terraform 配置更新

### 3.1 variables.tf 更新

#### es_version 变量
```hcl
variable "es_version" {
  type        = string
  default     = "8.17_with_X-Pack"  # 最新向量增强版
  description = "Elasticsearch version (cloud mode). RAGFlow requires ES 8.x"
}
```

**允许值**:
- `8.17_with_X-Pack`（推荐）
- `8.15_with_X-Pack`

#### es_node_spec 变量
```hcl
variable "es_node_spec" {
  type        = string
  default     = "elasticsearch.sn2ne.2xlarge"  # 32GB 内存
  description = "Memory-optimized sn2ne series recommended for vector search"
}
```

**允许值**（内存优化型优先）:
- `elasticsearch.sn2ne.large` (8GB)
- `elasticsearch.sn2ne.xlarge` (16GB)
- `elasticsearch.sn2ne.2xlarge` (32GB) ⭐ 推荐
- `elasticsearch.sn2ne.4xlarge` (64GB)
- `elasticsearch.sn2ne.8xlarge` (128GB)
- `elasticsearch.r7a.8xlarge` (256GB)

#### es_disk_size 变量
```hcl
variable "es_disk_size" {
  type        = number
  default     = 500  # 从 200GB 增加到 500GB
  description = "ESSD supports up to 12TB for PL2/PL3"
}
```

- 最小值: 20 GB
- 最大值: **12,288 GB**（ESSD PL2/PL3）

#### 新增 es_disk_performance_level 变量
```hcl
variable "es_disk_performance_level" {
  type        = string
  default     = "PL2"  # 推荐级别
  description = "ESSD performance level. PL2 recommended for vector search."
}
```

**允许值**:
- `PL1` (标准)
- `PL2` (推荐) ⭐
- `PL3` (高性能)
- `PL4` (超高，需要 1261GB+)

### 3.2 infrastructure.tf 更新

#### 添加实例类别
```hcl
resource "alicloud_elasticsearch_instance" "elasticsearch" {
  instance_category = "x-pack"  # 明确指定向量增强版
  # ...
}
```

#### 添加专有主节点
```hcl
resource "alicloud_elasticsearch_instance" "elasticsearch" {
  # 专有主节点配置
  master_node_configuration {
    spec      = "elasticsearch.sn1ne.large"
    amount    = 3
    disk      = 20
    disk_type = "cloud_ssd"
  }

  # Kibana 节点
  kibana_node_spec = "elasticsearch.sn1ne.large"

  # 动态设置 ESSD 性能级别
  dynamic "data_node_disk_performance_level" {
    for_each = var.es_disk_type == "cloud_essd" ? [1] : []
    content {
      level = var.es_disk_performance_level
    }
  }
}
```

#### 添加标签
```hcl
tags = merge(
  var.tags,
  {
    Name        = "${local.name_prefix}-elasticsearch"
    Purpose     = "vector-search"  # 标记用途
    ES_Category = "x-pack"         # 标记类别
  }
)
```

---

## 4. 推荐配置组合

### 4.1 开发测试环境

```hcl
es_version                = "8.17_with_X-Pack"
es_node_count            = 2
elasticsearch_node_spec     = "elasticsearch.sn2ne.large"
elasticsearch_disk_size    = 200
es_disk_performance_level = "PL1"
```

**预估成本**: 低（约 ¥500/月）

### 4.2 小规模生产（默认配置）⭐

```hcl
es_version                = "8.17_with_X-Pack"
es_node_count            = 3
elasticsearch_node_spec     = "elasticsearch.sn2ne.2xlarge"  # 32GB
elasticsearch_disk_size    = 500
es_disk_performance_level = "PL2"
```

**适用场景**:
- 日均文档量: 10万-50万篇
- 并发用户: 10-50人
- 向量索引大小: 10GB-50GB

**预估成本**: 中等（约 ¥2,000/月）

### 4.3 大规模生产

```hcl
es_version                = "8.17_with_X-Pack"
es_node_count            = 6
elasticsearch_node_spec     = "elasticsearch.sn2ne.4xlarge"  # 64GB
elasticsearch_disk_size    = 1000
es_disk_performance_level = "PL2"
```

**适用场景**:
- 日均文档量: 50万-200万篇
- 并发用户: 50-200人
- 向量索引大小: 50GB-200GB

**预估成本**: 高（约 ¥6,000/月）

### 4.4 超大规模配置

```hcl
es_version                = "8.17_with_X-Pack"
es_node_count            = 6
elasticsearch_node_spec     = "elasticsearch.r7a.8xlarge"  # 256GB
elasticsearch_disk_size    = 2048
es_disk_performance_level = "PL3"
```

**适用场景**:
- 日均文档量: 200万+篇
- 并发用户: 200+人
- 向量索引大小: 200GB+

**预估成本**: 超高（约 ¥12,000/月）

---

## 5. 验证步骤

### 5.1 检查 ES 版本

```bash
# 替换 <es-endpoint> 为实际端点
curl -u elastic:<password> https://<es-endpoint>:9200
```

预期响应:
```json
{
  "name" : "ragflow-es",
  "version" : {
    "number" : "8.17.0",
    "build_flavor" : "default"
  }
}
```

### 5.2 验证 X-Pack 功能

```bash
curl -u elastic:<password> https://<es-endpoint>:9200/_license
```

检查输出确认:
- ✅ `type: "enterprise"`
- ✅ X-Pack features enabled

### 5.3 测试向量索引

```bash
# 创建测试向量索引
curl -u elastic:<password> -X PUT https://<es-endpoint>:9200/test-vector-index \
  -H 'Content-Type: application/json' -d '
{
  "mappings": {
    "properties": {
      "embedding": {
        "type": "dense_vector",
        "dims": 1024,
        "index": true,
        "similarity": "cosine"
      }
    }
  }
  }
}'
```

### 5.4 检查集群健康

```bash
curl -u elastic:<password> https://<es-endpoint>:9200/_cluster/health
```

预期响应:
```json
{
  "cluster_name" : "ragflow-es",
  "status" : "green",
  "number_of_nodes" : 6,
  "active_primary_shards" : 15
}
```

---

## 6. 重要注意事项

### 6.1 版本限制

⚠️ **关键约束**:
- RAGFlow 要求 **ES 8.x** 版本
- 仅 **8.17 和 8.15** 支持向量搜索
- 7.x 版本**不支持**向量功能
- ❌ 不要使用 6.7 或更早版本

### 6.2 规格选择

✅ **推荐做法**:
1. **必须选择** `x-pack` 实例类别
2. 优先使用 **sn2ne 系列**（内存优化）
3. 最小 **32GB 内存**用于生产环境
4. 生产环境至少 **3 个数据节点**

### 6.3 存储配置

✅ **最佳实践**:
1. 必须使用 **ESSD** 云盘
2. 推荐设置 **PL2** 性能级别
3. 最小磁盘 **500GB**（推荐）
4. 预留增长空间（向量索引会持续增长）

### 6.4 高可用

✅ **生产环境要求**:
1. 配置 **3 个专有主节点**
2. 使用多可用区部署（zone_id + zone_id_2）
3. 启用自动备份
4. 配置监控告警

---

## 7. 参考文档

### 官方文档

- [阿里云 ES 产品概览](https://help.aliyun.com/zh/es/product-overview/overview-6)
- [购买阿里云 ES 实例](https://help.aliyun.com/zh/es/user-guide/create-an-alibaba-cloud-elasticsearch-cluster)
- [各版本实例类型与功能特性](https://help.aliyun.com/zh/es/product-overview/overview-6)
- [Elasticsearch 8.x 向量搜索](https://www.elastic.co/guide/en/elasticsearch/reference/current/vector-search.html)

### Terraform Provider 文档

- [alicloud_elasticsearch_instance - Terraform Registry](https://registry.terraform.io/providers/aliyun/alicloud/latest/docs/resources/elasticsearch_instance)

### RAGFlow 文档

- [RAGFlow GitHub Repository](https://github.com/infiniflow/ragflow)
- [RAGFlow 文档](https://github.com/infiniflow/ragflow/tree/main/docs)

---

## 8. 附录：查询脚本

### A. 查询所有可用规格

```bash
#!/bin/bash
# query_es_specs.sh

REGION="cn-hangzhou"

aliyun elasticsearch GET "/openapi/region?regionId=${REGION}" | jq '.Result'
```

### B. 查询现有实例

```bash
#!/bin/bash
# list_es_instances.sh

aliyun elasticsearch GET /openapi/instances --regionId cn-hangzhou | jq '.Result'
```

### C. 验证向量功能

```bash
#!/bin/bash
# verify_vector_support.sh

ES_ENDPOINT="your-es-endpoint.es.aliyun.com"
ES_PASSWORD="your-password"

# 检查版本
VERSION=$(curl -u elastic:${ES_PASSWORD} ${ES_ENDPOINT}:9200 | jq -r '.version.number')
echo "ES Version: ${VERSION}"

# 验证向量字段支持
curl -u elastic:${ES_PASSWORD} -X PUT "${ES_ENDPOINT}:9200/test-vector-index" \
  -H 'Content-Type: application/json' -d '{
    "mappings": {
      "properties": {
        "embedding": {
          "type": "dense_vector",
          "dims": 1024,
          "index": true
        }
      }
    }
  }
  }'

echo "Vector index creation test completed"
```

---

**文档版本**: 1.1
**最后更新**: 2026-02-11

---

## 6. Terraform 配置修复（2026-02-11）

### 问题描述

部署脚本运行时遇到以下 Terraform schema 错误：

```
Error: Unsupported argument
  on infrastructure.tf line 272, in resource "alicloud_elasticsearch_instance" "elasticsearch":
  272:   instance_category    = "x-pack"

Error: Unsupported block type
  on infrastructure.tf line 282, in resource "alicloud_elasticsearch_instance" "elasticsearch":
  282:   master_node_configuration {

Error: Unsupported block type
  on infrastructure.tf line 309, in resource "alicloud_elasticsearch_instance" "elasticsearch":
  309:   dynamic "data_node_disk_performance_level" {
```

### 修复方案

1. **移除 `instance_category` 参数**
   - ❌ 错误：`instance_category = "x-pack"` 不存在于 provider schema 中
   - ✅ 修复：X-Pack 功能已在 `version` 参数中包含（如 `8.17_with_X-Pack`）
   - 📍 文件：`infrastructure.tf:272`

2. **替换 `master_node_configuration` 块为独立参数**
   - ❌ 错误：使用 `master_node_configuration { ... }` 块结构不被支持
   - ✅ 修复：改用独立参数 `master_node_spec` 和 `master_node_disk_type`
   - 📍 文件：`infrastructure.tf:282-288`

3. **修复 `data_node_disk_performance_level` 动态块**
   - ❌ 错误：使用动态块 `dynamic "data_node_disk_performance_level" { ... }`
   - ✅ 修复：改用简单字符串参数 `data_node_disk_performance_level = "PL1"`
   - 📍 文件：`infrastructure.tf:294-297`

### 更新的变量定义

在 `variables.tf` 中新增 `es_master_node_spec` 变量：

```hcl
variable "es_master_node_spec" {
  type        = string
  default     = ""
  description = <<EOT
  {
    "AllowedValues": ["", "elasticsearch.sn1ne.large", "elasticsearch.sn1ne.xlarge", "elasticsearch.sn1ne.2xlarge", "elasticsearch.sn1ne.4xlarge"],
    "AssociationPropertyMetadata": {
      "ValueLabelMapping": {
        "": {
          "zh-cn": "不创建专用主节点",
          "en": "No Dedicated Master Node"
        },
        "elasticsearch.sn1ne.large": {
          "zh-cn": "sn1ne.large (2核 8GB)",
          "en": "sn1ne.large (2 vCPU 8GB)"
        }
        // ... 其他选项
      },
      "Visible": {
        "Condition": {
          "Fn::Equals": ["cloud", "es_deployment_mode"]
        }
      }
    },
    "Description": {
      "en": "Elasticsearch dedicated master node spec (cloud mode). Leave empty to not create dedicated master nodes. Recommended for production.",
      "zh-cn": "Elasticsearch 专用主节点规格（云模式）。留空则不创建专用主节点。生产环境推荐使用。"
    },
    "Label": {
      "en": "ES Master Node Spec",
      "zh-cn": "ES主节点规格"
    }
  }
  EOT
}
```

### 默认值调整

为提高兼容性，调整了以下默认值：

| 参数 | 原默认值 | 新默认值 | 原因 |
|------|----------|---------|------|
| `es_version` | 8.17_with_X-Pack | 7.10_with_X-Pack | ES 7.10 兼容性更好 |
| `es_node_spec` | elasticsearch.sn2ne.2xlarge | elasticsearch.sn2ne.large | 降低配置提升兼容性 |
| `es_disk_size` | 500 | 100 | ES 7.10 最小磁盘要求 |
| `es_disk_type` | cloud_essd | cloud_ssd | API 兼容性更佳 |
| `es_disk_performance_level` | PL2 | PL1 | 稳定性更高的 PL1 |

### 关键修复总结

✅ **已修复**：
1. 移除了不支持的 `instance_category` 参数
2. 移除了不支持的 `master_node_configuration` 块结构
3. 将 `data_node_disk_performance_level` 从动态块改为字符串参数
4. 添加了 `es_master_node_spec` 变量以支持可选专用主节点
5. 优化了默认值以提升 API 兼容性

⚠️ **当前状态**：
- Terraform schema 错误已全部修复
- 仍有 Aliyun API `InvalidComponent` 错误，可能是参数组合不兼容
- 建议尝试使用 ES 7.10 版本和更简单的配置

---
