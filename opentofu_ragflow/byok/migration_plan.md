# RAGFlow 迁移计划：Docker Compose 到 GCP GKE Autopilot

## 1. 迁移背景与问题概览

当前 RAGFlow demo 站点在四个节点（`ragflow01`, `ragflow02`, `ragflow03`, `ragflow04`）上通过 Docker Compose 部署，现需迁移至 GKE 集群 `autopilot-cluster-1`。

主要涉及以下有状态应用（StatefulWorkloads）的数据迁移：

| Stateful 应用 | 当前状态 | 迁移目标与挑战 |
| --- | --- | --- |
| **MySQL** | `ragflow01` 节点上的 `/var/lib/docker/volumes/docker_mysql_vol` | 需要以逻辑备份的方式导入到 GKE 集群的 MySQL Pod 中。 |
| **MinIO** | 4个节点的本地 volume，总计约 2TB | 需要全量导入到 GCS bucket `ragflow-77cbf49639d548639a329344f063ba299dc5388f`。需要保证 RAGFlow 更改对象存储配置后能够无缝访问历史数据。 |
| **Elasticsearch** | 4 个节点的 `~/es_data` 是 ES 8 格式的 | 已通过logstash方式（升级至 ES 9 格式）同步到新的4个结点(`rag01`, `rag02`, `rag03`, `rag04`)的`~/es_data`。需要导入到 GKE 集群中对应的 4 个 ES Pod。 |

---

## 2. 环境变量准备

在执行迁移命令前，需要设置源集群和目标集群的密码环境变量：

```bash
### 源集群密码（ragflow01 节点上的容器）
export SOURCE_MYSQL_ROOT_PASSWORD="$(ssh -i ~/.ssh/infinity.pem ubuntu@35.237.247.13 'docker exec ragflow-mysql printenv MYSQL_ROOT_PASSWORD')"
export SOURCE_ELASTIC_PASSWORD="$(ssh -i ~/.ssh/infinity.pem ubuntu@35.237.247.13 'docker exec ragflow-es printenv ELASTIC_PASSWORD')"
export SOURCE_MINIO_ACCESS_KEY="rag_flow"
export SOURCE_MINIO_SECRET_KEY="$(ssh -i ~/.ssh/infinity.pem ubuntu@35.237.247.13 'docker exec ragflow-minio printenv MINIO_ROOT_PASSWORD')"

### 目标集群密码（当前 K8s 集群）
export TARGET_MYSQL_ROOT_PASSWORD="$(kubectl get secret mysql-password -n ragflow -o jsonpath='{.data.password}' | base64 -d)"
export TARGET_ELASTIC_PASSWORD="$(kubectl get secret elasticsearch-es-elastic-user -n ragflow -o jsonpath='{.data.elastic}' | base64 -d)"

# 目标集群内部负载均衡器 IP（K8s Services）
export MYSQL_INTERNAL_LB_IP="$(kubectl get svc mysql-ilb -n ragflow -o jsonpath='{.status.loadBalancer.ingress[0].ip}')"
export ES_INTERNAL_LB_IP="$(kubectl get svc elasticsearch-es-http-ilb -n ragflow -o jsonpath='{.status.loadBalancer.ingress[0].ip}')"
```

---

## 3. 迁移执行流程 (Execution Flow)

### 方案调研：MySQL 在线迁移技术对比

在进行 MySQL 迁移前，需要选择合适的技术方案。本节对比三种主流方案：mysqldump + Binlog Replication、GTID Replication 和 mydumper/myloader。

#### 1. mysqldump + Binlog Replication（传统方案）

**工作原理**：
- 使用 `mysqldump --single-transaction` 获取一致性快照
- 记录 dump 开始时的 binlog position
- 恢复全量备份后，从记录的 position 开始 Apply binlog

**优势**：
- 工具内置于 MySQL，无需额外安装
- 简单易用，适合小规模数据库

**劣势**：
- 单线程 dump，200GB 数据可能需要 10+ 小时
- 需要手动管理 binlog position，容易出错
- Failover 时需要人工确定 position

#### 2. GTID Replication（现代方案）

**GTID (Global Transaction ID)**：
```
UUID:transaction_id
例如：3E11FA47-71CA-11E1-9E33-C80AA9429562:1-2345
```

**GTID 解决的问题**：

| 问题 | file/pos replication | GTID |
|------|---------------------|------|
| 自动定位复制起点 | ❌ | ✅ |
| 自动 failover | ❌ | ✅ |
| 多源复制 | 困难 | 容易 |
| replica 重新加入 | 困难 | 简单 |
| 自动跳过已执行事务 | ❌ | ✅ |

**建立 GTID 复制**：
```sql
CHANGE MASTER TO MASTER_AUTO_POSITION=1;
```

**GTID 简化 dump 恢复**：
```bash
mysqldump --single-transaction --set-gtid-purged=ON
```
配合 `MASTER_AUTO_POSITION=1`，slave 会自动根据 dump 中记录的 GTID_EXECUTED 找到缺失的事务。

**注意**：GTID 的核心价值在于**复制运行期间**的自动化，而非 dump 本身。在”初始化复制位点”这个狭义场景下，GTID 不是必须的。

#### 3. mydumper/myloader（高性能方案）

**为什么 mysqldump 慢**：
- 单线程顺序导出
- 200GB 数据可能需要 10+ 小时

**mydumper 核心设计**：
- 并行 dump：将表分成多个 chunk，多线程同时导出
- 自动记录 binlog 位置到 metadata 文件

**metadata 文件示例**：
```
Started dump at: 2026-03-15 10:22:00
SHOW MASTER STATUS:
Log: mysql-bin.033463
Pos: 589718290
```

**典型命令**：
```bash
# 导出
mydumper --host 35.237.247.13 --port 7488 --user root --password "${SOURCE_MYSQL_ROOT_PASSWORD}" \
  --database rag_flow --outputdir backup --threads 16

# 导入
myloader --directory backup --threads 16 --database rag_flow
```

**mydumper vs mysqldump**：

| 功能 | mysqldump | mydumper |
|------|-----------|----------|
| 并行 dump | ❌ | ✅ |
| 并行 restore | ❌ | ✅ |
| 自动记录 binlog | 部分 | ✅ |
| 1TB dump 耗时 | 10h+ | ~1h |

#### 4. 方案对比总结

| 功能 | mydumper | GTID |
|------|----------|------|
| 生成 snapshot dump | ✅ | ❌ |
| 记录复制位点 | ✅ (metadata) | 可选 |
| 初始化 replication | ✅ | 可选 |
| 复制自动定位 | ❌ | ✅ |
| failover 自动化 | ❌ | ✅ |

**结论**：
- mydumper 解决 **snapshot 生成**
- GTID 解决 **ongoing replication**
- 两者经常一起使用

#### 5. 本次迁移采用的方案

考虑到 RAGFlow MySQL 数据量约 200GB，**推荐使用 mydumper + Binlog Replication**：
1. 使用 mydumper 并行导出，缩短 dump 时间（预计 1-2 小时 vs 10+ 小时）
2. 使用 file/position 方式建立复制（GTID 可选，但对单次迁移价值有限）
3. 业务不停机，后台静默同步
4. 割接时只需秒级切换

> ⚠️ **高并发写入场景注意事项**：mydumper 的并行 chunk 机制在某些高并发写入 workload 下可能比 mysqldump 更容易触发 replication 1032/1062 错误。这与 chunk snapshot 与 secondary index 可见性有关，在 RAGFlow 这种高写入场景下需要特别注意。

---

### Phase 1: MySQL 数据库迁移 (mydumper + Binlog Replication)

鉴于 200GB 数据的逻辑 Dump 与重放导入过程会引发长达上小时的业务停服，为追求极致的平滑割接，采用 **mydumper 并行导出 + Binlog 流式同步** 方案。相比传统的 mysqldump，mydumper 的并行导出可以将 200GB 数据从 10+ 小时缩短至 1-2 小时。


#### 1. 前提与环境检查

登录 `ragflow01`，确认以下内容：

- **Binlog 已开启**：
  ```sql
  SHOW VARIABLES LIKE 'log_bin';
  ```
- **server-id 唯一**（从库不能与主库相同）：
  ```bash
  # 主库 (ragflow01)
  mysql -h 35.237.247.13 -P 7488 -uroot -p"${SOURCE_MYSQL_ROOT_PASSWORD}" -e "SHOW VARIABLES LIKE 'server_id';"
  # 从库 (GKE)
  mysql -h 10.142.15.195 -P 3306 -uroot -p"${TARGET_MYSQL_ROOT_PASSWORD}" -e "SHOW VARIABLES LIKE 'server_id';"
  ```

  当前状态：主库 `server_id=1000`，从库 `server_id=1`，不冲突。
- **安装 mydumper**（如未安装）：
  ```bash
  # Ubuntu/Debian
  apt-get install mydumper

  # 或从源码编译
  git clone https://github.com/mydumper/mydumper.git
  cd mydumper
  cmake .
  make
  sudo make install
  ```

#### 2. 使用 mydumper 导出数据

mydumper 会自动在 metadata 文件中记录 binlog 位置，无需手动执行 `SHOW BINARY LOG STATUS`。

```bash
# 创建输出目录
mkdir -p ~/ragflow_mydump

# 执行并行导出（16 线程）
mydumper \
  --host 35.237.247.13 \
  --port 7488 \
  --user root \
  --password "${SOURCE_MYSQL_ROOT_PASSWORD}" \
  --database rag_flow \
  --outputdir ~/ragflow_mydump \
  --threads 16 \
  --rows 100000 \
  --chunk-filesize 64 \
  --compress \
  --verbose 2 \
  --kill-long-queries
```

**参数说明**：

| 参数 | 值 | 说明 |
|------|-----|------|
| `--threads` | 16 | 并行导出线程数，根据源库 CPU 核心数调整 |
| `--rows` | 100000 | 按行数分块，每个 chunk 约 10 万行 |
| `--chunk-filesize` | 64 | 按文件大小分块，每个文件 64MB |
| `--compress` | - | 压缩导出文件，节省磁盘 I/O |
| `--verbose` | 2 | 输出详细日志 |

**输出文件结构**：

```
~/ragflow_mydump/
  metadata                  # 包含 binlog position
  rag_flow-schema.sql       # 建表语句
  rag_flow.user.00000.sql   # 数据文件（多个）
  rag_flow.user.00001.sql
  rag_flow.orders.00000.sql
  ...
```

**查看 metadata 文件**（记录了 binlog 位点）：

```bash
cat ~/ragflow_mydump/metadata
```

预期输出：
```
Started dump at: 2026-03-15 10:22:00
SHOW MASTER STATUS:
Log: mysql-bin.033463
Pos: 589718290
Finished dump at: 2026-03-15 11:45:00
```

> **重要**：
> - mydumper 使用 `FLUSH TABLES WITH READ LOCK` 获取全局读锁后立即记录 binlog 位置，然后开启事务快照并释放锁
> - 因此从 metadata 记录的 binlog 位置到 dump 完成之间的写入**不会**出现在 dump 中，但会记录在 binlog 中
> - 建立复制时从记录的 position 开始重放即可，dump 中不包含的数据会被正确同步

#### 3. 传输导出文件到 GKE 环境

由于导出文件较大（压缩后），需要传输到 GKE 集群所在区域。可以使用以下方式：

**方式一：通过 GCS 中转**：

```bash
# 上传到 GCS
gcloud storage cp -r ~/ragflow_mydump gs://ragflow-migration/mysql/

# 在 GKE 节点下载
kubectl exec -n ragflow mysql-0 -- gcloud storage cp -r gs://ragflow-migration/mysql/ /tmp/ragflow_mydump
```

**方式二：直接 SCP 到 GKE 节点**（如果有 bastion 或 VPN）：

```bash
# 打包
tar -czf ~/ragflow_mydump.tar.gz -C ~/ragflow_mydump .

# 传输到 GKE worker node（通过 bastion 或 LoadBalancer 节点）
scp ~/ragflow_mydump.tar.gz user@bastion:/tmp/
```

#### 4. 在 GKE 环境创建 MySQL Internal LoadBalancer

为保证大文件导入的稳定性，使用 Internal LoadBalancer 而非 `kubectl exec`：

```bash
kubectl apply -f opentofu_ragflow/byok/mysql-internal-lb.yaml

# 记录 EXTERNAL-IP（实际是 VPC 内部私网 IP）
kubectl get svc mysql-ilb -n ragflow
NAME        TYPE           CLUSTER-IP       EXTERNAL-IP     PORT(S)          AGE
mysql-ilb   LoadBalancer   34.118.237.155   10.142.15.195   3306:31478/TCP   18m

# 若迁移机与 GKE 集群不在同一 Region，还需要为区域 ILB 打开 global access
gcloud compute forwarding-rules list --filter='IPAddress=10.142.15.195'
gcloud compute forwarding-rules update a91eaeadf7b694eb6a7401f5ed217606 --region us-east1 --allow-global-access
```

#### 5. 使用 myloader 导入数据

在 GKE MySQL Pod 中执行导入（建议在 Pod 内运行，避免网络不稳定）：

```bash
# 进入 MySQL Pod
kubectl exec -it -n ragflow mysql-0 -- bash

# 解压（如果压缩过）
cd /tmp
tar -xzf ragflow_mydump.tar.gz

# 使用 myloader 并行导入（16 线程）
myloader \
  --directory /tmp/ragflow_mydump \
  --threads 16 \
  --database rag_flow \
  --verbose 2
```

**或通过 ILB 从外部导入**：

```bash
# 使用前面已设置的 TARGET_MYSQL_ROOT_PASSWORD 环境变量

# 解压到临时目录
tar -xzf ~/ragflow_mydump.tar.gz -C /tmp/

# 使用 myloader 导入
myloader \
  --host 10.142.15.195 \
  --port 3306 \
  --user root \
  --password "${TARGET_MYSQL_ROOT_PASSWORD}" \
  --directory /tmp/ragflow_mydump \
  --threads 16 \
  --database rag_flow
```

> **恢复 baseline 前必须确保**：所有会连接目标库的应用实例都已停止，并在 baseline 恢复完成前保持冻结，避免应用初始化/迁移逻辑提前连接目标库并自动建表，从而引入 schema drift。

#### 6. 建立主从复制链路

从 metadata 文件中读取 binlog 位置，配置 GKE MySQL 作为源库的从库：

```bash
# 从 metadata 读取 binlog 位置
BINLOG_FILE=$(grep "Log:" ~/ragflow_mydump/metadata | awk '{print $2}')
BINLOG_POS=$(grep "Pos:" ~/ragflow_mydump/metadata | awk '{print $2}')

echo "Binlog: ${BINLOG_FILE} @ ${BINLOG_POS}"
```

配置复制（使用专用复制账号）：

```bash
# 使用前面已设置的 TARGET_MYSQL_ROOT_PASSWORD 环境变量

mysql -h 10.142.15.195 -P 3306 -uroot -p"${TARGET_MYSQL_ROOT_PASSWORD}" -e "
STOP REPLICA;
RESET REPLICA ALL;
CHANGE REPLICATION SOURCE TO
  SOURCE_HOST='35.237.247.13',
  SOURCE_PORT=7488,
  SOURCE_USER='repl_ragflow_migration',
  SOURCE_PASSWORD='4P0nZx7vJ9LmQ2sK',
  SOURCE_LOG_FILE='${BINLOG_FILE}',
  SOURCE_LOG_POS=${BINLOG_POS},
  GET_SOURCE_PUBLIC_KEY=1;
START REPLICA;
"
```

> **注意**：
> - `TARGET_ROOT_PASSWORD` 必须填写 **GKE mysql-0 本地 root 密码**，不是源库密码
> - `SOURCE_USER` / `SOURCE_PASSWORD` 使用源端提前创建的专用复制账号

**验证复制状态**：

```bash
mysql -h 10.142.15.195 -P 3306 -uroot -p"${TARGET_MYSQL_ROOT_PASSWORD}" -e "SHOW REPLICA STATUS\G" | grep -E "Seconds_Behind_Source|Relay_Log_File|Replication_Log_File"
```

#### 7. 秒级割接与解除复制

当 `Seconds_Behind_Master` 为 `0` 时，说明新老数据库已完全实时拉齐。

1. **停止前端写入流量**
2. **确认复制追平**：
   ```bash
   mysql -h 10.142.15.195 -P 3306 -uroot -p"${TARGET_MYSQL_ROOT_PASSWORD}" -e "SHOW REPLICA STATUS\G" | grep Seconds_Behind_Source
   ```
3. **解除从库身份**：
   ```bash
   mysql -h 10.142.15.195 -P 3306 -uroot -p"${TARGET_MYSQL_ROOT_PASSWORD}" -e "STOP REPLICA; RESET REPLICA ALL;"
   ```
4. **切换 DNS 或服务注册**，让 RAGFlow 连接新库

停机时间可压缩至 **不到 1 分钟**。

#### 8. 复制账号创建（如需要）

在源库（ragflow01）上创建专用复制账号：

```sql
CREATE USER 'repl_ragflow_migration'@'%' IDENTIFIED BY '4P0nZx7vJ9LmQ2sK';
GRANT REPLICATION CLIENT, REPLICATION SLAVE ON *.* TO 'repl_ragflow_migration'@'%';
FLUSH PRIVILEGES;
```

### Phase 2: MinIO 到 GCS 的平滑切换
*注：因 RAGFlow 原生支持 GCS（通过设置 `STORAGE_IMPL=GCS`），且会将原 MinIO 的 Bucket 映射为 GCS 的 Folder Prefix，此迁移不涉及业务侧逻辑重构，只需将裸对象进行 API 级别抽取并塞入 GCS 即可。*

1. **核心原则（为什么仍建议走 API）**: 经过检查，旧环境中的四个 MinIO 实例都是**单机独立运行 (Standalone)**，并不是分布式纠删码集群。从纯技术上说，直接拷贝底层对象文件并非绝对不可行；但为了保证对象路径、元数据语义、增量追平能力以及割接时的可重复执行性，仍然建议通过 `rclone` 或 `mc` 走 S3 API 抽取明文对象流并写入 GCS，而不是直接操作 Docker Volume。
2. **多阶段物理同步法保障不丢数据 (Zero Data Loss via Convergence)**:
   经过检查，老的 RAGFlow 环境下的四个节点 (`ragflow01`~`ragflow04`) 中的 MinIO 均为**单机独立运行 (Standalone) 模式**，并未组成分布式集群，因此各节点存储的数据完全独立且没有交集。这与传统的分布式集群抽取模型完全不同。
   这意味着，我们**必须在四个老节点上分别执行文件系统的增量同步**。这也是非常高效的，因为我们可以直接调用 Rclone 将各节点的挂载目录或直接使用 S3 API 同步到同一个 GCS Prefix 下进行合并：

   **⚠️ 关键警告：必须使用 `rclone copy` 而非 `rclone sync`**
   由于我们将 4 个独立节点的数据汇聚到同一个 GCS Bucket，`rclone sync` 的机制是“让目标端与源端完全一致”，这意味着当它在节点 A 运行时，会检测到 GCS 上由节点 B、C、D 上传的文件在节点 A 本地不存在，从而**错误地将它们删除**。
   因此，**必须严格使用 `rclone copy`**。`copy` 命令仅负责将本地新增或变更的文件复制到目标端，**绝不删除**目标端上已存在的任何文件，从而安全实现多源数据汇聚。

   **对象 key 映射示例**：
   假设旧环境中某个对象位于 MinIO 的 bucket `ragflow` 下，完整对象 key 为 `tenant_a/kb_42/doc_99/2026/03/report.pdf`，那么执行下述命令后：
   ```bash
   rclone copy minio:ragflow/ gcs:ragflow-77cbf49639d548639a329344f063ba299dc5388f/
   ```
   它在 GCS 中会映射为同一个 bucket 下的完整对象 key：`tenant_a/kb_42/doc_99/2026/03/report.pdf`。
   也就是说，映射关系是：
   `minio:rag_flow.tenant_a/kb_42/doc_99/2026/03/report.pdf`
   `-> gs://ragflow-77cbf49639d548639a329344f063ba299dc5388f/tenant_a/kb_42/doc_99/2026/03/report.pdf`
   如果后续决定在 GCS 中额外人为增加前缀隔离，例如写入到 `gs://.../minio-migrated/`，则命令和目标 key 也应同步调整，避免 RAGFlow 切换到 GCS 后找不到历史对象。

   > **传输优化考量**：若是公网跨州传输 2TB 可能极其耗时、容易中断兼产生高额出向 Egress 费用。建议考虑内网直链，或使用 [GCP Storage Transfer Service](https://cloud.google.com/storage-transfer/docs) 起一台中转 Compute Engine 加速拉取。

3. **rclone 配置详解**: 在每个旧节点（ragflow01~ragflow04）上执行以下配置步骤：

   ```bash
   # 安装 rclone（如未安装）
   curl -s https://rclone.org/install.sh | sudo bash

   # 配置 MinIO Source（交互式配置）
   rclone config create minio s3 \
     provider Minio \
     endpoint http://127.0.0.1:9000 \
     access_key_id "${SOURCE_MINIO_ACCESS_KEY}" \
     secret_access_key "${SOURCE_MINIO_SECRET_KEY}" \
     region us-east-1

   # 验证 MinIO 连接
   rclone lsd minio:
   ```

   **参数说明**：
   | 参数 | 值 | 说明 |
   |-----|-----|-----|
   | `provider` | `MinIO` | 标识 S3 兼容存储类型 |
   | `endpoint` | `http://127.0.0.1:9000` | 本地 MinIO API 地址（集群内网 IP） |
   | `access_key_id` | MinIO root 用户 AK | 见 `docker/.env` 中的 `MINIO_ROOT_USER` |
   | `secret_access_key` | MinIO root 用户 SK | 见 `docker/.env` 中的 `MINIO_ROOT_PASSWORD` |

4. **GCS 授权配置**: 本次迁移采用**方式一：Service Account 密钥文件**

   **为什么选择方式一？**
   - **跨账户迁移**：旧环境（GCP 项目 A）-> 新环境（GCP 项目 B），OAuth 需要浏览器交互授权，不适合自动化
   - **无浏览器远程节点**：ragflow01~04 是远程服务器，无法进行 OAuth 浏览器交互
   - **密钥可复用**：创建的密钥文件可以分发到 4 台服务器重复使用
   - **可靠性高**：不依赖用户登录会话，密钥长期有效

   **方式一：Service Account 密钥文件（本次迁移采用）**

   ```bash
   # 在有 GCS Bucket 写入权限的机器上配置 GCS remote
   # 可使用新 GCP 项目的 Service Account 或跨项目授权的 SA
   rclone config create gcs s3 \
     provider GCS \
     access_key_id <GCS_Service_Account_Email> \
     secret_access_key "<GCS_Service_Account_Json_Key>" \
     region us-central1

   # 或使用环境变量方式（更安全，避免密钥明文写入配置）
   export RCLONE_GCS_SERVICE_ACCOUNT_FILE=/path/to/service-account-key.json
   ```

   **方式二：OAuth 交互式授权（仅限有浏览器访问权限的机器）** - ❌ 不适合本次迁移

   ```bash
   rclone config create gcs s3 provider GCS
   # 按提示完成 OAuth 授权流程
   ```

   **方式三：Workload Identity（若在 GKE 环境中运行）** - ❌ 不适合本次迁移（ragflow01-04 是 Docker 环境，不是 GKE）

   ```bash
   # 在 GKE Pod 中使用 Workload Identity
   rclone config create gcs s3 provider GCS
   # rclone 会自动使用 Pod 关联的 Service Account 凭证
   ```

   **GCS Bucket 权限要求**：
   对目标 GCS Bucket `ragflow-77cbf49639d548639a329344f063ba299dc5388f` 授予以下角色：
   - `roles/storage.objectAdmin` 或
   - `roles/storage.objectCreator` + `roles/storage.objectViewer`

   > **⚠️ 常见问题：OAuth scope 授权失败**
   >
   > 如果使用 OAuth 方式授权，可能会遇到 `Provided scope(s) are not authorized` 错误：
   > ```
   > ERROR : xxx.pdf: Failed to copy: googleapi: Error 403: Provided scope(s) are not authorized, forbidden
   > ```
   > **解决方案**：使用 Service Account 密钥文件方式授权，并在配置中指定 `service_account_file` 参数：
   > ```ini
   > [gcs]
   > type = gcs
   > project_number = <GCP_PROJECT_NUMBER>
   > service_account_file = /path/to/service-account-key.json
   > ```
   >
   > rclone 配置文件默认位置：`~/.config/rclone/rclone.conf`

5. **完整同步命令示例**:

   ```bash
   # 在每个旧节点上执行（ragflow01~ragflow04）
   # 注意：源端使用 minio: 而不是 minio:bucket_name，这样会保留所有 bucket 的前缀结构
   # rclone copy minio: 会遍历所有 bucket，路径结构会自动保留为 bucket_name/file_name
   # 这与 RAGFlow 的 bucket/filename 访问方式完全兼容

   rclone copy minio: gcs:ragflow-77cbf49639d548639a329344f063ba299dc5388f/ \
     --progress \
     --s3-upload-cutoff 1G \
     --transfers 8 \
     --check-first \
     --fast-list \
     --retries 3 \
     --retries-sleep 5s \
     --ignore-existing \
     --size-only \
     --stats 2s \
     --log-level INFO \
     --log-file /tmp/rclone_migration.log

   # 参数说明：
   # - --fast-list          使用 ListObjectsV2 减少 API 调用，避免几十万文件时看起来像“卡住”
   # - --check-first        先做完检查再开始传输，进度条总数更准确
   # - --size-only          仅对比大小（忽略修改时间），加快检查速度
   # - --retries 3          失败重试次数
   # - --ignore-existing    跳过已存在文件
   # - --log-file           写入日志方便排查
   ```

   **rclone 配置要求**:
   - GCS remote 必须使用 Service Account 密钥文件授权，配置示例：
   ```bash
   # 在各节点上配置（参考上方第 4 步）
   # 配置文件位于 ~/.config/rclone/rclone.conf

   [minio]
   type = s3
   provider = Minio
   endpoint = http://localhost:11321
   access_key_id = "${SOURCE_MINIO_ACCESS_KEY}"
   secret_access_key = "${SOURCE_MINIO_SECRET_KEY}"
   region = us-east-1

   [gcs]
   type = gcs
   project_number = <GCP_PROJECT_NUMBER>
   service_account_file = /tmp/gcs-sa-key.json
   bucket_policy_only = true
   ```

6. **增量同步命令**:

   ```bash
   # 阶段二/阶段三：增量同步（使用 --ignore-existing 跳过已传输的文件，避免覆盖）
   # 同样建议加上 --fast-list 加速检查
   rclone copy minio: gcs:ragflow-77cbf49639d548639a329344f063ba299dc5388f/ \
     --ignore-existing \
     --progress --s3-upload-cutoff 1G --transfers 8 --retries 3 --retries-sleep 5s \
     --fast-list --check-first --size-only \
     --log-file /tmp/rclone_migration.log \
     --gcs-bucket-policy-only

   # 如需限速（避免影响业务）
   rclone copy minio: gcs:ragflow-77cbf49639d548639a329344f063ba299dc5388f/ \
     --ignore-existing \
     --bwlimit 100M \
     --progress \
     --s3-upload-cutoff 1G \
     --transfers 8 \
     --retries 3 \
     --retries-sleep 5s \
     --fast-list --check-first --size-only \
     --log-file /tmp/rclone_migration.log
   ```

7. **验证同步结果**:

   ```bash
   # 统计 MinIO 源端文件总数 (加上 --fast-list 加速)
   rclone size minio: --fast-list --json

   # 统计 GCS 目标端文件总数
   rclone size gcs:ragflow-77cbf49639d548639a329344f063ba299dc5388f/ --fast-list --json

   # 对比两端差异（检查缺失文件，--fast-list 极大加速对比过程）
   rclone check minio: gcs:ragflow-77cbf49639d548639a329344f063ba299dc5388f/ \
     --fast-list --size-only \
     --missing-on-gcs /tmp/missing_on_gcs.txt
   ```

8. **注入 GKE 集群变量配置**: 确保在 Opentofu 提起的部署文件中，存储后端切换为 `STORAGE_IMPL=GCS`，并配对 Workload Identity 实现无感 AK/SK（原 `terraform.tfvars` 已做预留）。

### Phase 3: Elasticsearch 跨架构迁移 (ES 9)

**当前状态**：数据已在 rag01、rag02、rag03、rag04 四台机器上组成活跃的 ES 9 集群运行。源集群有28000+个 `ragflow_*` 索引。
**方案选择**：采用 **Logstash 并发同步 + 全局 Index Template** 方案，直接从源 ES 集群拉取数据到目标 GKE ECK ES 集群。
**可行性检查结论**：
- 直接使用 Logstash 通配同步会丢失 RAGFlow 关键的向量字段 Mapping（由于目标集群自动降级为普通 float 数组导致向量检索瘫痪）。
- 解决方案：在目标 GKE 集群**提前注入全局 RAGFlow Index Template**，后续可直接用 Logstash 无脑同步。
- 运行期接入路径上，`kubectl port-forward` 只适合短时调试；在持续 bulk 写入场景下，已实测出现 `Connection reset`、`Connection refused`、TLS handshake timeout，而集群内直接访问 ES 正常。因此战时应优先为目标 ES 暴露 **VPC 内部可达的 Internal LoadBalancer**，让 Logstash 直接连接私网 ILB 地址，而不是长期依赖 `port-forward`。

**操作步骤**：

1. **为目标 ES 写入全局 RAGFlow 索引模板（极其关键）**：
   目标集群 GKE ECK 处于空数据状态，在导入数据前必须建立适配 RAGFlow 的 Mapping 模板。使用代码库中自带的 `mapping.json` 推送到目标 GKE ES。

   ```bash
   # 已编写自动化脚本：提取并组装 conf/mapping.json 成为符合 template 接口标准的结构
   # (默认保持 1 副本数，并带上了相似度配置及动态向量模板)
   python3 opentofu_ragflow/byok/create_es_template.py

   # 预期输出: Success: {"acknowledged":true}
   # 验证: 
   # curl -k -X GET "https://localhost:9200/_index_template/ragflow_template" -u elastic:${TARGET_ELASTIC_PASSWORD}
   ```

1.5 **战时为目标 ES 创建稳定入口（推荐 Internal LoadBalancer）**：
   迁移期如果 Logstash 需要长时间持续写入，不建议继续经由 `kubectl port-forward` 访问目标 ES。推荐直接为 `elasticsearch-es-http` 暴露一个 **Internal LoadBalancer**，仅在当前 VPC 内提供私网访问。

   ```bash
   # 应用仓库内置的 ILB Service 清单
   kubectl apply -f opentofu_ragflow/byok/elasticsearch-internal-lb.yaml

   # 等待分配 ILB 地址
   kubectl get svc elasticsearch-es-http-ilb -n ragflow -w

   # 记录 EXTERNAL-IP（实际是 VPC 内部私网 IP），后续在 rag01 上供 Logstash 直连
   kubectl get svc elasticsearch-es-http-ilb -n ragflow

   # 如果迁移机与 GKE 集群不在同一 Region（例如 tower01 在 asia-northeast1，而 GKE 在 us-east1），
   # 还必须为区域 ILB 打开 global access，否则会出现 ILB 已分配私网 IP、后端健康正常，
   # 但跨 Region 客户端访问超时的问题。
   gcloud compute forwarding-rules list --filter='IPAddress=<ES_INTERNAL_LB_IP>'
   gcloud compute forwarding-rules update <FORWARDING_RULE_NAME> --region us-east1 --allow-global-access
   ```

   > 说明：
   > 1. `Gateway` 在战时也可行，但对当前迁移只需要一个稳定的 TCP/HTTPS 入口，配置明显更重，不如 ILB 直接。
   > 2. `NodePort` 比 `port-forward` 更稳，但仍依赖节点 IP、防火墙和节点轮换，不如 ILB 适合作为临时生产入口。
   > 3. 如需进一步收敛暴露面，可在 Service 上补 `loadBalancerSourceRanges`，限制为 `tower01` 所在私网段。
   > 4. 本次实测中，ILB 后端健康、NodePort 可直连，但 ILB 从 `tower01` 访问超时，根因是 **跨 Region VPC 访问区域 ILB 时未开启 global access**。使用 `gcloud compute forwarding-rules update ... --allow-global-access` 后即恢复正常。

2. **部署并启动 Logstash 进程（业务运行期间，全量与流式同步）**：
   我们复用此前经验中成功使用过的极简版 Logstash 配置，并依托上方建立的 Template，开始向新集群灌库。

   当前已验证可工作的仓库参考文件见同目录 [logstash_full.conf](logstash_full.conf)。该文件与 `rag01` 当前生效配置保持一致，但已去掉明文密码，落地前只需填回源端与目标端 ES 凭据。

   *全量同步 Logstash 配置示例 (`logstash_full.conf`):*
   > 注意：在 Logstash 9.3.x 中，不应依赖 `docinfo` 默认落点。启用 ECS 兼容模式时，input 插件会把元数据默认写到 `[@metadata][input][elasticsearch]`，继续使用 `index => "%{[@metadata][_index]}"` 会触发 `Badly formatted index`。为避免版本差异，显式指定 `docinfo_target => "[@metadata][doc]"`，并在 output 中统一读取 `[@metadata][doc][_index]` / `[@metadata][doc][_id]`。
   >
   ```conf
   input {
     elasticsearch {
       hosts => ["https://127.0.0.1:11314"] # 配合 ssh tunnel 到源集群
       user => "elastic"
       password => "${SOURCE_ELASTIC_PASSWORD}"
       index => "ragflow_*"
       query => '{ "version": true, "query": { "match_all": {} } }'
       docinfo => true
       docinfo_target => "[@metadata][doc]"
       docinfo_fields => ["_index", "_id", "_version"]
       size => 100
       scroll => "5m"
       ssl_enabled => true
       ssl_verification_mode => none
     }
   }
   filter {
     mutate {
       remove_field => ["@timestamp", "@version", "[@metadata][_type]"]
     }
   }
   output {
     elasticsearch {
       hosts => ["https://10.142.0.57:9200"] # 目标集群
       user => "elastic"
       password => "${TARGET_ELASTIC_PASSWORD}"
       index => "%{[@metadata][doc][_index]}"
       document_id => "%{[@metadata][doc][_id]}"
       action => "index"
       version => "%{[@metadata][doc][_version]}"
       version_type => "external"
       ssl_enabled => true
       ssl_verification_mode => none
     }
   }
   ```

   *增量追平 Logstash 配置示例 (`logstash_incremental.conf`):*
   > 在割接窗口内执行。通过指定 `query` 条件，仅拉取 `create_timestamp_flt`（Unix 毫秒时间戳）大于某时刻的新增或修改文档，大幅缩短停机时间。
   ```conf
   input {
     elasticsearch {
       hosts => ["https://127.0.0.1:11314"] # 配合 ssh tunnel 到源集群
       user => "elastic"
       password => "${SOURCE_ELASTIC_PASSWORD}"
       index => "ragflow_*"
       query => '{ "version": true, "query": { "match_all": {} } }'
       docinfo => true
       docinfo_target => "[@metadata][doc]"
       docinfo_fields => ["_index", "_id", "_version"]
       size => 100
       scroll => "5m"
       ssl_enabled => true
       ssl_verification_mode => none
     }
   }
   filter {
     mutate {
       remove_field => ["@timestamp", "@version", "[@metadata][_type]"]
     }
   }
   output {
     elasticsearch {
       hosts => ["https://10.142.0.57:9200"] # 目标集群
       user => "elastic"
       password => "${TARGET_ELASTIC_PASSWORD}"
       index => "%{[@metadata][doc][_index]}"
       document_id => "%{[@metadata][doc][_id]}"
       action => "index"
       version => "%{[@metadata][doc][_version]}"
       version_type => "external"
       ssl_enabled => true
       ssl_verification_mode => none
     }
   }
   ```

   **执行阶段划分**：
   1. 业务静默期（提前数天）：执行全量导入
   建议将 Logstash 配置为 Systemd 后台服务运行，避免终端断开导致同步中断。目标 ES 的访问路径建议使用上一步创建的 Internal LoadBalancer，而不是让 Logstash 长期依赖 `kubectl port-forward`。`port-forward` 仅保留给短时调试。
   ```bash
   # 先确认目标 ES 的 ILB 已拿到私网地址
   kubectl get svc elasticsearch-es-http-ilb -n ragflow

   # 复制配置文件到 logstash 默认目录并修权限
   sudo cp opentofu_ragflow/byok/logstash_full.conf /etc/logstash/conf.d/
   sudo chown logstash:root /etc/logstash/conf.d/logstash_full.conf
   sudo chmod 644 /etc/logstash/conf.d/logstash_full.conf
   
   # 启动并允许开机自启
   sudo systemctl daemon-reload
   sudo systemctl enable logstash
   sudo systemctl restart logstash

   # 查看运行状态与持续追踪日志
   sudo systemctl status logstash
   sudo tail -f /var/log/logstash/logstash-plain.log
   ```
   2. 业务割接停机点（断网）：执行增量追平
   停止上述全量守护进程，然后执行增量跑表。
   ```bash
   sudo systemctl stop logstash
   # 执行前需替换 logstash_incremental.conf 中的 gt 时间戳
   /usr/share/logstash/bin/logstash -f logstash_incremental.conf
   ```

3. **等待首次全量追平与基于 _version 的等价重放**：
   通过配置 `version_type => "external"` 将源端的 `_version` 透传到目标端，实现了完全幂等的写入。这就避免了 Logstash 使用 `doc_as_upsert => true` 所导致的目标端数据无限被覆写、`_version` 被动拉高耗尽 I/O 的缺陷。未变更的旧数据将触发 Elasticsearch 的 `version_conflict_engine_exception` 状态码 409 被忽略处理，极大地减轻了集群写入压力。

4. **监控数据量对齐**：
   ```bash
   # 查看目标集群中 ragflow_* 各个 index 的文档数目：
   curl -k -u elastic:${TARGET_ELASTIC_PASSWORD} -X GET "https://10.142.0.57:9200/_cat/indices/ragflow_*?v&h=index,docs.count"

   # 汇总查询目标集群中所有 ragflow_* 文档的总数：
   curl -k -u elastic:${TARGET_ELASTIC_PASSWORD} -X GET "https://10.142.0.57:9200/ragflow_*/_count"

   # （在rag01结点）汇总查询源集群中所有 ragflow_* 文档的总数：
   curl -k -u elastic:${SOURCE_ELASTIC_PASSWORD} -X GET "https://127.0.0.1:11314/ragflow_*/_count"
   ```

从监控来看，同步速度约为 1.5M 个文档每小时。

| 项目 | Vanilla StatefulSet 跳板 | GCS 快照 | Remote Reindex (并发脚本) | ✅ Logstash + Template (本次采用) |
|------|------------------------|----------|------------------------|------------------------|
| **前置条件** | 需要 4 节点 Vanilla 集群 | 需要 OAuth 配置 | 无 | **需配置目标集群 Index Template** |
| **并发度** | 高 | 高 | 可控 (通过脚本) | 强依赖 Logstash 自身管道并发 |
| **复杂度** | 高 (物理传输) | 高 (GCP 权限锁死) | 中 (需管理 2.8万次 API 请求) | **极低 (依靠通配符一把梭)** |
| **断点续传** | 支持 | 支持 | 支持 (单索引粒度) | 依赖自身的 scroll/记录机制或全量覆盖 |

**最终选择原因**：
- **操作最简**：用户倾向于一键式 Logstash 同步方案，省去了写多线程脚本来调度成千上万个小 Task 的管理繁琐度。
- **Schema 安全**：利用自动注入的 Global Index Template (`ragflow_template`)，完美解决了 Logstash 会引发“动态映射导致向量检索瘫痪”的致命伤。

---

## 5. 验证与检查 (Verification)

1. **MySQL 按表行数校验**: 在旧环境 `ragflow01` 上的源 MySQL 与 GKE 中的新 MySQL 分别统计核心业务表的行数，确认逐表一致。建议至少覆盖 `user`、`tenant`、`knowledgebase`、`document`、`file` 等核心表，并对全库做一次汇总抽查。
   ```bash
    # 使用前面已设置的 MYSQL_INTERNAL_LB_IP、TARGET_MYSQL_ROOT_PASSWORD 和 SOURCE_MYSQL_ROOT_PASSWORD 环境变量

   # 源库示例：通过 MySQL 直接连接统计核心表行数
   mysql -h 35.237.247.13 -P 7488 -uroot -p"${SOURCE_MYSQL_ROOT_PASSWORD}" -N -e \
       "SELECT table_name, table_rows FROM information_schema.tables WHERE table_schema='rag_flow' ORDER BY table_name;"

    # 目标库示例：通过 MySQL Internal LoadBalancer 执行相同查询
    mysql -h "${MYSQL_INTERNAL_LB_IP}" -P 3306 -uroot -p"${TARGET_MYSQL_ROOT_PASSWORD}" -N -e \
       "SELECT table_name, table_rows FROM information_schema.tables WHERE table_schema='rag_flow' ORDER BY table_name;"
   ```
   > 若需要更高准确度，可针对核心表执行 `SELECT COUNT(*) FROM <table>;` 做逐表精确对账；`information_schema.tables.table_rows` 对 InnoDB 而言可能是估算值。
2. **MinIO / GCS 对象数目校验**: 对四个旧节点上的 MinIO 数据分别统计对象总数，再与目标 GCS bucket 中的对象总数进行比对，确认对象数量一致；必要时再做总大小校验。
   ```bash
   # 旧环境：分别在 ragflow01~ragflow04 上统计对象数
   mc find local/ragflow --type f | wc -l

   # 目标 GCS：统计对象总数
   gcloud storage ls -r gs://ragflow-77cbf49639d548639a329344f063ba299dc5388f/** | grep -v '/$' | wc -l
   ```
   > 若旧环境未预置 `mc alias set local ...`，则可改用 `rclone lsf minio:ragflow -R --files-only | wc -l`。最终应满足：`ragflow01 + ragflow02 + ragflow03 + ragflow04` 的对象数之和与 GCS 中对象总数一致。
3. **Elasticsearch 健康检查、分片校验与节点存储空间对比**: 除了检查健康状态与分片状态，还应对比 4 个旧节点和 4 个 GKE 内 ES 节点的存储占用是否基本一致（允许存在少量 segment merge 或 metadata 差异）。
   ```bash
   # 使用前面已设置的 ES_INTERNAL_LB_IP 环境变量

   # 推荐使用 Internal LoadBalancer 作为迁移期稳定入口，而非长期依赖 kubectl port-forward
   kubectl get svc elasticsearch-es-http-ilb -n ragflow
   curl -k -u elastic:${TARGET_ELASTIC_PASSWORD} "https://${ES_INTERNAL_LB_IP}:9200/_cluster/health?pretty"
   # 预期状态: green，data_nodes: 4
   
   # 检查分片分布状态：
   curl -s -k -u elastic:${TARGET_ELASTIC_PASSWORD} "https://${ES_INTERNAL_LB_IP}:9200/_cat/shards?v" | grep -E "STARTED|RELOCATING"

   # 检查 GKE 内各 ES 节点存储占用
   curl -s -k -u elastic:${TARGET_ELASTIC_PASSWORD} "https://${ES_INTERNAL_LB_IP}:9200/_cat/allocation?v"
   ```
   旧环境则在 `ragflow01`、`ragflow02`、`ragflow03`、`ragflow04` 上分别统计 `~/es_data` 的实际占用，例如：
   ```bash
   du -sh ~/es_data
   ```
   > 目标是旧 4 节点总占用与 GKE 中 4 个 ES 节点总占用基本一致；单节点之间允许因分片重平衡出现轻微偏差，但不应出现某一节点明显缺失大量数据。
4. **GCS 对象总大小快速校验**:
   ```bash
   gcloud storage du -s gs://ragflow-77cbf49639d548639a329344f063ba299dc5388f/ | awk '{print $1/1024/1024/1024 " GB"}'
   ```
5. **RAGFlow 端到端测试**: 在 GKE 网页界面尝试操作过往的知识库并进行一次新的文档解析，确保 GCS 读写均畅通。

### 割接后 Checklist 命令清单

以下命令清单按 `MySQL -> MinIO -> Elasticsearch -> RAGFlow 应用联调` 的顺序排列，适合在割接完成后逐项执行并勾选。

1. **MySQL: 核对核心表行数**
   ```bash
    # 使用前面已设置的 MYSQL_INTERNAL_LB_IP、TARGET_MYSQL_ROOT_PASSWORD 和 SOURCE_MYSQL_ROOT_PASSWORD 环境变量

   # 旧环境：通过 MySQL 直接连接执行，导出源库表行数
    mysql -h 35.237.247.13 -P 7488 -uroot -p"${SOURCE_MYSQL_ROOT_PASSWORD}" -N -e \
       "SELECT table_name, table_rows FROM information_schema.tables WHERE table_schema='rag_flow' ORDER BY table_name;" \
       > /tmp/mysql_rows_source.txt

    # 新环境：通过 MySQL Internal LoadBalancer 导出目标库表行数
    mysql -h "${MYSQL_INTERNAL_LB_IP}" -P 3306 -uroot -p"${TARGET_MYSQL_ROOT_PASSWORD}" -N -e \
       "SELECT table_name, table_rows FROM information_schema.tables WHERE table_schema='rag_flow' ORDER BY table_name;" \
       > /tmp/mysql_rows_target.txt

   # 对比结果
   diff -u /tmp/mysql_rows_source.txt /tmp/mysql_rows_target.txt
   ```
   > 注意：这一步读取的是 `information_schema.tables.table_rows`，对 InnoDB 来说通常只是估算值，即使主从已经完全追平，也可能出现大量表“看起来有差异”。它适合做快速烟雾检查，不适合作为最终验收结论。若 `SHOW REPLICA STATUS\G` 已显示 `Seconds_Behind_Source: 0` 且复制线程正常，请改以第 2 步的精确 `COUNT(*)` 抽查为准；若仍需扩大核验范围，可对差异较大的表逐个执行 `COUNT(*)`，必要时再补 `CHECKSUM TABLE` 或业务侧抽样验证。

   ```bash
   python3 opentofu_ragflow/byok/check_mysql_diff_tables.py \
     --source-rows /tmp/mysql_rows_source.txt \
       --target-rows /tmp/mysql_rows_target.txt \
       --chunk-size 5 \
       --output /tmp/mysql_diff_count_results.tsv
   ```

   该脚本会自动：
   1. 从 `diff -u` 输出中提取显示有差异的表名。
   2. 通过 MySQL 直接连接分批查询源库精确 `COUNT(*)`。
   3. 通过 MySQL ILB 分批查询目标库精确 `COUNT(*)`。
   4. 输出 `table_name / source_count / target_count / delta / status` 五列结果，便于快速识别真正不一致的表。
   5. 若只想看真正不一致的表，可追加 `--only-mismatch`。

2. **MySQL: 对关键表做精确 `COUNT(*)` 抽查**
   ```bash
    # 使用前面已设置的 MYSQL_INTERNAL_LB_IP、TARGET_MYSQL_ROOT_PASSWORD 和 SOURCE_MYSQL_ROOT_PASSWORD 环境变量

   mysql -h 35.237.247.13 -P 7488 -uroot -p"${SOURCE_MYSQL_ROOT_PASSWORD}" -N -e \
       "SELECT 'user', COUNT(*) FROM rag_flow.user UNION ALL SELECT 'tenant', COUNT(*) FROM rag_flow.tenant UNION ALL SELECT 'knowledgebase', COUNT(*) FROM rag_flow.knowledgebase UNION ALL SELECT 'document', COUNT(*) FROM rag_flow.document UNION ALL SELECT 'file', COUNT(*) FROM rag_flow.file;"

    mysql -h "${MYSQL_INTERNAL_LB_IP}" -P 3306 -uroot -p"${TARGET_MYSQL_ROOT_PASSWORD}" -N -e \
       "SELECT 'user', COUNT(*) FROM rag_flow.user UNION ALL SELECT 'tenant', COUNT(*) FROM rag_flow.tenant UNION ALL SELECT 'knowledgebase', COUNT(*) FROM rag_flow.knowledgebase UNION ALL SELECT 'document', COUNT(*) FROM rag_flow.document UNION ALL SELECT 'file', COUNT(*) FROM rag_flow.file;"
   ```

3. **MinIO: 分别统计 4 个旧节点对象数**
   ```bash
   ssh ragflow01 "mc find local/ragflow --type f | wc -l"
   ssh ragflow02 "mc find local/ragflow --type f | wc -l"
   ssh ragflow03 "mc find local/ragflow --type f | wc -l"
   ssh ragflow04 "mc find local/ragflow --type f | wc -l"
   ```

4. **GCS: 统计目标 bucket 对象总数**
   ```bash
   gcloud storage ls -r gs://ragflow-77cbf49639d548639a329344f063ba299dc5388f/** | grep -v '/$' | wc -l
   ```

5. **GCS: 统计目标 bucket 总大小**
   ```bash
   gcloud storage du -s gs://ragflow-77cbf49639d548639a329344f063ba299dc5388f/ | awk '{print $1/1024/1024/1024 " GB"}'
   ```

6. **Elasticsearch: 检查集群健康状态**
   ```bash
   # 使用前面已设置的 ES_INTERNAL_LB_IP 环境变量

   kubectl get svc elasticsearch-es-http-ilb -n ragflow
   curl -k -u elastic:${TARGET_ELASTIC_PASSWORD} "https://${ES_INTERNAL_LB_IP}:9200/_cluster/health?pretty"
   ```

7. **Elasticsearch: 检查分片是否正常分配**
   ```bash
   # 使用前面已设置的 ES_INTERNAL_LB_IP 环境变量
   curl -s -k -u elastic:${TARGET_ELASTIC_PASSWORD} "https://${ES_INTERNAL_LB_IP}:9200/_cat/shards?v" | grep -E "STARTED|RELOCATING"
   ```

8. **Elasticsearch: 验证目标集群文档总汇**（非常重要，建议静默同步期每天执行对账）
   ```bash
   # 使用前面已设置的 ES_INTERNAL_LB_IP 环境变量

   # 查看目标集群中 ragflow_* 文档总数：
   curl -k -u elastic:${TARGET_ELASTIC_PASSWORD} "https://${ES_INTERNAL_LB_IP}:9200/_cat/indices/ragflow_*?h=docs.count" | awk '{sum+=$1} END {print "Total Documents(Target): " sum}'
   
   # 与源端汇总作对比（源端在 ragflow01 执行）：
   curl -k -u elastic:${SOURCE_ELASTIC_PASSWORD} "https://10.142.0.5:1314/_cat/indices/ragflow_*?h=docs.count" | awk '{sum+=$1} END {print "Total Documents(Source): " sum}'
   ```

9. **Elasticsearch: 验证 Mapping 与 Index Template 是否正确生效（防止由 Logstash 通配导入丢失向量索引导致检索系统瘫痪）**
   ```bash
   # 使用前面已设置的 ES_INTERNAL_LB_IP 环境变量
   # 随机选取一个拥有较大数据的索引查看其 Mapping，检查是否存在 'dense_vector' 结构
   # 预期输出应该能够过滤出大量 'dense_vector' 行
   curl -s -k -u elastic:${TARGET_ELASTIC_PASSWORD} "https://${ES_INTERNAL_LB_IP}:9200/ragflow_cfa4cd54955711efbec00242ac120006/_mapping" | grep -o 'dense_vector' | wc -l
   ```

10. **Elasticsearch: 验证单条文档完整性**
   ```bash
   # 使用前面已设置的 ES_INTERNAL_LB_IP 环境变量
   # 抓取一条文档，确认 _id、内容段(_source) 与向量空间都完整同步：
   curl -s -k -u elastic:${TARGET_ELASTIC_PASSWORD} "https://${ES_INTERNAL_LB_IP}:9200/ragflow_cfa4cd54955711efbec00242ac120006/_search?size=1&pretty"
   ```

11. **Elasticsearch: 检查 GKE 内 4 个 ES 节点的存储占用**
   ```bash
   # 使用前面已设置的 ES_INTERNAL_LB_IP 环境变量
   curl -s -k -u elastic:${TARGET_ELASTIC_PASSWORD} "https://${ES_INTERNAL_LB_IP}:9200/_cat/allocation?v"
   ```

9. **Elasticsearch: 检查 4 个旧节点磁盘占用**
   ```bash
   ssh rag01 "du -sh ~/es_data"
   ssh rag02 "du -sh ~/es_data"
   ssh rag03 "du -sh ~/es_data"
   ssh rag04 "du -sh ~/es_data"
   ```

10. **RAGFlow: 应用侧联调验证**
   ```text
   1. 登录 GKE 上的新 RAGFlow。
   2. 打开历史知识库，抽查若干旧文档是否可见。
   3. 随机打开若干历史文档预览，确认 GCS 回读正常。
   4. 新建一次文档上传与解析任务，确认 MySQL/MinIO(GCS)/ES 新写入链路正常。
   5. 发起一次检索与问答，确认召回与对话链路正常。
   ```

---

## 6. 关键决策与进一步考量 (Decisions & Considerations)

- **为什么 MinIO 仍优先用 API 同步，而不是直接 Copy 文件夹？**
   虽然我们已经确认旧环境的四个 MinIO 实例是独立单机模式，并非分布式纠删码集群，但迁移目标是 GCS 而不是另一套原样的 MinIO Volume。直接复制底层 `docker_minio_vol` 缺少稳定的增量追平机制，也不利于在割接窗口内重复执行校验；通过 `rclone` 调用 S3 API 获取对象流，能更稳妥地保留对象语义并支持多轮增量同步。
- **关于 2TB MinIO 独立节点的并发传输与合并 (Concurrency and Merge)**
  1. **必须在 4 个旧节点上分别平行传输**：我们确认了旧集群中的四个节点 (`ragflow01`~`ragflow04`) 各自运行着**独立的单机版 MinIO (Standalone)** （并非分布式纠删码集群），共计存储了约 2TB (分别约 474G, 800G, 481G, 667G) 的相互独立的数据。这意味着它们的数据必须**四路并发、各自独立地向 GCS 发送**，GCS 将作为统一的合并存储底座接收它们的所有对象。因为 RAGFlow 前端是通过统一哈希分发或者应用层路由写入的，所以它们的文件路径一定不会相互覆盖冲突。
  2. 因新老 RAGFlow 归属不同的 Google Cloud 账户，若通过外网 IP 走公网传输 2TB 的 MinIO 数据和 1TB 的 ES 快照，将产生极高昂的**公网出向流量费 (Internet Egress)**。
  **降本架构方案**：
  1. **同区域内部网络对等 (VPC Peering)**: 若两个账户的资源位于同一个 GCP Region（如 `us-central1`），请在双方的 VPC 网络之间建立 [VPC Network Peering](https://cloud.google.com/vpc/docs/vpc-peering)。通过内网 IP（Private IP）互相通信时，**同可用区的内网 Egress 是完全免费的**（跨可用区也极度便宜且安全）。
  2. **GCS 的私有服务内网访问 (Private Google Access)**: 让旧集群机器通过 `private.googleapis.com` 或跨 VPC 共享的底层路由直连新的 GCS Bucket，避免对象存储的流量从公网路由绕出结算。
  3. **IAM 的跨账户/跨项目授权**: 无需把老数据先下载再传给新账户。只需要对旧集群机器使用的 Service Account 赋予新账户 GCS Bucket 的 `Storage Object Admin` 角色。旧集群便可**直接 (Direct write)** 写向新账户的存储桶。
- **关于 Elasticsearch 9 Cluster Identity**
  在当前方案中，源集群（rag01-04）做快照时使用 GCS 作为存储后端。快照恢复时，ECK 目标集群**无需与源集群保持相同的 `cluster.name`**，ES 的 `_restore` API 会自动处理底层的索引和元数据适配。这意味着通过 OpenTofu 部署的目标 ECK 集群可以拥有**任意独立的命名状态和底层证书体系**，真正实现历史负担的抛弃。

---

## 7. 迁移保障与推荐实践 (Best Practices)

1. **先小规模验证**：正式迁移前，建议用 1 个知识库 + 100MB 测试数据走一遍完整流程，确认 GCS 前缀、ES shard 重建、MySQL 数据表映射都能顺利生效。
2. **停机窗口规划**：若采用 Binlog 流式联机同步配合 ES/MinIO 的增量收敛策略，整体业务彻底阻断的切换“停机断网窗口”可压缩至 **小于 15 分钟** 的超低闪断。
3. **回滚方案兜底**：执行迁移后 48 小时内，请**不要**清理原始 Docker Compose 上的旧有本地 PVC 与旧的数据目录/Bucket，以此作为绝对回滚锚点。

## 8. 迁移耗时预估表 (Time Estimation)

以下是基于 RAGFlow 现有数据量（Elasticsearch 约 1TB，MinIO 约 2TB，MySQL 约 200GB），公网/万兆内网复合条件下的迁移阶段时间估算，帮助明确**业务不停机的后台预热耗时**与**最终断网割接的停机耗时**。

| 数据源 | 数据量 / 节点数 | 阶段 | 实施方法 | 预计耗时 (后台在线操作) | 预计耗时 (停机断网操作) | 风险与耗时波动因素 |
| :--- | :--- | :--- | :--- | :--- | :--- | :--- |
| **MySQL** | 200GB (大单表可能较多) | 在线导基线与导入 GKE | 带 `--source-data=2` 标记的逻辑导入导出 | **2 ~ 4 小时** | / | 导入大体积 SQL 到线上依旧消耗较大时间，但此状态下业务处于读写允许状态。 |
| **MySQL** | 日志残流 | 建立主从同步并追赶 (Binlog) | `CHANGE REPLICATION SOURCE TO ...` | **持续后台静默进行** | / | GKE Pod 必须存在公网/内网打通以访问 Docker 本地机器的 3306。 |
| **MySQL** | Zero | 断开同步，流量割接 | 校验延迟并切走前端流量 | / | **极快，小于 1 分钟** | / |
| **MinIO** | 2TB (百万级海量小文件) | 阶段一: 全量底层预热 | `rclone copy ...` 多线程拉取 | **24 ~ 72 小时** | / | 跨州公网可能被限速，海量极其细碎的文件会导致 API QPS 碰顶（长尾）。建议切分目录拉取。 |
| **MinIO** | 增量 (千兆以内) | 阶段二: 增量追赶与收尾 | `rclone copy` 对比 Hash 后仅传增量 | 视变化率，约 **5 ~ 30 分钟** | / | / |
| **MinIO** | 收尾尾巴 | 阶段三: 停机最终断网同步 | `rclone copy` (彻底无新写入) | / | **1 ~ 3 分钟** | 此步可确保 100% 0丢失。 |
| **Elasticsearch** | 4 节点，ES 9 集群 | 活跃集群全量迁移 | Remote Reindex | **数小时~数十小时** | 同上 | 速度比快照慢，但无需解决 GCS OAuth 问题 |
| **汇总** | -- | **总长预估** | -- | **约 1 ~ 2.5 天 (业务无感知)** | **约 15 ~ 30 分钟 (极短暂闪连停服)** | 这是一套典型的“三阶段异地无感知切换重铸”方案。 |



## 9. 故障排查记录与避坑指南 (Troubleshooting)

1. **解决 Logstash 同步时 _version 被抛弃导致版本冲突与覆盖失效的问题**：
   在最初使用 Logstash 配合 `elasticsearch` 插件抓取旧数据时，Logstash 获取到目标 ES 端常常报 `_version` 为 `null` (如 `For input string: "%{[@metadata][doc][_version]}"`) 的错误，或因为没有携带正确的源 `_version` 导致发生覆盖写入。
   **避坑方案 (必做)**：Elasticsearch 的 `_search` 接口在默认情况下不返回文档的版本信息。必须在 Logstash 的 input query 区块中显式下发 `{ "version": true }` 参数：
   ```logstash
   query => '{ "version": true, "query": { "match_all": {} } }'
   ```
   随后并在 output 区块配合使用 `version_type => "external"` 才能实现完美幂等。

2. **解决源端包含海量 Dense Vector 导致 Logstash OOM 和极慢速瓶颈问题 (性能调优)**：
   由于 RAGFlow 的知识库索引存在大量的二进制数组向量(数千维的浮点串)，在 Logstash 源端反序列化提取的时候极其耗费 CPU 和内存。未调优时，极易因 `java.lang.OutOfMemoryError: Java heap space` 导致 Logstash 崩溃，或者因读取等待导致源与日标 ES CPU 利用率冰点（个位数百分比）。
   **避坑方案 (吞吐量极限调优必做)**：
   *   **加大堆内存 (地基)**: 向量的 JSON 反序列化吃缓存极大。坚决不要用默认的 `1g`。修改 `/etc/logstash/jvm.options`（或者 Systemd 的 `/etc/systemd/system/logstash.service.d/override.conf` 配置，这很重要别漏了环境变量层掩盖）设为 `-Xms8g -Xmx8g`。
   *   **开启 Input 多分片并发读取**: 在 `input` `elasticsearch` 插件内部一定要配合添加 `size => 500` 和 `slices => 8`。它能让单次的 Scroll 操作通过 `slice` 参数打散成多线程同时向源 ES 抽数据。
   *   **加速 Pipeline 并发**: 修改 `/etc/logstash/logstash.yml` 设置 `pipeline.workers: 8` 和 `pipeline.batch.size: 500`，彻底打平两端通信带宽。（配置好后应该能让读取吞吐量达到 400+ Docs/sec，完全吃满源端所有 vCPU 算力）。


3. **崩溃重启根因 (`PipelineAction::Create<main>, action_result: false`)**：如果日志中抛出此错误并在 systemd 中不断重启（`aborting due to shutdown request while waiting for connections`），极有可能是源 ES 不可达导致的连接超时，而非 pipeline 语法错误。当前已验证可工作的做法是把 Logstash 放在与 GKE 同 Region 的 `rag01`，并通过 SSH 隧道访问 `ragflow01` 上的源 ES：`ssh -i ~/.ssh/infinity.pem -N -L 127.0.0.1:11314:127.0.0.1:9200 ubuntu@35.237.247.13`，然后将 `hosts` 配置为 `["https://127.0.0.1:11314"]`。

4. **日志体积爆满根因 (Log volume issues)**：系统级别运行 Logstash 时，默认的 `info` 或 `warn` 级别在发生警告或发送失败时，会把整条 Document 连同 metadata 打进日志。**解决方案**：修改 `/etc/logstash/logstash.yml`，设定 `log.level: error`，然后重启服务。

5. **目标 ES 无 `ragflow_*` 索引但集群健康正常**：如果目标 ES 健康正常，但 `_cat/indices/ragflow_*` 为空，不要先怀疑 Mapping；优先检查 Logstash 日志里是否有 `Badly formatted index, after interpolation still contains placeholder`。这是 output 没拿到 `_index` 的特征性报错。

6. **`kubectl port-forward` 不适合作为长期写入链路**：已实测在持续 bulk 写入时会出现 `Connection reset`、`Connection refused`、TLS handshake timeout，而从 ES Pod 内直接访问集群接口正常。因此正式全量迁移时，目标 ES 建议优先暴露为 **Internal LoadBalancer**，并让 Logstash 直连私网 ILB 地址。 
