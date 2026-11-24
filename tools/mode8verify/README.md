# mode8verify

Check, from the Codex side, that a RAGFlow **mode-8** (`reasoning: 8`, Codex app-server) turn
really exercised the capabilities mode 8 exposes. The chat answer alone cannot tell whether the
model used a tool or merely asserted a result, so this tool drives the API and then reads the
Codex app-server's own rollouts under `$CODEX_HOME/sessions`.

## What it verifies

| probe | capability | pass signal |
|-------|------------|-------------|
| `mcp` | knowledge-base retrieval over the MCP bridge | a thread called `search_bm25_chunks` / `search_semantic_chunks` / `grep_chunks` / `list_chunks` |
| `code` | generating Python, importing a **stdlib package**, and executing it | the run imports `datetime`, parses two fixed day-precision dates, and the answer carries the exact integer year difference (`28`) and day count (`10328`) |
| `agent` | sub-agent delegation (`features.multi_agent_v2`) | a spawned child received a **readable** task and **itself** ran a retrieval tool |

The `code` probe checks *what* was executed, not merely that the shell was used: the captured
command line must invoke Python **and** import a standard-library module (`datetime`), so a run
that answers from a non-code route, shells out to something else, or cannot import a stdlib
package fails instead of passing on the shell counter alone. The date pair is fixed
(`1998-07-05` → `2026-10-14`) so the expected `28` years / `10328` days are deterministic — the
floor-days/365 and calendar-year readings agree. mode 8's sandbox is read-only, so "generate and
run a script" means `python3 -c …` or `python3 - <<'PY' … PY` rather than writing a `.py` file.

The `agent` probe is the important one. A delegated task can look fine in the rollout yet never
reach the child model — the child then reports "no task" and idles while the parent waits out
`wait_agent`. That is the third-party-provider failure mode upstream tracks in
[openai/codex#34833](https://github.com/openai/codex/issues/34833),
[#36376](https://github.com/openai/codex/issues/36376), and
[#46939](https://github.com/openai/codex/issues/46939); the probe fails loudly instead of
letting the turn quietly degrade into "the root did the work itself".

## Prerequisites

- A running RAGFlow API with a mode-8 chat (a chat whose model provider speaks the Responses
  API), and its shared Codex app-server reachable.
- The API key and chat id, supplied through the environment (never commit them):
  `RAGFLOW_API_KEY`, `RAGFLOW_CHAT_ID`.
- For sub-agents on a **non-OpenAI** provider, the chat's thread config must set
  `features.multi_agent_v2.message_delivery = "plaintext"`. `internal/codexagent/run.go`
  already does this. Without it, collaboration task payloads are delivered as Codex's
  first-party `agent_message` item / `encrypted_content`, which a third-party Responses
  endpoint ignores, and the `agent` probe will fail.

## Usage

```sh
export RAGFLOW_API_KEY=...
export RAGFLOW_CHAT_ID=...

# one question through the chat completions API
go run ./tools/mode8verify ask -q "用一句话解释量子纠错，并附出处"

# summarize the newest Codex rollouts (no API call)
go run ./tools/mode8verify inspect -n 6

# run all three probes and assert the signals
go run ./tools/mode8verify verify
```

Useful flags: `-base-url` (default `http://127.0.0.1:9380`), `-reasoning` (default `8`),
`-codex-home` (default `$CODEX_HOME` or `~/.codex`), `-timeout` (per-request seconds).

`verify` exits non-zero if any probe fails.

## Reading `inspect` output

```
[sub] 01a13993-fcda… (/root/research_qec -> 01a13993-e03d…) model=MiniMax-M3 \
      tools={grep_chunks=4, list_chunks=13, search_bm25_chunks=4, search_semantic_chunks=3} task=user-message
```

The `task=` label says how the delegated task was delivered to the child:

- `user-message` — a standard `message`/`user` item; provider-neutral, the child can read it.
- `agent_message(plaintext)` — Codex's own item type carrying readable text; a non-OpenAI
  provider ignores the item, so the child likely will not see it.
- `agent_message(encrypted)` — the payload arrived as `encrypted_content` (unreadable even to
  a first-party consumer without the decryption path).
- `none recorded` — no task was delivered at all.

`tools={…}` lists what each thread actually called, so "it answered" can be checked against
"it retrieved".
