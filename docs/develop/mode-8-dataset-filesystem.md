# mode 8 设计：dataset 语料对 Codex 的访问方式（方案评估与决策）

状态：草案（**关键前提已实测**，见 §3）
作者：zhichyu
日期：2026-10-16
关联文档：[mode-8-codex-mcp-agent.md](./mode-8-codex-mcp-agent.md)（mode 8 本体：共享 Codex + MCP 检索桥）
关联代码：`internal/codexagent/`、`internal/ingestion/task/`、`internal/storage/`、`internal/service/document/`、`internal/mcp/`
关联探针：`internal/codexagent/` 下的 `mount_visibility_*`、`sandbox_capability_*`、`sandbox_localipc_*`、`cwd_missing_*`、`read_barrier_*`（均 `//go:build integration`）
关联提交：`627a3b854`（删除 cwd 机制）、`b30ffdae3`（挂载可见性探针）

**结论先行**：以 **MCP 桥上的可导航目录树 + L0/L1 摘要（方案 B）为基线**，
把 **沙箱外 FUSE 挂载（方案 A）作为 opt-in 辅助层**。
两者**共享同一棵语料树与同一个内容层**，差别只在访问通道。

---

## 1. 问题与约束

### 1.1 要解决什么

让 Codex 能**有结构地访问整个 dataset 语料**，而不是只能做零散的相关度检索：
浏览目录、按需展开、对全量原文做过滤。

两条内容侧的硬要求（用户确定）：

1. **不暴露原始文件**——路径沿用原始文件的路径，**内容替换为解析后的 markdown**；
2. 内容源可插拔，但**本计划只落一个 adaptor**（复用原始文件所用的同一个 `storage.Storage`）。

### 1.2 评估尺子（分布式多租户场景）

| # | 要求 |
|---|---|
| **R1** | **租户隔离是硬边界**（实测：只读沙箱不限制读 ⇒ 边界只能靠基础设施，不能靠 cwd/提示词） |
| **R2** | **可横向扩展**：多 API/MCP 副本 |
| **R3** | **生命周期动态**：dataset/文档随时增删改，不能要求重启或重挂 |
| **R4** | **运维扇出不随租户数线性增长** |
| **R5** | **凭证不扩散**：存储凭证不该复制到 N 个执行侧 pod |
| **R6** | **作用域可收窄**（理想是按会话） |
| **R7** | **新增面尽量小**（与仓库"最小必要"一致） |

---

## 2. 一条先验：文件系统语义不提供检索，也无法加速 grep

这条不依赖任何实现，是后面判定 A / B 的依据。

### 2.1 POSIX 没有"检索"这个原语

`open` / `read` / `stat` / `readdir` 就是全部能力。文件系统里**没有倒排表、没有相关度、没有"召回"的位置**。
⇒ 全文检索只能建在 FS **之外**：进程读完字节自己算，或者另有一个索引服务。

### 2.2 grep 能跑，但无法被文件系统加速

`grep` 是用户态 `read()` 循环，FS 既无法拦截、也无法把索引注入给它——除非换掉 `grep` 本身。
⇒ 任何"挂载 + grep"的成本恒为 **O(全部字节) + O(对象数) 次 RTT**；
`--cache` 之类的本地缓存只加速**重复读**，不改变首次仍是全扫。

### 2.3 归因不是"虚拟"，是"POSIX 语义 + 远程 I/O"

本地 ext4 上的 `grep -r` **同样是全量扫描**，只是 800 MB/s 与几十次网络往返的差别。
所以限制不在 FUSE，也不在"虚拟"：**read 循环无法变成索引查询**。

### 2.4 三条推论（直接约束候选方案）

| 推论 | 落在本文何处 |
|---|---|
| **A（挂载）只能提供"地址化 + 本地工具链"，不提供检索，也不能承诺交互级 grep** | §8.3 把它写成已知代价 |
| **B（MCP 树）不受此限**：它不是 POSIX，同一工具面里可以既有导航也有索引化检索 | §7.2 |
| **把 B 做成"只有 `ls`/`read` 的假文件系统"，就继承 A 的缺陷、又拿不到 A 的工具链**——两头最差 | §7.2、§13 |

因此后面两条是硬性的：

- B 的工具面必须**导航与检索成对**：`ls` / `read` 负责定位与读取，`search_*` / `grep_chunks`
  负责发现；提示词要明确"发现内容用检索，不要遍历"。
- 任何语料级 grep **只能是索引化的**（复用现有 `GrepService` 的 ES `regexp` 下推），
  **不做请求期全桶扫描**：那是 O(语料) 的操作，与交互式调用的时延不相容。

---

## 3. 已实测的事实（决定各方案的边界）

### 3.1 T1 挂载在沙箱内可见 —— 已实测

`mount_visibility_integration_test.go`：sshfs 真 FUSE 挂载 + canary。

| 对象 | 宿主可见 | 沙箱内可见 |
|---|---|---|
| `fuse.sshfs` 挂载 | ✅ | ✅（`command/exec` readOnly exit=0；模型驱动 turn 也读到） |
| `/dev/shm`（对照） | ✅ | ❌（bwrap 合成自己的 `/dev`） |

### 3.2 T2 cwd 不是读屏障 —— 已实测

`read_barrier_integration_test.go`：只读沙箱下可读宿主任意路径（`$HOME` canary、`/etc/passwd`、`~/.codex/config.toml`）。

### 3.3 T3 cwd 语义 —— 已实测（机制已删除）

`cwd_missing_integration_test.go`：`thread/start` 不校验 cwd；cwd 在宿主不存在则每条命令 ENOENT 且无回落；不设 cwd 可用。

### 3.4 T4 沙箱不能挂载 / 不能联网 —— 已实测

`sandbox_capability_integration_test.go`（生产姿态 `SandboxReadOnly` + on-request）：

| 探针 | 结果 |
|---|---|
| `/dev/fuse` | ABSENT |
| `CapEff`/`CapPrm`/`NoNewPrivs` | `0` / `0` / `1`（setuid 提权失效） |
| `mount -t tmpfs none /mnt` | `must be superuser to use mount`（exit=32） |
| TCP connect | `bash: socket: Operation not permitted` |
| `touch /tmp/…` | `Read-only file system` |

⇒ **绝不可让模型自己挂载**：要放开需同时给 seccomp/网络/`CAP_SYS_ADMIN`/`/dev/fuse`/可写目录，
而沙箱是 mode-8 唯一剩下的隔离关（mode-8 计划 §5.5）。

### 3.5 T5 沙箱内没有任何主动通信通道 —— 已实测（本轮新增，决定性）

`sandbox_localipc_integration_test.go`（宿主机侧由测试进程起真实 unix socket 服务端）：

| 探针 | 结果 |
|---|---|
| `socket(AF_INET)` | `PermissionError` (EPERM) |
| `socket(AF_UNIX)` | **OK**（能建对象） |
| `socketpair` + `send` | socketpair 建成，**`send` 被拒** (EPERM) |
| 宿主 socket 文件在沙箱内 | **可见**（`srwxr-xr-x`） |
| `connect(宿主 unix socket)` | `PermissionError` (EPERM) |
| `curl http://127.0.0.1:8790/` | `http_code=000`, exit=7 |

⇒ **拦的是 socket 的数据面系统调用（`send`/`connect`），不是路径可见性**；
沙箱内**没有任何可用的进程间/网络通道**。

> **推论（本文多处依赖）**：沙箱内唯一能主动对外通信的通道，是 **app-server 自己持有并转发的那条连接**
> ——即 **MCP 是唯一可用通道**，不是我们选了它，而是其他全被沙箱封死。

### 3.6 内容侧：markdown 目前没有落盘 —— 已核实

`internal/ingestion/task/pipeline_executor.go:268` 的 `processOutput` 只
`NormalizeChunks(...)` → `s.indexWriter.Write(...)`（写 doc engine），**没有文档级 markdown 持久化**；
`entity.Document`（`internal/entity/document.go`）**没有** content/markdown 列；全仓无此约定。
markdown 只瞬时存在于 `pipelineOutput["markdown"]`（`indexdoc/normalize.go:44`、`debug_result_dsl.go:50`）。

⇒ **无论选哪个方案，"落盘 markdown"（M0）都是必须的第一件事。**

### 3.7 存储侧：默认每 dataset 一个桶

默认模式（`minio.bucket=""`）下**逻辑桶 = dataset ID**（`internal/storage/minio.go` 的 `resolveBucketAndPath`），
即一个租户的 dataset 分布在**不同物理桶**里。⇒ 若走挂载路线，**单个挂载点跨不了桶**，
markdown 必须进**独立于原始文件桶**的专用位置。

---

## 4. 候选方案

| | 方案 | 访问通道 | 能力 |
|---|---|---|---|
| **A** | **沙箱外 FUSE 挂载**（Mountpoint / GeeseFS / s3fs / rclone + CSI / initContainer） | 内核 VFS | `ls`/`find`/`grep`/`pandoc`/`python open()`，全量遍历与脚本处理 |
| **B** | **MCP 桥上的可导航目录树 + L0/L1 摘要** | MCP 工具（**唯一可用通道**，T5） | 浏览、按需读、目录级摘要、**索引化**语料检索（非扫描，见 §2） |
| **C** | **OpenViking 式逻辑 VFS**（`viking://` URI 树 + 向量索引 + L0/L1/L2） | Python SDK / HTTP / CLI | 同上，外加"内容树 ↔ 语义索引树"一致性模型 |
| **D** | **不新增**，只用现有 `grep_chunks`/`list_chunks`/`search_*` | MCP 工具 | 仅检索 |

已排除的两个想法（不必再考虑）：

- **"把 mounter 二进制放 PATH + 提示词教模型挂载"** —— T4 四条独立阻断，不可行；
- **"一组 fs 风格 MCP 工具读原始桶"** —— 原始二进制对 Codex 本不可读；其正确形态就是 B。

---

## 5. 对比矩阵

| | A 挂载 | B MCP 树 | C OpenViking | D 不新增 |
|---|---|---|---|---|
| **R1** 租户隔离 | 每租户实例 + 每租户挂载 + 每租户凭证 | 每租户实例 + **RAGFlow 侧票据/绑定** | 需第二套多租户有状态服务 | 现有边界 |
| **R2** 横向扩展 | 挂载随节点/pod 扇出 | ✅ 无状态 + Kvrocks（现成，mode-8 D11） | 多一个有状态服务要保高可用 | ✅ |
| **R3** 动态生命周期 | ⚠️ 只能租户粒度挂载；有 TTL 陈旧窗口 | ✅ 每请求由 DB 计算，**永远最新** | ⚠️ 异步刷新，可能有部分变更（其文档自承） | ✅ |
| **R4** 运维扇出 | ❌ **随租户数线性增长**（CSI/特权/挂载配置） | ✅ 零新增基建 | ❌ 新增高可用有状态服务 | ✅ |
| **R5** 凭证 | ❌ **存储凭证复制到每个 Codex pod** | ✅ 凭证不出 RAGFlow | ❌ 同 A 或更差 | ✅ |
| **R6** 作用域收窄 | ❌ 挂载静态 ⇒ 连租户内都收窄不了 | ✅ **可按会话收窄**（binding 的 DatasetIDs） | 取决于其 scope 路由 | ✅ |
| **R7** 新增面 | 大 | **小** | 最大（与既有能力重复） | 零 |
| **独有能力** | 全量遍历（= 全字节扫描）/ 任意脚本 / `pandoc` | 浏览 + 摘要 + **索引化检索** | 同 B | 检索 |

---

## 6. 决策

### 6.1 采用 B 作基线

- **R2/R4/R5/R6 全面占优**，且不需要任何新基建；
- **R3**：树由 DB 实时计算，dataset/文档增删改**立刻**反映——挂载方案的 TTL 陈旧窗口不存在；
- **R6**：作用域可按会话收窄，比挂载**更强**（挂载连租户内都隔离不了，见 5.2）；
- **R1**：边界回到 RAGFlow 侧票据/绑定，凭证不出 RAGFlow；
- 复用 mode-8 已验证的 per-turn binding 与 Kvrocks（`internal/mcp/codex.go`）。

### 6.2 不把 A 作基线

A 的机制成立（T1），但在**分布式多租户**下：

- **R4/R5 致命**：每租户一个 Codex 实例本已必要（迁移隔离），再叠"每租户一套挂载配置 + 一份存储凭证"，
  扇出与凭证面**随租户数线性增长**，且凭证落在执行侧；
- **R6 退化**：挂载是 pod 级静态的，只能做到**租户级**可见性（dataset 作挂载点子目录），
  同租户内任意会话可读任意 dataset；B 反而能按会话收窄；
- **R3 需要绕**：为避开"每次 dataset 增删都要改挂载表"，必须把挂载粒度降到租户、让 dataset 活在 key 空间里；
- **路径镜像的代价**：key 镜像 `document.location` ⇒ **文档重命名要搬对象**，否则树陈旧；
- 附带成本：`--prefix` + 只读凭证的 IAM 边界、mounter 选型与 MinIO 兼容性（未验证）、TTL 调参。

### 6.3 不引入 C

- **T5 是决定性的**：C 的三种接口（Python SDK / HTTP / CLI）在沙箱内**一条都不可达**，
  要用它**只能再包一层 MCP** ⇒ 等价于 B，却多一个服务、多一套向量库、多一份一致性维护；
- **能力重复**：C 的每一层在 RAGFlow 都有对应物——
  内容树 ≈ `Document`+`File`+`storage.Storage`；向量索引 ≈ doc engine chunks；
  L0/L1 ≈ RAPTOR(``tree/raptor.go``)/wiki/knowledge_compiler 的摘要产物；
  `find` ≈ `grep_chunks`/`search_*`；`read` ≈ `list_chunks`；`rm`/`mv` 级联 ≈ 现有删除/改名链路；
- **它自己付的代价**：按 URI（路径）索引 ⇒ `mv` 必须同步改 `uri`/`parent_uri`；
  而 RAGFlow 的 chunk 按 `doc_id` 索引，**重命名本来就免费**。
  **值得借鉴**的是它的心智模型：L0/L1 分层 + "内容树 ↔ 索引树"一致性（已吸收为 §7.1 与 §7.3）。

### 6.4 为什么"先 B 后 A"是低后悔路径

**内容层完全共享**：M0（落盘 markdown）+ 图片物化 + L0/L1 摘要，**两条路都要做、且一样**。
差别只在访问层。因此先做 B 不封闭 A —— 将来某租户真要脚本批处理，**加上 A 即可，内容层不返工**。

**同一棵树，两种通道**：树形布局（`<dataset_id>/<原路径>.md` + 同级 `.assets/`）对 B 与 A 一致，
所以提示词描述的路径、以及用户的心智模型都不变。

```
        ┌──────────── 内容层（B/A 共享，且无论如何都要做）────────────┐
        │  M0: ingestion 落盘 markdown + 图片（当前缺失）              │
        │  L0/L1 摘要物化（.abstract.md / .overview.md）              │
        └───────────────────────────┬───────────────────────────────┘
                                    │  同一棵树
              ┌─────────────────────┴─────────────────────┐
              ▼                                           ▼
   B（默认，所有租户）                          A（opt-in，少数租户）
   MCP 工具：tree/ls/read/abstract/find/grep   沙箱外挂载 → 真 POSIX
   凭证不出 RAGFlow                            需 CSI + 租户级前缀只读凭证
```

---

## 7. 基线方案 B 的设计

### 7.1 内容层（M0，共享）

- **落盘 markdown**：捕获点在**所有 parser 侧变换之后、chunker 切分之前**的文档级 `markdown`；
  写入 `MarkdownStore`，key = `<tenant>/<dataset_id>/<原 location>.md`。
- **图片物化**到同级：`<dataset_id>/<原 location>.assets/<img_id>`
  （chunk 图片当前 key 为裸 `chunk_id`，不在该路径空间内）。
- **L0/L1 摘要物化**（**借自 C 的关键一招**）：
  每个 dataset 与目录下写 `.abstract.md`（约一句话）与 `.overview.md`（目录结构/关键信息/使用场景），
  素材来自**已有**的 RAPTOR / wiki / knowledge_compiler 产物。
  价值：模型先 `ls` + 读两个小文件，再决定是否下沉到 L2 全文，**避免把整个语料塞进上下文**。
- **生命周期**：重解析则重写；删文档则删；**删 dataset 清整棵子树**；文档改名则移动对应对象。

`MarkdownStore` 接口（可插拔，**本计划只落一个 adaptor**）：

```go
type MarkdownStore interface {
    Put(ctx, tenantID, datasetID, docID string, md []byte) error
    Get(ctx, tenantID, datasetID, docID string) ([]byte, error)
    Delete(ctx, tenantID, datasetID, docID string) error
}
// 唯一实现 storageMarkdownStore：复用原始文件所用的同一个 storage.Storage。
// ⛔ 其它后端不在本计划范围内：不写代码、不加开关、不做双实现。
```

### 7.2 MCP 工具面（挂在现有桥上）

复用 `internal/mcp/codex.go` 的 per-turn binding 与票据链路，新增一组**目录树导航**工具：

| 工具 | 作用 |
|---|---|
| `tree(path?)` | 列目录（数据集 → 目录 → 文档 + 摘要文件），一次一层 |
| `ls(path)` | 同上，条目形态（可选，与 `tree` 合并） |
| `read(path)` | 读文档 markdown；读 `.abstract.md`/`.overview.md` 同样走它 |
| `abstract(path)` / `overview(path)` | L0 / L1 便捷读取 |
| `find(query, path?)` | **在子树内**做语义检索（包装现有检索实现） |
| `grep_corpus(regex, path?)` | 对 markdown 做**索引化**正则检索（复用 `GrepService`），**只回命中片段**；**不是**扫描对象存储 |

- `grep_corpus` **只有建了 markdown 段索引才成立**（见 §2.4）：
  请求期扫桶是 O(语料) 的操作，不能放进交互式工具。
  第一优先的替代是**给 `grep_chunks` 补 `path` 与行号**（零新索引）；
  只有实测证明 chunk 级正则漏掉了 markdown 的关键结构（表格边界、代码围栏、重复标题）时，
  才为 markdown 建段索引——按行对齐切窗口（≤ 8KB，避开 Lucene 32,766 字节的 term 上限），
  与 chunk 索引同为**派生数据、可重建**。
- 工具参数里的 `path` 只能落在 binding 的 dataset 集合内（与现有 `resolveDatasetScope` 同语义：**只能收窄、不能放宽**）。
- 无 scope 时返回明确错误，绝不用空作用域检索（沿用现有守卫）。

### 7.3 作用域与多租户

- 边界 = **每租户一个 app-server**（mode-8 已定的安全前提）+ RAGFlow 侧票据/绑定；
- **可按会话收窄**：binding 携带本会话的 `DatasetIDs`，树与检索都受其约束（**优于 A**）；
- 凭证（对象存储）**始终只留 RAGFlow**；
- 无状态、可多副本（binding 在 Kvrocks，mode-8 D11）。

### 7.4 提示词

说明语料以**解析后 markdown** 形式提供，路径形如 `<dataset_id>/<原路径>.md`，
每个目录有 `.abstract.md` / `.overview.md` 供快速判断，**先摘要后全文**；
并界定与检索工具的分工（§13）。提示级别，**不是边界**。

---

## 8. opt-in 方案 A 的设计（真 POSIX）

### 8.1 形态

- **沙箱外**由 CSI（Mountpoint 有官方 `s3.csi.aws.com`；GeeseFS 侧有 `k8s-csi-s3`）或 initContainer 一次性挂载：
  `mount-s3 --read-only --prefix <tenant>/ s3://<md-bucket> /mnt/ragflow`；
- **挂载粒度 = 租户**，dataset 是挂载点下的子目录（否则 dataset 增删要求改挂载表）；
- 挂载点在**普通路径**（不放 `/dev` 下，T1 对照）；
- 模型**只用绝对路径**；不设 `WithThreadCWD` / `codex.work_dir`（T2/T3）；
- 只读。

### 8.2 何时才该上

**仅当明确需要"对原文跑任意工具链"**：脚本批处理、`pandoc` 转换、把语料当代码仓库处理。
且须同时满足：能付 CSI/特权成本；接受**租户级**（非会话级）可见性；接受存储凭证落在执行侧并做好 IAM 前缀级只读。

### 8.3 已知代价（必须写进取舍）

| 代价 | 说明 |
|---|---|
| 凭证落在 Codex pod | 必须**前缀级只读**（`arn:…:<md-bucket>/<tenant>/*` 仅 `GetObject`）；`--prefix` 只限制工具**列什么**，不限制凭证**能读什么** |
| 只能租户级可见 | 同租户内任意会话可读任意 dataset（POSIX 无调用者身份） |
| **检索只能是全扫** | `grep -r` = N 次 GET + O(全部字节)，本地 cache 只加速重复读（§2）；所以 A 只适合"按路径读 / 批量脚本"，不适合交互式检索，也不能替代 B 的检索工具 |
| 路径镜像 | key 镜像 `location` ⇒ **重命名要搬对象**，否则树陈旧 |
| 陈旧窗口 | mounter 元数据 TTL 决定"新建多久可见、删除多久生效"，需调短、不开数据缓存 |
| 选型未验证 | Mountpoint **未声明支持非 AWS 兼容服务**（MinIO 需 `--endpoint-url`+`--force-path-style` 实测）；读路径**不支持 `mmap`**、inode 号不稳定 |

---

## 9. 安全模型

- **内容面**：只暴露 markdown 与摘要，原始二进制从不进入模型可见范围。
- **基线 B**：凭证不出 RAGFlow；边界在 RAGFlow 的 binding/票据，可多副本。
- **opt-in A**：边界下移到**存储策略（IAM 前缀级只读）**——这是该方案最需要评审的一点。
- **多租户**：唯一边界是**每租户一个 app-server**（T2 证明沙箱不限制读，cwd/提示词均非边界）。
- **残余风险**：pod/进程被攻陷时，B 泄露的是该租户的 markdown（走 RAGFlow 权限链）；
  A 泄露的是该租户的 markdown（走 IAM）。两者都不触及原始文件桶——这正是"只暴露 markdown"的价值。

---

## 10. 验证

已实测：**T1**（挂载在沙箱内可见）、**T2**（cwd 非读屏障）、**T3**（cwd 语义）、
**T4**（沙箱不能挂载/联网）、**T5**（沙箱无任何主动通信通道）。见 §3。

待补：

| # | 内容 | 服务于 |
|---|---|---|
| **T6** | markdown 落盘：解析后内容一致；重解析覆盖、删文档、**删 dataset 清子树** | B/A 内容层 |
| **T7** | 图片联动：markdown 里的引用能在 `.assets/` 下解析到 | B/A 内容层 |
| **T8** | 摘要物化：`.abstract.md`/`.overview.md` 由现有 RAPTOR/wiki 产物生成，且质量可用 | B |
| **T9** | 树与 scope：越界 `path` 必须报错而非返回数据；无 scope 时明确失败 | B |
| **T10** | `grep_corpus` 在大语料上的延迟与命中裁剪 | B |
| **T11** | （仅 A）Mountpoint 在 **MinIO** 上的可用性 —— **Go/No-Go** | A |
| **T12** | （仅 A）前缀级只读凭证**无法**读到原始文件桶 | A |
| **T13** | （仅 A）目标 k8s 集群上确认 CSI/initContainer 的挂载传播到主容器 | A |

---

## 11. 里程碑

| # | 内容 | 出口条件 |
|---|---|---|
| **M0** | 内容层：落盘 markdown + 图片 + 摘要物化 | T6、T7、T8 通过 |
| **M1** | B：树导航工具 + `grep_corpus` + scope 强制 | T9、T10 通过 |
| **M2** | B：提示词（树约定 + 摘要优先）与端到端 | 端到端可用 |
| **M3** | （opt-in，按需）A：mounter 选型 → 专用桶 + 前缀凭证 → k8s 挂载 | T11、T12、T13 通过 |

M0 是唯一的重活，且为 A/B 共享。M3 不阻塞前两项。

---

## 12. 未决

1. **切分前 markdown 里图片的形态**（内联 base64 / markdown 引用 / 占位符）——决定 assets 能否由引用直接驱动。
2. **"文档 markdown"的权威快照点位**：多 parser 输出（deepdoc / vision enhancement 改写图片描述）之后、chunker 之前的哪一个。
3. **摘要生成的触发时机与粒度**：目录级 `overview` 是随解析更新还是批量重建；空目录要不要占位。
4. **`grep_corpus` 的实现面**：复用 `grep_chunks` 的多少、是否要独立索引。
5. **树是否要暴露"未解析完成"的文档**（只有原文、还没有 markdown 时）。
6. （仅 A）mounter 选型、共享 md 桶 vs 每租户一桶。
7. **与检索工具的分工如何在提示词里界定**（§13）。

> 已决：路径层用 `dataset_id`（重命名安全）；内容源 = ingestion 落盘；命名 = 原路径 + `.md`；图片用同级 `.assets/`。

---

## 13. 与 MCP 检索工具的关系

B 与现有检索工具是**同一条通道上的互补面**，不是替代：

| 需求 | 走哪条 |
|---|---|
| **有结构地浏览**：先看有什么、再决定读什么 | `tree`/`ls` + L0/L1 摘要（B 新增） |
| **对全量原文做过滤** | `grep_corpus`（B 新增，**索引化**；无索引时先扩 `grep_chunks`，见 §7.2） |
| **读某篇全文** | `read`（B 新增）或 `list_chunks`（现有） |
| **按相关度召回并引用** | `grep_chunks` / `search_semantic_chunks` / `search_bm25_chunks`（现有） |

需要注意的张力：语料有两份"真相"——**落盘 markdown**（解析后、未切分）与
**doc engine 里的 chunk**（切分并可能被 chunker 改写）。提示词里应说明：
markdown 是"文档全文视图"，chunk 是"检索与引用视图"。

**方案 A 的定位**：在这之上再加一层**内核文件系统**，服务"任意脚本/工具链处理原文"这一具体需求；
若该需求不出现，A 不必上（§6.2、§8.2）。
