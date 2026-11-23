# 查询策略 RSI（递归自改进）框架 — 计划书

> 目标读者：实现者。本文只描述设计与落地步骤，不含最终代码。
> 一句话：在 BrowseComp-Plus 上，用一个 Go 程序**模仿模型训练循环**做查询策略的批式自改进——
> 每题用**当前全局策略**测一次；答错则在同一会话上自省出 ≤2 条「策略教训」；凑满一批（`batch_size` 默认 10）
> 就用这批评分和教训**更新全局策略**；如此按 pass（轮）推进，**跑到收敛**（§6.3）。

---

## 1. 目标与非目标

**目标**

1. 从 `/home/zhichyu/github.com/infiniflow/ragflow/outputs/` 下**历次 browsecomp benchmark 结果**聚合出**答错频次最高的 150 题**，持久化为一个**冻结文件**，训练期间 **readonly**（§3.2/§3.3）。
2. 维护一个**全局策略候选池**（可超过 10 条）+ 单调递增的 **version**；按累计自省评分排序，仅前 10 条生效，作为所有题共享的「搜索策略」提示块。
3. **批式训练循环**（核心）：
   - 每个问题在**本轮测试**时都带**当前全局策略**（不再有「无策略首测」——空集合的那一轮天然就是基线）。
   - 答错 → **同一会话**上自省，给生效策略评分并产出 ≤2 条带预测评分的**策略教训**。
   - 每批题目完成（`batch_size`，默认 **10**）→ 累计评分、归并候选并重新排名（仅前 10 条生效）。
   - 按 **pass** 遍历题目集，**跑到收敛为止**（准确率 ≥ `target_accuracy` 或相对上一 pass 提升 < `min_delta`）；`max_passes` 仅作可选兜底（0=不限）。
4. 全程**并发**跑题；一批内并发、**批间为同步屏障**（冻结版本）。
5. 可持久化、可断点续跑、可复现；每 pass 出学习曲线（准确率）。
6. 策略满足硬约束：**极简 / 不重叠 / 不矛盾 / 无语料库特定词汇**（§4、§9.2）。

**非目标（本期不做）**

- 不改 RAGFlow 服务端的 API/引擎（不新增一等公民 `strategy` 字段）。策略通过**把提示块前置到 `question`** 注入——对 level 0/1-4/5/6/7/8 全部引擎都生效（见 §7）。
- 不引入新的判分模型/协议：复用 benchmark 既有的 grader（见 §10）。
- **不做**策略的跨题留出（holdout）泛化验证（已定，见 §17）。

---

## 2. 现有资产（复用，不重造）

| 资产 | 位置 | 复用什么 |
|---|---|---|
| benchmark 工具 | `tools/benchmark/main.go`（3920 行） | 题目/语料加载、HTTP 客户端、建会话、提问、判分、leaderboard 组装 |
| 语料 | `/home/zhichyu/Downloads/data/browsecomp-plus/` | `questions.jsonl`（830 题）+ `corpus/<doc_id>.md`（94k 页） |
| 历史结果 | `/home/zhichyu/github.com/infiniflow/ragflow/outputs/*/leaderboard.json`（31 份含 `per_query_judgements`） | 训练题集来源：聚合高频错题 top-150 |
| 轻量客户端范式 | `tools/mode8verify/main.go:102-209` | `post`/`ask`/`createSession` 的写法与环境变量约定 |

**可精确复用的函数**（`tools/benchmark/main.go`）

- `loadQuestions` (`:1173`) / `loadQuestionsJSONL` (`:1227`) / `normalizeQuestion` (`:1281`)：读 `questions.jsonl`，字段别名见 `:1149-1161`；`question` 结构体 `:1163-1170`（`questionID/question/goldAnswer/expectedDocs/evidenceDocs`）。
- `newClient` (`:790`)：API key 取 `cfg.ragflow_api_key` 或 `RAGFLOW_API_KEY`；base url 取 `backend.base_url` 或 `scheme://host:port`。
- `createSession` (`:1000`)：`POST /api/v1/chats/{chatID}/sessions {"name": ...}`，会话 id 由 `extractSessionID` (`:983`) 提取。
- `askRagflow` (`:1014-1042`)：`POST /api/v1/chat/completions`，body = `{chat_id, question, stream, [session_id], [llm_id], [quote/…], reasoning}`；**`reasoning` 是引擎选择器**。
- `parseGraderVerdict` (`:3148`)：解析判分 → `{correct, confidence, extracted_final_answer, verdict}`；grader 模板固定在 `:734-759`。
- `extractRunStats` (`:2138`)：抓 `retrieved/served docids`、token、`llm_calls`——用于成本核算。
- `buildLeaderboard` (`:3387`)：最终汇总（`per_query_judgements` 在 `:3662-3674`）。

---

## 3. 输入数据契约

### 3.1 语料 `questions.jsonl`（全量问题宇宙）

每行一条（实测）：

```json
{
  "id": "769",
  "question": "Please tell me the name of the learning institution ...",
  "gold_answer": "Queen Arwa University",
  "expected_doc_ids": ["5412", "82002"],
  "expected_sources": ["5412", "82002"],
  "evidence_doc_ids": ["5412", "82002"],
  "reference_granularity": "document",
  "missing_expected_doc_ids": []
}
```

- `question`/`gold_answer`：给 RSI 用的题面与标准答案。
- `expected_doc_ids`：**引用/金标文档**；`evidence_doc_ids`：recall 的 qrel 超集。
- 复用 `normalizeQuestion` 的别名表，避免绑定单一写法。

### 3.2 训练题集：历次 benchmark 高频错题 top-150（冻结）

**来源**：`/home/zhichyu/github.com/infiniflow/ragflow/outputs/` 下**所有** browsecomp 系列 benchmark 结果（即所有带 `per_query_judgements` 的 `*/leaderboard.json`）。

**聚合规则**：逐份读 `per_query_judgements`（行 schema：`{query_id, run_key, correct, confidence, extracted_final_answer, verdict{reasoning}}`），按 `query_id` 累计「判分次数 `seen`」「答错次数 `wrong`」。**只统计已判分行**（`correct` 为 `null` 的未判分/判分失败行既不计 `seen` 也不计 `wrong`）。排序取**前 150 题**：

> **排序键 = `(wrong/seen desc, wrong desc, qid asc)`** —— **先按答错率**，再看绝对答错次数。答错率高但曝光少的题会被提前（“稳定答错”比“偶发答错”更值得作为训练信号）；`wrong` 仅作并列时的次序。

**实测（本机现状，用于校准；严格口径）**：**25 份结果、3037 行已判分**（3111 行中 74 行 `correct=null` 被排除）、**830 个唯一 `query_id`、289 个曾答错**；top-150 截断线为 `wrong/seen = 1/3 ≈ 0.333`（榜首为 `9/9` 全错）。

**冻结契约（关键）**：训练题集**一次性生成**（`--build-trainset`）后写入文件，**在 RSI 训练期间 readonly**：
- 训练进程**只读打开**该文件（`--trainset <file>`，绝不回写）；文件带 `sha256`，启动时校验，不一致即终止。
- 训练参数变化**不改变**题集；要换题集必须重新 `--build-trainset` 产出新文件。
- 冒烟测试可用 `--ids` 选子集，但题面仍取自该冻结文件。

### 3.3 冻结训练题集文件 `trainset.jsonl`

每行一题（含 provenance，可离线复核）：

```json
{"id":"1193","question":"...","gold_answer":"...",
 "expected_doc_ids":["..."],"evidence_doc_ids":["..."],
 "stats":{"wrong":10,"seen":11,"sources":["c2_smoke68_full830_20261005","browsecomp_retry2_20260921"]}}
```

- 伴随 `trainset.meta.json`：`{built_at, benchmark_outputs_dir, run_filter, sort_rule, n_runs, n_rows, top_n, cutoff_wrong, cutoff_seen, questions_path, sha256, runs[], excluded_runs[]}`。
- 训练运行**只用这个文件**，不再直接读 `outputs/`——保证实验可复现、题集不再漂移。

---

## 4. 术语

- **策略（strategy）**：一条**极简、祈使、跨题通用、且不含语料库特定词汇**的方法学指令。用户样例的内容方向对，但括号里的举例属题目特定内容，入库前必须剥掉——
  ```
  Follow the following search strategy:
  - Conduct parallel searches using multiple clues simultaneously rather than sequentially;
    assign each clue to a separate subagent to pursue in parallel.
  - Group subagents by the specificity of the clues, not evenly distributed by clue count.
  ```
- **策略教训（lesson）**：单题答错后、在**同一会话**上自省得到的 ≤2 条改进。**格式与硬约束同「策略」**——它就是待并入全局集合的候选策略。
- **策略质量要求（教训 / 归并 / 校验器三重强制；四条均由大模型判定）**：
  1. **极简**〔LLM 判定〕：每条 = 一个祈使句、尽量短、无铺垫/解释/举例（机械兜底：渲染前对 >`maxStrategyWords`(30) 词做截断）。
  2. **无语料库特定词汇**〔LLM 判定〕：**不得包含任何「特定高价值可检索项」**——人名 / 机构名 / 地名 / 时间日期 / 数字，或只对本题成立的**具体事实/短语**。策略只能是**通用方法**（怎么做），不能是**具体线索**（查什么）。
  3. **不重叠**〔LLM 判定〕：任意两条应落在不同决策轴，不得同义复述。
  4. **不矛盾**〔LLM 判定〕：任何一条不得与另一条或已在册策略冲突。
  > **判据只有大模型**：**不设** DF、白名单、词面相似度（Jaccard）或冲突模式表等机械/启发式规则——它们会误杀方法学词、也会漏判同义异形。大模型对每条候选给出逐项结论（`terse / corpus_specific / duplicate_of / contradicts` + 理由），程序只据其**执行**（丢弃 / 映射 / 保留高分者）。
- **全局策略集合（strategy set）**：包含全部通过校验的候选策略，按累计自省评分排序，**仅前 `max_strategies`（默认 10）条生效**并渲染成一个提示块（§7）；其余候选保留记录。带单调递增 **version**；生效条目持久化为版本化 text 文件，完整候选池及评分另行持久化（§12）。
- **批发生器 / 批（batch）**：**从整个训练题集随机抽样组批**——每个 pass 先对全集做**无放回随机抽样**（即一个随机排列），再按 `batch_size`（默认 **10**）切批；因此**同一 pass 内各批互不相交、必然不同**，且**每个题目在该 pass 内恰被抽中一次**；**pass 之间重新抽样，题目可跨 pass 重复**。批内每题用当前全局策略测一次，答错者贡献 ≤2 条教训；整批完成即触发一次全局策略更新。`batch.version` = 该批测试时所用的全局版本。
- **pass（轮）**：一次**无放回随机抽样**覆盖整个训练题集的过程，共 `ceil(N / batch_size)` 批；pass 之间重新抽样、用更新后的全局策略重跑。**默认不限轮数、跑到收敛**（§6.3；`max_passes=0` = 不限，可选设上限兜底）。
- **批级失败（无「回滚」语义）**：指本批只产生**「不更新」**的结果——(a) 归并失败（超时/永久），或 (b) 整批全对（无教训无评分）。处理 = **集合与评分都不应用、`version` 不变、继续下一批**。**不存在「回滚已提交状态」**：已提交的都是永久保留的历史版本，**无需也不应撤销**。逐题失败/跳过另有规则（§6.4），不影响本批其余部分正常更新。
- **会话（session）**：RAGFlow 对话框下的一次对话（`session_id`）。用于「测试」与「同会话自省」；自省复用测试那次会话。
- **尝试（attempt）**：某题在某 pass 的一次「测试→判分」。

---

## 5. 总体架构与数据流（批式训练循环）

```
  训练题集（冻结文件：高频错题 top-150）
        │
        ▼
  ┌───────────────┐   冻结 global@V    ┌───────────────────────────┐
  │  批发生器      │ ────────────────► │  一批(≤B 题)并发测试        │
  │ (取下一批题目) │                   │  ask(策略块@V + question)  │
  └───────▲───────┘                   └─────────────┬─────────────┘
          │                                         │ judge
          │                             ┌───────────┴────────────┐
          │                             │ 答错 → 同会话自省 ≤2 教训 │
          │                             └───────────┬────────────┘
          │          global@V+1                      │ 教训入批
          └────── 归并(候选池 + 本批评分/教训 → 排名，前10生效) ◄──┘
                    通过 §9.2 校验器 → 落盘 → version+1
```

要点：
- **批内并发、批间同步屏障**；更新全局是批级别事件（单写者，§8）。
- 训练是**在线**的（批间策略版本在变）；**每个 pass 结束另跑一次「冻结评估」**（用该 pass 末版本、全题集、不更新、顺序固定），学习曲线与收敛判据**一律以冻结评估为准**（§6.3）——否则「最后一批刚归并出的、从没测过的策略」会直接进最终输出。
- 空集合**不用于在线基线**；基线由**独立的无策略对照跑**得到（§15）。

---

## 6. 批式训练循环（伪代码）

```text
global = StrategySet(items=[])                      # 初始为空 → 第一批即基线

for p in 1, 2, ...:                                 # 不限轮数，跑到收敛（§6.3）
    sample = samplePass(trainQuestions, seed, p)    # pass 内无放回随机抽样（=随机排列）；随 checkpoint 持久化（§12）
    gen = newBatchGenerator(sample, B=batch_size)   # 切批：各批互不相交、必然不同

    for batch in gen:                               # 每批 ≤B 题
        V = global.version                          # 冻结本批所用版本
        results = parallelMap(batch.questions, C) { q ->
            s   = createSession(chatID)
            ans = ask(s, renderStrategy(global) + "\n\n" + q.question, reasoning=L)  # §7
            # 失败分流（§6.4）：额度/网络 → endless 重试；超时 → 该题 skip（本批其余题照跑）
            v   = judge(q, ans)                             # judge 失败同样按 §6.4 分流
            record(q, p, V, ans, v)
            if v.correct:
                return {q, correct=true}
            reflection = selfReflect(s, q.gold, active(global))  # 生效策略评分 + ≤2 条新教训
            return {q, correct=false, lessons:reflection.lessons, ratings:reflection.ratings}
        }

        batch.lessons = flatten(answers where correct==false).lessons   # ≤ 2B 条
        batch.ratings = flatten(results).ratings
        if batch.lessons non-empty or batch.ratings non-empty:
            global = mergeAndValidate(global, batch.lessons, batch.ratings, meta={pass:p, batch:gen.index})  # §8/§9
        commitBatch(global, p, gen.index)           # 含完整候选池/评分；无更新批也提交（§12）
        # 瓶颈：本批全部完成才更新 → 下一批用新 global；天然同步屏障

    # 冻结评估：该 pass 末版本、全题集、不更新、顺序固定（§6.3）
    eval = frozenEvaluate(global, trainQuestions, L)
    persistPass(p, eval)                            # 学习曲线/收敛判据用 eval，不是在线准确率

finalLeaderboard()                                  # 汇总（§12）
```

**确定性收敛（worker 完成顺序不得影响结果）**：`parallelMap` 结果按**批内题目位置**收集（**不是**完成顺序）；每题教训按题目内序号排序；**新教训的临时 ID 按该稳定顺序分配**（`t<pass>.<batch>.<qpos>.<k>`）。因此网络快慢**不改变归并输入**，也不改变同分时的前 10 名。

### 6.1 自省提示词（renderImprovePrompt）

在该题测试用的同一会话上追加一个 user turn。提示给出**当前生效集合**，但**仅供评分与引用（含「替换」）**；**单题阶段不做任何跨策略去重/冲突裁决**（全部交给批级，§8/§9）——因此**允许**提出与已有策略重复的教训，也**允许**提出**替换**某条策略的教训：

```
Your search did not find the correct answer.
The correct answer is: <gold_answer>.

Search strategies currently in force (shown for reference — you may RATE them, and you may
give a lesson that RESTATES one (it will be mapped to it) or that REPLACES one, by naming its id):
- <strategy_id>: <strategy 1>
- <strategy_id>: <strategy 2>
...

Reflect on how you searched, then give AT MOST 2 lessons to improve the SEARCH STRATEGY
(NOT the answer to this specific question), subject to these rules:
- TERSE: each item is ONE imperative sentence, <= 30 words (hard cap; longer is truncated).
  No preamble, no explanation, no examples.
- FOCUSED: target a decision axis (planning / clue splitting / subagent assignment /
  parallelization / cross-verification / dedupe). Prefer an uncovered axis, but a RESTATEMENT
  is allowed — it will be mapped to the existing strategy, do NOT drop it.
- REPLACEMENTS ALLOWED: to correct an in-force strategy you believe is wrong, emit a lesson
  with "replaces":"<strategy_id>" instead of dropping it.
- CORPUS-AGNOSTIC: no entity names, proper nouns, years/numbers, quoted phrases, URLs, or any
  wording that only makes sense for this corpus or this question. Use only generic
  methodology words (search / clue / subagent / parallel / assign / verify / merge).
Rate EACH in-force strategy with an integer -2..+2 for how it helped this search:
-2 misleading, -1 unhelpful, 0 neutral/no evidence, +1 helpful, +2 very helpful. Add a short reason.
Also give each new lesson a predicted usefulness score on the same scale, and a short reason;
do not claim a new lesson was actually used.
Output ONLY JSON:
{"ratings":[{"strategy_id":"...","score":0,"reason":"..."}],
 "lessons":[{"text":"...","score":1,"reason":"...","replaces":"<strategy_id or omitted>"}]}
The lessons array must contain at most 2 items.
```

- **只取教训、不取其回答**：gold 仅用于让模型反思失败，产出的是通用策略。
- **结构化解析** `parseReflection`：解析上述 JSON 对象；评分须为 `[-2,2]` 内整数，策略 ID 须属于本题生效集合，且不得重复。非法/缺失评分不计分、不补零；**教训仅按结构/文本规则过滤**（见下条），解析失败按自省失败处理，不从散文猜测分数。**`replaces`（可选）**：须属于本题生效集合，否则**视为新教训**；**替换/映射/冲突裁决一律留给批级（§8/§9）**。
- **评分口径**：已有生效策略记 `basis=used`，新教训记 `basis=predicted`；只有答错后的自省产生评分，答对及冻结评估不评分。候选未生效期间不因未使用而扣分。
- **单题阶段只做机械规范化**：每条教训压成单句、去编号/引号/末尾标点、超 `maxStrategyWords`(30) 词截断。**质量判定（极简 / 无语料库特定词汇 / 重叠 / 矛盾）全部由大模型在批级做**（§9.2）；跨策略去重/映射/冲突裁决也**统一交给批级（§8/§9）**。清洗后 0-2 条入批，并分配**临时 ID**（§9.1）。
- 当前集合为空（首 pass 首批）时，该段渲染为 `(none yet)`。

### 6.2 批口径与触发

- **批口径（已定）**：**一批 = `batch_size`（默认 10）道题**；批内教训数 ≤2B、可能 <B（答对题无教训）。
- **抽样（已定）**：**pass 内无放回**——每个 pass 对全集做一次无放回随机抽样再切批，故**同一 pass 内各批互不相交、必然不同**、每题恰被抽一次；**跨 pass 重新抽样**，题目可重复出现（类似每 epoch 重新洗牌）。
- **触发**：本批**全部完成**即用其教训更新一次全局策略。
  > 备选（不采用）：口径 B「一批 = B 条教训」——贴合固定梯度数，但题目数与批边界脱钩，与「批发生器」语义不符。
- **残余批**：pass 末尾若还剩不足一批的题目，其教训**照常做一次收尾更新**（否则尾部题目白跑）。
- **全对批**：整批全对 → 无教训 = 无梯度，**不更新**，`version` 不变。
- **首批**：`global` 为空 → 该批**不带策略块**（纯题面）。但它只有 `batch_size` 题，**不构成全训练集基线**；基线由**独立无策略对照跑**得到（§15）。

### 6.3 在线训练 vs 冻结评估，与收敛判据

**必须区分两个准确率**：
- **在线准确率**（训练中各批实测）——同时受**策略版本、题序、批位置**影响，**不能**代表最终策略水平，仅作训练日志。
- **冻结评估准确率**（`frozen_accuracy`）——按 `eval_every_k_passes`（默认每 10 个 pass）用**该 pass 末版本**在**同一训练题集**上跑一遍**不更新、顺序固定**的评估。**收敛判据与 `after_accuracy` 一律用它**；`after_accuracy` = **最后一份已提交的冻结评估**（见下，退出/打断不补评）。

**收敛判据（三条同时满足才停）**：
1. **覆盖率达标**：`coverage = 本 pass 判分题数 / 训练题集大小 ≥ min_coverage`（默认 **0.9**）。防止「大量超时被跳过 → 分母很小 → 假 100%」（§6.4 跳过）。
2. **累计样本非零**：`scored > 0`；`scored == 0`（全跳过）时准确率记 `null`，**不满足收敛**。
3. 满足其一：冻结评估 `accuracy ≥ target_accuracy`（默认 100%）**或** 相对上一 pass 的 `improvement < min_delta`（默认 0.5 个百分点）。

- **不收敛时**：继续下一 pass；上一 pass 里 `skipped` 的题**下一 pass 重试**（§6.4）。
- **兜底**：可选 `max_passes > 0` 作为安全上限，防空转。
- **评估频率与比较口径**：每 `eval_every_k_passes`（默认 **10**）个 pass 跑一次冻结评估；`improvement` 只与 `last_committed_eval`（上一份**已提交**的冻结评估）比较；**未评估的 pass 不产生评估点、不参与比较**。
- **退出 / 外部打断不强制评估**：因收敛、`max_passes`、手动停止或**外部打断**（SIGINT/SIGTERM 等）结束时，**不补跑**最终版本的冻结评估——协调器只在**下一个提交边界**停下（完成当前批的原子提交，或放弃在途 attempt），写 checkpoint 后退出。因此**最终版本可能没有已提交的评估**；报告须标明「最后一份评估对应的 `pass/version`」与「最终版本 `evaluated=false`」。硬杀（SIGKILL）由 §12 的恢复语义兜底。
- **全新会话**：**基线跑**与**每一次冻结评估**都用**新建会话**（不复用任何自省会话），确保不读入含 gold 的自省历史。
- **代价**：评估走一遍全题集；默认每 10 个 pass 评一次（`eval_every_k_passes=10`）以摊薄成本。

### 6.4 错误分类与重试 / 跳过策略（按「阶段 × 失败原因」）

四个阶段：**测试(ask) / 判分(judge) / 自省(self-reflect) / 归并(merge)**。失败原因四类，分类顺序：**先超时 → 再额度 → 再网络 → 否则永久失败**。

| 阶段 | 额度 / 网络 | 超时 | 永久失败(4xx) |
|---|---|---|---|
| **测试 ask** | **先对账** → endless 重试 | **跳过该题**（`status=skipped`） | 跳过该题 |
| **判分 judge** | **先对账** → endless 重试 | 跳过该题（本题判分不可得） | 跳过该题 |
| **自省 self-reflect** | **先对账** → endless 重试 | **保留已完成的判分**（该题仍计 wrong），仅标 `lessons=failed`，**不跳过整题** | 同左 |
| **归并 merge** | **先对账** → endless 重试 | **放弃本批更新**（保留现有集合、`version` 不变），记 warning，继续下一批 | 同左 |

- **重试不是无副作用（重要）**：服务端在**调用模型前**就把 user turn 写入会话（`internal/service/chat_session.go:1388-1393`）。因此「响应丢失后原样重发 `question + session_id`」会**新增重复 turn**。
- **对账状态机（不能把「user turn 已存在」当成「请求已完成」）**：恢复前拉取 session 历史，按四种观察判定——**不得仅凭 user 消息就断言成功或失败**：

  | 观察 | 判定 | 动作 |
  |---|---|---|
  | 找到该 turn 的 **assistant 结果** | **已完成** | 读取结果，不重发 |
  | 只有 user turn、**无 assistant**（模型可能仍在跑） | **处理中** | 轮询等待至 `request_timeout`；到期仍无 → 归为「结果未知」 |
  | 有 **assistant/error 标记为失败** | **明确失败** | 按 §6.4 失败处理 |
  | 查不到该 turn ／ 无法可靠区分 ／ 等待超时 | **结果未知** | **隔离该请求为失败尝试**：**不复用原会话**，**改用新会话重试**；原会话标记为**已污染**（不得再作自省依据） |

- **历史查询不提供 exactly-once**：无法可靠区分时，**宁可隔离 + 新会话重试**，也不在原会话上轮询空转或臆断结果。
- 对 `ask` 与 `self-reflect` 都适用（二者都在会话上追加 turn）；自省若「结果未知」→ 按「**保留已完成判分 + `lessons=failed`**」处理（§6.4 表），**不原会话重试**。`judge`/`merge` 同样记录请求身份，避免重复计分（评分按 `attempt_id` 幂等，§8）。

- **自省失败 ≠ 整题失败**：判分结果已拿到，务必保留；只是这题不贡献教训。
- **归并没有「可跳过的题」**：超时/永久失败只能放弃**本批更新**——与 §8「失败不修改集合」一致，解决此前「网络报错就丢弃教训」与「endless 重试」的冲突。
- **识别依据**：额度 = HTTP 429 / rate limit / quota / “token 用量上限” / provider “high demand/overloaded”；网络 = connection reset/refused、EOF、DNS、TLS、5xx；超时 = `request_timeout` 到期 / 服务端 turn 超时 / `context deadline exceeded`。
- **endless 重试**：默认**无次数上限**，按失败类别等待：
  - **模型侧临时错误**（额度 / high demand / 用量耗光，`failQuota`）→ **固定 10 分钟**间隔（`retry_quota_seconds`，默认 600）。MiniMax 已把 5 小时额度的重置改成**滑动窗口**，槽位更早释放，30 分钟的 park 只会白等。
  - **传输类网络错误**（`failNetwork`）→ 指数退避 `retry_initial`(1s)→`retry_max`(60s) + `retry_jitter`(0.3)。
  - 重试会占据批屏障（同步批语义），期间持续打日志。
- **跳过的题**：本 pass 计入 `skipped`、**不计入准确率分母、也不计入覆盖率分子**；下一 pass 重试；最终仍失败则 `leaderboard` 单列。
- **批完整性**：某题被跳过只是少一条教训，更新逻辑不变（§6.2）。

---

## 7. 策略注入点：为什么前置到 `question`

现状（已核实）：RAGFlow 的 chat completions **没有** strategy/system-prompt 字段；未知顶层字段虽经 `internal/handler/chat_session.go:230-241` 作为 kwargs 透传，但各引擎只读各自认得的键（`reasoning`/`agent_mode`/`max_iterations`…）。

- **选定方案：`question = 策略块 + "\n\n" + 原始题面`**。
  - 对所有引擎一致生效（level 0/1-4/5/6/7/8 都读最后一条 user 消息：`agentic_rag/agent.go:1006`、`iterative.go:325`、codex 走 `Messages`）。
  - 与用户样例的措辞天然一致（前缀就是 `Follow the following search strategy:`）。
  - 不改服务端，风险最低。
- **渲染格式**（稳定、可 diff、便于复现）：
  ```
  Follow the following search strategy:
  - <strategy 1>
  - <strategy 2>
  ...

  <原始 question 原文，逐字不变>
  ```
  - `global` 为空时**不加策略块**（题目原样）；这样首批即纯基线。
- 备用方案（**非本期**）：为 level 5 增加 `kwargs["agent_mode"]` 指向 `conf/agentic_rag.yaml` 的专用模板 id（`agentic_pipeline.go:80-95`），或给 `Input` 增加 instruction 字段。留作扩展。
- 记录：每题每次尝试都要把**实际发送的 question（含策略块）+ 所用 global version** 原样落盘，保证可复现。

---

## 8. 全局策略集合、协调器模型与并发

**状态**：`StrategySet = {version, items:[{id,text,created_by,ts,total_score,rating_count,source_ids}], active_ids:[≤10]}`。`items` 是完整候选池；被合并或拒绝的条目及评分保留在审计历史，不再参与排名。

### 8.1 执行约束（协调器模型）

- **worker 只读**：一个 worker 只读**同一个不可变批次快照**（`{pass, batch_index, version, strategies_text, questions[]}`），**仅返回结果**（判定 + 评分 + 教训），**不改任何共享状态**。
- **协调器（单协程）**：按**固定顺序**构造下一状态（§8.2），**成功提交 checkpoint 后才发布下一批**给 worker；**提交失败 → 停止推进**（不进入下一批）。
- 因此**无需给全局策略加锁**——worker 从不写、只读冻结快照。

### 8.2 协调器每批的固定执行顺序

1. **冻结**：取 `global@V` 的不可变快照，连同本批题目组成 `BatchSnapshot` 发给 worker。
2. **收集（稳定顺序）**：按**批内题目位置**收集结果（**不是** worker 完成顺序，§6），每题教训按题目内序号排序。
3. **累计评分**：本批有效评分累加到对应策略（`basis=used|predicted`），按 `attempt_id` 幂等（重跑不重复计分）。
4. **归并 + 继承**：调用 §9.1（来源分组不相交、新教训临时 ID、分数继承规则）。
5. **校验 + 冲突裁决**：过 §9.2 校验器；冲突时**保留累计后总分更高者**（同分按稳定 ID）——与 §9.1 一致。
6. **排名**：累计总分降序、同分按稳定 ID 升序；`active_ids` = 前 ≤10，**超过 10 条仍全量记录**、不裁剪候选池。
7. **提交**：写版本化策略文件（§12.1）→ 原子 checkpoint（§12）；提交成功才回到第 1 步。

> **顺序不可交换（反例）**：A 原 5、本批 −2，B 原 4、本批 +2 → **先累计**得 A=3、B=6，冲突裁决保留 **B**；若先按旧分做冲突过滤则误留 **A**。故**累计必须早于冲突裁决与排名**。

### 8.3 并发与身份

1. **批内并发**：一批 B 题用 worker 池（`C`，默认 4）；**批内所有题冻结同一 `global@V`**。
2. **批间屏障**：一批全部完成 → 执行 §8.2 → `version+1` → 下一批用新集合（刻意模仿训练循环的同步 mini-batch）。
3. **失败处理（=「批级失败」，无回滚语义，与 §6.4 一致）**：归并**永久失败/超时** → **不修改**集合（`version` 不动），本批教训与评分均不应用，记 warning，继续下一批；归并**额度/网络失败** → **先对账**再 endless 重试（§6.4），直到成功。**已提交的历史版本不受影响，无需回滚。**
4. **评分幂等与身份**：评分按 `(attempt_id, qid, strategy_id, basis)` 计入（`attempt_id` 见 §12）；同题同策略最多计一次 `used`，`used` 优先于 `predicted`。多条合并时新 ID 记录来源 ID，继承来源中总分最高条目（同分按 ID）的总分与次数，**不相加**；实质改变方法的策略按新教训处理，不继承旧分。
5. **评分局限**：仅错题自省、曝光次数不等，累计分是**启发式**评价（记录次数与 `used/predicted` 明细）；集合效果仍以冻结评估为准。

### 8.4 独占与恢复校验

- **单进程独占**：启动时对 `output_dir` 加**独占文件锁**（flock）；**获取失败即报错退出**（不做只读降级）。单写者只在**单进程内**成立——两进程共享同一 `output_dir` 会互相覆盖 checkpoint/版本文件。
- **恢复前一致性校验**：续跑前核对 `trainset sha256`、`batch_size`、`reasoning`(引擎)、提示词/契约指纹、`seed`；**任一不符 → 拒绝续跑**（仅保存配置快照不足以防止改参后错位续跑）。

> **已定**：同步批（批内冻结版本）。异步/流式批不采用——吞吐更高但引入陈旧梯度。

---

## 9. 归并 LLM 调用与策略校验器

仅评分变化也形成新版本，确保每次排名及生效集合均可追溯；冻结评估使用该版本的前 10 条，不更新任何评分。

### 9.1 归并 LLM 调用（批级别）

- **输入（本次子集，非全池）**：完整候选池**保留在 checkpoint**，但归并**只取本次相关子集**——本批教训（≤2B 条，带临时 ID）+ 被其触及或需重排的候选（含 ID、文本、累计分、评分次数）+ pass/批序号与错题摘要。**预算**：池 ≤ `poolPromptBudget`（60）时**整池**进提示词（令模型判定覆盖全池）；超预算才裁为"active + replaces 目标 + 共享实词候选"并截断。仅有评分无新教训时，直接确定性累计及排序，**无需调用**归并 LLM。
- **输出契约**：严格 JSON 对象 `{"items":[{"id":"","text":"","source_ids":["..."],"terse":true,"corpus_specific":false,"duplicate_of":"","contradicts":[],"reason":""}],"rejected":[{"source_id":"...","reason":"..."}]}`；**不限于 10 条**。每个输入条目须保留、映射到合并条目或显式拒绝，不得静默删除低排名候选。**身份事实**：item 的 `id` 若指向一条已有策略，**必须**同时出现在它自己的 `source_ids` 里（否则违约）；两个 item 不得认领同一 id；未知 id 按"新建"处理。新 ID 由程序**单调分配器**分配（不复用已删除 id）。LLM 提供来源映射与**四条质量要求的逐项判定**，程序据其执行评分继承与排名，不接受模型改写累计分。
- **来源分组必须不相交（禁止评分复制）**：输出的来源引用须构成**不相交分组**——**每个输入 ID（含本批教训的临时 ID）最多出现一次**，且只能落入「某一个合并条目」或「显式拒绝」之一。**禁止**：重复引用同一 `source_id`、引用未知来源、把一条旧策略**复制成多条**各自继承其高分、凭空改写累计分。程序校验该不变式，**违反即拒绝本次归并**（§9.3），不逐条降级。
- **新教训的临时 ID**：每条新教训在**入批前**获得可引用的**临时 ID**（`t<pass>.<batch>.<qpos>.<k>`，§6/§6.1）；归并把它映射到某条策略（合并或新建）后临时 ID 退役。
- **相关子集选取与回填（分桶归并的正确性）**：归并子集 = 本批教训的**预期映射目标**（`replaces` 指向的策略 + 与教训文本/决策轴相关的候选）+ 当前 `active_ids`。**未触及候选原样保留**（文本/分数/ID 不变）。分桶结果**回填后**，对**整个候选池**做一次一致性检查：**词面近重网**——按 `(分数 desc, ID asc)` 稳定排序后逐条与**全部已保留项**比较，归一化 token 集合 **Jaccard ≥ 0.8** 的近重条目合并、保留高分（同分按稳定 ID）。**这是安全网，不是质量判据**；计划要求的**语义级**全池裁决需一次额外模型调用，**当前未实现（已知限制，见 §14）**。**来源不相交不变式作用于整次批更新的全部调用**（所有桶合起来），回填后统一校验，违反则**整批作废**。
- **提示词约束（四条质量要求全部由你判定，逐条给结论）**：
  - **极简**：每条一个祈使句、尽量短；输出 `terse: true|false`。
  - **无语料库特定词汇**：策略**不得含任何「特定高价值可检索项」**——人名 / 机构名 / 地名 / 时间日期 / 数字，或只对本题成立的具体事实/短语（那会把策略变成「具体线索」而非「通用方法」）。输出 `corpus_specific: true|false` + `reason`；`true` 者丢弃或改写为通用方法。
  - **不重叠**：同义/重复条目**合并为一条**；仍与被映射目标同义者输出 `duplicate_of: <已有策略 ID>`（程序映射到该 ID，不新建）。
  - **不矛盾**：与在册策略冲突者输出 `contradicts: [<id>...]`；程序据累计分保留一条（同分按稳定 ID），无法调和则显式拒绝并记原因。
  - 不因超过 10 条删减输出；程序在校验后排序，取前 10 条生效。
  - 输出**只有 JSON**（便于解析）。
- **模型选择（已定）**：归并**直接用判分对话框** `judge_chat_id`（`merge_chat_id` 缺省为空即取它）。走的是同一 `/api/v1/chat/completions`。

### 9.2 策略校验器（validator：大模型判定 + 机械规范化）

归并 LLM 的输出**不直接入库**：先做**机械规范化**，再据**大模型的逐项判定**执行裁决，最后校验**来源不相交不变式**。任一条不通过 → 丢弃该条并把原因记入变更历史（§12.1）。**例外**：违反来源不相交（重复/未知来源、评分复制）→ **整次归并作废**（§9.3），不逐条降级。

**四条质量要求全部由大模型判定**（不设 DF、白名单、Jaccard、冲突模式表等机械/启发式规则）：

| 要求 | 大模型输出 | 程序动作 |
|---|---|---|
| 极简 | `terse: true|false` | 非 `terse` → 丢弃（机械 30 词截断仅兜底） |
| 无语料库特定词汇（禁「特定高价值可检索项」） | `corpus_specific: true|false` + `reason` | `true` → 整条丢弃（或退回改写为通用方法） |
| 不重叠 | `duplicate_of: <id>` | 命中 → **映射到该 ID**（不新建条目） |
| 不矛盾 | `contradicts: [<id>...]` | 命中 → 保留**累计后总分更高者**（同分按稳定 ID）；确认冲突时丢弃较低者 |

- **为什么只用大模型**：机械/启发式判据不可靠——DF 会误杀核心方法学词（实测 `subagent/subagents/assign/merge/dedupe` DF=0、`parallel/verify/split` DF=1），Jaccard 会**漏判同义异形**（`Assign one subagent per clue` vs `Split clues across subagents`）、**误判形似义异**，反义词共现也不等于矛盾（`parallel` vs `avoid sequential` 实为一致）。语义判断只能由模型给出。（此处的「不设 Jaccard」指**逐条质量判定**；**全池一致性**另有一层词面近重网作安全网，见 §9.1/§14。）
- **机械部分只保留**：去编号/引号/末尾标点、>30 词截断（§4 兜底）；**来源不相交不变式校验**（程序）。其余一律不下放为规则。
- **判定与归并同源**：与 §9.1 的归并是**同一次 LLM 调用**（同一 `judge_chat_id`）；因此**不需要** `--semantic-adjudication` 之类的额外开关。
- **审计**：每次归并记录 `输入集合 / 本批教训 / LLM 输出（含逐项判定）/ 程序裁决 / 最终集合`，可离线复核。

### 9.3 健壮性

- **解析失败**（非 JSON / 不符合对象契约 / 来源映射不完整）→ 保守返回「原集合」（`version` 不动），记 warning。
- **来源不变式违反**（重复/未知来源、评分复制，§9.1）→ **整次归并作废**（保留原集合），记 warning；不逐条降级。
- **上下文超限的恢复路径（防永久停滞）**：候选池可全量记录，但**不得要求每次把全池放进模型上下文**。当本次子集超过上下文预算 → **分桶/分批归并**（按分数或决策轴分桶，逐桶归并后合并），或限定本次待处理候选；**「失败保留原池」不等于永久失败**——下一批以缩小的子集重试。**回填后必须做全池去重/冲突校验（§9.1）**，且来源不相交检查覆盖**整批更新**而非单次调用。
- **更新原子性**：评分累计与归并作为**同一次批更新**提交；归并失败则**均不应用**（连同本批评分），但保留原始自省记录（审计）。
- **调用失败**按 §6.4 分流：额度/网络 → **先对账**再 endless 重试；超时/永久 → 放弃本批更新（`version` 不变），继续下一批。
- 归并后若集合**变空**（全部被 validator 丢弃）→ 保留归并前的原集合（绝不回退到空，以免评估退化）。

---

## 10. 判分

- **直接复用 benchmark 的 grader**：相同 judge chat（`judge_chat_id`）与相同 grader 模板（`tools/benchmark/main.go:734-759`），解析用 `parseGraderVerdict`（`:3148`）。
- 每题每次尝试都判一次，落 `{correct, confidence, extracted_final_answer, verdict}`。
- 可选：`--judge-retries`（判分抖动时多次投票取多数）——默认 1。
- **不许用 gold 参与判分以外的用途**；gold 只用于 §6.1 的自省提问。**基线**取自 §15 的**独立无策略对照跑**，不取自任何单份在线批次。

---

## 11. 配置（JSON，复用 benchmark 风格）

```jsonc
{
  "ragflow_api_key": "ragflow-...",           // 或环境变量 RAGFLOW_API_KEY
  "backend": {"scheme":"http","host":"127.0.0.1","port":9380},

  "chat_id": "a476f1...",                     // 作答对话框
  "judge_chat_id": "728951...",               // 判分对话框
  "merge_chat_id": "",                        // 空 = 用 judge_chat_id（已定：归并用 judge）

  "reasoning": 8,                             // 固定 mode-8（codex 引擎）

  "batch_size": 10,                           // 批大小（一批 = 10 道题）
  "max_passes": 0,                            // 0 = 不限（默认，跑到收敛）；>0 为兜底上限
  "concurrency": 4,                           // 批内 worker 数
  "target_accuracy": 100,                     // 收敛：冻结评估准确率 ≥ 此值即停（%）
  "min_delta": 0.5,                           // 收敛：冻结评估 pass 间提升 < 此值即停（百分点）
  "min_coverage": 0.9,                        // 收敛：判分覆盖率须 ≥ 此值（防跳过造成假收敛，§6.3）
  "eval_every_k_passes": 10,                  // 冻结评估频率：每 10 个 pass 一次（§6.3）
  "seed": 20261015,                           // 抽样种子（决定 samplePass，§12）

  "request_timeout": 900,                     // 秒；单次调用超时 → 按阶段 §6.4 分流
  "retry_initial_seconds": 1,                 // 网络类失败：退避起点
  "retry_max_seconds": 60,                    // 网络类退避上限
  "retry_jitter": 0.3,                        // 退避抖动比例
  "retry_quota_seconds": 600,                 // 模型侧临时错误（额度/high demand）固定间隔 10 分钟
  "require_mode8": true,                      // 前置检查：引擎降级则终止实验（见下注）

  "max_strategies": 10,                       // 生效上限；候选池可超过此数
  "max_lessons": 2,                           // 每轮每题教训上限
  "max_strategy_words": 30,                   // 机械兜底：每条 >30 词即截断（质量判定由 LLM，§9.2）

  "dataset": {
    "questions_path": "/home/zhichyu/Downloads/data/browsecomp-plus/questions.jsonl",
    "corpus_path":    "/home/zhichyu/Downloads/data/browsecomp-plus/corpus"
  },
  "benchmark_outputs_dir": "/home/zhichyu/github.com/infiniflow/ragflow/outputs",  // --build-trainset 的来源
  "trainset": "train/browsecomp_top150.jsonl",   // 冻结训练题集；训练期只读（§3.2）
  "strategies_file": "strategies.v%04d.txt",     // 全局策略：携带版本号的 text 文件（§12.1）

  "output_dir": "outputs/strategy_rsi_20261015",
  "resume": true
}
```

CLI 覆盖：`--config --build-trainset --trainset --ids --batch-size --max-passes --concurrency --reasoning --target-accuracy --min-delta --min-coverage --no-stop-on-convergence --baseline --require-mode8 --output --resume`。

> **引擎固定 mode-8（codex）的前提与验收**：仅发 `reasoning=8` **不能**证明实验用了 Codex。`internal/service/chat_pipeline.go:367-377` 在下列任一不满足时会**静默降级**为普通 pipeline（只打 warning，不报错）：
> 1. `codex.endpoint`（`RAGFLOW_CODEX_ENDPOINT`）**且** `codex.mcp_public_base`——`codexConfigured()` 要求两者**齐备**；
> 2. 对话框模型的 provider 在 `codex.responses_providers` 内、且其实例 `base_url` 非空——`codexResponsesCapable()`。
>
> **前置检查（`require_mode8=true`，默认开）**：实验开始前做一次探测，确认**实际引擎为 codex**——例如发起探针 turn 后，`$CODEX_HOME/sessions` 下出现对应新 thread（`mode8verify` 的判定法），或日志命中 `codexagent`。**无法确认或确认已降级 → 立即终止实验并报错**，不产出任何学习结果。
> 子智能体要能读到协作任务，`internal/codexagent/run.go` 须在 thread 配置里下发 `features.multi_agent_v2.message_delivery = "plaintext"`（该文件已设置）。这不是进程级环境变量：Codex 0.166 起由配置项控制，逐 thread 生效。

---

## 12. 输出、原子提交协议与断点续跑

`outputs/<run>/`：

- `checkpoint.json`：**原子提交点**（tmp + `os.Rename`）。内容 = `{phase, version, items, active_ids, items_sha256, pass, next_batch_index, pass_sample[:已消费], committed_attempts[], eval:{eval_id, version, next_index, last_committed}, evaluated, seed}`。`items` 含完整候选池、累计总分、评分次数及来源 ID。**恢复只以它为准。**
- `strategies.v<N>.txt`：**全局策略的版本化 text 文件**（每版一个文件、历史全留；当前版本见 `checkpoint.json`）。格式见 §12.1。
- `merge_history.jsonl`：append-only 归并审计——`{before_version, batch_lessons, batch_ratings, llm_out, rejected:[{text,gate,why}], score_inheritance, ranked_items, active_ids, after_version}`。
- 自省审计：记录每条评分的 ID、分数、理由及 `basis=used|predicted`；归并历史包含全部候选排名（包括第 10 条之后的候选）。
- `batches.jsonl` / `state.jsonl` / `merge_history.jsonl`：**append-only，按 `attempt_id` 定身份**；去重键 `(attempt_id, qid)`（评分 `(attempt_id, qid, strategy_id, basis)`）。**汇总前先按 `checkpoint.committed_attempts` 选定每个 `(pass,batch_index)` 的已提交 attempt，再累计**；未提交 attempt 一律忽略。
- `attempts.jsonl`：每次批执行的 attempt 记录（`{attempt_id, pass, batch_index, started_at}`）——用于 `attempt_id` 唯一性分配与审计。
- `eval_state.jsonl`：评估逐题结果（append-only，键 `(eval_id, qid)`）——`{eval_id, qid, verdict, status}`。
- `passes.jsonl`：每 pass 的**冻结评估**结果（审计副本）——`{pass, eval_id, version, evaluated, frozen_accuracy, scored, skipped, coverage}`；`evaluated=false` 表示该 pass 未到评估频率。
- `leaderboard.json`：**冻结评估**学习曲线（仅**已提交**评估点）+ 汇总（`frozen_accuracy[]`、`baseline_accuracy`、`after_accuracy`=最后一份已提交评估、`final_version`、`final_evaluated`、`updates`、`skipped`、`avg_llm_calls`、`by_strategy_hit_counts`）。
- `_config_snapshot.json` / `_prompt_fingerprint.json` / `seed`：复现用。

**每次批执行 = 一次 attempt，`attempt_id` 全局唯一**：用 **UUID**（或启动时扫描 `attempts.jsonl` 分配单调序号）生成——**不得仅从 checkpoint 游标推算 `#n`**（否则会复用未提交 attempt 的编号）。`attempt_id` 连同 `pass`/`batch_index` 作为字段记录。结果、评分、教训、归并产物都带该 `attempt_id`；**只有 checkpoint 引用的 attempt 才算已提交**，未提交 attempt 的日志行**保留但被忽略**（不参与汇总/排名/学习曲线/评分累计）。**重跑不保证得到与上次相同的归并结果**——以已提交的 attempt 为准。

**每批提交协议（协调器执行，§8.2）**：
1. 跑完一批（worker 只读不可变快照、返回结果）→ 得到判定、评分、教训；
2. 归并 → 新集合（§9）；
3. **先** append 结果行（带 `attempt_id`）到 `state.jsonl` / `batches.jsonl` / `merge_history.jsonl`；
4. **最后**原子写 `checkpoint.json`：`phase=train`、`next_batch_index+1`、`committed_attempts += attempt_id`、新 `version/items/items_sha256`。
> 崩溃在 3↔4 之间 → checkpoint 未推进 → 该 attempt **未提交**（日志保留、被忽略）→ 恢复**重跑该批**（新 `attempt_id`）。**先结果、后游标**，checkpoint 是唯一真相边界。

**阶段状态机**：`checkpoint.phase ∈ {train, evaluate, pass_complete}`：
- `train`：处理批；字段 `pass`, `next_batch_index`, `committed_attempts[]`。
- `evaluate`：冻结评估**进行中**；字段 `pass`, `eval{eval_id, version, next_index}`。
- `pass_complete`：本 pass 结束；字段 `pass`, `evaluated: bool`, `eval.last_committed`。

**冻结评估的提交协议（与批提交同构，解决「结果与游标之间崩溃 → 重评/漏计」）**：
- **评估身份** `eval_id`（全局唯一，规则同 `attempt_id`）；每题评估结果按 `(eval_id, qid)` append 到 `eval_state.jsonl`。
- **先结果、后游标**：每题评估结果 append 后，才推进 `checkpoint.eval.next_index`；崩溃 → 未提交的评估结果保留但**被忽略**，恢复按 `eval.next_index` 续评，**不重评、不漏计**。
- **累计统计恢复**：`scored/skipped/coverage` 由 `eval_state.jsonl` 中**属于已提交 `eval_id` 的行重新汇总**（不依赖内存计数）。
- **`passes.jsonl` 与 `last_committed` 的提交边界**：评估**全部完成后**，在**同一次提交**里原子写 `checkpoint.eval.last_committed = {pass, eval_id, version, accuracy, scored, skipped, coverage}` 并 **append** 该 pass 的 `passes.jsonl` 行；**`last_committed` 是唯一真相**（`min_delta` 只与它比），`passes.jsonl` 为审计副本。
- **未到评估频率的 pass**（`eval_every_k_passes=10`，即默认每 10 个 pass 才评一次）：**跳过 `evaluate`**，直接 `phase=pass_complete{evaluated:false}`，`last_committed` **沿用上一份**；`passes.jsonl` 记 `evaluated=false` 一行，**不参与** `min_delta` 比较。
- 恢复按 phase 精确续跑：`train`→从批游标；`evaluate`→从 `eval.next_index` 续评（**不重复评估、不重复记录曲线**）；`pass_complete`→推进到下一 pass 的 `train`，并据 `last_committed` 做收敛判断。

**抽样可复现（解决漏题/重复跑题）**：每个 pass 的**抽样结果（一个无放回随机排列）显式持久化**在 `checkpoint.pass_sample`，由 `seed` + pass 号**确定性生成**（`samplePass(seed, p)`），恢复时**重建同一样本**；绝不依赖运行时「重新抽样」。残余批（不足 `batch_size`）作为该 pass 最后一「批」提交。

**断点续跑**：启动读 `checkpoint.json` → 按 `phase` 精确续跑（`train` 从批游标、`evaluate` 从评估进度、`pass_complete` 进下一 pass）；**续跑前先做 §8.4 一致性校验**。`resume=false` / `--overwrite` 从头开始。

### 12.1 全局查询策略文件格式（携带版本号）

`strategies.v0030.txt`：

```
# ragflow query-strategy set
# version: 30
# updated: 2026-10-15T12:34:56+08:00
# origin: pass=4 batch=17
# sha256: <body 的 sha256>
- Conduct parallel searches using multiple clues simultaneously; assign each clue to a separate subagent.
- Group subagents by the specificity of the clues, not evenly distributed by clue count.
```

- **版本号既在文件名（`strategies.v%04d.txt`）也在头部**；每版一个文件、历史全留，便于 diff / 回滚。
- **条目格式**：仅输出排名前 10 的生效策略，按排名一行一条、前缀 `- `；`#` 开头为头部/注释。完整候选池及评分由 checkpoint 恢复，text 文件只用于注入及审阅。
- **空集合** = `strategies.v0000.txt`（仅头部，0 条目）。
- **原子写**：先写 `.tmp` 再 `rename`；`checkpoint.json` 记 `{version, strategies_sha256}`，恢复时校验（不符即终止）。
- 归并的 `before/after`、本批教训、validator 逐条结论记入 `merge_history.jsonl`（append-only）。

---

## 13. 代码布局

新建独立工具，不侵入 benchmark：

```
tools/strategy_rsi/
  main.go          # CLI/编排：加载 → 协调器循环（§8）→ 落盘
  client.go        # 复用 mode8verify 的 post/ask/createSession + 会话历史对账（§6.4）
  errors.go        # §6.4 失败分类（额度/网络/超时/永久）+ 对账后 endless 重试 / 跳过
  corpus.go        # 读 questions.jsonl（题面/金标/引用文档）
  trainset.go      # --build-trainset：聚合 outputs/ 高频错题 top-150；训练期只读加载 + sha256 校验（§3）
  batch.go         # 批发生器 + 不可变批次快照（samplePass 无放回抽样/切批，§6/§8.1）
  checkpoint.go    # §12 提交协议（attempt_id + phase）+ 输出目录独占锁（§8.4）+ 续跑/评估落盘
  strategy.go      # StrategySet：稳定 ID/累计评分/继承/排名/前10渲染 + 版本化 text 文件读写（§12.1）
  validator.go     # §9.2 校验器（机械规范化 + 应用大模型逐项判定 terse/corpus_specific/duplicate_of/contradicts）
  rsi.go           # 单题：测试 + 同会话自省（§6.1 renderImprovePrompt、renderStrategy）
  judge.go         # grader 模板 + parseGraderVerdict（移植/抽取自 benchmark）
  merge.go         # 批级归并 LLM 调用（§9.1）
  *_test.go        # 纯函数单测（解析/渲染/归一化/校验器/切批/选集合）
```

> 若不想复制 `client/judge`：可先小改 `tools/benchmark` 把 `createSession/askRagflow/parseGraderVerdict` 抽到内部共享包，再被 `strategy_rsi` 依赖。**优先复制**（低耦合、零回归）——benchmark 是活跃文件。

---

## 14. 边界与风险

| 风险 | 处理 |
|---|---|
| 建议/归并返回非结构化文本 | 多级解析 + 截断 + 保守回退原集合（§6.1/§9.3） |
| 策略冗长 | 提示词硬约束 + 30 词硬截断（§4/§9.2） |
| 多条策略**重叠**（同义异形） | **大模型判定** `duplicate_of` → 映射到已有 ID（§9.2） |
| 多条策略**互相矛盾** | **大模型判定** `contradicts` → 保留累计分更高者（§9.2） |
| 策略**含「特定高价值可检索项」**（人名/机构/地名/时间/数字/单题事实短语） | **大模型判定** `corpus_specific`（§9.2） |
| 策略不够简练 | **大模型判定** `terse`；机械 30 词截断仅兜底（§9.2） |
| 自省评分偏差（仅错题评分、曝光不等、预测不等于使用效果） | 保存总分、次数、理由及 used/predicted；集合效果用冻结评估衡量（§8） |
| **候选池增长导致归并输入过长** | 候选池全量记录但**不整体进上下文**；本次只处理相关子集，超预算时**分桶/分批归并**（§9.1/§9.3）——「保留原池」不等于永久停滞 |
| **跳过题制造假收敛**（分母变小→假 100%） | 收敛加**覆盖率门** `min_coverage`；`scored==0` 记 `null` 不收敛（§6.3） |
| **在线准确率 ≠ 最终策略水平**（策略版本/题序/批位混杂；末批策略没测过） | 收敛与 `after_accuracy` 一律用**冻结评估**（§6.3） |
| **恢复漏题/重题**（随机抽样没持久化） | 持久化 `pass_sample`（无放回样本）+ `seed`，确定性重建（§12） |
| **策略/游标不一致**（两处分别写盘） | **先结果、后游标**的原子 checkpoint **+ `attempt_id`**（§12） |
| **自省失败**被误当整题失败 | 保留已完成判分，仅标 `lessons=failed`（§6.4） |
| **归并失败口径冲突**（丢弃 vs 无限重试） | 统一为 §6.4：额度/网络→重试；超时/永久→放弃本批更新，`version` 不变 |
| **mode 8 静默降级**（只发 `reasoning=8` 不能证明用了 Codex） | 前置检查 `require_mode8`：确认实际引擎，降级即终止（§11 注） |
| 瞬时**额度/网络**失败 | §6.4：**先对账**再 endless 重试（指数退避 + 抖动） |
| **超时**失败 | §6.4：按阶段处理（测试/判分→跳过；自省→保留判分；归并→放弃本批） |
| endless 重试**阻塞批屏障** | 同步批语义下本批等待；持续日志；`--retry-wall` 观测耗时（默认关） |
| 某批全对 → 无教训 | 无梯度：不更新，`version` 不变（§6.2） |
| 尾部残余批 | pass 末强制收尾更新一次（§6.2） |
| 判分抖动 | 可配置 `judge_retries` 投票 |
| 成本/时延（不限轮数 × 题 + 每 pass 冻结评估 + 每批一次归并） | 批内并发 + 收敛即停 + `eval_every_k_passes` + 可选 `max_passes` 兜底 |
| 基线不纯净 | 基线取**独立无策略对照跑**（§15），不取自在线批次 |
| **训练题集漂移**（多次实验题集不一致） | 冻结 `trainset` 文件 + 训练期 readonly + sha256 校验（§3.2） |
| **策略文件版本/内容不一致** | 版本化 text 文件（版本号在文件名+头部）+ checkpoint 记 sha256（§12.1） |
| **批次重跑后审计对不上**（重跑可能产出不同归并） | 每次批执行分配 `attempt_id`；checkpoint 只引用**已提交 attempt**，未提交记录保留但被忽略（§12） |
| **phase 缺失**（评估中途/评估已写/未推进都回到同一游标） | checkpoint 存 `phase∈{train,evaluate,pass_complete}` + 评估进度 + `last_committed`（§12） |
| **来源复制高分**（一条旧策略复制成多条各占生效位） | 来源分组**不相交**不变式；重复/未知来源/评分复制 → 整次归并作废（§9.1/§9.3） |
| **重试重复 turn / 结果未知**（服务端在调用模型前已存 user turn） | 四态对账（已完成 / 处理中 / 明确失败 / **结果未知**）；未知 → **隔离为失败并换新会话重试**，不污染原会话（§6.4） |
| **评估恢复重评/漏计**（结果与游标之间崩溃） | `eval_id` + 逐题结果 append `eval_state.jsonl` + **先结果后游标** + 全量重算统计；`last_committed` 与 `passes.jsonl` 同一次提交（§12） |
| **重复 `attempt_id`**（仅由游标推算会复用未提交编号） | `attempt_id` 用 **UUID**（或扫描 `attempts.jsonl` 分配）（§12） |
| **分桶回填后全池非法**（跨桶同义、与未触及候选冲突） | 回填后对**全池**做词面近重合并（Jaccard≥0.8，§9.1）；来源不相交检查覆盖**整批更新**（§9.1） |
| **全池语义裁决未实现**（词面不同的同义/冲突策略可能漏选） | 回填后仅做**词面近重**合并；语义级裁决需要第二次模型调用，**当前未实现**（已知限制，§9.1） |
| **提示词提前丢弃可映射/可替换的教训** | 提示词允许 restate / `replaces`；去重与替换裁决**下移到批级**（§6.1） |
| **worker 完成顺序影响归并/同分排名** | 按批内题目位置**稳定排序** + 临时 ID 按序分配（§6/§6.1） |
| **多进程共享 `output_dir` 互相覆盖** | 启动时对输出目录加**独占锁**；第二个进程报错退出（§8.4） |
| **最终版本未被评估** | **允许**：退出 / 外部打断**不补评**；`after_accuracy` 取**最后一份已提交评估**，报告标明最终 `version` `evaluated=false`（§6.3） |
| **评分累计/冲突裁决顺序颠倒**（反例 A/B） | 固定顺序：**累计 → 归并继承 → 校验裁决 → 排名**（§8.2） |
| **改参后续跑错位** | 续跑前校验 `trainset`/sha256、`batch_size`、引擎、提示词指纹、`seed`（§8.4） |
| 陈旧梯度 | 同步批（冻结版本）；异步不采用（§8） |

---

## 15. 验证计划

1. **纯函数单测**：`parseReflection`（含 **`replaces`** 解析与非法 ID 处理）/`renderStrategy`/归一化裁剪/切批（`batch_size` 边界、残批、全对批）/**训练题集聚合**（`--build-trainset`：跨 `outputs/` 计 `wrong/seen`、top-150、截断线、稳定排序与去重）/**`samplePass` 抽样（同 `seed+pass` 复现；同 pass 各批互不相交且并集=全集；跨 pass 不同）**/**策略文件读写**（版本号在文件名+头部、条目解析、空集合 `v0000`、原子写 round-trip）；**错误分类器**（§6.4 四阶段 × 四原因）；**校验器**（§9.2）——机械规范化（29/30/31 词截断边界）；**应用大模型四条判定**：构造 `terse=false`→丢弃、`corpus_specific=true`→丢弃、`duplicate_of`→映射到已有 ID 不新建、`contradicts`→保留累计分更高者（同分按稳定 ID）；**来源不相交不变式**（重复/未知来源、评分复制 → 整次归并作废）；**收敛判据**（覆盖率不足不收敛、`scored==0` 不收敛、正常收敛）。
   **评分与排名单测**：分数范围/整数校验、未知及重复 ID、缺失评分不补零、累计与负分、同分稳定排序、超过 10 条完整保留但仅前 10 注入、重复教训同题不重复计分、改写保留身份、多条归并取最高分不相加、实质变更不继承，以及失败更新不应用评分；**来源不相交不变式**（重复/未知来源、评分复制 → 整次归并作废）；**稳定顺序**（打乱 worker 完成顺序 → 同一归并输入与同一前 10）；**统一执行顺序**（A/B 反例：先累计保留 B、先冲突保留 A）；**分桶归并**（超上下文预算时降级成功）。
2. **恢复的正确性**：在 §12 的**每个写入点**注入中断（结果已写/游标未写、游标已写/结果未写），断言恢复**不重不漏**、`version` 与 `pass_sample` 一致，候选池及总分一致且重跑不重复累计；**`attempt_id` 语义与唯一性**（未提交 attempt 的日志被忽略、不参与汇总；崩溃后不重用编号）；**`phase` 恢复**（在 `train`/`evaluate`/`pass_complete` 三处各注入中断，断言不重复评估、不重复记录曲线、不漏收敛判断）；**评估提交协议**（`eval_state.jsonl` 逐题 append + `eval_id`；在结果/游标之间中断断言不重评不漏计；`evaluated=false` 的 pass 不参与 `min_delta`）。
3. **前置检查与运行约束**：`require_mode8` —— 探针确认实际引擎为 codex；**构造降级场景**（缺 `mcp_public_base` 或非 Responses 模型）断言**实验被终止**；**对账四态**（构造：有 assistant=已完成、仅 user=处理中→超时、error=明确失败、查不到=结果未知 → 断言未知一律隔离并换新会话、不污染原会话）；**输出目录独占锁**（第二进程报错退出）；**续跑一致性校验**（改 `batch_size`/引擎/`seed` 后拒绝续跑）；**分桶回填全池校验**（构造跨桶同义 / 与未触及候选冲突，断言回填后全池合法）；**降频评估**（未评估 pass 不入 `min_delta`；**退出/外部打断不触发补评**，`after_accuracy` 取最后一份已提交评估）。
4. **小样本冒烟**：`--ids 3,8,70`（3 题，`batch_size=2`，`max_passes=1`）跑通全链路；核对 `checkpoint.json`/`batches.jsonl`/`state.jsonl`/`passes.jsonl`，确认入库策略无题目特定词、且**引擎未降级**。
5. **独立无策略基线**：对**同一 150 题（冻结题集）**、同一引擎/模型/配置，用**空策略**单独跑一遍（`--baseline`）得到 `baseline_accuracy`；记录与历史 leaderboard 的**引擎/模型/配置差异**。注意这 150 题是历史高频错题（历史准确率低），**重测不一定仍为 0**——此步是为拿「同条件」对照，而非复现历史数字。
6. **成效**：150 题、`batch_size=10`、跑到收敛；报告**冻结评估**学习曲线、`updates`、`skipped`、validator 拒绝率；与第 5 步基线对照。

---

## 16. 里程碑

1. **M1 骨架 + 题集**：`corpus.go`、`trainset.go`（`--build-trainset` 聚合 top-150 + 只读加载 + sha256）、`client.go`、`batch.go`（`samplePass` 无放回抽样切批）、`errors.go`（分类器）、纯函数单测、`--ids` 冒烟。
2. **M2 单题 + 前置检查**：`rsi.go` + `judge.go` + `require_mode8` 探测；跑通「带 global 测试 → 判分 → 同会话自省」，落实 §6.4 **四阶段**分流。
3. **M3 批循环 + 冻结评估 + 校验器**：批屏障/触发 + `frozenEvaluate` + `strategy.go` + `validator.go`（机械规范化 + 应用大模型四条判定）。
4. **M4 批级 LLM 归并**：`merge.go`（严格 JSON 契约 + §6.4 分流 + 回退）。
5. **M5 提交 / 续跑**：`checkpoint.go` 提交协议（`attempt_id` + `phase`）+ 输出目录独占锁 + `pass_sample` 持久化 + per-pass 冻结评估曲线 + leaderboard。
6. **M6 全量 150 题（冻结题集）+ 独立基线对照**，跑到收敛，出报告。

---

## 17. 已定 / 待决

**已定**
- **批口径 / 抽样**：一批 = `batch_size` 道题（默认 10）；**批发生器从整个训练题集随机抽样组批，pass 内无放回**（同 pass 各批互不相交、必然不同；每题每 pass 恰抽一次；跨 pass 重抽）（§6.2）。
- **并发/协调器**：同步批（批内冻结版本、批间屏障）；**worker 只读不可变批次快照、只返回结果**，**协调器按固定顺序构造下一状态、提交成功才发布下一批**（§8.1/§8.2）；**无需给全局策略加锁**；异步不采用（§8）。
- **pass/收敛**：不限轮数；**收敛以冻结评估为准**（默认每 10 个 pass 评一次，`eval_every_k_passes=10`），且须 `coverage ≥ min_coverage`(0.9) 与 `scored > 0`；**退出/外部打断不强制评估最终版本**，`after_accuracy` 取最后一份已提交评估（§6.3）。
- **评分与生效**：错题自省评分 `-2～+2`，累计总分降序、同分按 ID；候选全保留，前 10 条生效；区分 used/predicted，归并继承最高分、不相加（§6.1/§8）。
- **归并模型**：用 `judge_chat_id`（§9.1）。
- **引擎**：固定 `reasoning=8`（mode-8 codex），并做 `require_mode8` 前置检查、降级即终止（§11 注）。
- **失败策略**：测试/判分/自省/归并 **四阶段 × 额度·网络·超时·永久** 逐格规则（§6.4）；额度/网络 → **先对账**再 endless 重试。
- **跳过题口径**：不计入准确率分母、也不计入覆盖率分子；下一 pass 重试；`scored==0` 记 `null` 不收敛。
- **holdout**：不做；收敛/`after_accuracy` 用**同题集的冻结评估**。
- **校验器（四条质量要求全部由大模型判定）**：极简 / 无语料库特定词汇 / 不重叠 / 不矛盾，均由 LLM 逐条输出 `terse / corpus_specific / duplicate_of / contradicts`（§9.1/§9.2）；程序只做机械规范化（去编号/引号/末尾标点、>30 词截断）+ 应用判定 + 来源不相交校验。**逐条质量判定不设** DF / 白名单 / Jaccard / 冲突模式表 / `--strict-lexicon` / `--semantic-adjudication` 等机械或启发式规则；**唯一例外**是回填后的**全池词面近重网**（token Jaccard ≥ 0.8，仅做安全网、非质量判据，§9.1/§14）。
- **训练题集**：从 `outputs/` 历次 browsecomp benchmark **聚合答错频次 top-150**，冻结为 `trainset.jsonl`（含 provenance + sha256），训练期 **readonly**（§3.2/§3.3）。
- **全局策略持久化**：**携带版本号的 text 文件** `strategies.v<N>.txt`（版本号在文件名 + 头部；每版一文件、历史全留）（§12.1）。
- **恢复**：以**原子 checkpoint**（策略文件引用+sha256 + 游标 + `pass_sample` + `phase` + `attempt_id`）为唯一真相（§12）。
- **提交/恢复语义**：每次批执行一个**全局唯一** `attempt_id`（UUID），checkpoint **只引用已提交 attempt**（未提交记录保留但被忽略）；去重键含 `attempt_id`；`phase∈{train,evaluate,pass_complete}` 精确恢复（§12）。
- **评估提交协议**：`eval_id` + 逐题结果 append `eval_state.jsonl` + **先结果后游标**；统计由已提交 `eval_id` 重算；`last_committed` 与 `passes.jsonl` 同一次提交；`evaluated=false` 的 pass 不参与 `min_delta`（§12）。
- **对账四态**：已完成 / 处理中 / 明确失败 / **结果未知**；未知 → **隔离为失败并换新会话重试**，不污染原会话（§6.4）。
- **独占与续跑校验**：启动对 `output_dir` 加**独占锁**（第二进程报错退出）；续跑前校验 `trainset`/sha256、`batch_size`、引擎、提示词指纹、`seed`（§8.4）。
- **执行顺序（不可交换）**：**累计评分 → 归并/继承 → 校验与冲突裁决 → 排名**（§8.2）。
- **来源不相交**：每个输入（含新教训临时 ID）最多引用一次、只落入一个合并条目或显式拒绝；**禁止评分复制**，违反即整次归并作废；分桶归并**回填后对全池**去重/冲突校验，来源不相交覆盖**整批更新**（§9.1）。
- **提示词允许映射/替换**：自省提示词**允许 restate 与 `replaces`**（标注目标策略 ID），去重/映射/替换裁决**全部下移到批级**（§6.1）。
- **确定性**：`parallelMap` 按批内题目位置**稳定排序**、临时 ID 按序分配；完成顺序不影响归并输入与同分前 10（§6/§6.1）。
- **评估点**：只比**已提交**评估点；**退出/外部打断不强制评估最终版本**；基线与冻结评估用**全新会话**（§6.3）。
- **重试对账**：不确定是否成功时**先拉会话历史对账**再决定是否重发（§6.4）。

**待决**
- （无）—— 上轮阈值已确认：`min_coverage = 0.9`、`eval_every_k_passes = 10`。
