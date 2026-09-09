# RAGFlow Performance Benchmark v1

## Environment Setup

本目录可脱离 RAGFlow 主工程单独部署，只依赖 3 个第三方包：

| 依赖 | 用途 |
|------|------|
| `requests` | 调用 RAGFlow REST API |
| `PyYAML` | 解析 `config/benchmark_config.yaml` |
| `numpy` | 计算延迟分位数（P50/P90/P99） |

其余均为标准库。资源监控通过 `subprocess` 调用 **`docker stats`**，因此需要本机装有 docker CLI
（没有 docker 时监控会静默降级，各项 CPU/内存指标为 0，不影响压测本身）。

```bash
cd test/benchmark_v1

# 用 uv 创建 .venv 并安装依赖（不安装本目录自身，它只是一个运行环境，不是可安装包）
uv sync

# 运行脚本（也可以用 source .venv/bin/activate 后直接 python ...）
uv run python scripts/retrieval_test.py --test-type baseline --kb-id <KB_ID>

# 可选：ruff 做代码检查
uv run ruff check .
```

> `uv sync` 会生成 `uv.lock`（建议提交以保证复现）。
> 所有脚本都是相对当前工作目录读取 `config/` 与写入 `results/`，
> 因此**必须在 `test/benchmark_v1/` 目录下执行**。

## Directory Structure

```
test/benchmark_v1/
├── config/
│   └── benchmark_config.yaml    # 配置文件
├── pyproject.toml               # uv 环境定义（可独立部署）
├── scripts/
│   ├── ingestion_test.py        # 批量入库压测脚本
│   ├── retrieval_test.py        # 纯文字检索压测脚本
│   ├── mixed_workload_test.py   # 混合负载测试脚本
│   ├── long_run_test.py         # 长稳测试脚本
│   └── concurrent_user_test.py  # 并发用户场景模拟脚本
├── utils/
│   ├── config_loader.py         # 配置加载工具
│   ├── api_client.py            # RAGFlow API 客户端
│   ├── metrics_collector.py     # 指标收集工具
│   └── monitor.py               # 系统资源监控工具
├── results/                     # 测试结果输出目录
├── test_data/                   # 测试数据目录（需准备）
│   ├── warmup/                  # 预热小文件
│   ├── small/                   # 小PDF (1-2MB, 5-10页)
│   ├── medium/                  # 中PDF (3-5MB, ~24页)
│   ├── large/                   # 大PDF (>5MB, ~48页)
│   └── chunks/                  # 检索测试PDF文件 + queries.json
└── README.md
```

## Configuration

**配置文件：`config/benchmark_config.yaml`**

| 配置项 | 说明 | 默认值 |
|--------|------|--------|
| `ragflow.base_url` | RAGFlow 服务地址 | `http://127.0.0.1:9380` |
| `ragflow.api_key` | API Key | - |
| `ragflow.timeout` | 请求超时(秒) | `300` |
| `knowledge_base.kb_id` | 知识库 ID | `auto` |
| `test_data.chunks_pdfs_dir` | 检索测试PDF目录 | `./test_data/chunks` |
| `test_data.query_file` | 查询语句JSON文件 | `./test_data/chunks/queries.json` |

**注意**：各脚本对该配置项的处理方式不同：
- `retrieval_test.py` / `mixed_workload_test.py` / `long_run_test.py` / `concurrent_user_test.py`
  **不会创建知识库**：`kb_id` 必须通过 `--kb-id` 或本配置项指定一个**已存在**的知识库，
  未配置（含 `auto` / 留空）或知识库不可访问时，脚本会报错退出。
- `ingestion_test.py` 不支持 `--kb-id`：本配置项为 `auto` / 留空时自动创建新知识库，
  填写具体 ID 时复用已有知识库。

## Test Data Preparation

### 1. 预热文件 (`test_data/warmup/`)
放置 3-5 个小 PDF 文件（<1MB），用于正式测试前的预热

### 2. 入库测试文件
| 类型 | 大小 | 页数 | 目录 |
|------|------|------|------|
| 小文件 | 1-2MB | 5-10页 | `test_data/small/` |
| 中文件 | 3-5MB | ~24页 | `test_data/medium/` |
| 大文件 | >5MB | ~48页 | `test_data/large/` |

### 3. 检索测试数据 (`test_data/chunks/`)
- **PDF 文件**：放置用于检索测试的 PDF 文件（会被解析生成 chunks）
- **queries.json**：查询语句文件，**其中的查询是针对本目录的 PDF 生成的**

> 本目录的 PDF 灌出来的知识库，就是 `retrieval_test.py` / `mixed_workload_test.py` /
> `long_run_test.py` / `concurrent_user_test.py` 要用的 `<KB_ID>`。

`queries.json` 格式：
```json
{
  "queries": [
    {"id": 1, "text": "查询语句1"},
    {"id": 2, "text": "查询语句2"}
  ]
}
```

## Running Tests

**每次正式测试前请先执行 Step 0 的预热**，否则首批请求会包含模型加载、缓存冷启动等一次性开销，
导致入库/检索的首批耗时明显偏高。

```bash
cd test/benchmark_v1

# ============================================
# Step 0: 预热（每次正式测试前必做）
# ============================================

# 上传 test_data/warmup/ 下的小文件（目录为空时自动回退到 small/ 的前 3 个文件），
# 用于拉起模型、填充缓存、建立连接池，其耗时不计入任何统计分析。
python scripts/ingestion_test.py --warmup

# ============================================
# Step 1: 准备知识库并灌入数据
# ============================================

# 必须用 --file-type chunks 把 test_data/chunks/ 里的 PDF 灌入知识库。
# 该知识库就是 Step 2 / Step 3 要用的 <KB_ID>：
# queries.json 里的查询是针对 chunks 目录中的文档生成的，
# 用其他目录（small / medium / large）生成的知识库，检索/混合/长稳/并发用户测试的结果没有意义。
#
# ingestion_test.py 不支持 --kb-id，只读取配置文件 knowledge_base.kb_id：
#   - 留空 / auto  -> 自动创建新知识库（脚本会打印新 KB 的 ID，记录下来供 Step 2/3 使用）
#   - 填写具体 ID  -> 复用已有知识库
python scripts/ingestion_test.py --test-type baseline --file-type chunks

# 入库压测本身可以用其他目录的 PDF（其结果只用于入库指标，不供 Step 2/3 使用）
python scripts/ingestion_test.py --test-type baseline --file-type medium
python scripts/ingestion_test.py --test-type gradient --file-type small
python scripts/ingestion_test.py --test-type stability --concurrency 4 --duration 30

# ============================================
# Step 1.1: 把 chunks 知识库的 ID 写进配置文件（推荐）
# ============================================
# 配置后再跑 Step 2 / Step 3 的脚本就不需要每条命令都带 --kb-id 了。
# config/benchmark_config.yaml:
#
#   knowledge_base:
#     # kb_id: "auto"                          # 注释掉 auto
#     kb_id: "c9e621ceab4811f1a5c9d74defc7f02d"   # 用 chunks 文档构建的知识库 ID
#
# 也可以临时用环境变量指定配置文件：
#   BENCHMARK_CONFIG=/path/to/benchmark_config.yaml python scripts/retrieval_test.py ...

# ============================================
# Step 2: 检索压测
# <KB_ID> = Step 1 中用 --file-type chunks 灌入 chunks 文档后得到的知识库 ID
# 若已在配置文件里填好 knowledge_base.kb_id，下面的命令均可省略 --kb-id
# ============================================

# 基准测试
python scripts/retrieval_test.py --test-type baseline --duration 5 --concurrent 1 --kb-id <KB_ID>

# 梯度测试
python scripts/retrieval_test.py --test-type gradient --concurrent-levels 1,2,4,8,16 --kb-id <KB_ID>

# 稳定测试
python scripts/retrieval_test.py --test-type stability --concurrent 10 --duration 20 --kb-id <KB_ID>

# ============================================
# Step 3: 其他测试类型
# 同样使用 Step 1 中 chunks 生成的知识库 ID（queries.json 的问题针对 chunks 文档）
# ============================================

# 混合负载测试
python scripts/mixed_workload_test.py --retrieval-concurrent 10 --ingestion-concurrent 4 --duration 3 --kb-id <KB_ID>
python scripts/mixed_workload_test.py --test-matrix --kb-id <KB_ID>

# 长稳测试
python scripts/long_run_test.py --test-type retrieval --duration 240 --concurrent 10 --kb-id <KB_ID>
python scripts/long_run_test.py --test-type ingestion --duration 240 --concurrent 4 --kb-id <KB_ID>

# 并发用户场景模拟
python scripts/concurrent_user_test.py --concurrent-users 20 --duration 3 --kb-id <KB_ID>
python scripts/concurrent_user_test.py --run-matrix --drain-timeout 10 --kb-id <KB_ID>
```

> `--drain-timeout`：测试窗口到点后，先等待所有仍在飞行中的请求（通常是上传后仍在等待解析的请求）返回，
> **全部结束后才统计指标**，避免遗漏慢请求导致样本偏小。等待上限由该参数控制（分钟，默认 `10`）。
> 超时会打印告警（说明这些在途请求未计入统计）并继续；设为 `0` 可关闭等待。

> **知识库选择**：除 `ingestion_test.py` 外，所有脚本的 `<KB_ID>` 都必须是
> **用 `test_data/chunks/` 里的 PDF 构建的知识库**（Step 1 的 `--file-type chunks`）。
> `queries.json` 中的查询是针对这些文档生成的，用 `small` / `medium` / `large` 目录
> 生成的知识库跑检索类测试，召回内容和指标都不可比。
>
> 除 `ingestion_test.py` 外，所有脚本都要求知识库事先存在：未传 `--kb-id` 且配置文件未配置时直接报错退出，
> 传入的 ID 不可访问时同样报错退出。

**参数说明：**
- `--kb-id`: 指定知识库 ID
  - `retrieval_test.py` / `mixed_workload_test.py` / `long_run_test.py` / `concurrent_user_test.py`：
    命令行参数优先，未传时回退到配置文件 `knowledge_base.kb_id`。
    两者都没有（或为 `auto` / 留空）时脚本报错退出。脚本只做存在性校验，
    **不会创建知识库**；不支持 `auto`。应传入 chunks 文档构建的知识库 ID。
  - **推荐做法**：把 chunks 知识库的 ID 写进 `config/benchmark_config.yaml` 的
    `knowledge_base.kb_id`，之后 Step 2 / Step 3 的所有命令都不用再重复写 `--kb-id`。
    `ingestion_test.py` 也读同一项：填具体 ID 时复用该知识库，需要重新灌数据时把它改回 `auto`
    就会新建知识库。
  - `ingestion_test.py`：不支持该参数，知识库 ID 只能来自配置文件 `knowledge_base.kb_id`；
    为 `auto` / 留空时自动创建，填写具体 ID 时复用已有知识库。
- `--drain-timeout`（仅 `concurrent_user_test.py`）：测试窗口结束后等待在途请求收尾的
  超时时间（分钟，默认 `10`，`0` 表示不等待）。收尾完成后才统计指标。

## Test Scripts Overview

| 脚本 | 测试目标 | 主要指标 |
|------|----------|----------|
| `ingestion_test.py` | 批量入库压测 | 入库吞吐量、端到端延迟、各阶段耗时、成功率 |
| `retrieval_test.py` | 纯文字检索压测 | QPS、P50/P90/P99延迟、成功率 |
| `mixed_workload_test.py` | 混合负载测试 | 读写混合吞吐量、延迟、性能衰减 |
| `long_run_test.py` | 长稳测试 | 内存趋势、队列稳定性、性能一致性、成功率 |
| `concurrent_user_test.py` | 并发用户场景模拟 | TTFR、并发承载、会话吞吐量、端到端成功率 |

## Ingestion Stage Breakdown

入库阶段耗时分解（从 `progress_msg` 解析）：

| 阶段 | 区间定义（取自 `progress_msg` 的 `HH:MM:SS` 时间戳） |
|------|------|
| `upload` | 客户端文件上传耗时 |
| `parse` | `Start to parse.` → `Finish parsing.` |
| `embedding` | `Embedding chunks (Xs)` 完成时刻往前推 X 秒 |
| `indexing` | `Indexing done (Xs)` 完成时刻往前推 X 秒 |
| `chunk` | 未单独统计 |

**并行修正**：一个文档会被拆成多个 `Page(a~b)` 分段**并行**解析，各分段的耗时区间互相重叠。
因此上述各阶段的耗时不是简单相加，而是把所有分段的区间做**合并去重叠**后再统计，
得到的是真实墙钟耗时。例如两段各自耗时 75s 且完全并行，合计仍按 ~75s 计，而非 150s。

若 `progress_msg` 完全没有时间戳（异常情况），则回退为各分段上报时长求和，
此时并行场景下的耗时会偏大。

## Output

测试结果保存在 `results/` 目录下：
- `*_metrics.json` - 聚合指标
- `*_raw.json` - 原始数据
