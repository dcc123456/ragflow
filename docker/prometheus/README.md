# Service monitoring

Prometheus monitors MinIO/Silo and ClickHouse directly, plus MySQL, Kvrocks, NATS
and Elasticsearch/OpenSearch through separate exporter containers using their
published images. The containers join the `ragflow` bridge network and
communicate by service name. Prometheus publishes its UI on host port `9090`.
Exporter ports stay inside the Compose network.

## Start monitoring

Run every command on this page from the repository root, the directory containing
`docker/`, `internal/`, and `web/`. Paths such as `docker/.env` and
`docker/docker-compose-base.yml` are relative to that directory. If your shell is
currently in `docker/`, run `cd ..` first.

Use the deployment's `docker/.env`. If the application uses a custom Compose
project name, apply the same project setting to these commands.

```bash
docker compose --env-file docker/.env -f docker/docker-compose-base.yml up -d prometheus
```

`-d` runs the containers in the background. To wait until services are running or
healthy before the command returns, replace `-d` with `--wait`. Compose enforces
the initialization dependencies in both modes.

Starting `prometheus` also starts the exporters selected by `DB_TYPE` and
`DOC_ENGINE`, plus the Kvrocks and NATS exporters and the credential
initialization jobs.

To include monitoring in normal deployment startup, append
`prometheus,prometheus-db-${DB_TYPE},prometheus-doc-${DOC_ENGINE}` to
`COMPOSE_PROFILES` in `docker/.env`.

## Configuration

Targets follow the deployment settings in `docker/.env`:

| Job | Default metrics endpoint | Enabled when |
| --- | --- | --- |
| `minio-job` | `minio:9000/minio/metrics/v3` | `MONITORING_MINIO_ENABLED=true` (default) |
| `clickhouse` | `clickhouse:9363/metrics` | `MONITORING_CLICKHOUSE_ENABLED=true` (default) |
| `mysql` | `mysql-exporter:9104/metrics` | `DB_TYPE=mysql` |
| `kvrocks` | `kvrocks-exporter:9121/metrics` | Always |
| `nats` | `nats-exporter:7777/metrics` | Always |
| `elasticsearch` | `elasticsearch-exporter:9114/metrics` | `DOC_ENGINE=elasticsearch` |
| `elasticsearch` | `opensearch-exporter:9114/metrics` | `DOC_ENGINE=opensearch` |
| `ragflow_api_server_request_stats` | `api:9380/metrics` | `MONITORING_RAGFLOW_API_SERVER_ENABLED=true` (default) |

The `ragflow_api_server_request_stats` job collects only the `apis_`-prefixed
request stats (`apis_requests_total`, `apis_failures_total`,
`apis_latency_seconds`, labeled by `api_path`) that the Ragflow API server
exposes on `/metrics`. Override `RAGFLOW_API_SERVER_METRICS_ADDRESS` with
`host:port` for external API server deployments.

Use lowercase `DB_TYPE` and `DOC_ENGINE` values that match the application. Set
`MONITORING_MINIO_ENABLED=false` or `MONITORING_CLICKHOUSE_ENABLED=false` when
that service is unused.

MinIO and ClickHouse use their container ports, independently of host port
mappings. Override `MINIO_METRICS_ADDRESS` or `CLICKHOUSE_METRICS_ADDRESS` with
`host:port` or `[IPv6]:port` for external services. HTTPS settings belong in the
corresponding job in `prometheus.yml`.

Exporters connect to `mysql:3306`, `kvrocks:6379`, `nats:8222`, `es01:9200` or
`opensearch39:9201`. External services can override
`MYSQL_EXPORTER_ADDRESS` (`host:port`),
`KVROCKS_EXPORTER_ADDRESS` (`kvrocks://host:port`), `NATS_MONITORING_URL`,
`ELASTICSEARCH_EXPORTER_URL` or `OPENSEARCH_EXPORTER_URL`. NATS uses its HTTP
monitoring port, normally 8222. External addresses must be reachable from the
container; `localhost` refers to the container itself.

The default NATS monitoring target follows `NATS_HOST:8222`, independently of
the NATS client port (`NATS_PORT`, normally 4222). `EXPOSE_NATS_MONITORING_PORT`
publishes the monitoring port on the host and does not change the internal port.
The default ES/OpenSearch monitoring target follows `ES_HOST:9200` /
`OS_HOST:9201`. `ES_PORT` and `OS_PORT` publish ports on the host; they are not
the ports used between these containers.

Exporter listener ports use `MYSQL_EXPORTER_PORT`, `KVROCKS_EXPORTER_PORT`,
`NATS_EXPORTER_PORT` and `ES_EXPORTER_PORT`. Prometheus uses the same values.
Each exporter listens in its own container, so they can reuse the same port.
Prometheus marks ports outside 1024–65535 as **Not configured**.

`MEM_LIMIT` in `docker/.env` sets the memory limit for each exporter container.

Adjust the `global` time settings in `prometheus.yml` to control scrape frequency,
scrape timeout and rule evaluation frequency. Each alert's `for` controls how long
its condition must hold before firing. After changing deployment settings or
exporter credentials, repeat the startup command above.

## Credentials

The MySQL exporter and Prometheus wait for their respective initialization
jobs. The jobs are optional startup dependencies, so a credential failure leaves
the monitoring services available and `up --wait` reports the failed job.

### MySQL

For `MYSQL_EXPORTER_ADDRESS=mysql:3306`, `mysql-monitoring-init` waits for the
bundled MySQL health check and connects as root with `MYSQL_PASSWORD`. It creates
`MYSQL_EXPORTER_USER` if absent, verifies `MYSQL_EXPORTER_PASSWORD`, and applies
the `PROCESS`, `REPLICATION CLIENT`, and `SELECT ON performance_schema.*` grants.
Existing passwords stay in place. For an external address, prepare the account
on that server and set the exporter username and password in `.env`.

### MinIO/Silo

When MinIO monitoring is enabled and `MINIO_METRICS_ADDRESS=minio:9000`,
`silo-monitoring-init` signs a v3 token offline with `MINIO_USER` and
`MINIO_PASSWORD`. Prometheus reads it from the `prometheus_credentials` volume.
For an external address, put its v3 bearer token in `secrets/minio.token` with
owner UID/GID 65534 and mode 600 before starting Prometheus.

A missing or unreadable token marks MinIO monitoring as **Not configured**.
An invalid nonempty token makes its scrape fail. Other jobs continue running.

### Other services

Kvrocks uses `REDIS_PASSWORD`. Elasticsearch/OpenSearch use the deployment
credentials; override `ES_EXPORTER_USER` / `ES_EXPORTER_PASSWORD` or
`OPENSEARCH_EXPORTER_USER` / `OPENSEARCH_EXPORTER_PASSWORD` for monitoring accounts.
ClickHouse uses its existing `clickhouse/prometheus.xml` metrics endpoint.

## Status and alerts

Open <http://localhost:9090/targets> for scrape status, or query
`ragflow_monitoring_target_state` in <http://localhost:9090>:

| Value | Meaning |
| --- | --- |
| `0` | Disabled in the deployment |
| `1` | Monitoring configuration is missing or invalid |
| `2` | Target discovery, scraping or required backend metrics are failing |
| `3` | Metrics collection is healthy |

The status distinguishes an exporter responding from its backend being healthy.
`MonitoringNotConfigured`, `MonitoringTargetMissing`, `MonitoringTargetDown` and
`MonitoringBackendUnavailable` cover those failures. Disabled services do not
raise availability alerts. MySQL, ES and MinIO performance/capacity rules remain
enabled.

Rules run locally. Sending notifications requires a configured Alertmanager;
Prometheus itself needs an external uptime check.

## Operations

```bash
# Inspect monitoring containers.
docker compose --env-file docker/.env -f docker/docker-compose-base.yml ps \
  prometheus mysql-exporter kvrocks-exporter nats-exporter elasticsearch-exporter opensearch-exporter

# Inspect credential initialization.
docker compose --env-file docker/.env -f docker/docker-compose-base.yml logs \
  mysql-monitoring-init silo-monitoring-init

# Reload rule changes.
docker compose --env-file docker/.env -f docker/docker-compose-base.yml kill -s SIGHUP prometheus

# Stop monitoring.
docker compose --env-file docker/.env -f docker/docker-compose-base.yml stop \
  prometheus mysql-exporter kvrocks-exporter nats-exporter elasticsearch-exporter opensearch-exporter

# Validate configuration and rules using the image's promtool.
docker compose --env-file docker/.env -f docker/docker-compose-base.yml run --rm --no-deps prometheus --check-config

# Validate Compose configuration across all profiles.
docker compose --env-file docker/.env -f docker/docker-compose-base.yml --profile '*' config --quiet
```

Prometheus creates its target files and expected inventory at startup, and copies
`prometheus.yml` unchanged into its runtime directory. Restart it after changing
that file. Rule files stay linked to their source, so SIGHUP reloads rule edits.
Metric history stays in the `prometheus_data` volume.

The validation command uses the deployment's Compose environment and removes
the temporary container after checking. It requires no host installation of promtool.

## MinIO metric details

MinIO/Silo uses one v3 scrape. Throughput is MiB/s, daily totals are GiB, and
storage I/O measures the Silo process. Silo omits zero-valued counters; recording
rules fill the gaps only for healthy targets before calculating rates and totals.
Daily totals cover the available history up to 24 hours and are not backfilled.
Object usage follows the background scanner.
