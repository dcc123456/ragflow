# Enterprise observer

Build with `RAGFLOW_GO_TAGS=ee`. The community build uses no-op hooks.

Both API and admin expose unauthenticated Prometheus text at `GET /metrics`.
Scrape each API instance for its API and retrieval metrics and the admin instance for
active-user and queue gauges. `HOSTNAME` supplies the `hostname` label and
defaults to `ragflow` when unset.

The API records requests, failures, and latency in seconds for four fixed stages:

| Prefix | Operation |
| --- | --- |
| `retrieval` | NLP retrieval |
| `api_retrieval` | POST `/api/v1/retrieval`, `/api/v1/datasets/search`, and `/api/v1/datasets/:dataset_id/search` |
| `retrieval_embedding` | Query vector generation |
| `retrieval_rerank` | Model reranking |

Each prefix has `_requests_total`, `_failures_total`, and `_latency_seconds`.
Returned operation errors and panics count as failures; API failures are panics
or HTTP 5xx responses. Handled application errors returned with HTTP 200 do not
count as API failures. Latency includes every exit path and uses the Python
observer's histogram buckets.

All matched API-server routes except `/metrics` also record `apis_requests_total`,
`apis_failures_total`, and `apis_latency_seconds`, with the same failure rules and
histogram buckets. These metrics have `hostname` and `api_path` labels; `api_path`
is the HTTP method followed by a space and the full registered route, for example
`POST /api/v1/retrieval` or `GET /api/v1/datasets/:dataset_id`. Path parameters
stay templated and query strings are excluded so label cardinality stays bounded
by the registered routes. Unmatched requests do not create metric series.
Requests rejected by request-size validation before reaching the enterprise
middleware are not recorded.

Authenticated clients send `GET /api/v1/heartbeat` to update their user ID in
the Redis sorted set `active_users`, scored by Unix seconds. Repeated heartbeats
refresh the same member. The admin refreshes `active_users_total` immediately
and every minute, removing entries at or before the five-minute cutoff.

The same worker updates `queue_tasks_pending`, `queue_tasks_in_progress`, and
`queue_tasks_completed_total`, labeled by `queue`, for the NATS task,
knowledge-compile, and syncer streams when present. Pending and in-progress
come from consumer pending and unacknowledged counts. Before a consumer exists,
the stream's message count is pending. Completed uses the consumer's contiguous
acknowledgement floor: it can lag out-of-order acknowledgements and resets when
the consumer is recreated. The floor counts delivery sequences, so redelivery
can make it exceed the number of unique completed tasks. It remains a gauge,
as in the source observer.

Redis and NATS refreshes have separate 15-second deadlines. A failed refresh
logs a warning and leaves the last successful gauge values in place. The worker
logs a summary of successfully refreshed active-user and queue values each
cycle, and stops and is joined during admin shutdown.

Run focused checks through the native build wrapper:

```bash
RAGFLOW_GO_TAGS=ee bash build.sh --test ./internal/observability/... ./internal/engine/nats/... ./internal/service/nlp/... ./internal/router/... ./internal/admin/... ./internal/server/admin/...
```
