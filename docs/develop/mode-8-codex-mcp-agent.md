# mode 8 开发计划：Codex Agent（RAGFlow 作为前端与共享 Codex 的中介，经 MCP 访问 agent_rag 工具）

状态：草案（待评审）
作者：zhichyu
日期：2026-10-09
关联代码：`internal/service/chat_pipeline.go`、`internal/agentic_rag/`、`internal/mcp/`
外部依赖：[`codex-app-server-go`](https://github.com/yuzhichang/codex-app-server-go/tree/feat/schema-alignment-and-coverage)（本地 checkout 于 `~/github.com/Zealbase/codex-app-server-go`）

---

## 1. 目标

在现有 session mode 0–7 之外新增 **mode 8**。该模式在对话阶段：

1. **RAGFlow Go 后端作为中介**：连接一个**共享的、监听 WebSocket 的 Codex app-server**（下称“共享 Codex”），代表前端发起并驱动一次 Codex turn。
2. **Codex 通过 MCP 访问 agent_rag 检索工具**（`grep_chunks` / `list_chunks` / `search_bm25_chunks` 等），而不是由 RAGFlow 自己跑 ReAct 循环。
3. 对前端而言，mode 8 与 mode 5/6/7 完全同构：一次对话请求进来，`AsyncChatResult` 增量流出去，最终带引用与引用计数。

一句话：**mode 5/6/7 是“RAGFlow 用自己的模型 + 自己的工具循环”；mode 8 是“Codex 当大脑，RAGFlow 把 agent_rag 工具经 MCP 借给它”。**

### 非目标（本期不做）

- 不改造 Codex app-server 本体，不改其协议；只作为 WS 客户端接入。
- 不把 mode 8 暴露给 OpenAI 兼容入口（`internal/service/openai_chat.go`）——与 mode 5/6/7 一致，`reasoning` 不从这里转发。
- 不实现 Codex 端的文件系统/命令执行审批后端（审批姿态见 §5.5：**accept MCP 检索工具、decline 命令/文件**）。
- 不删除 mode 5/6/7；它们与 mode 8 并存，由 level 选择。

---

## 2. 现状：session mode 0–7

“session mode”是聊天请求里的 **数值 `reasoning` 级别**，由 `resolveReasoningLevel`（`internal/service/chat_pipeline.go:5645`）从请求 kwargs 优先、dialog `prompt_config` 次之解析出来。分发只有一个入口：`selectAgenticEngine(level, hasKBs)`（`chat_pipeline.go:5618`），在 `AsyncChat` 早期（`chat_pipeline.go:355`）调用。

| Level | 含义 | 引擎 | 主要代码 |
|---|---|---|---|
| 0 | 关闭 | 普通 RAG / solo | pipeline |
| 1–4 | low/medium/high/ultra | harness graph | `internal/rag/agentic-rag/`，`harnessModeForLevel`（`chat_pipeline.go:5680`） |
| 5 | react（eino ReAct，自探索语料库后直接作答） | eino ReAct explorer | `internal/service/agentic_pipeline.go:65` `agenticRag` |
| 6 | planer-explorer-auditor（先规划问题分解，再在交付门禁下探索） | eino ReAct explorer + delivery gate | `chat_pipeline.go:5695` `planExplorerAuditorRag`、`internal/agentic_rag/plan_agent.go` / `delivery_gate.go` |
| 7 | IterSynth（每轮重写研究摘要） | 角色解耦、摘要驱动循环 | `internal/service/iterative_pipeline.go:55` `iterativeSynthesis`、`internal/agentic_rag/iterative.go:315` `RunIterative` |
| 8 | coding-agent（Codex） | 外部共享 Codex（经 MCP 借 agent_rag 工具） | `internal/service/codex_pipeline.go:231` `codexAgent`、`internal/codexagent/` |

关键约束（新增 level 必须遵守）：

- 常量与分发：`reasoningLevelAgentic=5`、`reasoningLevelPlanExplorer=6`、`reasoningLevelIterative=7`、`reasoningLevelCodex=8`（`chat_pipeline.go:5548/5555/5566/5577`）、`agenticEngine` 枚举（`chat_pipeline.go:5581`）、`selectAgenticEngine`（`chat_pipeline.go:5618`）。
- `harnessMaxLevel=4` 同时约束 `harnessModeForLevel` 与 `reasoningNeedsAgenticGraph`（`chat_pipeline.go:5680/2312`）。**任何新 level 不得落到 harness 的 `ultra` 分支。**
- 无 KB 对 5/6/7 的处理：`selectAgenticEngine` 在 `!hasKBs` 时返回 `""`，因为 5/6/7 都对 `kb_id` 作用域做引用解析，空作用域会让引用从任意 KB 解析出来。
- **mode 8 不受“无 KB 则无引擎”约束**：Codex 是通用助手，无 KB 也能对话/推理，仍可访问。此时 MCP 检索类工具（`grep_chunks` 等）在**没有 `kb_id` 作用域的调用上返回错误**，而不是回退到空/默认作用域去检索——由 Codex 读取工具错误并据此应对。因此 `selectAgenticEngine` 必须在 `!hasKBs` 早退**之前**处理 mode 8（见 §4.1）。
  - 相应地在 `AsyncChat` 里，dispatch（`chat_pipeline.go:355`）位于 solo 快路径（`chat_pipeline.go:358` 的 `!hasKBs && !useWebSearch`）之前，所以 level==8 且无 KB 时仍会进 mode 8 引擎，不会被短路成普通 LLM 回答。
- 前端 `web/src/components/message-input/next.tsx::normalizeThinkingLevel`（第 59 行，正则 `/^[0-8]$/`）与 `thinkingOptions`（第 168 行）是 level 变得用户可见的唯一位置；漏改会让存储的级别静默降级为 1。

### 2.1 agent_rag 工具现状（MCP bridge 要复用的东西）

模板工具注册表在 `internal/agentic_rag/config.go:86` `toolRegistry()`：

| 工具 | 构造 | 依赖 |
|---|---|---|
| `search_chunks` | `NewSearchChunksTool(tenantID, datasetIDs)` | `runtime.GetRetrievalService()` |
| `search_bm25_chunks` | `NewSearchBm25ChunksTool(...)` | `runtime.GetBm25Service()` |
| `search_semantic_chunks` | `NewSearchSemanticChunksTool(...)` | `runtime.GetRetrievalService()`（VectorOnly） |
| `grep_chunks` | `NewGrepChunksTool(...)` | `runtime.GetGrepService()` |
| `list_chunks` | `NewListChunksTool(...)` | 读 chunk（`dao.DB`） |
| `think` / `todo_write` / `run_javascript` / `check_decomposition` | 无作用域 | 纯计算 |

> 上表是 agent_rag 现有工具全集。**mode 8 的 MCP bridge 只暴露其子集**：`search_chunks`、`think`、`todo_write`、`run_javascript`、`check_decomposition` 均**不暴露**（见 D7/D9）；`web_search` 只在会话启用了网络搜索时暴露（逐轮决定，见 D9/§5.7③）。

- 检索实现的核心是 `runLocateSearch`（`internal/agentic_rag/locate_search.go:68`），tenant/dataset 作用域在构造时注入，**运行时从 `ctx` 取 ledger**（`internal/agentic_rag/agent.go` 里 `withServedLedger`）。
- `tenantID` 必须是 **chat 的 owning tenant**（`chat.TenantID`），不是请求用户 ID；索引名由 tenant 构造，用错会稳定返回空结果。

---

## 3. mode 8 目标架构

```
              ┌────────────────────────────────────────────────────────────┐
 前端/BFF ──▶ │  RAGFlow Go 后端 (mode 8 engine)                            │
 (AsyncChat)  │                                                            │
              │  1. 解析 level==8, 取 chat.TenantID / KBIDs / session id        │
              │  2. 铸造 thread 级 MCP 票据(token -> tenant/KB scope)          │
              │  3. codex SDK 客户端(WS) ──▶ 共享 Codex app-server           │
              │       StartThread(建)/ResumeThread(复用) + RunStreamed       │
              │       线程 config override: mcp_servers.ragflow = {         │
              │         url: http://<ragflow>/mcp/codex/<token> }           │
              │  4. 消费 Codex 事件(+恢复事件) -> AsyncChatResult 增量        │
              │  5. 收尾:引用/工具计数 -> 最终 AsyncChatResult               │
              └───────────────┬────────────────────────────┬───────────────┘
                              │ WS (JSON-RPC)              │ 事件
                              ▼                            │
              ┌───────────────────────────┐                │
              │ 共享 Codex app-server      │                │
              │  (WebSocket, 多租户共享)   │                │
              │  ── 调 MCP tools ──────────┼────────────────┘
              └───────────┬───────────────┘
                          │ MCP (streamable HTTP)
                          ▼
              ┌───────────────────────────────────────────┐
              │ RAGFlow MCP bridge (internal/mcp 扩展)     │
              │  grep / bm25 / semantic / list_chunks      │
              │  -> 检索实现 / grep / read chunk           │
              │  scope = 票据解析出的 tenant + datasets    │
              └───────────────────────────────────────────┘
```

### 三方角色

- **共享 Codex app-server**：长期运行、监听 WebSocket、多租户共享。它自带推理循环，是真正的“大脑”。
- **RAGFlow Go 后端**：既是 Codex 的 WS 客户端（驱动 thread/turn、消费事件），又是检索工具的 MCP 服务端（把 agent_rag 工具按每次会话的作用域暴露给 Codex）。
- **前端**：不变，仍是 `AsyncChat` 的消费方。

### 关键设计决策

| # | 决策 | 理由 |
|---|---|---|
| D1 | MCP 传输选 **streamable HTTP**（不是 stdio） | 共享 Codex 可能远程；stdio MCP server 必须与 Codex 同机同进程环境，无法承载 per-thread 的 tenant/KB 作用域。HTTP 端点可由 RAGFlow 直接暴露，作用域随票据走。 |
| D2 | 作用域用 **KV 的 per-session handle（无权限）+ per-turn binding** 传递，**binding 同时就是 per-session turn 锁** | 线程 URL 里只放一个**不可猜、不携带权限的 per-session handle**（`HandleForSession`，按 `(tenant, session)` 幂等铸造）；真正的授权是本轮 turn 开始时 `Bind(CodexScope{TenantID, DatasetIDs, UserID})`、turn 结束时 `Unbind` 的 **binding**。因此授权是 **per-turn**：handle 单独不授权，turn 之外一律 403。`Bind` 是一次 KV **原子 `SetNX`**（绑不上就是 `ErrCodexTurnBusy`），所以它**天然充当同会话的 turn 锁**——互斥跨进程/多副本成立，且无需任何进程内锁。handle 与 binding 都存 **Kvrocks**（D11）。 |
| D3 | 通过 **thread config override** 注入 MCP server（**已实测确认**），**建/恢复都注入**；但 **MCP 绑定只在建 thread 时生效** | SDK 的 `WithThreadConfigOverride`（`thread.go:86`）经 `thread/{start,resume}.config` 深合并。实测（本地 codex 0.162.0）：`thread/start` 的 `mcp_servers.<name>` overlay 生效（codex 会对该 URL 发起 MCP handshake，`mcpServerStatus/list` 可见）；**`thread/resume` 的 overlay 不会重载 `mcp_servers`**（resume 时改 URL，origin 不变）。`Client.ResumeThread` 丢弃 opts 的问题已在本 fork 修复（commit `412c6d4`），resume 会转发的字段对 model/sandbox/instructions 等有效。**该限制现已无害**：URL 里的 handle 每 session 稳定且不携带权限，授权来自 per-turn binding，故 resume 不重载 `mcp_servers` 不影响作用域；当前实现仍把 KB 集合折进 `scope_fingerprint`（KB 变化仍重建 thread），去掉它是可选后续。 |
| D4 | **创建 Codex thread 时，模型由 dialog 配置的模型决定** | 与 dialog 的 `llm_id` 一致：用 `WithThreadModelProvider(providerID)` + `WithThreadConfigOverride("model_providers.<id>", {...})` 声明并选用 dialog 解析出的 provider/model（见 SDK `examples/custom-provider`）。模型是创建 thread 时的固定选择，不随 turn 变更。 |
| D5 | 引用回填由 **thread 的 system prompt** 约定，交给 Codex 实现 | 在创建 thread 时（`WithThreadBaseInstructions` / `WithThreadDeveloperInstructions`）说明引用要求：每条有据可依的行**以 provenance tuple 结尾**——语料 `(doc: …, doc_id: …, chunk_id: <id>, snippet: "…")`，web `(source: web, url: <url>, title: <title>, snippet: "…")`。后端据 `chunk_id` 校验语料（KB 作用域）、web 条目信任模型，并把 tuple **原地替换**为 `[ID:N]`；模型自己不写标记（见 §5.4 / D12）。 |
| D6 | **Codex thread 与 RAGFlow chat session 一一对应** | 一个 chat session 一个 thread，持久映射，跨 turn 复用与 resume；见 §5.2。 |
| D7 | **不向 Codex 暴露 `think` / `todo_write` / `run_javascript` 等玩具工具** | Codex 内置的 plan / reasoning 能力远强于这几个工具，重复暴露只会造成工具选择冲突。 |
| D8 | **mode 8 不因无 KB 而禁用；无 scope 时 MCP 检索工具返回错误** | Codex 是通用助手，无 KB 也能对话/推理。检索类工具在无 `kb_id` 作用域时返回 `IsError`，交由 Codex 读取错误后应对，绝不用空作用域检索。 |
| D9 | **不暴露 MCP 工具 `search_chunks`；`web_search` 按会话设置逐轮决定** | 用户决定（2026-10-09）。检索工具保留：`search_semantic_chunks` / `search_bm25_chunks` / `grep_chunks` / `list_chunks`。`web_search` 是**会话能力**而非语料工具：binding 携带 `ChatID` + `WebSearch`（= 会话配了 provider 且本次请求开启 internet，即 `shouldUseWebSearch`），桥在处理请求时按 `ChatID` 从 DB 读 `chat.PromptConfig` 解析 provider（见 §5.7③），解析成功才把 `web_search` 加入工具集——凭据只留在 DB，绝不进 KV 里的 binding。不满足时不暴露（与进程内引擎「缺席的能力绝不向模型广告」一致）。Codex **自身**的服务端 `web_search` 则在每个 thread 上被关闭（`web_search="disabled"`，因其由服务商计费，见 §5.5），故模式 8 的联网完全受本开关控制。 |
| D10 | **对话历史重放给该 Codex thread** | RAGFlow 保存的会话历史在 thread（首建或重建）时经 `thread/inject_items`（`ThreadInjectItems`，`thread.go:500`）注入 thread 的模型可见历史，使 Codex 具备多轮上下文，不依赖 Codex 侧持久记忆。 |
| D11 | handle/binding 存 **Kvrocks**（`mcp.KV`），**不是进程内存** | RAGFlow API（mint handle、bind scope）与服务 `/mcp/codex` 的进程可能不同（独立 MCP server，或 API 多副本），进程内 map 会让 resolve 落空。`internal/mcp` 只依赖一个注入的 `KV` 接口；Kvrocks 适配器在 service 层（`codex_kv.go`），boot 时 `SetCodexKV` 安装。键：`ragflow:codex:handle:<token>`、`ragflow:codex:sess:<tenant>\x00<session>`、`ragflow:codex:bind:<token>`。键 TTL 统一 **10 分钟**；保活**不靠全局 keeper**，而是由 **binder 在每个 turn 内**起一个 goroutine（`Bind` 成功时启动、`Unbind` 时退出，每 1 分钟 `Renew` 一次 handle TTL 与 binding 租约）。因此 handle/binding 只在 turn 期间被续期，turn 之间不续期：崩溃的 turn 的 binding 会在 ≤TTL 内到期释放锁，服务停摆后遗留键也会自行消失。 |
| D12 | **去掉工具台账 / 引用白名单**；引用改由 **KB 作用域校验** | 用户决定（2026-10-12）：bridge 不再逐 chunk 记台账。turn 末把模型写的 `chunk_id` 经 `buildAgenticReferenceWithWhitelist(..., nil)` → `fetchChunksByIDs(tenantID, datasetIDs, cited)` 校验存在性与归属后再生引用。代价：mode 8 不再产出 `tool_call_counts` / `retrieved_docids` / `deep_read_*` / `shallow_read_*`（benchmark 遥测；Web UI 不消费）。 |
| D11 | **无 session id 的入口退化为新建 thread** | `openai_chat.go` / `bot_completion.go` 未提供 session id，无法做 1:1 映射：该次请求新建一次性 thread（StartThread + 重放请求携带的历史 + 跑 turn + 弃用），不查/不写映射、不 resume。 |

---

## 4. 组件与改动清单

### 4.1 调度层（`internal/service/chat_pipeline.go`）

1. 新增常量 `reasoningLevelCodex = 8`，注释说明它像 5/6/7 一样命名“引擎”而非 harness 深度。
2. `agenticEngine` 增加 `engineCodex agenticEngine = "codex"`。
3. `selectAgenticEngine(level, hasKBs)`：case `reasoningLevelCodex` 必须放在 `if !hasKBs { return "" }` **之前**返回 `engineCodex`，使 mode 8 不受 KB 约束：
   ```go
   func selectAgenticEngine(level int, hasKBs bool) agenticEngine {
       if level == reasoningLevelCodex {
           return engineCodex // mode 8 无需 KB：检索工具在无 scope 时返回错误
       }
       if !hasKBs {
           return ""
       }
       switch level { ... }
   }
   ```
4. `AsyncChat` 的 dispatch（`chat_pipeline.go:355`）增加 `case engineCodex:`，调用新引擎入口。
5. `reasoningNeedsAgenticGraph` 的 `reasoningLevel > harnessMaxLevel` 已能拒绝 8，无需改动；但**必须加注释**说明 8 与 5/6/7 同级。

### 4.2 新引擎入口（新文件 `internal/service/codex_pipeline.go`）

仿 `iterativeSynthesis`（`internal/service/iterative_pipeline.go:55`）的通道契约：

```
func (s *ChatPipelineService) codexAgent(ctx, userID, chat, messages, stream, kwargs, useWebSearch, quote) (<-chan AsyncChatResult, error)
```

- 打开 buffered channel（容量 16，与 5/6/7 一致），goroutine 生产、`defer close(out)`。
- 解析 `datasetIDs`（照抄 `chat_pipeline.go:2703` 的写法）与 `tenantID = chat.TenantID`。`datasetIDs` **允许为空**（会话未绑定 KB）：此时仍进入 mode 8，只是 MCP 检索工具会返回错误（见 §4.4 第 6 点），`tenantID` 仍从 chat 取。
- 从 `common.SessionIDFromContext(ctx)` 取 session id 作为 thread 键。
- 调 `internal/codexagent` 包跑一次 turn：传入 `messages`（历史 + 当前 user 消息）用于在 thread 新建/重建时重放历史（§5.2 第 4 点），并把事件回调映射到 `AsyncChatResult`（见 §5.3）。
- 收尾时写最终结果（answer + reference + tool counts）。

### 4.3 新包 `internal/codexagent/`（Codex 引擎本体）

职责：封装“铸造票据 → 连接共享 Codex → 起/复用 thread → 跑 turn → 流事件 → 释放票据”。

文件建议：

- `client.go`：Codex 客户端生命周期的获取（进程级单例连接池，多个 turn 复用同一 WS 连接；`WithReconnectingWSTransport` + `WithAutoReconnect`）。
- `thread.go`：thread 与 RAGFlow session 的映射（key = `(tenant_id, session_id)`，session id 取自 `common.SessionIDFromContext(ctx)`；见 §5.2）。
- `history.go`：RAGFlow 消息历史 → Responses API item 的转换与 `ThreadInjectItems` 重放（D10）。
- `run.go`：一次 turn 的流式执行与事件翻译。
- `mcp_ticket.go`：票据的铸造/校验/回收（与 `internal/mcp` 协作）。

### 4.4 MCP bridge（扩展 `internal/mcp/`）

现有 `internal/mcp/server.go` 已用 `modelcontextprotocol/go-sdk v1.8.0` 暴露 `ragflow_retrieval` / `ragflow_list_datasets` / `ragflow_list_chats`，并有 `NewHandler`（`internal/mcp/http.go:52`）支持 streamable HTTP。mode 8 需要：

1. 新增一个 **按本轮 binding 作用域的 server 工厂**：`NewCodexToolServer(scope CodexScope) *sdk.Server`，其中 `CodexScope{ TenantID string; DatasetIDs []string; UserID string; ChatID string; WebSearch bool }`（纯数据，可 JSON 序列化进 KV）。`UserID` 是发起该 turn 的用户，桥在工具调用前注入到 ctx（`agentrunt.WithUserID`），供下游数据集权限校验使用（见 §5.7 第③段）。`ChatID` + `WebSearch` 用于 web_search 的逐轮注入：桥按 `ChatID` 从 DB 解析该会话的 provider（**provider 回调本身是进程内的**，作为 `CodexScope` 的非导出字段由 handler 填好，绝不进 KV）。
2. 注册与 `toolRegistry()`（`config.go:86`）**同名同义**的工具，参数 schema 复用 `internal/agentic_rag` 内对应工具的结构（如 `search_bm25_chunks` / `search_semantic_chunks` 的参数结构），避免两套契约漂移。
3. 工具实现**直接调用现有的检索实现**（`runLocateSearch` / grep / chunk 读取），作用域从 `scope` 注入，而不是从全局 `runtime.GetRetrievalService()` 里兜底 tenant。暴露的最小切面：
   - `search_semantic_chunks` → `locate_search.go` 的 `locateSearchSpec`（vectorOnly，tenantID=scope.TenantID, boundDatasetIDs=scope.DatasetIDs）。
   - `grep_chunks` → `internal/agentic_rag/grep_service.go` 的 `GrepService`。
   - `search_bm25_chunks` → `runtime.GetBm25Service()`。
   - `list_chunks` → `internal/agentic_rag/tool_list_chunks.go` 的读取逻辑。
   - **不暴露**：`search_chunks`（D9）、`think` / `todo_write` / `run_javascript` / `check_decomposition`（D7）。
   - **条件暴露**：`web_search`——仅当 binding 的 `WebSearch` 为 true 且按 `ChatID` 解析出 provider 时加入（D9/§5.7③）。
4. 新增路由：`POST /mcp/codex/:token`（同 handler 内区分），走 `StreamableHTTPHandler`，`Stateless: true`。票据校验放在接收中间件里，参照 `internal/mcp/http.go:70` 的 `AddReceivingMiddleware` 模式。
5. handle 未知 → **401**；handle 存在但本轮无 binding → **403**；都在构建任何 server/工具之前拒绝，绝不落到默认 KB 作用域。
6. **无 scope 的检索调用返回错误**：当 `Scope.DatasetIDs` 为空（会话未绑定 KB）时，`search_semantic_chunks` / `search_bm25_chunks` / `grep_chunks` / `list_chunks` 一律返回 `IsError: true` 的 MCP 结果（消息说明“本次会话未绑定知识库”），**绝不**以空 `dataset_ids` 去检索。这样无 KB 的 mode 8 仍可正常对话/推理，检索请求则显式失败，Codex 读到错误后自行应对（例如直接回答或告知用户）。该守卫是**逐工具**的（`codexToolSpec.requiresKBs`）：`web_search` 属于会话能力、不依赖 KB，**不受此守卫约束**，无 KB 会话照样能联网检索。

### 4.5 配置（`internal/server/config/`）

新增 `CodexConfig`（参照 `mcp_config.go` 的写法与测试）：

| 键 | 说明 | 默认 |
|---|---|---|
| `codex.endpoint` | 共享 Codex 的 WS 地址 | 空 → mode 8 不可用 |
| `codex.bearer_token` | WS 鉴权凭据；**由运维生成**（app-server 不产出也不打印），见 §5.7① | 空（loopback 场景可省） |
| `codex.mcp_public_base` | RAGFlow 对 Codex 可达的 MCP base URL | 由 `api_server` 推导 |
| `codex.approval_policy` | 见 §5.5（能力隔离），默认最严格的可用档 | `untrusted` |
| `codex.sandbox_mode` | sandbox 档位（见 §5.5）：宿主直跑用只读；容器即边界时用 `danger-full-access` | `read-only` |
| `codex.turn_timeout` | 单 turn 墙钟预算 | 30m（对齐 `iterativeSynthesisTimeout`） |

> 注意：**不设 `codex.model` 默认值**。模型由 dialog 配置（`chat.LLMID`）在创建 thread 时决定（D4）；配置层只提供 Codex 侧的连接、隔离与超时参数。

未配置 `codex.endpoint` 时：`selectAgenticEngine` 仍返回 `engineCodex`，但引擎入口应**降级到普通 RAG 并明确记日志**，而不是 panic（对齐“无 KB 走普通分支”的诚实降级原则）。

**模型解析（D4）**：创建 thread 时，复用现有的 dialog 模型解析（`getLLMModelConfig` / `agenticModelChain` 所依赖的模型配置读取路径）拿到 `chat.LLMID` 对应的 provider 信息，在 `StartThread` 时**同时**声明模型名与 provider：

```go
client.StartThread(ctx,
    codexgo.WithThreadModel(resolvedModelName), // ← 必填：dialog 模型解析出的实际模型名
    codexgo.WithThreadModelProvider(providerID),
    codexgo.WithThreadConfigOverride("model_providers."+providerID, map[string]any{
        "name": providerID, "base_url": baseURL,
        "wire_api": "responses", // Codex 仅支持 Responses API
        "experimental_bearer_token": apiKey,
    }),
    // D5：引用约定写进 thread 的 system/developer instructions
    codexgo.WithThreadBaseInstructions(citationContract),
    // P1#2：只读 sandbox（见 §5.5）
    codexgo.WithThreadSandbox(codexgo.SandboxReadOnly),
)
```

- ⚠️ **必须传 `WithThreadModel(resolvedModelName)`**：只传 `WithThreadModelProvider` 会继承 Codex 的默认模型名，可能调用错误模型或被自定义 provider 拒绝。`getLLMModelConfig`（`chat_pipeline.go:2292`）已返回 `(cfg, modelName, factoryName, baseURL, err)`——用其中的 **`modelName`** 作为 `WithThreadModel` 的实参，`factoryName`/`baseURL` 用于 provider override。**`chat.LLMID` 的复合 id（如 `provider/model@instance`）不等于模型名**，不能直接拿 llm_id 当模型名。
- Codex 的 wire API 只有 `responses`（见 SDK `examples/custom-provider` 注释）；dialog 模型若不支持 Responses API，需要在计划阶段确认（U2 剩余部分）。
- thread 一旦建立，模型即固定；后续 turn 走 `ResumeThread` 复用同一 provider（注意 D3：resume 不重新声明配置，依赖服务端持久化）。


### 4.6 前端（`web/`）

1. `web/src/components/message-input/next.tsx:60`：正则 `/^[0-6]$/` → `/^[0-8]$/`（引擎档现在到 8；漏改会让存储的级别静默降级为 1）。
2. `thinkingOptions`（`next.tsx:168`）：补齐引擎档 `{ value: '8' }`（coding-agent / Codex）、`{ value: '7' }`（IterSynth）、`{ value: '6' }`（planer-explorer-auditor），与既有的 `{ value: '5' }`（react）一并排在 harness 档（1–4）之上。
3. locale 增加 `thinkingLevelCodex` / `thinkingLevelCodexDescription`：**`en.ts`、`zh.ts`**（与现有 mode 5/6/7 的键位置一致；其余语言（ja/tr/az/uz）本就没有 5–8 的引擎档键，按同一先例回退英文，不单独注入未翻译键）。
4. 遵循 `web/AGENTS.md`（前端约定）与 `web/CLAUDE.md`（后端变体仅迁移期指引，新工作面向 Go API）。

### 4.7 引用与 quote

- quote gate 语义与 mode 5/6/7 保持一致：请求 kwargs 优先，`prompt_config` 只能进一步收紧（`quoteEnabled`）。
- **引用由 Codex 按 thread system prompt 的约定实现（D5）**：创建 thread 时通过 `WithThreadBaseInstructions`（或 `WithThreadDeveloperInstructions`）注入一段“引用契约”，要求 Codex 标注来源，后端据台账生成 `reference.chunks`。
  - ⚠️ **契约必须固定为「引用 chunk ID」，而不是让 Codex 自己编 `[ID:N]`**：现有 `ExtractCitedChunkIDs` 只识别 `chunk_id:\s*<id>`（`citations.go`），而 `RewriteProvenance` 只处理 provenance tuple / 裸 `chunk_id:` 字段。因此**模型的原始 `[ID:N]` 既无法被解析、也无法被可靠校正**——一旦后端过滤掉无效 chunk，模型给的编号会与最终 reference 顺序错位。
  - **正确做法**：契约要求 Codex 在其答案中按 `chunk_id: <id>` 形式（或等价的稳定、可解析 id 形式）标注来源；后端收集这些 id（复用 `ExtractCitedChunkIDs`）→ 语料按 KB 作用域校验、web 条目信任模型 → **由后端按校验后的顺序统一生成 `[ID:N]` 标记与 `reference.chunks`**（`agentic_rag.RewriteProvenance`），模型不参与编号。
  - 契约内容需写明：id 的书写形式（逐字复制检索结果里的 chunk_id）、以及“只标注来自检索工具结果的来源”；不得要求模型输出 `[ID:N]` 数字。
  - `reference.chunks[N]` 的下标语义与 mode 5/6/7 一致（前端 `[ID:N]` 解析不变）。
  - Codex 标注的 id 若不在本 turn 台账内，保留文本但不生成引用（与 mode 5“不可解析则不标注”一致）。


---

## 5. 详细设计

### 5.1 MCP 工具映射表

| MCP 工具名 | 底层实现 | 作用域来源 | 备注 |
|---|---|---|---|
| `search_semantic_chunks` | 复用 `NewSearchSemanticChunksTool` 的 schema 与执行路径（`runLocateSearch` VectorOnly） | 票据 `Scope` | 无 scope 返回错误 |
| `search_bm25_chunks` | 复用 `NewSearchBm25ChunksTool` / `runtime.GetBm25Service()` | 票据 | 无 scope 返回错误 |
| `grep_chunks` | 复用 `NewGrepChunksTool` / `GrepService` | 票据 | 引擎不支持 regexp 时回 `ErrRegexpNotSupported`；无 scope 返回错误 |
| `list_chunks` | 复用 `NewListChunksTool` 的 schema 与执行路径 | 票据 | 参数为 `doc_id`(必填) + `anchor_chunk_ids`(必填, ≤20) + `number_neighbors`(可选)；**无 `offset/limit`**（见 `tool_list_chunks.go:53-57`）；无 scope 返回错误 |

> **MCP bridge 必须复用现有工具的 schema 与执行路径**（同名同义）：不要为 MCP 另写参数（例如给 `list_chunks` 加 `offset/limit`，或直接读 `dao.DB`）。构造器、`Info()` 的 JSON schema、`InvokableRun` 的语义三者都从 `internal/agentic_rag` 复用，仅把作用域从「构造时注入」改为「票据解析的 `Scope`」。

> **不暴露 `search_chunks`**（D9）：用户决定，其余检索工具保留。注意 `search_semantic_chunks` 是 `search_chunks` 的纯向量版，功能与之高度重叠。
>
> **无 scope（会话未绑定 KB）时**：以上检索类工具全部返回 `IsError: true` 的错误结果，不会用空 `dataset_ids` 发起检索（见 §4.4 第 6 点）。这是 mode 8 能在无 KB 下仍访问 Codex 的前提——对话可用，检索显式失败。
>
> **不暴露** `think` / `todo_write` / `run_javascript` / `check_decomposition`（D7）：Codex 内置的 plan/reasoning/命令能力远强于这些，重复暴露会造成工具选择冲突。MCP 只暴露“检索语料”这一类 Codex 自身没有、必须借用 RAGFlow 的能力。

> 工具返回格式：沿用现有 XML（`<search_results>` / `<tool_error>`），Codex 侧作为纯文本工具输出消费即可，无需 Codex 理解 XML 语义。

### 5.2 Thread 与 RAGFlow 会话的映射（一一对应）

**目标：多轮复用同一 Codex thread。** 但 thread 只承载「模型对话状态」；**权威数据（历史、KB 作用域、权限）每轮从 RAGFlow 现算**，thread 与之漂移时重建。

**映射表** `codex_thread_map`：`(tenant_id, session_id)` 唯一索引（复用 root `AGENTS.md` 的 GORM 唯一索引约定），存 `codex_thread_id`、`scope_fingerprint`（KB 集合 + 授权摘要，用作复用键）、`history_fingerprint`（已注入历史的版本）、`handle`（per-session MCP handle，用于存活检查与吊销）、`created_at/updated_at`。

1. **会话标识 = RAGFlow session id（会话，不是 dialog/chat id）**：一个 dialog（`chat.ID`）下可有多个 session，每段多轮各自独立。`ChatCompletions` 已把 session id 放进 ctx（`chat_session.go:1262/1315` 的 `common.WithSessionID`），mode 8 引擎经 `common.SessionIDFromContext(ctx)`（`internal/common/logger.go:381`）读取。
2. **是否用持久 thread 取决于存储语义，而非仅 session id 是否存在（P1#7）**：`store_history_messages=false` 的请求必须走**一次性 thread**（`StartThread` + 跑完即弃，不读/不写映射），即使 ctx 里带着 session id——否则非存储/测试输入会留在持久 thread 污染后续正常对话。判定依据是入口的「不存储历史」语义，不是 `SessionIDFromContext != ""`。
3. **无 session id**（`openai_chat.go` / `bot_completion.go`）→ **一次性 thread（D11）**：`StartThread` + 重放本次请求携带的历史 + 跑 turn + 丢弃，不查/不写映射、不 `ResumeThread`。
4. **每轮流程**：
   - 取当前 session 的授权、KB 集合、历史；计算 `scope_fingerprint` / `history_fingerprint`；取该 session 的稳定 handle（`HandleForSession`）。
   - 查映射：
     - 命中、两个 fingerprint 均一致、且 handle 仍存活 → `client.ResumeThread(ctx, codexThreadID, opts...)`。
     - 未命中，或 **fingerprint 不一致**，或 handle 已失效 → **重建**：`StartThread` + 重放历史 + 覆盖映射（见本节第 5、6 点）。handle 每 session 稳定，重建沿用同一 handle，**无需吊销/换 URL**。
   - **命中复用后 resume 的行为**：调用 `client.ResumeThread(ctx, codexThreadID, opts...)`，返回一个绑定到既有远端 thread 的新 `*SessionThread`。明确「发生什么 / 不发生什么」：
     - **复用模型侧状态，且不重放历史**：thread 自创建起就持有其对话历史，resume 直接续跑；**RAGFlow 不在 resume 时重放历史**（重放只发生在 `StartThread`/重建那一轮，见第 8 点）。RAGFlow 与 thread 是否一致靠 `history_fingerprint` 校验，而不是靠每次重放。
     - **不重载 `mcp_servers`（D3 实测）**：MCP URL（即 handle）保持创建时的值，resume 的 config overlay 不会改它。由于授权由 **per-turn binding** 承担、URL 不携带权限，这无影响；也**不能**用「resume 时刷新 URL」来换作用域（第 5 点）。
     - **会转发其余 `ThreadOption`**：model / provider / sandbox / instructions 等经 `thread/resume.config` 深合并、每轮重新声明（SDK 修复 `ResumeThread` 丢弃 opts 后生效，见 D3 / commit `412c6d4`；合并见第 9 点）。
     - **与本轮 handle/binding 的关系**：resume 的 thread 用它 baked-in 的 URL（= 每 session 稳定的同一个 handle），本轮已在该 handle 上 `Bind`，故 Codex 的工具调用被授权 —— 这正是 handle 做成 **per-session 稳定**的原因（第 5 点）。
     - **收尾与失败**：本 turn 跑完 `th.Close()` 释放 SDK 槽位（`WithMaxThreads`），下一轮再 resume（第 11 点）；`ResumeThread` 连接失败 / thread 被共享 Codex 回收 / 服务端重启 → **回退重建**（第 10 点）。
     - **必须在 per-session 锁内**：否则同会话两个 turn 会交错（第 7 点）。
5. **KB 作用域逐轮绑定（P1#8）**：作用域**不固定于首轮**，且现在由 **per-turn binding** 承载：每轮从当前 dialog/session 配置解析 `DatasetIDs` 与授权，runner 在会话锁内 `Bind(handle, CodexScope{…})`，turn 末 `Unbind`（D2/§5.7③）。**实测确认 `thread/resume` 的 config overlay 不会重载 `mcp_servers`（D3）**，但该限制现已无害（URL 里的 handle 稳定且无权限）。当前实现仍把 KB 集合折进 `scope_fingerprint`，故 KB 变化（增删/改绑、无 KB→有 KB）仍会**重建 thread**；既然授权已 per-turn，去掉该维度是可选后续。绝不能用「resume 时刷新 MCP URL」来改作用域。**`WebSearch`（会话是否联网检索）同样折进 `scope_fingerprint`，且这一维不可去掉**：Codex 按 thread 缓存 MCP 工具目录，只有重建 thread 才能让 `web_search` 的出现/消失真正生效（见 §5.7③）。
6. **历史一致性（P1#6）**：Codex thread 的历史会与 RAGFlow 漂移——`DeleteSessionMessage` 删除消息（`chat_session.go:523`）、同一 session 内切换 reasoning level、其他模式写入的新回合，都不会进入已有 thread。用 `history_fingerprint`（如可见消息 id 序列的哈希）逐轮校验；不一致则**重建 thread 并重放**（若 SDK/服务端支持，也可用 `Revert` + `ThreadInjectItems` 精准同步）。**不能只在 `ResumeThread` 失败时才重放。**
7. **并发安全（P1#3）**：互斥由 **binding 本身**提供，不是任何进程内锁。`Bind` 是对 `ragflow:codex:bind:<handle>` 的一次 KV **原子 `SetNX`**：抢到 = 得到本轮锁；已有 binding（另一个 turn 在跑）= 返回 `ErrCodexTurnBusy`，引擎回一条 `session <id> is busy with another turn` 的终局错误。锁在 turn 全程持有（`Bind` → resume-or-start → run → 收尾 → `Unbind`），因此**一个 handle 在任何时刻至多一个 binding**，且互斥**跨进程 / 多副本**成立（KV 原子性），无需 sticky 路由。
   - **为什么这样已经足够**（对照原来的进程内锁）：两个并发 turn 若都 `SetNX`，只有一个成功；失败的那个根本不进 thread 阶段，所以既不会覆盖对方的 scope，也不会在对方 `Unbind` 时误删 binding。锁的生命周期与 binding 完全一致，天然覆盖「Bind → run → Unbind」。
   - **保活**：binding 是**租约**（TTL 10 分钟）。正在跑的 turn 由 **binder** 起的 goroutine 每 1 分钟 `Renew` 一次（`Bind` 成功时启动、`Unbind` 时退出）；崩溃/挂死的 turn 不再续期，租约到期后锁自动释放，下一个 turn 即可进入。因此**不需要全局 keeper**。
   - **`turnMu` 只是补充、不参与本层互斥**：SDK 的 `SessionThread.turnMu`（`thread.go:168`）只保证**同一个 `SessionThread` 对象上不并发跑两个 turn**（保护该对象的协议状态；`ResumeThread` 每次返回**新对象**，故它挡不住跨请求并发）——binding 锁已经把同会话串行化，`turnMu` 只是对象内的最后一道保险。
   - **session 删除的竞态**：删除不是靠锁排他，而是 `RevokeHandle` + 「turn 写映射前校验 handle 仍存活」——被删会话的 handle 会被吊销，在途 turn 的收尾 Put 因此跳过，不会复活映射。
8. **对话历史重放（D10）**：对新 thread（首次创建、fingerprint 变化导致的重建、无 session id / 非存储的一次性 thread）注入历史，再跑当前轮：
   - 机制：`client.ThreadInjectItems(ctx, ThreadInjectItemsRequest{ThreadID, Items})`（`thread.go:500`），`Items` 是 **Responses API item 的原始 JSON**。
   - 注入内容：可用历史（有 session 时取 `chat_session` 的 history；无 session / 非存储时取本次请求的 `messages`）**去掉最后一条（当前 user 消息）**，逐条转为 Responses API item。当前 user 消息作为 `Run`/`RunInputs` 的输入，不重复注入。
   - 转换：RAGFlow message（`role`/`content`）→ Responses API item 的映射函数（如 `{"type":"message","role":"user","content":[{"type":"input_text","text":...}]}`），在 `internal/codexagent` 内实现并单测。
9. **建/恢复都注入配置**：模型（D4）、MCP override（D3）、引用契约 instructions（D5）、sandbox（P1#2）在 `StartThread` **和每次 `ResumeThread`** 都以同一 `ThreadOption` 集合声明（SDK 修复后 resume 会转发，见 D3）。这消除了「配置只在首轮生效」的隐患；作用域不靠这个刷新，而是靠 per-turn binding（第 5 点）。
10. **失效回退**：`ResumeThread` 失败（共享 Codex 重启 / thread 被回收）→ `StartThread` + 覆盖映射 + 按第 8 点重放 RAGFlow 历史，记录告警。
11. **生命周期**：turn 结束后可 `thread.Close()` 释放 SDK 信号量槽（`WithMaxThreads`），下一 turn 再 `ResumeThread`；session 删除时清理映射、`RevokeHandle`（失效 handle 及其 binding）、删除/归档 Codex thread。

### 5.3 事件 → AsyncChatResult 映射

参考 SDK 事件（`events.go` / `events_extra.go`）：

| Codex 事件 | 映射 |
|---|---|
| `ItemAgentMessageDeltaEvent{Text}` | `AsyncChatResult{Answer: text, Final:false}` |
| `ItemReasoningTextDeltaEvent{Text}` / `ItemReasoningSummaryTextDeltaEvent{Text}` | `AsyncChatResult{Reasoning: text}`，配合 `StartToThink/EndToThink` 标记（同 `chat_pipeline.go:2813` 的 think 边界逻辑） |
| `ItemCompletedEvent(kind=reasoning)` | **无 delta 时的思考兜底**（见下） |
| `ItemStartedEvent(kind=mcpToolCall)` / `ItemCompletedEvent(kind=mcpToolCall)` | **把检索步骤（tool + 参数）写进思考流**，并按工具名计数（见下） |
| `ThreadTokenUsageUpdatedEvent{TokenUsage}` | **本轮 token 用量**（见下） |
| `ItemCommandExecutionOutputDeltaEvent` | 忽略或仅 debug 日志（mode 8 不面向工具执行回显） |
| `TurnCompletedEvent{TurnID, Status}` | 收尾：写最终结果 |
| `ErrorEvent` | 转 `**ERROR**: ...` 最终结果 |
| `McpToolCallProgressEvent` | 暂不消费（只带进度文本；工具名/参数在 `mcpToolCall` item 上） |
| `sdk/reconnect*` / `sdk/sessionBackfilled` | **必须处理，不能忽略**（见下） |

**断线与 backfill（P1#5）**：SDK 明确「断线期间的通知不会重放」，`RunStreamed` 只转发匹配当前 turn 的事件、且**不转发 `sdk/sessionBackfilled`**（`thread.go:243-292` 的 `eventMatchesTurn` 过滤）。因此若 turn 在断线期间完成，仅靠 `RunStreamed` 会**收不到终局、丢文本、甚至挂到超时**。处理方式：

- 除 turn 的流式通道外，**同时订阅 `client.Events()`**，处理 `sdk/reconnectStarted/Succeeded/Failed`、`sdk/sessionRecovered`、`sdk/sessionBackfilled`。
- 收到 `sdk/sessionBackfilled` 时按 **`threadID`/`turnID` 合并权威状态**：用 `TurnRead` / 回填的 turns 取该 turn 的终局 `Items` 与 status，补齐缺失的最终答案，并对已流出的增量**去重**（按 turn id + item id 归并，不以文本拼接为准）。
- 终局判定以「权威 turn 状态」为准，不能以「流式通道关闭」为准（通道也可能因消费者落后而被终止）。
- 事件里的 `sdk/*` 只用于内部归并与补齐，**不原样转发给前端**（避免重复/协议泄漏）。

> **实测修正（codex 0.162.0）**：SDK 的 backfill 依赖 `thread/turns/list`，而本机 codex **不支持**该 RPC（`list_turns is not supported yet`），故 `sdk/sessionBackfilled` 实际不携带可合并的 turns —— **上述归并逻辑在本版本不可达**。据此实现**已移除** backfill 订阅/缓存（避免死代码与每 thread 的 map 泄漏），仅保留必需的兜底：**流式通道在未到终局前关闭 → 判该 turn 失败**（绝不把半截答案当终局）。若未来 codex 支持 `thread/turns/list`，再按本节恢复归并。

think 边界：mode 8 的 reasoning 与 content 可能交错，必须像 `agenticRag` 一样用 `thinking` 布尔量保证 `<think>` / `</think>` 各发一次，避免前端 `mergeAnswerChunk` 拼接错位（见 `chat_pipeline.go:2813` 注释）。

**思考过程为空的原因与兜底（实测）**：Codex 只在**服务商真的流式吐** `response.reasoning_text.delta` / `response.reasoning_summary_text.delta` 时，才发上面那两个 delta 通知（`codex-api/src/sse/responses.rs:377,396`）。而 MiniMax 这类**非 OpenAI 的 Responses 后端**把整段 reasoning 作为**一个完成项**下发（rollout 里是 `reasoning` item + `item_completed`，`summary` 为空、无任何 delta）——此时 runner 只认 delta，思考面板就永远是空的。修法（**两种都支持**）：`ItemCompletedEvent` 里，若该 item 是 reasoning 且**它的 id 从未产生过 delta**（用 `reasoningDeltaItems` 记录 delta 的 item id 去重），就取该 item 的文本（payload 的 `content`，空则 `summary`）走同一个 `OnReasoning`。这样流式与非流式 provider 都能出思考，且不重复。

**检索步骤（tool + 参数）写进思考流（已实现）**：mode 8 没有客户端工具循环可回报，因此 runner 直接读 Codex 的 `mcpToolCall` item —— `item/started` 给出 `server`/`tool`/`arguments`，`item/completed` 给出 `status`/`durationMs`（若某 provider 不发 started，则由 completed 那次一并报出调用，用 `toolCallsSeen` 去重）。服务层把每一步以 `<tool> <紧凑 JSON 参数>` 与 `<tool> returned in <ms>ms` 写进**思考流**，**每行前后恰好一个换行符**（`narrationChunk`：仅在流处于行中时前置 `\n`，行尾恒补 `\n`——否则模型的推理文本会直接粘上工具行），并同时带上结构化 `think_event`（`kind=tool_call|tool_result`，含 `tool`/`args`/`status`/`duration_ms`）。**无 `[MCP]` 前缀**：工具名即首 token。**为什么写进思考流而不只发 think_event**：前端当前只渲染 `answer` 里的 `<think>` 文本，**完全不消费 `think_event`**——只发结构化事件，用户是看不到的。

**工具调用计数（对齐 mode 5/6/7 的 benchmark）**：同一个 `RoundTrip` 流顺带做**按工具名计数**（`toolTally`：`item/started` 记一次调用；若 provider 不发 started 则由 completed 那次记，不重不漏；`status=failed` 另记 `ToolCallErrors`），最终落到 `AsyncChatResult.ToolCallCounts` / `ToolCallErrors`。因此 mode-8 的一行 benchmark 与 mode 5 的 `{"tool_call_counts":{"grep_chunks":8,"list_chunks":12,…}}` **同形可直接对比**。注意 v2 的 status 是 camelCase（`inProgress`），需归一化为内部的 `running`——否则「调用」会被当成「结果」叙述。

**检索会计（retrieved/served 文档、深读/浅读）——与 mode 5 同一实现**：这块**不是**从检索层内部拿的，而是**解析工具输出文本**：`<chunk …>` 元素计数、`chunk_id` / `doc_name` 属性（见 `agentic_rag` 的 ledger 注释）。因此 mode 8 完全够得着——**工具返回值就在 `mcpToolCall` item 的 `result.content` 里**（Codex 把 MCP 返回原样回传；bridge 用 `textResult` 塞的是同一份 XML）。做法：

- 分类逻辑抽到**叶子包 `internal/retrievalaccount`**（`AccountToolOutput(tool, out)`），**mode 5 与 mode 8 调用同一个函数**，所以口径不可能漂移。做成叶子包是必须的：`agentic_rag` 间接 import `dao`，而 `dao` import `codexagent`，故 `codexagent` **不能**反向 import `agentic_rag`。
- 口径（与 mode 5 逐条一致）：`RetrievedDocIDs` = **完整正文工具**（`search_chunks`/`search_semantic_chunks`/`list_chunks`）输出的 doc stem（去重排序）；`ServedDocIDs` = **所有语料工具**输出的 doc stem，按"被服务次数"降序（同次数按名字升序）；`DeepReadChunks`/`ShallowReadChunks` = 按工具类别计数的 `<chunk>` 元素数，并附 `DeepReadChunkIDs`/`ShallowReadChunkIDs`（去重排序）。`web_search` 渲染 url/title，**不计入语料**。
- ⚠️ **一处已知差异（诚实记录）**：`ServedDocIDs` 的**权重单位**不同——mode 5 由检索层按 `(query, doc)` 记录（权重 = **不同 query 数**），mode 8 只有渲染后的文本、无法把命中归因到具体 query，故以**一次调用**为单位。**集合相同**，仅排序权重不同。

**Token 用量（per-turn，已实现）**：SDK 明确 `ThreadTokenUsageUpdatedEvent`（app-server 的 `thread/tokenUsage/updated`）**是唯一携带 token 用量的通道**；每个**模型调用**发一次，`TokenUsage{last, total, modelContextWindow}` 中 `last` 是**该次调用**的用量、`total` 是 thread 累计（见 `Last`/`Total` 语义：`core/src/session/mod.rs:4968-5035`）。因此 runner **累加每次的 `Last`**（`inputTokens`/`outputTokens`/`totalTokens`）并**计数**，得到本轮消耗；累加 `Total` 会跨轮重复计数，不可用。结果经 `TurnOutcome.Usage` → `AsyncChatResult.Usage`（`service.TurnUsage`：`prompt_tokens`/`completion_tokens`/`total_tokens`/`llm_calls`）出现在终局 SSE 帧的 `usage` 字段——**与 mode 5/6/7 的形状一致**，benchmark 可直接对比。未收到任何用量通知时 `Usage` 为 `nil`（而非全 0 块）。
  - ⚠️ **流的序列化器是白名单结构**：`sseMarshalChunk`（`chat_session.go`）手写 `sseAnswerChunk` 结构体**逐字段**拷贝，producer 设了而它没拷的字段会在**流式**路径被静默丢掉（非流式路径直接返回 map，所以不受影响）。这正是「token 用量只在非流式 benchmark 里看得到、UI（流式）看不到」的原因——也是 mode 5/6/7 共有的老问题。已补齐：`usage` / `elapsed_seconds` / `rounds` / `tool_call_counts` / `tool_call_errors` / `tool_error_samples` / `retrieved_docids` / `served_docids` / `gate_audit` / `deep_read_chunks` / `shallow_read_chunks` / `deep_read_chunk_ids` / `shallow_read_chunk_ids`。

### 5.4 引用（tuple 契约 + 原地替换 + 无台账）

**契约（D5）**：Codex 按 thread 的 base instructions，把每条有据可依的行**以 provenance tuple 结尾**，两种形状之一（与 plan 引擎交付物同形）：

```
(doc: <doc_name>, doc_id: <doc_id>, chunk_id: <chunk_id>, snippet: "<短逐字片段>")
(source: web, url: <url>, title: <title>, snippet: "<短逐字片段>")
```

⚠️ **契约对「本会话是否启用联网」保持 generic**（`codexRootContract(quote)`，**不接收** webSearch）。原因：

- **提示词与工具列表必须是同一事实的单一来源**。base instructions 在建 thread 时写入、**thread/resume 不会重载**；而 web_search 是**逐轮**决定。若把该决定编进提示词，就会出现「提示词说 A、工具列表是 B」的两份状态，二者必须永远一致才安全——一旦某条路径（resume、指纹未命中、重建失败）只更新其一，模型就会被告知一个不存在的工具，或被告知没有它实际拥有的工具。
- **因此能力声明交给 MCP 工具列表**：桥只在会话启用时才广告 `web_search`，契约则写明「**你的工具是本次会话提供的 MCP 工具；工具列表本身是权威——只用列表里真实存在的工具，不要假设未列出的能力**」。于是提示词在开关两种情况下**逐字相同**，永远不会过期。
- **另一条恒定项：shell 只做本地计算，且不得以任何方式联网、也不得申请解除沙箱**。Codex 会给模型 `<environment_context>`（含 `<shell>bash</shell>` 与工作目录），模型据此会去试 `curl`。故契约写明：「shell 仅用于**本地计算**（算术/计数/短脚本，python3/node 皆可，但只对已有数据计算）；**无网络**——不得用 `curl`/`wget`，**也不得在脚本里发 HTTP**（python `urllib`/`requests`/`http.client`/`socket`、node `fetch`/`http` 等一律是死路）；**同样不得申请联网**：不要传 `sandbox_permissions`/`require_escalated`、不要为访问网络请求审批（一律被拒）；检索一律走上方的工具」。（shell 在联网开关两种情况下都无网络，这条与开关无关。）
- ⚠️ **实测教训（2026-10-14，q11）**：旧措辞只写"无网络、会被沙箱挡住"，模型把它读成**"可申请解除"**——两次传 `sandbox_permissions: require_escalated` 去 curl（自述 *"The exec_command requires user approval for network access"*），两次均被拒（Dispatcher 只接受 ragflow 的 MCP elicitation，防线守住了，只白耗两步）。**结论：边界要同时封住"做"和"申请"两条路**；且不能只靠沙箱默认——审批从 `never` 改为 `on-request` 后，"申请"这条路是**活的**。触发场景是 KB 无覆盖 + 会话没有 web_search 工具（模型没有正当出口）。
- `source: web` 出处形**无条件写进**契约，但措辞是**条件式**的（「若该段落由 web search 工具返回，则用 …」）——与「只用列表里的工具」一致，不会主动勾引没有联网的会话去搜索。
- 以上都**不改变安全边界**（审批层/沙箱照旧兜底），只让模型把 turn 花在正确的工具上。
- 附注：联网开关**仍**折进 `ScopeFingerprint`（`WebSearchFingerprint`）——不是因为提示词，而是因为 Codex 按 thread 缓存 MCP 工具目录，warm resume 不重列，**必须重建 thread** 工具列表才会更新（见 §5.7③）。

**契约按 root / sub-agent 拆分**（`codex_pipeline.go`）：

- 共用三段：`codexRetrievalToolsLine()`（能力以 MCP 工具列表为准，不自称有无某能力）、`codexShellPolicy()`（shell 仅用于本地计算；**任何方式**都不得联网，也不得申请解除沙箱）与 `codexProvenanceContract(quote)`（provenance tuple 格式，quote 关时为空）。三者对 root 与子智能体**逐字相同**——因此子智能体产出的 tuple 可以被 root 直接复用、无需重新推导 id。
- `codexRootContract(quote)` = 联网兜底句（"知识库覆盖不到再考虑 web search；否则据检索段落作答并如实说明缺口"）+ 工具行 + `codexShellPolicy()` + provenance。**不再包含 `Answer the user in prose`**：Codex 自带 agentic 提示词本就产出 prose，这句是冗余。
- `codexSubagentContract(quote)` = `codexRetrievalToolsLine()` + `codexShellPolicy()` + `codexProvenanceContract(quote)`。只保留 Codex **无法**知道的领域内容（工具权威性、**shell/网络策略**、引用 tuple）。**不含** worker 口吻与 root 的收尾句（Codex 的 `<multi_agent_role>` 已经给了）。

  ⚠️ **shell 策略曾一度从子契约里删掉（"沙箱已强制同一边界"），2026-10-14 撤销**：q11 的 root 实测表明这条边界**可以被申请绕开**（模型传了 `sandbox_permissions: require_escalated`），而子线程拥有同一个 shell、同一个沙箱、同一个申请通道，所以子契约必须带上同一句。

**归属与投递**（承接 §5.9）：子智能体的**身份/角色/模式**提示由 **Codex 自己**生成（`<multi_agent_role>` / `<multi_agent_mode>`，按 fork 模式、depth、role 选择），RAGFlow 不写这一层。RAGFlow 只提供**领域内容**，经 Codex 的专用接缝 `features.multi_agent_v2.subagent_developer_instructions` 下发。该键的语义（`core/src/agent/control/spawn.rs`）：**键不存在**时子线程**继承父线程的 developer instructions**（即 root 契约，正是要避免的错位）；**键存在**时用其值**替换**继承（空串即"抑制继承"，只剩 Codex 自身提示）。该键与 root 契约一样是 create 时烘焙，故一并折进复用键（`InstructionsFingerprint`）。

**后端（`buildCodexReference`）**：
- `ExtractCitedChunkIDs` 取语料 id，`ExtractCitedWebSources` 取 web 条目（url/title/snippet）。
- **校验按来源不同，这是刻意的**：语料 id 经 `fetchChunksByIDs(tenantID, datasetIDs, cited)` 做**存在性 + KB 归属**校验；**web 条目完全由模型负责其可信**——本轮不记录任何「搜索服务过什么」，url/title/snippet 直接取自答案。因此**不把 web 结果写进 KV**，也没有 served set。
- 通过者编成**一条从 0 开始的统一序列**：**先语料 chunk（按被引顺序），再 web 条目（按被引顺序）**，`reference.chunks[N]` 用同一顺序，前端不需要按来源分流。
- **原地替换**：tuple（或裸 `chunk_id: <id>` 字段）在**原位置**被替换成 `[ID:N]`（`agentic_rag.RewriteProvenance`），整段机器可读字段（含 doc/snippet 列）不再出现在答案里。未解析的 tuple **原样保留**（不可验证的引用宁可可见，也不指向错误编号）；因此该 pass 幂等。
- 该重写对 **mode 6/7 同样生效**（它们的交付物本来就是 `chunk_id:` / web 形状的 tuple，走同一个 `RewriteProvenance`）；mode 5 写的是 `[ID:n]` handle，不受影响。
- **没有台账**：语料侧是纯作用域校验（D12），模型可以引用**该 KB 内**任何真实 chunk（即使本轮没检索到）。
- **展示与历史**：流式下 DB 的 message content 保留**模型原始输出**（下一轮 replay 用），另存一份 `content_display`（= 已流出的 `<think>` 段 + 后端重写后的答案，与前端 `mergeAnswerChunk` 的合并规则一致）；读取接口（`buildSessionPayload` → `messagesForDisplay`）用它呈现，因此**重载页面仍显示 `[ID:N]` 引用**，与 live 一致。

代价（已接受）：模型可以引用**该 KB 内**任何真实 chunk；web 引用的可信完全依赖模型（可能给出不存在的 URL）；并且 mode 8 不产出 `ToolCallCounts` / `RetrievedDocIDs` / `ServedDocIDs` / `DeepReadChunks` / `ShallowReadChunks`（benchmark 遥测；Web UI 不消费这些字段）。

### 5.5 审批 / 安全（能力隔离）

⚠️ **`ApprovalNever()` 不是「拒绝执行」**：SDK 定义它为「**从不请求审批**」（`types.go:488`）——即 agent 会**直接执行**而不询问，是更宽松、不是更严格。mode 8 只应让 Codex 做「推理 + 调 MCP 检索」，绝不能执行命令/改文件。因此隔离必须靠**沙箱 + 禁用内置执行能力**，而不是靠审批策略：

- **sandbox 档位由 `codex.sandbox_mode` 决定（2026-11-03 起配置真正生效；此前 `run.go` 硬编码只读，该配置从未被使用）**：
  - `read-only`（宿主直跑，默认）：建 thread 时 `WithThreadSandbox(codexgo.SandboxReadOnly)`。禁止写文件、联网**与改变工作目录**；但**不限制读**，故只有 app-server 所在机器上**没有敏感数据**时才成立。
  - `danger-full-access`（容器部署）：边界交给容器——容器不挂载语料与宿主文件系统，Codex 只能看到镜像自身的文件系统，且只能经 MCP 桥访问 dataset。此时内层只读沙箱已无内容可限制，而无特权容器也跑不了它的 `bwrap`/`landlock` 助手（每次 shell 命令都会失败）。**这正是语料隔离的落点：读集合无法靠 thread config 收窄，只能靠环境（容器挂载面）。**
- **不设 `WithThreadCWD`（2026-10-16 实测修正；`WithThreadCWD` / `codex.work_dir` / `newWorkDir` 机制已删除）**。曾把「`WithThreadCWD` 指向一个空的、无敏感的目录」当作隔离手段，实测证伪了三点：
  1. **只读沙箱不限制「读」**——它只禁写与联网。cwd 只决定"站在哪"，不决定"能看到什么"：cwd 指向空目录时，shell 仍能 `cat $HOME/...`（模型驱动的普通 turn，以及显式 `{"type":"readOnly"}` 的 `command/exec`，均实测成功），`~/.codex/config.toml` 可读。**cwd 因此买不到任何隔离。**
  2. **cwd 在 app-server 宿主上不存在时，每条 shell 命令都失败**（`Failed to create unified exec process: No such file or directory`，`exitCode=-1`），turn 在重试里空转。而旧 `newWorkDir` 的默认分支是 `os.MkdirTemp`——在 **RAGFlow 宿主**建目录，分 pod 部署时对 Codex 毫无意义。
  3. **不设 cwd 完全可用**：Codex 回退到 app-server 自己的工作目录（存在），本地计算照常运行。
  删掉它之后 `internal/codexagent/run.go` 的 `threadOptions` 留了一条说明注释。要点是：**读集合无法靠 thread config 收窄**（Codex 的 `readOnly` 策略只有 `writable_roots`，没有只读根），唯一真正的边界是**环境级**的（每租户一个 app-server + 最小文件系统容器，见下"多租户隔离"）。复现：`internal/codexagent/cwd_missing_integration_test.go`（cwd 不存在 vs 不设 cwd 的对照）与 `read_barrier_integration_test.go`（读屏障）。
- **审批策略：`on-request`（2026-10-14 实测修正）。** 目标是既要**本地计算**、又不能碰坏检索。两点都已核对（codex 0.162.0）：
  - **`never` 会杀死 MCP 检索**：`core/src/mcp_tool_call.rs` 里，当一条 MCP 工具调用需要审批而策略为 `Never` 时，codex **直接拒绝**——`ReviewDecision::denied("MCP tool call requires approval, but approval policy is never")`。实测：`ApprovalNever` 下 `search_bm25_chunks` 全部 `failed in 0ms`，模型在答案里报告"检索工具被拒绝"。**这是不能用的档位。**
  - **`on-request` + 只读沙箱 = 计算可用、检索可用**：`core/src/exec_policy.rs` 中 `OnRequest` 对 **Restricted**（只读）沙箱里"非提权、非危险"的命令直接 `Decision::Allow`（注释原话：*do not prompt … let the sandbox enforce restrictions without a user prompt*），所以 `python3 -c ...` 照跑；而 `OnRequest ≠ Never`，MCP 的 elicitation 路径保持活跃 → `Dispatcher()` accept `ragflow` 的 `mcp_tool_call` → 检索正常。危险命令仍 `Prompt` → 我们拒绝；提权请求（`requests_sandbox_override`）也 `Prompt` → 拒绝。
  - 净效果：**计算能力来自沙箱档位（Restricted）而非审批豁免**；沙箱是唯一的边界（写文件、联网仍被拒）。
- **审批策略的历史值（均已弃用）**：① `untrusted`（`UnlessTrusted`）——"每个未被 exec policy 显式允许的命令都要审批"，配合我们的 `Dispatcher()`（只 accept `mcp_tool_call`、未配置 `Exec`/`File`）会把**所有命令**拒掉，因此挡住了我们想要的本地计算。② `never`——见上，会拒掉 MCP 检索。
- **审批策略的历史值 `untrusted`**：曾用 `AskForApproval::UnlessTrusted`，其语义是「**未在 exec policy 规则里显式允许的每个命令**都要审批」（`core/src/exec_policy.rs`），而 `codexagent.Dispatcher()`（`internal/codexagent/approval.go`）只 accept `mcp_tool_call` elicitation、未配置 `Exec`/`File` handler → SDK 对 `execCommandApproval` 返回 `DenyReview()`，对 v2 `item/commandExecution/requestApproval` 因 `Exec==nil` 同样拒绝。实测：某 session 的 rollout 里模型尝试 `exec_command: curl https://en.wikipedia.org/…`，输出为 `exec_command failed: … Rejected("rejected by user")`——命令没有执行。**该档位现已弃用**（它同时挡住了我们想要的本地计算）。
- **内置命令执行工具（`shell` / `exec_command`）：保留、且现在可运行，只由沙箱约束**（不通过 config 禁用）。曾考虑 `features.shell_tool=false` 整个摘掉，**判定为过度**（堵网络不需要牺牲工具）；现在则更进一步——**主动允许本地计算**：
  1. **审批关：`on-request` 下仍然活跃，且必须保持 accept 的只有 `mcp_tool_call`。** `Dispatcher()` 只 accept `mcp_tool_call`，`Exec`/`File` 为 nil → 命令的**提权请求**、文件变更、权限申请仍被拒（`interaction.go`）。但普通命令**不再经过审批**——`OnRequest` + 只读沙箱直接 `Allow`（见上条），所以本关不再承担"挡住命令"的职责。
  2. **沙箱关：唯一且必须成立的一道。** 只读 sandbox 对应 `PermissionProfile::read_only()`，其 `NetworkSandboxPolicy` 为 **`Restricted`**（`protocol/src/permissions.rs`，默认即 Restricted）；Linux 上由 seccomp 拒绝 socket 系统调用（`linux-sandbox/src/seccomp.rs:114`）并由 bubblewrap 隔离网络命名空间（`bwrap.rs` 的 `BwrapNetworkMode::Isolated`）兜底。且 `SandboxPolicy::ReadOnly{ network_access }` 的 `network_access` 默认 **false**（宿主 `~/.codex/config.toml` 无 `sandbox_workspace_write` 覆盖，已核）。
  - **因此**：命令**可以执行**（本地计算），但**写文件被拒、网络出不去**；危险命令（`dangerous_command_match`）即便 `Never` 仍是 `Decision::Forbidden`。
  - **副作用/代价（已接受）**：命令的执行面从"两道关"收窄为"一道关（沙箱）"。沙箱若在某平台失效或存在逃逸，模型即获得宿主命令执行能力——这正是 §5.5 前置验证与"按租户隔离 Codex"该覆盖的场景。
- **关闭所有「服务端工具」（hosted tools）——已实现**：建 thread 时下发 `WithThreadConfigOverride("web_search", "disabled")`（`internal/codexagent/run.go` 的 `threadOptions`）。
  - **「服务端工具」是什么**：Responses API 里有一类工具**不由 Codex 本地执行**，而是把 `{"type": …}` 声明发给**模型服务商**、由服务商在其服务端运行并把结果回灌给模型——本地上看不到任何调用，也**无法用 sandbox/审批拦**。对自定义 Responses provider，Codex 只会发出其中一个：内置 `web_search`（`core/src/tools/hosted_spec.rs` 的 `create_web_search_tool`，随 `web_search_mode` 决定 `external_web_access`/`indexed_web_access`）。另一个 hosted 工具（image generation）要求 OpenAI 官方鉴权，自定义 provider 下**从不发出**（`spec_plan.rs:800-829` 的 `image_generation_available`）。
  - **为什么要关（收费）**：这些工具由**服务商计费**，例如 MiniMax 的 tools 定价（<https://platform.minimax.cn/docs/pricing/overview#tools/tools>）。而且它与会话设置无关——Codex 默认打开（`WebSearchMode` 默认 `Cached`；provider capability `web_search` 默认 `true`，且 provider 侧**没有** `web_search=false` 的覆盖项）。不关的话，一个「没开联网」的会话也会悄悄联网并产生费用，且检索数据绕过了 RAGFlow 的作用域与引用链路。关掉后，模式 8 的联网**只能**走 RAGFlow 自己的 `web_search`（MCP 工具，仅在会话启用时注入，见 §4.4/D9），与本地引擎的语义一致。
  - **验证级别**：integration 层已实测（`internal/codexagent/websearch_integration_test.go`）——(a) app-server **接受** `"disabled"`、**拒绝**越界枚举值（证明该键被校验、不是被静默忽略）；(b) 用本地 mock provider 抓取**真实出站请求**：带 override 时 `tools` 里**没有** `{"type":"web_search"}`，不带 override 的对照**有**；(c) **`thread/resume` 不会重载该策略**——把一个未带 override 建好的 thread 用 override 去 resume，出站请求**仍然**带 `web_search`（`TestResumeDoesNotReloadHostedToolPolicy`）。因此策略标记折进 thread 复用键（`codexagent.HostedToolsFingerprint`，见 `run.go` 的 `RunTurn`）：策略一变 → fingerprint 变 → **重建** thread，旧 thread（策略变更前建的）才会真正丢掉服务端工具。
- **前置验证（进入 M2 的门槛，P1#2）**：必须在真实共享 Codex 上实测确认——(a) 只读 sandbox 生效（写文件/执行命令被拒）；(b) 审批请求被 `decline` 后 turn 不挂死；(c) 无内置执行工具可用。三条全绿才允许接入，并把验证脚本/清单纳入 CI（integration 层）。
- **多租户隔离（重点）**：
  - handle 必须不可猜测（32 字节随机），**但它本身不授权**：授权是本轮 turn 开始时绑定、结束时解绑的 binding，turn 之外一律 403。session 删除 `RevokeHandle` 立即失效。
  - binding 的作用域完全由服务端决定，**绝不接受工具参数里的 `dataset_ids` 越权**（工具参数里的 `dataset_ids` 只能落在 binding `DatasetIDs` 子集内，参照 `resolveDatasetScope` 的“bound 优先、请求须在内”语义，越界直接报错）。
  - **binding 携带属主 `UserID`**：MCP 请求没有 agent canvas state，下游 `NLPRetrievalAdapter.Search` 的数据集 `OperationUse` 校验必须拿到调用者身份；桥在工具调用前把 binding 的 `UserID` 注入 ctx（`agentrunt.WithUserID`），否则校验以空用户执行并返回 `Authentication required`（详见 §5.7 第③段）。
  - **binding `DatasetIDs` 为空时，检索类工具返回错误**（§4.4 第 6 点），而不是用空作用域检索——这既保证无 KB 对话可用，也杜绝“空 scope 泄漏到任意 KB”。
  - tenantID 一律取 `chat.TenantID`。
  - **残余风险（诚实记录）**：共享 Codex 持有它每个 thread 的 `mcp_servers.url`（即 handle）。若它被攻陷，可在**受害者 turn 进行期间**用受害者的 handle 调桥（MCP 请求不带 thread 身份），读到受害者的 KB。per-turn binding 把窗口从 24h 收窄到 turn 时长，但**未消除**。缓解：**按租户隔离 Codex 实例**（每租户一个 app-server），或在 SDK 支持 `mcp_servers` 的 header 后加 per-request nonce。多副本另有中间态：binding 在 Kvrocks 中一致，但 turn 的跨实例串行化仍在本计划之外（§13 / P1-5）。binding 的 TTL（10 分钟）只需大于保活间隔（binder 每 1 分钟续期一次），**与 `codex.turn_timeout` 无关**——turn 再长也靠心跳在到期前续期，锁不会中途丢失。
- 共享 Codex 若还开放 fs/命令能力，需与 MCP 检索能力解耦（靠上面的 sandbox + 审批 + 内置工具收敛约束，并以前置验证为准）。⚠️ **但 2026-10-16 实测表明「读」这一面并未被收敛**：只读沙箱下 shell 可读宿主任意路径（含运维的 `~/.codex/`），见上方「不设 `WithThreadCWD`」条。共享实例上这意味着任一租户的 turn 都可能读到其它租户与运维留在宿主上的文件——**因此"按租户隔离 Codex 实例"从 memory 隔离的前提升级为安全前提**。

#### memory（跨会话记忆）与租户隔离 —— 无租户作用域

Codex 的 memory **无法按租户/线程作用域隔离**，这是实现层的硬约束（codex 0.162.0 源码核实）：

- **根路径只有两个维度**：`codex_home.join(version.directory_name())`（`core/src/config/mod.rs:3564-3565`；`directory_name()` 只返回 `"memories"` / `"memories_v2"`，`protocol/src/memory_version.rs:17-22`）。`ext/memories/src/local/*` 后端里**没有** tenant / project / thread 命名空间——文件按版本目录平铺。
- **`codex_home` 不是可被 thread 覆盖的配置键**：`ConfigToml` 与它相关的只有 `allow_symlinked_codex_home`（`config/src/config_toml.rs:231`）；真正的 home 来自 `find_codex_home()`（`CODEX_HOME` 环境变量 / `~/.codex`），**只能在进程构造时**用 `ConfigOverrides::codex_home`（`core/src/config/mod.rs:1483-1550`）覆盖。因此 `thread/start` 的 config 覆盖表**触不到它**——`WithThreadConfigOverride` 对 memory 无效。
- **协议上没有作用域字段**：`MemoryStatusParams` 无 scope；`ThreadMemoryMode` 只有 `enabled | disabled`（每线程 kill switch，不是命名空间）。
- **唯一杠杆 = 每租户一个 `CODEX_HOME`**，即**每租户一个 app-server 进程**。当前部署是**单实例共享** → 一旦开启 memory，**A 租户的记忆会注入 B 租户 thread 的上下文**（比 §5.5 记录的 handle 泄漏更直接）。生产环境若要 memory，必须先做**按租户隔离 Codex**（同一条基础设施改动也能消除 handle 泄漏的残余风险）。

**开关**（都可在 thread config override 下发）：

| 键 | 默认 | 作用 |
|---|---|---|
| `features.memories` | **false** | `Feature::MemoryTool` 的 canonical key 是 `memories`（`features/src/lib.rs:1269`；`memory_tool` 只是 legacy 别名）。**这是总开关**：`MemoriesExtensionConfig.enabled = features.enabled(MemoryTool) && memories.use_memories` |
| `memories.use_memories` | true | 注入 memory 使用指令（关掉即不注入上下文） |
| `memories.dedicated_tools` | **false** | 暴露 memory 工具（ad_hoc_note/list/read/search）；不开则模型**没有** memory 工具 |
| `memories.generate_memories` | true | 新建 thread 的 `memory_mode`；false → 新 thread 记为 `disabled` |
| `memories.disable_on_external_context` | false | **置 true 会自废 mode-8**：MCP 工具调用会把 thread 的 memory_mode 标为 polluted（`core/src/mcp_tool_call.rs:933`），而 mode-8 全程 MCP |

memory 的写入由 app-server 进程内完成（`LocalMemoriesBackend` 用 `tokio::fs` 直接写 `CODEX_HOME`；startup extractor 同理），**不经过沙箱**——所以 §5.5 的 read-only 沙箱不阻碍 memory；反过来，memory 的隔离也不受 thread 配置影响，只能靠独立 `CODEX_HOME`。

> 现状：mode 8 在 thread config 里开启 `features.memories` + `memories.dedicated_tools`（能力上限测试用）。**这是共享实例，故记忆跨租户**；若要上线必须改为按租户 endpoint 池（每租户独立 CODEX_HOME/volume）。

### 5.6 错误、超时、降级

- 单 turn 墙钟预算 `codex.turn_timeout`（默认 30m），与 mode 5/6/7 对齐；超时 → 中断并写终局错误结果。
- **同 session 并发 turn**：`Bind` 是 KV 里的原子 `SetNX`，抢不到（已有 binding）→ `ErrCodexTurnBusy`，引擎**不发起 Codex 调用**、直接写终局错误 `session <id> is busy with another turn`。binding 由在跑的那个 turn 在 `Unbind` 时释放，或崩溃 / 挂死后随租约 TTL（≤10 分钟）自动释放。
- 未配置 `codex.endpoint` → 降级普通 RAG + 明确告警。
- WS 断开 → 依赖 `WithAutoReconnect`；恢复后仅凭流式事件补齐终局（backfill 依赖的 `thread/turns/list` 在本机 codex 不可达，见 §5.3），`sdk/*` 不原样转发前端。
- MCP 工具全失败 → 以 `<tool_error>` 形态返回给 Codex（Codex 可见），同时引擎记 `ToolCallErrors`。
- **provider 故障转移（failover）**：一轮 turn 因 provider 侧持续性故障失败时，按对话框的 `failover_llm_ids` 用下一个候选重跑——换 provider 即**重建 thread**。触发条件、候选筛选与思考流可见性见 §5.10。

### 5.7 认证链路（RAGFlow → Codex → RAGFlow MCP bridge）

三段各自独立鉴权，互不复用凭据。

**① RAGFlow API → 共享 Codex app-server（WebSocket）**

- 拨号：`codexagent.Dial(ClientConfig{Endpoint, BearerToken})`（`internal/service/codex_pipeline.go` 的 `getCodexRunner`）。
- 配置：`codex.endpoint`（`ws://` / `wss://`）+ `codex.bearer_token`（可空）；env `RAGFLOW_CODEX_ENDPOINT` / `RAGFLOW_CODEX_BEARER_TOKEN` 覆盖。
- 服务端：`codex app-server --listen ws://<host>:<port>`。**非 loopback 绑定拒绝在无鉴权下启动**，必须加 `--ws-auth capability-token`（配 `--ws-token-file` 或 `--ws-token-sha256`）或 `--ws-auth signed-bearer-token`（配 `--ws-shared-secret-file` / `--ws-issuer` / `--ws-audience`）；loopback 免鉴权（实测：`0.0.0.0` 无鉴权被拒；加 `--ws-auth capability-token` 后 0.0.0.0 可起；`127.0.0.1` 免鉴权）。
- **token 由运维自行生成——app-server 既不产出也不打印它。** 实测：启动日志只有 `listening on / readyz / healthz` 和一行“非 localhost 需要 ws 鉴权”的提示，没有 token；CLI 也没有生成 token 的子命令。因此不能“从日志或 stdout 里拿”，只能在启动前自己决定并交给服务端：
  - `capability-token`（推荐）：先生成一个随机 token 写入文件，用 `--ws-token-file <path>` 传入；或只把它的 **SHA-256**（`--ws-token-sha256 <hex>`）交给服务端，服务端不接触明文。客户端在 WS 握手携带 `Authorization: Bearer <token>`（实测：无 header → 401、错 token → 401、正确 → 101）。服务端会 **trim token 文件末尾的换行**（实测文件 `tokNL\n` 用 `Bearer tokNL` 通过），但仍应保持两端一致、无多余空白。
  - `signed-bearer-token`：给 `--ws-shared-secret-file` 加 `--ws-issuer` / `--ws-audience`，客户端须出示用该 secret 签的 JWT。RAGFlow 的 `codex.bearer_token` 是静态串，故此模式要求**预先签好长期 JWT**（当前 RAGFlow 不自签 JWT），一般用上面的 capability-token。
- `RAGFLOW_CODEX_BEARER_TOKEN`（等价 `codex.bearer_token`）填**同一个 token**。非 loopback 的最小起法：
  ```bash
  openssl rand -base64 32 | tr -d '\n' > /tmp/codex-ws-token        # 运维生成，非服务端产出
  codex app-server --listen ws://0.0.0.0:8790 \
      --ws-auth capability-token --ws-token-file /tmp/codex-ws-token
  RAGFLOW_CODEX_ENDPOINT=ws://<codex-host>:8790 \
  RAGFLOW_CODEX_BEARER_TOKEN="$(cat /tmp/codex-ws-token)" \
      ./bin/ragflow_server --api
  ```

**② Codex → 模型 provider（Responses API）**

- 每 thread 注入 provider：`WithThreadModelProvider(ProviderID)` + `WithThreadConfigOverride("model_providers."+ProviderID, {name, base_url, wire_api:"responses", experimental_bearer_token})`（`internal/codexagent/run.go`）。
- `base_url` / token 来自对话所选模型实例（`resolveCodexModelTarget`），`base_url` 先经 `normalizeResponsesBaseURL` 归一化（无版本段则补 `/v1`）。
- 门控（两层）：provider driver `Name()` 若在**已知支持集** `codex.responses_providers`（默认 `openai,azure-openai,minimax,deepseek,tongyi-qianwen,zhipu,moonshot,tencent hunyuan`）内则直接信任；否则对该模型实例的 `base_url` 做**动态探测**——向 `<base_url>/responses` 与一个必然不存在的对照路径发同一 POST（带实例真实 key），仅当两者状态码**不同**才判定支持。探测结果按 `base_url + key` 缓存（正负皆缓存，TTL 10 分钟）。两层都不通过才降级普通 RAG、不发起该调用。空集表示"无已知支持者"，即全部走探测。

**③ Codex → RAGFlow MCP bridge（streamable HTTP），及其下游数据集权限**

- RAGFlow 取 **per-session handle**：`HandleForSession(tenantID, sessionID)`——32 字节随机 → base64url，按 `(tenant, session)` 幂等铸造，**本身不携带任何权限**。
- URL 写进 thread 配置：`mcp_servers.ragflow.url = <MCPPublicBase>/mcp/codex/<handle>`；Codex 之后只按该 URL 回调。
- **授权是本轮 turn 的 binding，且它就是同会话的 turn 锁**：runner 在开跑前 `Bind(handle, CodexScope{TenantID, DatasetIDs, UserID, ChatID, WebSearch})` —— 一次 KV **原子 `SetNX`**。已有 binding（另一个 turn 在跑）→ `ErrCodexTurnBusy`，引擎回 `session <id> is busy with another turn`；抢占成功才继续。turn 结束 `Unbind`。因为 handle 每 session 稳定，这条 URL 在 resume 与 rebuild 下都一样，引擎无需知道 runner 选了哪条路。
- handle 与 binding 都存 **Kvrocks**（D11）：mint/bind 的 API 进程与服务 `/mcp/codex` 的进程可能不是同一个，KV 原子性使 binding 锁**跨进程 / 多副本**成立。
- 桥：`internal/router/router.go` 注册 `Any /mcp/codex/:token` → `mcp.NewCodexHandler(DefaultCodexTickets())`。**先按 binding 鉴权**：未知 handle → **401**；handle 存在但本轮没有 binding → **403**；都在构建任何 server/工具之前拒绝。
- 作用域由 binding 决定：工具参数里的 `dataset_ids` 只能收窄到 binding 子集（`resolveDatasetScope` 的“bound 优先、请求须在内”），越界报错；binding `DatasetIDs` 为空则检索类工具返回 `IsError`。
- **`web_search` 逐轮注入（会话能力）**：`/mcp/codex` 的 handler 在通过鉴权后调用 seam `resolveCodexWebSearch(ctx, scope)`——仅当 `scope.WebSearch` 为 true 且安装了 resolver（`service.InstallCodexWebSearchResolver()`，boot 时挂上）时，按 `scope.ChatID` 从 DB 读 `chat.PromptConfig` 并 `resolveWebSearchProvider` 解析出 provider，构造 `WebSearchFunc` 填进 `scope` 的**非导出**字段（绝不进 KV）。解析成功 → 工具集追加 `web_search`；否则不暴露。`web_search` 不依赖 KB，故不受「无 KB → 检索工具 `IsError`」守卫约束。若 `ChatID` 为空 / 无 provider / 无 resolver，一律静默不注入。
  - **这是 RAGFlow 自己的工具，不是 Codex 的**：Codex 另有**服务端（hosted）**的 `web_search`，由模型服务商执行并**计费**。它在每个 thread 上被**关闭**（`WithThreadConfigOverride("web_search", "disabled")`，见 §5.5），所以模式 8 的联网**只**经上面这条 MCP 工具、且完全受会话设置控制。
  - **不做记录**：web 引用的可信**完全由模型负责**——本轮不把搜索结果写入 KV（也没有 served set）。答案里的 `(source: web, url: …, title: …, snippet: "…")` 原样作为引用条目（见 §5.4）。
  - **为什么切换能生效（已核对 Codex 源码）**：Codex 把 MCP 工具缓存在连接的 **tool catalog** 里（`codex-mcp/src/connection_manager/tool_catalog.rs`），每轮只走 `refresh_mcp_if_dirty()`——**不 dirty 就不重列**；`thread/resume` 命中「已加载的 thread」（warm）时直接复用该缓存目录（`app-server/.../thread_processor.rs` 的 `RunningThreadResumeResult::Handled`），只有 **冷 resume**（thread 被卸载 / daemon 重启）或 **MCP 配置变化** 才会重列。`config/mcpServer/reload` 只在配置差异时生效，我们的工具集变化对 Codex 不是「配置变了」，故**不可依赖它**。因此**开关 internet 必须让 thread 重建**：`WebSearch` 折进 `ScopeFingerprint`（`codexagent.WebSearchFingerprint`，见 `internal/codexagent/run.go` 的 `RunTurn`）——决策一变 → fingerprint 变 → 复用被拒 → 重建 thread → Codex 重新 `tools/list`，桥按新 binding 返回新工具集（历史由 RAGFlow 重放，D10）。代价：每次切换重建一次 thread。
- **TTL 与保活**：所有 mode 8 键（handle / session 索引 / binding）TTL = **10 分钟**。保活**不是全局 keeper**，而是 **binder 在每个 turn 内**的 heartbeat：`Bind` 成功时起一个 goroutine，每 1 分钟 `Renew` 一次（延长 handle TTL + 刷新 binding 租约），`Unbind` 时退出。于是 handle/binding 只在 turn 期间被续期——长 turn 不会中途丢锁；崩溃 / 挂死的 turn 不再续期，binding 租约在 ≤10 分钟内到期、锁自动释放；服务停摆后遗留键也自行消失（不挂 24h）。`HandleForSession` 与 `Bind` 另外各自刷新 handle 的 TTL。
- **删除**：`RevokeHandle`（session 删除）与 `Unbind`（turn 结束）立即删除对应键。**`Resolve` 不校验 handle 有效性，只管读取当前 binding 并刷新其 TTL** —— binding 的生命周期由 `Bind`/`Unbind` 加租约 TTL 兜底决定；handle 在 turn 内不会失效，因为 `Bind` 与 turn 内 heartbeat 都在保活它。session 删除与在途 turn 的竞态由「turn 写映射前校验 handle 仍存活」处理（被删会话的 Put 被跳过）。
- **下游权限校验以 binding 属主身份执行**：`NLPRetrievalAdapter.Search` 对每个数据集做 `OperationUse` 权限校验，调用者身份取自 agent canvas state；而 MCP 请求是普通 HTTP、没有 canvas state，故 binding 携带 `UserID`（= 发起该 mode 8 turn 的用户），桥在工具调用前 `agentrunt.WithUserID(ctx, scope.UserID)` 注入、`Search` 优先读它。否则校验会以**空用户**执行并返回 `permission.ErrUnauthenticated`（归一化消息即 `Authentication required`）——只有经过该适配器的语义检索（`search_semantic_chunks`）会中招，词法工具（`grep_chunks` / `search_bm25_chunks` / `list_chunks`）不经此校验。
- 访问日志会把 `/mcp/codex/<handle>` 的 handle 段**脱敏**（`common.GinLogger` 的 `redactLogPath`）——handle 虽不授权，仍不应写进日志。

**信任边界**

- MCP bridge 端点**不用用户 token 鉴权**，而是「URL 里的 handle + 本轮 binding」。handle 不可猜、无权限、可即时吊销；授权只在本轮 turn 内有效（turn 外一律 403），且 `Bind` 不是 HTTP 路由，Codex 无法改写/放宽 binding。`MCPPublicBase` 必须指向可信的 RAGFlow 实例。
- **残余风险**：共享 Codex 能看到它每个 thread 的 URL，若被攻陷可在受害者 turn 期间借用其 handle——详见 §5.5“多租户隔离”的残余风险条目（缓解：按租户隔离 Codex）。
- 共享 Codex 若需暴露到非 loopback，必须启用 `--ws-auth`，否则任何能连上 WS 的人都可驱动它。

---

### 5.8 上下文压缩（compaction）、窗口与思考级别

Codex 在对话逼近上下文窗口时会**压缩**历史：把较早的 turn 交给模型归纳成摘要，替换掉原文。mode 8 必须知道此事，但它**不改变 mode 8 如何重建 thread**。

**两个信号面**（本机 codex 0.162.0 / 源码 tag `rust-v0.162.0-alpha.20`）：

- **item** `contextCompaction`：以 `item/started` / `item/completed` 送达，`ThreadItem::ContextCompaction { id }` —— **只有 id**（`protocol/src/items.rs` 的 `ContextCompactionItem`）。
- **通知** `thread/compacted`：`ContextCompactedNotification { thread_id, turn_id }` —— 同样**不带摘要**。

**摘要拿不到（源码结论，已复核）**：

- v2 历史构建器 `handle_compacted(&mut self, _payload)` **显式忽略 payload**；`thread_history_projection` 把 `RolloutItem::Compacted(_)` 归为空变更集；`EventMsg::ContextCompacted(..)` 是 deprecated 且**不转发**给 v2 客户端。
- 远端压缩的摘要是 `encrypted_content`（不透明）；本地压缩虽有明文摘要，但它只注入 core 历史 + rollout 文件，**不经协议暴露**。

**因此：不做截断（零损失）**。既然拿不到摘要，任何"重建时按压缩边界丢弃早期 turn"的做法都会**永久丢失信息**。所以 mode 8 的规则是：

> **重建 thread 时永远重放完整的存储历史**（`createAndRun` → `MessagesToItems(HistoryWithoutCurrent(req.Messages))`，见 run.go）。压缩只影响 live thread 的模型侧上下文；一旦发生 rebuild，上下文反而恢复完整。RAGFlow 存储的会话历史（MySQL 消息）**不做任何改动**。

**窗口与思考级别必须告知 Codex**（否则全量重放不安全）：

- Codex 只用 `model_info.context_window` 推导窗口与自动压缩阈值（`full_context_window_limit`、`auto_compact_token_limit()`）。**不在 Codex 自带 manifest 里的模型**（如 MiniMax-M3）走 `model_info_from_slug` 回退：`context_window = None` → 既不知道窗口、**也不会自动压缩** → 全量重放可能超出供应商真实窗口并在供应商侧失败。
- 共享 server 的全局 config 若设了 `model_context_window`（如宿主 `~/.codex/config.toml`）会经 `with_config_overrides` 套用到未知模型；但那多半是别的模型的数（Docker 镜像的 `CODEX_HOME` 更是**没有** config.toml）。
- mode 8 因此**逐 thread 显式告知**（`threadOptions` 的 config overrides）：
  - `model_context_window` ← 对话框模型的 `context_length`（`conf/all_models.json`，经 `dao.ResolveModelContentLength`）。
  - `model_auto_compact_token_limit` ← `context_length - max_output`（输入预算；`dao.ResolveModelMaxOutput`）——同时覆盖共享 server 的全局默认，避免过早压缩。
  - `model_reasoning_effort` ← 固定值（配置键 `codex.reasoning_effort`，默认 **`high`**）。
- 三者都是 **thread 创建时烘焙、resume 不重载**，故折进 scope fingerprint（`ModelParamsFingerprint`）：窗口/级别变化会触发一次重建。

**记账与可观测**：

- `TurnOutcome.Compactions` 统计本轮压缩次数（0/1，两个信号面去重）；经 `AsyncChatResult.Compactions` → `sseAnswerChunk.Compactions` → benchmark 行 `compactions`，并在思考流播一行提示。
- runner 读取 `thread/tokenUsage/updated` 的 `ThreadTokenUsage.ModelContextWindow` 记入 `TurnOutcome.ContextWindow`：为 0 即"Codex 不知道窗口"，可用于诊断。

---

### 5.9 提示词承载位置与计划工具（2026-10-14 修正）

**问题**：`threadOptions` 曾用 `WithThreadBaseInstructions` 承载引用契约。Codex 的 base instructions 优先级是 *config > history > 模型默认*（`core/src/session/mod.rs`），即 **config 一旦提供就完全替换** `instructions` 字段——而 Codex 自带的默认 base prompt（`models-manager/prompt.md`，275 行 / 3389 词）里含 `## Planning`、`## update_plan`、验证纪律、答案形态等 agentic 指导。替换后 mode-8 只剩 ~200 词的引用契约，且 `tools.update_plan`（计划工具）**默认关闭**（`UpdatePlanToolConfig.enabled` 默认 false；`resolve_update_plan_enabled` 对缺失的 `tools` 表按 false 处理）→ 模型既无规划指导也无规划工具，是准确率下降的一个可疑来源。

**修正**：

1. **引用契约改走 developer instructions**（`WithThreadDeveloperInstructions`）。developer instructions 是 **追加** 的 developer-role world-state fragment（`core/src/context/developer_instructions.rs`），**不替换** `instructions` 字段 → Codex 保住默认提示词，契约同时生效。
2. **打开计划工具**：thread config override `tools.update_plan.enabled = true`，使默认提示词里的 `## Planning` / `## update_plan` 与真实工具对齐。

两者都是 **create 时烘焙、resume 不重载**，故折进 scope fingerprint：`InstructionsFingerprint(根契约)` + `InstructionsFingerprint(子智能体契约)` + `InstructionsPolicyFingerprint()`（策略标记 `instructions=developer-v1;update_plan=on-v1;approval=on-request-v1;memory=on-v1;extras=clock,sleep,agents-v4`）。策略标记是**无条件**折入的，因此旧策略下建的 thread 会**重建一次**。

其余工具面：provider 托管 `web_search` 仍关闭（计费）；**shell 现在被允许用于本地计算**（审批改为 `never`，见 §5.5）——契约改成"可用 shell 做本地算术/短脚本，但 shell 无网络、不得用它抓网页"，沙箱的 read-only + `network_access=false` 是唯一的边界（写文件、联网仍被拒）。

**命令执行的叙事与计数**：runner 现在也读 `commandExecution` item（`commandRoundTrip`），把每条本地命令作为 `RoundTrip{Kind:"command", Tool:"shell", Args:<命令行>, ExitCode}` 交给 `OnTool`；服务层用 `commandNarration` 在思考流里渲染成 `shell: python3 -c '…' (exit 0, 42ms)`（`stage:"shell"`），与 MCP 步骤区分。命令计入 `tool_call_counts["shell"]`（**独立键**，不影响 benchmark 的 Search Calls——后者只累加配置里的检索工具），但**不进入** retrieved/served 检索台账。**子智能体同理**：`collabAgentToolCall` item（`collabRoundTrip`）作为 `RoundTrip{Kind:"agent", Tool:"agent:<spawnAgent|sendMessage|wait|listAgents|…>"}` 叙事成 `agent: spawnAgent <任务>`（`stage:"agent"`），计入 `tool_call_counts["agent:<tool>"]`。

**额外开启的能力开关**（能力上限测试）：`features.current_time_reminder`（加 `current_time` 工具）、`features.sleep_tool`（默认已开但 mode 默认 `model_driven`，仅在时钟提醒开启时才注册 —— 这里显式设 `{enabled:true, mode:"always_on"}`）、`features.multi_agent_v2` + `features.multi_agent_v2_dynamic_tools`（V2 协作工具：spawn/send_message/followup_task/list_agents/wait_agent；子智能体会额外开 thread，token 与并发都会上升）。`features.multi_agent_v2` 以**表**（而非简写 `true`）传入，以便同时携带 `subagent_developer_instructions`（子智能体契约，见 §5.4）。

**`features.token_budget` 已关闭（2026-11-02）**。此前为“能力上限测试”开启，实测收益为零、副作用是破坏性的：
- 它注册的 `get_context_remaining` / `new_context` 在 815 个 mode-8 会话里，`new_context` 被调用 **0 次**，`get_context_remaining` 被调用 7 次且**返回值全是 `0`**（只在预算已耗尽时才被想起，此时该数字无信息量）；`reminder_threshold_tokens` 未配置，提醒分支从不触发。
- 它把自动压缩换成 `compact_token_budget::run_inline_auto_compact_task`（`core/src/session/turn.rs` 的 `if features.enabled(Feature::TokenBudget)` 分支），该路径**跳过模型摘要**、直接开新窗口，写入 `CompactedItem.message = ""` 且 `replacement_history` 不含任何 user 消息。结果：压缩后模型看不到题目，回复 "What would you like me to work on?"。全部 501 次 mode-8 压缩摘要均为空；对照的 127 次 `codex-tui`/`codex_vscode`/`codex_exec` 压缩（不启该特性）全部带真实摘要。
- 关闭后走正常 `run_auto_compact`：对没有 `RemoteCompactionSupport::V2` 的 provider（所有非 OpenAI/Azure，含 MiniMax）即 `compact.rs` 的本地压缩，保留 user 消息并由模型生成真实摘要。

---

### 5.10 provider 故障转移（failover，2026-10-14 新增）

**目标**：一轮 turn 在**主 provider** 上因 provider 侧的持续性故障失败时，自动改用对话框配置的下一个候选（其 base_url / key / model）重跑这一轮。失败触发、自动、有界（每个候选最多试一次）。

**候选来源**：复用对话框的 `llm_setting.failover_llm_ids`——与 mode 5/6/7 的 eino 链**同一份名单**（读取者 `agenticFailoverModelIDs`）。主模型在前，成员按配置顺序在后；`codexTurnTargets` 逐个解析（`ResolveChatTarget` + `normalizeResponsesBaseURL` + **各自 llm id** 的 window/output），并**跳过**解析失败 / 不在 `codex.responses_providers` 白名单 / 无 `base_url` / 与前面重复的候选（逐条告警，best-effort，与 `agenticModelChain` 的取舍一致）。

**触发条件**（`codexFailoverEligible`）——以下任一即**不**转移：

- `ErrCodexTurnBusy`：会话锁，不是 provider 故障；
- 请求 ctx 已结束：客户端已走，重试没有接收方；
- turn 被 interrupted：人为停止；
- **已经向客户端输出过 answer 文本**（`contentDelta.Len() > 0`）：再试会把第二份答案接在第一份之后。

其余情况下只有**类型化**的 provider 故障才转移。为此补了两层类型：

- SDK（fork `codex-app-server-go`）：`CodexErrorKind` + `ParseCodexErrorInfo` / `ErrorEvent.Classify()` / `ClassifyTurnError(*Turn)`，把上游 `error.codexErrorInfo` 的两种联合形态（字符串枚举；`{"httpConnectionFailed":{"httpStatusCode":500}}` 式单键对象）解析成类型；
- 引擎（`internal/codexagent`）：`TurnError{Status, Kind, HTTPStatus, Message}` 实现 `error`，可用 `errors.As` 取；`TurnError.RetryableProviderFailure()` 承载转移策略。

**可转移的 kind**：`internalServerError`（provider HTTP 500）、`serverOverloaded`、`responseTooManyFailedAttempts`（429）、`httpConnectionFailed`、`responseStreamConnectionFailed`、`responseStreamDisconnected`、`unauthorized`。**`other` 与空 kind 一律不转移**：上游映射有损（未知模型 400 / 被拒提示词 / 内容过滤都落到 `other`），属确定性失败，换 provider 只会再花一轮。这是刻意的保守取舍；实测若发现漏判再放宽。

**线程**：provider 是 thread 创建时烘焙的，**换 provider 必然重建 thread**。首次尝试走正常复用路径；重试带 `TurnRequest.ForceRebuild`——**既不复用、也不写回** ThreadRecord。不写回是刻意的：provider 身份不在复用键里，写回会让下一轮的主模型指纹命中这条记录、静默续用指向备用端点的 thread；不写回则保持"每轮仍先试主模型"。

**可见性**：转移时向思考流写一条模型级事件 `ThinkEvent{Kind:"provider_failover", Stage:"provider"}`，行文为 `provider failover: <model1>(<instance1>) failed (<kind>[, http N]); retrying on <model2>(<instance2>)`——**必须带 instance 名**：候选之间往往 provider 与 model 相同、只有 instance（即 base_url/凭据）不同，不写 instance 就会退化成"<model> 失败，改用 <model>"。并 `WarnCtx` 记一条日志（含 chat_id / from / to / kind / http_status，from/to 用同样的带 instance 标签）。全部候选失败 → 仍走既有的 `**ERROR**` 终局（busy 文案不变）。

**未做**：候选冷却（eino 的 30s `failoverCooldown` 短路）。主模型每轮都会先被尝试，若它持续故障则每轮多一次快速失败 + 一次重建；后续可按需补上。

**运维注意**：候选成员与主模型共用同一门控（见 §5.5）：driver `Name()` 在已知支持集内、或其实例 `base_url` 探测出 Responses 路由，才算可用；否则被跳过（日志带 `reason`）。跨 provider 的故障转移若用未列入已知集的自定义端点，探测会自动放行而无需加白名单。

**实测（2026-10-14，本机）**：让对话框主模型指向一个固定返回 HTTP 500 的本地端点、成员指向 MiniMax-M3，跑一轮 mode 8：

- 错误已类型化：终局/日志从 `code="\"internalServerError\""` 变为 `kind=internalServerError`；
- 触发转移并落 `codex: provider failover` 告警（含 from/to/kind/http_status）；
- 重试**确实重建了 thread**（codex 侧出现两条独立 rollout）；
- 思考流出现 `think_event{kind:"provider_failover", stage:"provider"}` 及其文本行。实测原文（主模型 = `MiniMax-M3` 实例 `sim500` 指向死端点，成员 = `MiniMax-M3` 实例 `xjx`）：`provider failover: MiniMax-M3(sim500) failed (internalServerError); retrying on MiniMax-M3(xjx)`；日志同形（`from` / `to` 用同一标签）；
- 把成员换成 SiliconFlow 时被正确跳过：`reason: provider "SILICONFLOW" is not in codex.responses_providers and <base_url> exposes no Responses route`（best-effort 语义生效；此处 reason 文案为两层门控后的当前形态，原实测为旧白名单文案）。
- **未演示"备用成功"**：白名单内只有 MiniMax，而当时它自身也在返回 500；要演示成功路径，需把某个健康 provider 加入白名单。

## 6. 依赖与版本固定

- `github.com/zealbase/codex-app-server-go` 要求 **Go ≥ 1.25**；RAGFlow 当前 `go 1.27`，兼容。
- 本地 checkout 的 `go.mod` 声明 module 为 `github.com/zealbase/codex-app-server-go`，而 GitHub fork 在 `github.com/yuzhichang/codex-app-server-go`。**注意 module path 与仓库 URL 不一致**：
  - 方案 A：直接 `require github.com/zealbase/codex-app-server-go`（若该路径可拉取）。
  - 方案 B：`require github.com/zealbase/codex-app-server-go` + `replace ... => github.com/yuzhichang/codex-app-server-go <commit/branch>`，锁定 `feat/schema-alignment-and-coverage` 的具体 commit。
  - 建议 **方案 B**，把 feature 分支固定到 commit，避免跟踪浮动分支。
- `modelcontextprotocol/go-sdk v1.8.0` 已在依赖中（`go.mod:55`），MCP bridge 无需新增依赖。
- ⚠️ **已知 SDK 缺陷：`RunStreamed` 事件订阅竞态（P1#9）——已修复（M0-a）**。原 `RunStreamed` 先调 `TurnStart`、返回后才订阅（`thread.go:243-256`），快速服务端会丢事件/挂超时。**已在本 fork 修复并提交 `412c6d4`**：改为「先 `sub := client.Events()`，再 `TurnStart`」（与 `RunInputs` 对齐），并加确定性回归测试 `thread_runstream_race_test.go::TestRunStreamedSubscribesBeforeTurnStart`（在 `turn/start` 返回前注入事件；已验证未修复时该测试失败/超时）。
- ✅ **同一提交还修复了 `Client.ResumeThread` 丢弃 opts 的问题**：现在把 `ThreadOption` 集映射到 `ThreadResumeParams`（该结构本就支持完整 override），每轮 resume 都会重新声明 model/provider/config(MCP)/sandbox/instructions。回归测试：`thread_config_test.go::TestResumeThreadForwardsOverrides`。
- 依赖锁定：`replace github.com/zealbase/codex-app-server-go => github.com/yuzhichang/codex-app-server-go 412c6d4`（M0-e 落 `go.mod`）。
- ⚠️ **同步 `SessionThread.Run` 与本机 codex 不兼容（实测 0.162.0）**：其收尾走 `thread/read`（`IncludeTurns`），而该 codex 返回 `[-32601] list_turns is not supported yet`。**M2 必须用 `RunStreamed`**（它基于事件订阅，实测可用：能收到 `turn/completed`），不要用 `Run`/`WaitForTurn`。turn 的最终文本从流式事件（`ItemAgentMessageDeltaEvent` + `ItemCompletedEvent`/`TurnCompletedEvent`）组装。

---

## 7. 测试计划

按 `AGENTS.md` 的 Go 测试分层（build tag）：

- **Unit（无 tag，`bash build.sh --test`）**
  - `selectAgenticEngine`：新增 `{8, true} -> engineCodex`、**`{8, false} -> engineCodex`**（mode 8 不受 KB 约束；注意 5/6/7 的 `{5,false}/{6,false}/{7,false} -> ""` 行为不变）。扩展 `chat_pipeline_test.go:2730` 的表。
  - `resolveReasoningLevel` / `harnessMaxLevel` 边界：mode 8 不进 harness（扩展 `chat_pipeline_test.go:2664` 系列）。
  - MCP bridge 工具：用 in-memory/httptest 桩验证“票据作用域 → 检索请求”的映射，以及越权 `dataset_ids` 被裁剪；**空 `DatasetIDs` 时检索类工具返回 `IsError` 而非空作用域检索**。
  - 事件 → `AsyncChatResult` 翻译：用假的 Codex 事件序列驱动，断言 think 边界、增量和终局字段。
  - `codexagent` 票据铸造/校验/回收的单测。
  - **thread 映射**：`(tenant_id, session_id)` 命中→`ResumeThread`、未命中→`StartThread`+落库；**整个 turn 期间同一 session 的并发请求被串行**（P1#3）；**无 session id 或 `store_history_messages=false` 时走一次性 thread（不读/不写映射）**（D11/P1#7）。
  - **历史重放（D10）**：RAGFlow messages → Responses API items 的转换；首建时注入“历史去掉最后一条 user 消息”，当前 user 消息作为 turn input 不重复注入。
  - **fingerprint 漂移**：`scope_fingerprint` 变化（加/删/改绑 KB、无→有 KB）→ 重建 thread；`history_fingerprint` 变化（删除消息 `DeleteSessionMessage`、同 session 切换 reasoning level、他模式写入）→ 重建或同步（P1#6/P1#8）。
  - **模型选择**：断言 `StartThread` 带上 `WithThreadModel(resolvedModelName)`（`getLLMModelConfig` 的 `modelName`），而非仅 provider（P1#4）。
  - **backfill 归并**：模拟断线期间 turn/completed，断言收到 `sdk/sessionBackfilled` 后能补齐终局且与已流出增量去重（P1#5）。
  - **RunStreamed 快速完成**：协议桩在 `TurnStart` 前就发 delta/completed，断言无丢失、终局可达（P1#9）。
  - **引用编号**：模型输出 `chunk_id:` 后，断言由后端统一生成 `[ID:N]` 且过滤无效 id 后编号仍与 `reference.chunks` 对齐（P2#11）。
  - **list_chunks 契约**：断言复用现有 schema（`doc_id`/`anchor_chunk_ids`/`number_neighbors`），无 `offset/limit`（P2#10）。
- **Integration（`-tags integration`）**
  - 对真实（或容器化）共享 Codex WS 端点跑一次 `StartThread` → `RunStreamed` → MCP 工具被调用并返回命中。
  - **能力隔离前置验证（P1#2）**：只读 sandbox 拒绝写/执行、审批 decline 不挂死、无内置执行工具可用——三条全绿。
  - 对真实 ES/Infinity + 真 KB 验证 MCP 工具返回非空、引用可解析。
  - 多轮：同一 session 连续两轮复用同一 thread，第二轮能看到第一轮上下文（历史重放生效）。
- **E2E（`-tags e2e`）**
  - 前端 level=7 → `AsyncChat` → Codex → MCP → 检索 → 最终答案含引用；同一 session 多轮。
- **Manual（`-tags manual`，仅本地）**
  - 长会话/断线重连/票据过期的演练。

回归：确认 mode 0–7 行为不变（现有测试全绿）。

---

## 8. 已决与未决

### 已决（2026-10-09 评审）

- **D4 模型**：创建 thread 时由 dialog 配置的模型决定——`WithThreadModel(resolvedModelName)` + `WithThreadModelProvider` + `model_providers.<id>` override；`modelName` 取自 `getLLMModelConfig`，不得用复合 llm_id。
- **D3 MCP 注入**：当前 Codex 已支持经 thread config override 注入 streamable-HTTP MCP server；**仅在建 thread 时注入**（`ResumeThread` 不转发配置，见 D3 注）。
- **D5 引用**：thread 的 system prompt 约定「引用 chunk ID」，**由后端统一生成 `[ID:N]`**（§4.7 / P2#11）。
- **D7 工具集**：不暴露 `think` / `todo_write` / `run_javascript` 等玩具工具。
- **D6 映射**：Codex thread 与 RAGFlow session 一一对应；thread 只承载模型状态，权威数据每轮现算、漂移即重建。
- **D8 无 KB 也可用**：mode 8 不受 `hasKBs` 约束，无 KB 仍访问 Codex；MCP 检索类工具在无 `kb_id` 作用域时返回错误，而非空作用域检索。
- **D9 不暴露 `search_chunks`**：MCP 检索工具集为 `search_semantic_chunks` / `search_bm25_chunks` / `grep_chunks` / `list_chunks`；`web_search` 仅在会话启用网络搜索时逐轮注入（binding 带 `ChatID`+`WebSearch`，桥按 `ChatID` 从 DB 解析 provider，凭据不入 KV）。
- **D10 对话历史重放**：thread（首建/重建/一次性）时经 `ThreadInjectItems` 把历史注入 thread。
- **D11 无 session id 退化**：`openai_chat.go` / `bot_completion.go` 无 session id → 每次调用新建一次性 thread（重放请求携带的历史）。

### 未决

- **U1（前置验证，逐项实测）**：(a) `mcp_servers.<name>` 的 streamable-HTTP 配置字段/鉴权形状；(b) **resume 的 config overlay 是否会在服务端重载 `mcp_servers` 条目**（决定 KB/票据变化走「resume 刷新」还是「重建 thread」）——**已非架构阻塞**（SDK 已修复为 resume 转发 override，见 D3/commit `412c6d4`，即使不重载也可回退重建）；(c) 只读 sandbox + decline 的能力隔离效果（P1#2）。三项状态在 §11.2 独立记录。
- **U2（已定，2026-10-16）**：dialog 模型需支持 Codex 的 `responses` wire API。判定分两层：driver `Name()` 在 `codex.responses_providers` 已知支持集内 → 直接信任；否则对该实例 `base_url` 做差分探测（`/responses` vs 对照路径，带实例真实 key）→ 仅当状态码不同才支持。不再要求运维预先把每个 provider 列入白名单。
- **U3（已定）**：引用契约固定为「引用 chunk ID，后端编号」——见 §4.7，不再是未决。
- **U4**：`codex.turn_timeout` 与 Codex 侧最大 turn 时长是否冲突；中断（`TurnInterrupt`）如何触发。

### 评审修正记录（2026-10-09，均已落文）

| # | 问题 | 处理位置 |
|---|---|---|
| P1#1 | 票据生命周期与 thread 复用冲突；`ResumeThread` 不转发配置 | D2/D3、§5.2 第 9 点、§5.4 |
| P1#2 | `ApprovalNever` 不能禁止执行 | §5.5（只读 sandbox + decline + 禁内置工具 + 前置验证）、D3 |
| P1#3 | SDK 不保证同 thread 跨请求串行 | §5.2 第 7 点（锁覆盖整个 turn + 分布式锁） |
| P1#4 | 模型示例未选 dialog 模型 | §4.5（加 `WithThreadModel`）、D4 |
| P1#5 | 忽略 backfill 会丢答案/挂超时 | §5.3（订阅恢复事件、按 turn 合并去重） |
| P1#6 | 历史删除/切换模式后仍用旧上下文 | §5.2 第 6 点（history_fingerprint 校验/重建） |
| P1#7 | 非存储请求污染持久 thread | §5.2 第 2 点（按存储语义决定一次性 thread） |
| P1#8 | 固定 KB 作用域不反映配置变化 | §5.2 第 5 点（逐轮绑定 scope + 变化重建） |
| P1#9 | SDK `RunStreamed` 订阅竞态 | §6（修复或绕开 + 快速完成桩测试） |
| P2#10 | `list_chunks` 契约写错 | §5.1（复用现有 schema，无 offset/limit） |
| P2#11 | 模型 `[ID:N]` 无法可靠校正 | §4.7（固定 chunk ID 契约，后端统一编号） |

---

## 9. 里程碑与实施顺序

**M0 是硬门禁；M0-b…M0-d 未全部实测通过前，M1 及之后仍视为阻塞，不得开工。** M0 内部按以下顺序执行：

1. **M0-a — 修复 SDK 订阅竞态（P1#9）✅ 已完成（代码+单测级）**：fork 提交 `412c6d4`——`RunStreamed` 改为先订阅再 `TurnStart`；并附带修复 `ResumeThread` 丢弃 opts。回归测试 `TestRunStreamedSubscribesBeforeTurnStart`、`TestResumeThreadForwardsOverrides`；SDK 全量单测绿。
2. **M0-b — 验证 MCP 注入（U1(a)）**：确认 `mcp_servers.<name>` streamable-HTTP 的配置字段与鉴权形状，Codex 能实际加载并调用该 server。**未实测。**
3. **M0-c — 验证 resume 配置 overlay 是否重载 MCP（U1(b)，已非架构阻塞）**：确认 `thread/resume` 携带的 config overlay 是否会重载 `mcp_servers` 条目——决定 KB/票据变化走「resume 刷新」还是「重建 thread」。两种结果都可行（SDK 已能转发 override）。**未实测。**
4. **M0-d — 验证内置工具限制与隔离（U1(c) / P1#2）**：只读 sandbox 生效、审批 decline 不挂死、内置执行工具不可用。**未实测。**
5. **M0-e — 骨架 ✅ 已完成**：新增 `CodexConfig` 与解析测试；新增 `reasoningLevelCodex` + `engineCodex` + `selectAgenticEngine`（level 8 在 `!hasKBs` 早退前处理）+ AsyncChat dispatch（`codexConfigured()` 门控 + `codexAgent` seam）；前端 level 8 可见（正则 `[0-8]`、Codex 选项、en/zh locale）。SDK 依赖的 `replace 412c6d4` 推迟到 M2（`codexagent` 真正 import SDK 时再加，否则 `go mod tidy` 会删掉未使用的 require）。

后续阶段：

6. **M1 — MCP bridge ✅ 已完成**：`internal/mcp/codex.go` 新增线程级票据注册表（mint/resolve/revoke/TTL）、`NewCodexToolServer(scope)`（暴露 `search_semantic_chunks` / `search_bm25_chunks` / `grep_chunks` / `list_chunks`，**复用 eino 工具的 Info() schema 与 `InvokableRun`**）、无 scope 返回 `IsError`、`NewCodexHandler` 与路由 `Any /mcp/codex/:token`（用票据鉴权，不经过用户鉴权）。单测覆盖票据、工具集、无 scope 错误、未知 token 401。
7. **M2 — Codex 客户端接入**：`internal/codexagent` 连接共享 Codex、按 `(tenant_id, session_id)` + fingerprint 起/复用/重建 thread（D4/D6/P1#6/P1#8）、建 thread 时注入 MCP override（D3）、重放历史（D10）、按 dialog 模型选 `WithThreadModel`（P1#4）、只读 sandbox（P1#2）、**整个 turn 持锁**（P1#3）、**订阅恢复事件合并 backfill**（P1#5）、把事件翻译成 `AsyncChatResult`；集成测试。
8. **M3 — 贯通 ✅ 已实现（编译+单测级）**：`codexAgent` 接入 `internal/codexagent.Runner`；每轮 mint 线程级票据并拼 `MCPPublicBase + /mcp/codex/<token>`；解析 dialog 模型（`resolveCodexModelTarget`）并用 **Responses 能力白名单**（`codex.responses_providers`，决策 U2=A）门控——不兼容则降级普通 RAG；reasoning 用 `<think>` 边界、answer 增量、终局带 `Reference`（`buildAgenticReference`）。`codex_pipeline.go` 接入 dispatch。
9. **M4 — 加固**：多租户隔离、超时/降级、重连、非存储/无 session 入口，文档收尾。

每一步遵循根 `AGENTS.md`：小步、单一实现路径、无兼容分支、改完跑最窄相关测试（`bash build.sh --test ./internal/...`）。

---

## 10. 需要复核的既有事实（写代码前逐条核对）

- `selectAgenticEngine` 当前签名是 `(level int, hasKBs bool)`，注意 `agent_mode` 参数已被移除（`chat_pipeline.go:6100` 注释）。
- 前端 `normalizeThinkingLevel`（`next.tsx:60`）正则为 `/^[0-8]$/`，覆盖 0–8 的引擎档；注释已同步（5 = react, 6 = planer-explorer-auditor, 7 = IterSynth, 8 = coding-agent）。
- `AsyncChatResult` 字段（`chat_pipeline.go:93`）中 mode 5/6/7 用到的台账字段需与 mode 8 保持一致，便于 benchmark 按字段对比。
- `internal/mcp/http.go` 的 `Stateless: true` 是否满足 Codex MCP 客户端对会话的要求（若需有状态会话，需改用 SSE/有状态 streamable）。
- **SDK 行为已核对（2026-10-09，本地 checkout）**：`ResumeThread` 丢弃配置（`client.go:489-508`）；`ApprovalNever`=从不请求审批（`types.go:488`）；`RunStreamed` 先 TurnStart 后订阅（`thread.go:243-256`）；`turnMu` 为对象级（`thread.go:144`）；`list_chunks` 参数为 `doc_id`+`anchor_chunk_ids`+`number_neighbors`（`tool_list_chunks.go:53-57`）；`citations.go` 仅解析 `chunk_id:`、跳过已含 `[ID:` 的行。

---

## 11. 状态记录（文档修订 vs 运行验证）

两类状态**分开记录**，不得混为一谈：**「文档已修订」**只表示计划书已改，**「运行验证通过」**必须附实测环境、命令与结果。写入方各自独立，一方通过不代表另一方通过。

### 11.1 文档修订状态（本文件）

| 项 | 状态 | 日期 | 说明 |
|---|---|---|---|
| P1#1–#9、P2#10–#11 修订 | ✅ 已修订 | 2026-10-09 | 见 §8「评审修正记录」 |
| M0 门禁顺序（a→e）固定 | ✅ 已修订 | 2026-10-09 | §9 |
| U1(b) 由「架构关键阻塞」降级为「刷新 vs 重建的实测点」 | ✅ 已修订 | 2026-10-09 | SDK 修复 `ResumeThread` 转发 override（commit `412c6d4`），§3 D2/D3、§5.2、§8 U1 |

### 11.2 运行验证状态（实测，逐项独立）

> 记录格式：`项 | 状态 | 环境/命令 | 结果 | 日期`。未实测前一律 `⏳ 未验证`。

| 门禁项 | 状态 | 环境 / 命令 | 结果 | 日期 |
|---|---|---|---|---|
| M0-a SDK 订阅竞态修复 + 回归测试 | ✅ 已通过（单测级） | `cd ~/github.com/Zealbase/codex-app-server-go && go test -count=1 . ./internal/...` | 全绿；未修复时 `TestRunStreamedSubscribesBeforeTurnStart` 5s 超时失败 | 2026-10-09 |
| M0-b MCP 注入字段/鉴权（U1(a)） | ✅ 已通过（实测） | 本地 codex 0.162.0 + SDK：`WithThreadConfigOverride("mcp_servers.ragflow", {url})` | config 形状 `[mcp_servers.<name>] url=...` 生效；`mcpServerStatus/list` 可见该 server 且 codex 对其发起 MCP handshake | 2026-10-09 |
| M0-c resume overlay 是否重载 MCP（U1(b)） | ✅ 已通过（实测，结论=不重载） | 同上：resume 时把 URL 从 :9 改为 :10 | resume 后 `mcpServerStatus/list` 的 origin 仍为 :9 → **resume 不重载 mcp_servers**；作用域/票据变化必须**重建 thread** | 2026-10-09 |
| M0-d 隔离（U1(c)/P1#2） | ✅ 通过（(c) 例外见备注） | `go test -tags integration -run 'TestSandboxReadOnlyBlocksWrites\|TestApprovalDeclineDoesNotHang' ./internal/codexagent/` | (a) **read-only sandbox 生效**；(b) **审批 decline 不挂死**：生产 `Dispatcher`（命令/文件 decline）+ read-only 下，写文件 turn 自行完成、不写文件；(c) 未关闭内置命令/文件工具（codex 0.162 无该配置项；靠 read-only + decline 限制） | 2026-10-09 |
| M0-e 骨架编译/单测 | ✅ 已通过 | `bash build.sh --test ./internal/server/config/... ./internal/service/...` + `npx tsc --noEmit`（仅新改动文件）+ `npx oxlint` | config/service 全绿；前端改动文件无新增类型/lint 错误 | 2026-10-09 |
| M1 MCP bridge 单测 | ✅ 已通过 | `bash build.sh --test ./internal/mcp/... ./internal/router/...` | 全绿（票据 mint/resolve/revoke/过期、工具集不含 search_chunks、无 scope 返回 IsError、未知 token 401） | 2026-10-09 |
| M1 端到端（Codex 实调 MCP） | ✅ 已通过（实测） | `go test -tags integration -run TestCodexCallsBridgeTool ./internal/mcp/` | codex 经 bridge 实调 `search_semantic_chunks`：`answer="chunk-e2e-0001"`、`ledger.calls=map[search_semantic_chunks:1]`、`ledger.chunks=map[chunk-e2e-0001]`；根因与修复见 §12 | 2026-10-09 |
| 评审修正（P1#5/#7、§5.2/§5.4/§5.5） | ✅ 已通过（单测/集成级） | `bash build.sh --test ./internal/{codexagent,mcp,service,server/config}/...` + `go test -tags integration`（M2/sandbox/E2E） | 无终局不判成功（backfill 归并在 0.162 不可达、已移除，见 §5.3）；`store_history_messages=false`→一次性 thread；空 CWD；session 删除清映射+失效票据；`AsyncChatResult` 补齐工具计数/命中文档/deep-shallow | 2026-10-09 |
| §7 缺测补齐（作用域/并发/隔离） | ✅ 已通过 | `bash build.sh --test ./internal/agentic_rag/... ./internal/codexagent/...` + 上述 M0-d 集成 | `resolveDatasetScope` 越权即拒（不静默裁剪）；`sessionLock` 键=(tenant,session) 且同会话串行；审批 decline 不挂死（生产 Dispatcher） | 2026-10-09 |
| M2 runner 端到端（真 codex + 真模型） | ✅ 已通过 | `RAGFLOW_CODEX_IT=1 OPENAI_BASE_URL/KEY/MODEL=... go test -tags integration -run TestRunnerAgainstLocalCodex ./internal/codexagent/` | turn1 `rebuilt=true content="PONG"`；turn2 `rebuilt=false`（复用同一 thread）`content="PONG2"` | 2026-10-09 |
| M3 贯通（`codexAgent` 接线） | ✅ 已通过（编译+单测级） | `bash build.sh --test ./internal/service/... ./internal/server/config/...` | 全绿；`codexAgent` 走 Runner + 票据 + Responses 白名单门控 | 2026-10-09 |
| M3 端到端（AsyncChat → Codex → MCP → 引用） | ✅ 已通过（实测，范围有限） | `go test -tags cgo,static,integration -run TestCodexAsyncChatEndToEnd ./internal/service/`（需 CGO env） | `AsyncChat(level 7)` 经 dialog 模型解析→runner→本地 codex WS→answer `"E2EOK"`；**MCP 仅 handshake，未触发工具调用；无真实 KB/ES，引用链路未端到端**（当时 Codex 记为 level 7；现行编号为 level 8） | 2026-10-09 |
| P2#11 引用台账白名单 | ⤳ 已被 D12 取代（2026-10-12） | `bash build.sh --test ./internal/mcp/... ./internal/service/...` | 该行原描述 mode 8 用 bridge 台账白名单限定引用；台账已移除（D12），改用 KB 作用域校验，模式 5/6/7 不受影响 | 2026-10-09 |
| D2/D11 per-turn binding（KV handle+binding） | ✅ 已通过（单测级） | `bash build.sh --test ./internal/mcp/... ./internal/codexagent/... ./internal/service/...` | `codex_test.go` 覆盖：handle 按 session 幂等且无权限、binding 装/卸、未知 handle→401、无 binding→403、`RevokeHandle` 清 session+binding、一次性 handle 每次全新；runner 在 `TurnBinder` seam 上 Bind/Unbind | 2026-10-12 |
| binding 即分布式锁（`Bind`=SETNX）+ turn 内 heartbeat | ✅ 已通过（单测 + 运行） | `bash build.sh --test ./internal/mcp/ ./internal/codexagent/ ./internal/service/`；实测 API（新二进制）对真实 Kvrocks | `Bind` 二次 → `ErrCodexTurnBusy`（`TestCodexBindingIsPerTurn`）；`Renew` 延长 handle+binding 租约且**不复活**已释放的 binding（`TestCodexRenewExtendsHandleAndBindingLease`）；binder 在 `Bind` 起 heartbeat、`Unbind` 退出；Kvrocks `SetNX` 走原子 `SET..NX EX`。**实测**：未知 handle→401；handle 存在但无 binding→403 `no active turn for this handle`；写入 binding 后同 handle→200 且 `tools/list` 正常；访问日志 handle 段脱敏为 `/mcp/codex/***`。跨进程真 turn 的并发 busy 断言留待多实例压测 | 2026-10-12 |
| M3 端到端（真 KB + 工具实调 + 引用） | ✅ 已通过（实测） | `POST /api/v1/chat/completions`（`reasoning:8`，租户 API key） | 两轮 mode-8 turn 均 HTTP 200；codex rollout 显示经 `McpToolCall ragflow` 调 `search_bm25_chunks` / `list_chunks`；answer 引用真实 `chunk_id`（如 `e750849b1a6fe27b`、`e20543b45c2537c7`，doc=`三国演义.txt`） | 2026-10-12 |
| §5.7③ 语义检索鉴权（票据属主 `UserID` 注入） | ✅ 已通过（实测） | 用活 ticket 直连 bridge：`tools/call search_semantic_chunks` | 返回真实向量命中（`score=0.805`，`doc_name=三国演义.txt`）；修复前同调用返回 `Authentication required`（`permission.ErrUnauthenticated`） | 2026-10-12 |
| Responses `base_url` 归一化 + turn 错误明细 | ✅ 已通过（实测） | 同上 mode-8 调用 | MiniMax `https://api.minimaxi.com` 归一化为 `/v1` 后调通 `/v1/responses`；失败 turn 终局带 `turn.error` 明文（修复前为空 `(code=""):`） | 2026-10-12 |
| pretoken 标点正则修复 | ✅ 已通过（单测 + 运行） | `bash build.sh --test ./internal/service/nlp/`；重启后跑一轮 mode-8 | 新单测 `TestPretokenPunctPattern` 全绿；API 日志不再出现 `invalid regular expression` | 2026-10-12 |
| 非 loopback WS 鉴权（capability-token）与 token 来源 | ✅ 已通过（实测） | `codex app-server --listen ws://0.0.0.0:8791 --ws-auth capability-token --ws-token-file <file>`；对 `/` 发 WS 握手探测 | 0.0.0.0 无鉴权被拒、加 `--ws-auth capability-token` 后可起；无 header / 错 token → **401**，正确 token → **101**；启动日志**不含** token（token 由运维生成，CLI 无生成子命令）；token 文件末尾换行被 trim | 2026-10-12 |
| MCP `web_search` 按会话设置逐轮注入 | ✅ 已通过（单测级） | `bash build.sh --test ./internal/mcp/... ./internal/service/... ./internal/codexagent/... ./internal/agentic_rag/...` | binding 带 `ChatID`+`WebSearch`；桥经 `resolveCodexWebSearch` seam 解析 provider 后追加 `web_search`。单测：未开启（或 resolver 未装/返回 nil）→ 不暴露且 resolver 不被调用；开启 → 暴露，且在**无 KB** 会话下工具可用（不受 KB 守卫）；`WebSearchFingerprint` 改变组合 reuse key。**已核对 Codex 源码**：工具目录按 thread 缓存、warm resume 不重列 → 以 `WebSearch` 折进 `scope_fingerprint` 触发重建来保证切换生效。跨进程/多副本与真机端到端未实测 | 2026-10-12 |
| mode-8 引用：tuple 契约 + 原地 `[ID:N]` + web 条目 + 展示副本 | ✅ 已通过（单测级） | `bash build.sh --test ./internal/mcp/... ./internal/agentic_rag/... ./internal/service/...` | 契约为两种 provenance tuple（语料 doc/doc_id/chunk_id；web source/url/title）；`RewriteProvenance` 把 tuple（或裸字段）**原地**替换为 `[ID:N]`，未解析则保留、幂等；mode 6/7 走同一 pass；`buildCodexReference` 校验语料（KB 作用域）、web 由模型负责（**结果不写 KV**）；统一编号（语料先、web 后）；DB 存原始输出 + `content_display`，读取换取使重载仍显示引用。单测覆盖：原地替换/共享 chunk/未解析/裸字段/web tuple/片段含括号/web 提取去重/served set 移除后的记录路径。真机端到端未实测 | 2026-10-12 |
| 关闭 Codex 服务端（hosted）`web_search`（计费工具） | ✅ 已通过（集成级） | `RAGFLOW_CODEX_IT=1 go test -tags integration -run 'TestWebSearchOverride\|TestResumeDoesNotReloadHostedToolPolicy' ./internal/codexagent/` | (a) app-server **接受** `web_search="disabled"`、**拒绝**越界枚举（`TestWebSearchOverrideIsHonored`）——证明该键被校验；(b) 本地 mock provider 抓**真实出站请求**：带 override 时 `tools` 无 `{"type":"web_search"}`，**对照**（无 override）有（`TestWebSearchOverrideRemovesHostedTool`）；(c) **resume 不重载该策略**（`TestResumeDoesNotReloadHostedToolPolicy`）→ 策略折进 `HostedToolsFingerprint` 迫使旧 thread 重建；单测 `TestHostedToolsFingerprintChangesReuseKey` 锁住复用键参与。真机（MiniMax）端到端未实测 | 2026-10-13 |
| 契约 generic（能力交给 MCP 工具列表）+ 禁 shell 联网 | ✅ 已通过（单测级） | `bash build.sh --test ./internal/service/`（`TestCodexCitationContractIsGenericAndForbidsTheShell`） | 契约**不接收** webSearch，联网开关下**逐字相同**；含「tool list … authoritative / use exactly the tools it contains」（能力以工具列表为准，不自称有无 web）；含「no shell and no internet access」+「exec_command」「curl」「not a way to reach the web」（明确禁止用 shell 联网）；`source: web` 形**无条件**但以「returned by the web search tool」条件化；`quote=false` 仍保留能力声明、只去掉出处格式。联网开关仍折进 `ScopeFingerprint`（工具目录需重建），与提示词无关。**真机未实测** | 2026-10-13 |
| 思考过程（流式，真机实测） | ✅ 已通过（真机） | `POST /api/v1/chat/completions`（`reasoning:8`，stream） | 真机 MiniMax：`<think>` 开→**多帧持续更新**（单轮 6.4k~8.4k 字符，分 7~10 帧流式到达）→`</think>` 关；随后答案带 `[ID:N]` 与 `reference.chunks`。单测另覆盖无 delta 的 provider 兜底（`TestReasoningItemText`） | 2026-10-13 |
| app-server 容器化（语料隔离，真机实测） | ✅ 已通过（真机） | `docker compose up -d --no-deps codex`（`infiniflow/codex:0.166.0-local`，ubuntu:26.04，无语料挂载）+ `POST /api/v1/chat/completions`（`reasoning:8`） | 容器内 `/home` 仅 `codex`/`ubuntu`，`/home/zhichyu/Downloads/data/browsecomp-plus/corpus` **不存在**；容器可达 `host.docker.internal:9380`（未知 handle → 401）；`readyz=200`；两轮 turn `http=200 answer="OK"`，rollout 写入容器卷；`sandbox_mode=danger-full-access`（容器即边界）；`CODEX_NO_PROXY` 绕过 Docker 注入的代理，镜像补 `git` 后 memory workspace 报错消失 | 2026-11-03 |

| 每轮 token 用量（对齐 mode 5 形状，真机实测） | ✅ 已通过（真机） | `POST /api/v1/chat/completions`（`reasoning:8`，stream 与非 stream 各一轮） | 真机实测：非 stream `usage={"prompt_tokens":81962,"completion_tokens":1173,"total_tokens":83135,"llm_calls":5}`；stream 终局帧 `usage={"prompt_tokens":208018,"completion_tokens":2743,"total_tokens":210761,"llm_calls":8}` + `elapsed_seconds=129.5`。单测 `TestUsageOrNil`、`TestSSEMarshalChunkCarriesTurnAccounting` | 2026-10-13 |
| 检索步骤（tool + 参数）写进思考流（真机实测） | ✅ 已通过（真机） | 同上（stream） | 真机 MiniMax 单轮 **36 行工具叙述**，形如 `search_bm25_chunks {"queries":[...],"top_n":10}` 与 `grep_chunks returned in 5908ms`（**无前缀**，前后各一个换行）——工具名、参数、耗时、成败全部可见，且与思考文本同流。单测 `TestMcpToolRoundTrip` / `TestCompactArgs` / `TestNarrationChunkFraming` / `TestToolNarrationHasNoPrefix` | 2026-10-13 |
| 工具调用计数 `tool_call_counts`（对齐 mode 5） | ✅ 已通过（真机） | 同上（stream） | 真机单轮终局帧：`tool_call_counts={"grep_chunks":1,"search_bm25_chunks":1}`，与 mode 5 的 `{"search_bm25_chunks":16,"list_chunks":12,…}` 同形；`tool_call_errors` 仅在 `status=failed` 时出现。单测 `TestToolTally`、`TestMcpToolRoundTripNormalizesInProgress`（v2 的 `inProgress` 归一为 `running`） | 2026-10-13 |
| 检索会计 retrieved/served + deep/shallow（对齐 mode 5，真机实测） | ✅ 已通过（真机） | 同上（stream） | 真机单轮终局帧：`retrieved_docids=["三国演义.txt"]`、`served_docids=["三国演义.txt"]`、`deep_read_chunks=8`、`shallow_read_chunks=65`（附 `deep_read_chunk_ids`(8) / `shallow_read_chunk_ids`(59)，与 `tool_call_counts={grep_chunks:3,list_chunks:2,search_bm25_chunks:2}` 自洽）。分类逻辑与 mode 5 共用叶子包 `internal/retrievalaccount`（`AccountToolOutput`），口径同源；**已知差异**：`served_docids` 的权重单位为「调用次数」（mode 5 为「不同 query 数」），集合相同、排序权重不同。单测 `TestRetrievalAccounting` | 2026-10-13 |
| 流式序列化器补齐 turn 会计字段 | ✅ 已通过（单测 + 真机） | `bash build.sh --test ./internal/service/`（`TestSSEMarshalChunkCarriesTurnAccounting`）+ 上述真机 | `sseMarshalChunk` 是白名单结构，此前**静默丢弃** `usage`/`elapsed_seconds`/`tool_call_counts`/`retrieved_docids` 等（mode 5/6/7 同样受累）；补齐后真机 stream 终局帧已带 `usage` 与 `elapsed_seconds` | 2026-10-13 |

**门禁规则**：只要 11.2 中 M0-b…M0-d 任一项为 `⏳ 未验证` 或 `❌ 未通过`，**M1 及之后一律阻塞**。M0-c（U1(b)）已非架构阻塞：无论 resume 是否重载 MCP，都可分别走「resume 刷新票据」或「重建 thread」，两种路径均在计划内。

> 说明：M0-b…M0-d 需连真实共享 Codex，本环境不可执行；代码可先按计划推进（M0-e/M1 不含对真实 Codex 的运行时依赖），但 M1 之后的端到端验证必须以这三项实测通过为前提。

---

## 12. MCP 工具调用审批（2026-10-09 定位并修复 ✅，本机 codex 0.162.0）

**结论：MCP 工具调用受 `mcpServer/elicitation/request` 门控；不装 handler 会被 codex 拒绝，检索路径全死。已定位并修复。**

复现方法：本地 codex（stdio），线程注入 `mcp_servers.ragflow.url` 指向本进程 MCP bridge（活的 httptest），注册假检索服务返回已知 chunk；prompt 明确要求调用 `search_semantic_chunks`。用 `WithRequestHandler` 打印原始请求后定位到：

**根因**：MCP 工具调用前，codex 下发一个 `mcpServer/elicitation/request`，其 `mode="form"`、`_meta.codex_approval_kind="mcp_tool_call"`，message 形如 `Allow the ragflow MCP server to run tool "search_semantic_chunks"?`。**SDK 默认 `DeclineElicitation()`**（无 handler）→ codex 报 “rejected by the user” → 工具从不执行。
- 之前误以为走 `item/permissions/requestApproval`（那 4 个 `ApprovalHandler` 分支**从不被触发**，已加日志证实）。
- `approval_policy` / `approvals_reviewer` / `mcp_servers.*.approval_mode` 都不是正确杠杆（`approvals_reviewer=auto_review` 反而因缺 `codex-auto-review` 模型报错 2013）。

**修复**：`internal/codexagent/approval.go` 提供 `Dispatcher()`（`*codexgo.Dispatcher`，仅设 `Elicitation`）：命中 `codex_approval_kind=="mcp_tool_call"` 的 elicitation → **accept**（`Content={}`），其余 elicitation/命令/文件/权限一律默认拒绝；`Dial` 用 `WithRequestHandler(Dispatcher())` 安装。

**实测（`TestCodexCallsBridgeTool`，已提交）**：`answer="chunk-e2e-0001"`，`ledger.calls=map[search_semantic_chunks:1]`，`ledger.chunks=map[chunk-e2e-0001]` → **codex 真正经 bridge 调用了检索工具并拿到命中**。

**对 §5.5 的修正（重要）**：隔离姿态不能是“统一 decline”。正确姿态是 **accept MCP 工具调用（检索必需）** + **decline 命令执行/文件变更/权限/其它 elicitation**，再由 read-only sandbox 兜底。旧的“统一 decline”会连同检索工具一起挡掉。

（历史阻塞记录见 git 提交 `1f2f07b43`；本节为修复后的定稿。）

---

## 13. 已接受的设计约束（评审 P1-5 / P2-13 的处置）

### P1-5 跨实例整轮锁 —— **已实现**（KV binding 即分布式锁）

**决策（2026-10-12，取代早前 “不做” 的结论）**：会话级串行由 **per-turn binding** 提供，而 binding 是 Kvrocks 里对 `ragflow:codex:bind:<handle>` 的一次 **原子 `SetNX`**。因此互斥**跨进程 / 跨副本**成立，无需 sticky 路由，也不再有任何 `keyedLocks` 类的进程内锁（`internal/codexagent/run.go` 已删除）。

- **获取**：`Runner` 在进 thread 阶段前 `Bind(handle, CodexScope{…})`——抢到 = 得到整轮锁；已有 binding（另一个 turn 在跑）→ `ErrCodexTurnBusy`，引擎回一条 `session <id> is busy with another turn` 的终局错误，且**不发起 Codex 调用**（所以既不会覆盖对方 scope，也不会在对方 `Unbind` 时误删 binding）。
- **持有 / 释放**：锁在 turn 全程持有（`Bind` → resume-or-start → run → 收尾 → `Unbind`）；turn 末 `Unbind` 真正释放。binding 同时是**租约**（TTL 10 分钟），由 binder 在 turn 内每 1 分钟 `Renew` —— 崩溃 / 挂死的 turn 不再续期，租约到期自动释放锁。
- **续期约束**：binding 的 TTL 只需**大于保活间隔**，让心跳在到期前把租约续上即可（当前 TTL 10 分钟、每 1 分钟 `Renew`）——**与 `codex.turn_timeout` 无关**，TTL 远小于 turn 时长也没问题，turn 再长都靠心跳维持。反过来，TTL 也不宜过长：崩溃 / 挂死的 turn 会在停止续期后由 TTL 兜底释放锁，TTL 越长，锁被卡住的上限越久。
- **部署前提已去掉**：早前「单点登录 ⇒ 同 session 不会并发」的前提与「单副本 / 粘性路由」要求**不再需要**——KV 锁与部署拓扑无关。

> ⚠️ 早前版本（本文件 git 历史）曾记录“不实现分布式锁、依赖单点登录 + 粘性路由”。**该结论已作废**：锁迁到 KV 后，多副本无需粘性路由，`Put` 的覆盖式 upsert 也被同一把锁保护。

### P2-13 provider 默认 endpoint —— 采用方案 (b)：由实例显式提供 `base_url`

**决策**：mode 8 **不**从 provider 工厂默认值解析 endpoint；要求 mode 8 对话所选**模型实例显式配置 `base_url`**（指向支持 **Responses API** 的端点）。

**原因**：`codexAgent` 只用 `target.APIConfig.BaseURL`（即模型实例的 `base_url`）；Codex 需要 `wire_api="responses"`，而 provider 工厂默认 URL 未必指向 Responses 端点。把“实例 `base_url` 为空 → 回退工厂默认”留给 Codex 会静默打到错误的 wire API。显式配置让“该实例是否可用于 mode 8”在配置期即可判断。

**接入前校验（已实现）**：`codexResponsesCapable` 要求实例 `base_url` 非空，且能力判定通过——provider 在已知支持集内，或该 `base_url` 探测出 Responses 路由（见 §5.5 / U2）；不满足即**降级普通 RAG 并打明确告警**（含 `reason`）。`codexAgent` 亦对空 `base_url` 直接返回明确错误，不会以空 `base_url` 声明 provider。

### P1-7 超时中断：SDK 暴露 turn ID + 已知协议限制

**修复**：`RunStreamed` 从不返回服务端分配的 turn ID，导致「远端已启动、本地尚未消费首个携带 ID 的事件就被取消」时无法定向中断。已在本 fork 新增 `SessionThread.RunStreamedTurn(ctx, input, ...) (<-chan, turnID, err)`——`turnID` 由 `TurnStart` **同步返回**（事件流与 `RunStreamed` 相同，含「先订阅后启动」）；`consume` 改用之，两个取消出口都有 ID 可中断。SDK pin 提升到 `yuzhichang/codex-app-server-go v0.0.0-20261010044256-14e2b46c590a`（commit `14e2b46`）。

**已知协议限制（如实记录）**：若 `TurnStart` **取消或报错**，**不代表远端没有 turn**——服务端可能已启动、只是启动响应未送达。此时本地**缺少 turn ID**，属于「启动结果未知，无法定向中断」。这是协议层的固有限制（无 ID 无从指定要中断的 turn），不是实现缺陷；如需彻底消除，需要 Codex 协议支持「按 thread 中断当前 turn」或客户端可查询 thread 的当前 turn ID。
