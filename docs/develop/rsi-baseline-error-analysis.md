# RSI baseline — why 99 of 150 questions failed

Analysis of the no-strategy baseline for the RSI train set
(`train/browsecomp_top150.jsonl`), run by
`tools/benchmark/rsi_baseline_conf.json` (mode 8) into
`outputs/rsi_baseline_20261015/`.

- **Result**: Accuracy **34%** (51/150), Recall 28.97%, Surfaced Recall 53.71%.
- **Sources**: `answers.jsonl` (150 rows) and `leaderboard.json`
  (`per_query_judgements` 150 rows, `per_query_metrics`).
- **Per-bucket id lists**: `train/baseline_wrong_buckets.json`
  (sha256 `d42cb184d13898f4b6b0a232bcab78f885d89a6a5a366b18bbb52706a264e816`).

## Headline

Of the 99 wrong answers, **55% are retrieval misses** and **31% had the gold
document read and still failed**. Crucially, **every expected document exists in
the corpus** (183/183, 122/122, 138/138 across the groups), so none of these
questions is unanswerable — the failures are search/synthesis, not missing data.

## Classification (n = 99)

| Bucket | n | % | Meaning |
|---|---|---|---|
| **A1** retrieval miss + gave a (wrong) answer | 29 | 29% | the gold doc never appeared in retrieval; the model still asserted an answer |
| **A2** retrieval miss + honest refusal | 25 | 25% | never found it, and said so |
| **B1** gold doc **read**, mis-synthesised | 31 | 31% | recall=1 and `gold_doc_served`, yet the conclusion is wrong |
| **B2** evidence surfaced but **not read** | 14 | 14% | the doc was in the results, but was never opened |

Retrieval-side failures (A1+A2) = **54 (55%)**; evidence-in-hand failures
(B1+B2) = **45 (45%)**.

Bucket rule: `recall` comes from `per_query_metrics`; "read" =
`gold_doc_served` non-empty; "gave an answer" = the answer text is non-empty and
contains no refusal marker.

## Three signals that matter

### 1. Failure is mistargeted effort, not giving up early

| Group | LLM calls | tool calls | tokens |
|---|---|---|---|
| Correct | 15.3 | 21.7 | 1.22M |
| Wrong (all) | **22.6** | **33.8** | **2.27M** |
| B (evidence in hand) | **25.8** | **38.1** | **2.64M** |

The wrong questions burned *more* of everything — and the ones that had the
evidence burned the most. This is thrashing, not laziness.

### 2. Wrong answers used *more* of every search tool

```
CORRECT: bm25=10.9  semantic=5.0  grep=3.2  list=2.2
WRONG  : bm25=15.7  semantic=8.5  grep=7.3  list=1.9
```

More of the same searches does not help; the searches need to change *form*
(decompose constraints, search each clue in parallel, aim at the constraint).
Full breakdown in **Tool-call statistics** below.

### 3. The grader is not the problem — 34% is real

- Mean judge confidence on the wrong rows is **96**, and **84/99 are 100** —
  the judge is certain they are wrong.
- Only 4 rows are "near misses", and each is a genuine entity difference:
  `Eder` vs `Elder`, `Professor` vs `Dr`, `in Biafra` vs `at Biafra`,
  `Batman` vs `Pirates`. These puzzle questions require exact strings.

## Tool-call statistics

Source: `answers.jsonl.tool_call_counts` (the per-tool dict), which is byte-identical
to `leaderboard.json.per_query_usage[].tool_calls` (150/150 rows).

**Aggregate over 150 questions**

| metric | value |
|---|---|
| total tool calls | **4452** |
| mean / median / min / max per question | **29.7 / 27 / 0 / 101** |
| questions with zero tool calls | 2 |
| `tool_error_stats` | `{shell: 2}` (only 2 errors; `tool_error_samples` empty) |

Distribution (bucketed by 10):
`0-9:26, 10-19:26, 20-29:27, 30-39:30, 40-49:24, 50-59:5, 60-69:7, 70-79:2, 90-99:2, 100+:1`
— the mass sits at 0–50 with a long tail to 101.

**Per-tool totals and share**

| tool | calls | share |
|---|---|---|
| `search_bm25_chunks` | 2111 | **47.4%** |
| `search_semantic_chunks` | 1102 | 24.8% |
| `grep_chunks` | 885 | 19.9% |
| `list_chunks` | 300 | 6.7% |
| `shell` | 26 | 0.6% |
| `list_mcp_resources` | 25 | 0.6% |
| `list_mcp_resource_templates` | 3 | 0.1% |

**Mean per question: correct vs wrong**

| tool | correct (n=51) | wrong (n=99) | ratio |
|---|---|---|---|
| `search_bm25_chunks` | 10.92 | 15.70 | 1.44× |
| `search_semantic_chunks` | 5.04 | 8.54 | 1.69× |
| **`grep_chunks`** | 3.16 | **7.31** | **2.32×** |
| `list_chunks` | 2.24 | 1.88 | **0.84×** |
| `shell` | 0.24 | 0.14 | 0.60× |
| `list_mcp_resources` | 0.08 | 0.21 | 2.70× |
| **total** | **21.7** | **33.8** | **1.56×** |

Two asymmetries stand out: the wrong questions make **more calls of every kind**
(overall 1.56×), led by **`grep_chunks` at 2.32×** — the signature of falling back
to brute-force keyword scanning once normal retrieval fails. Meanwhile
**`list_chunks` goes *down*** — the successful runs did more structured browsing,
the failing ones more keyword hammering. The direction is inverted, so grep growth
is a *symptom*, not a cure.

**Definition and data-quality notes**

- `search_calls` (the leaderboard's "Search Calls", mean **19.97**) is exactly
  `search_bm25_chunks + grep_chunks` (150/150 verified). It **excludes semantic
  search** — do not read it as "all retrieval".
- Several `per_query_usage` columns are **unpopulated in mode 8** (they belong to
  the plan-based engine): `citation_groundings` 0/150, `llm_turns` 0/150,
  `audit_verdicts` 0/150, `compactions` 4/150, `retrieved_doc_count` /
  `deep_read_chunks` 137/150.
- `per_query_structure` is inert here: `answer_label` is 0 for 148 and 1 for 2;
  `kind` and `failure` are 0 for all 150; `blocks_total` is 0 for all 150.

## Cost: tokens and wall time vs history

**Baseline (n = 150, mode-8 codex, MiniMax-M3)**

| metric | per question | run total |
|---|---|---|
| total tokens | **1,912,318** (input 1,881,164 + output 30,867) | **286.8M** |
| LLM calls | 20.1 | 3016 |
| input tokens per LLM call | 93,559 | — |
| wall clock (`client_seconds`) | **1102 s** | 165,265 s summed ≈ **15 h 28 m** at concurrency 3 |

**Same 150 question ids, in history** (no mode-8 run exists, so this is
cross-engine — see caveats). The three full-830 runs cover all 150:

| run (engine) | tok/q | wall/q | LLM/q | input/call | acc on these 150 |
|---|---|---|---|---|---|
| **baseline (mode-8 codex)** | **1.91M** | **1102 s** | **20.1** | **94k** | **34.0%** |
| `browsecomp_20260918_114506` (older) | 5.62M | 530 s | 33.3 | 167k | 25.3% |
| `c2_smoke46_merged_830` (plan) | 5.80M | 830 s | 36.9 | 155k | 18.0% |
| `c2_smoke68_full830` (plan) | 7.02M | 1355 s | 39.2 | 177k | 30.7% |

Pooled over every historical run that covers these ids (n = 830 observations):
tok/q mean **5.59M** / median **4.72M**; wall/q mean **787 s** / median **628 s**.

**Against the history mean (≈6.15M tok/q, 905 s, 36.5 calls):** the baseline uses
**3.2× fewer tokens**, **1.8× fewer LLM calls**, a **1.8× smaller context per call**,
but is **1.2× slower per question** — and on these same 150 it scored *higher*
(34% vs 18–31%).

The shape is "fewer, smaller, slower calls": `llm_calls` tracks tool turns almost
1:1 (tools/call ≈ 1.5), so the saving comes from doing **fewer rounds with less
context**, not from doing less work per round. Wall time is then dominated by
orchestration latency rather than generation, which is why cheaper tokens still
cost more clock.

**Caveats — this is a cost-shape comparison, not a controlled A/B**

1. **Different engines.** History is plan/decomposition-based
   (`per_query_structure.blocks_total` non-zero for 759–828 of 830), the baseline is
   mode-8 codex (`blocks_total` 0 for all 150). No historical mode-8 run exists.
2. **`recall` is not comparable across eras** — the baseline reports a 0–1 fraction,
   history reports a percentage (e.g. 55.56, 80.0).
3. **Token accounting is engine-specific.** The baseline's internal accounting looks
   complete (`llm_calls` tracks tool turns ~1:1, up to 104 calls for one question),
   but there is no guarantee that mode-8 sub-agent threads roll up; if they do not,
   the baseline cost is *understated*.
4. **Selection effect.** These 150 were chosen as the historically most-wrong
   questions, so low historical accuracy here is by construction and says nothing
   about engine quality.

## What this implies for the RSI strategy loop

| Bucket | n | Strategy direction |
|---|---|---|
| A1+A2 retrieval miss | **54** | **parallel multi-clue retrieval** — split the multi-constraint puzzle into independent clues and give each clue its own subagent, instead of re-issuing one query serially |
| B1 evidence read, wrong | **31** | **cross-verification** — check the candidate against *every* constraint before committing |
| B2 surfaced, not read | **14** | **depth discipline** — read the surfaced candidate document before answering |

The largest block (54) is retrieval recall, which is exactly what the
query-strategy RSI is built to move — the baseline confirms the train-set choice
is well aimed.
