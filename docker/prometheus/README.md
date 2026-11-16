# Service monitoring

Prometheus monitors MinIO/Silo and ClickHouse directly, plus MySQL, Kvrocks, NATS
and Elasticsearch/OpenSearch through the combined components-exporter container.
Both monitoring containers join the `ragflow` bridge network and communicate by
service name. Prometheus publishes its UI at `127.0.0.1:9090`; exporter ports stay
inside the Compose network. Monitoring starts independently of the application
and monitored services.

## Start monitoring

Run from the repository root, using the same Compose project name as the
application deployment. Start the middleware first, then prepare credentials once:

```bash
bash docker/prometheus/setup.sh
```

Start monitoring after setup succeeds:

```bash
docker compose --env-file docker/.env -f docker/docker-compose-base.yml up -d --build --wait prometheus
```

Starting `prometheus` also starts `components-exporter`. The dependency waits
for container startup, not backend health.

To include monitoring in normal deployment startup, append `prometheus` to
`COMPOSE_PROFILES` in `docker/.env`.

## Configuration

Targets follow the deployment settings in `docker/.env`:

| Job | Default metrics endpoint | Enabled when |
| --- | --- | --- |
| `minio-job` | `minio:9000/minio/metrics/v3` | `MONITORING_MINIO_ENABLED=true` (default) |
| `clickhouse` | `clickhouse:9363/metrics` | `MONITORING_CLICKHOUSE_ENABLED=true` (default) |
| `mysql` | `components-exporter:9104/metrics` | `DB_TYPE=mysql` |
| `kvrocks` | `components-exporter:9121/metrics` | Always |
| `nats` | `components-exporter:7777/metrics` | Always |
| `elasticsearch` | `components-exporter:9114/metrics` | `DOC_ENGINE=elasticsearch` or `opensearch` |

Keep `DB_TYPE` and `DOC_ENGINE` consistent with the application. Set
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
An invalid or conflicting listener disables only that exporter.

`components-exporter` uses `MEM_LIMIT` from `docker/.env` as the total memory limit
for Supervisor and all exporter processes. Recreate the container with Compose
after changing this limit.

Adjust the `global` time settings in `prometheus.yml` to control scrape frequency,
scrape timeout and rule evaluation frequency. Each alert's `for` controls how long
its condition must hold before firing. After changing deployment settings or
exporter credentials, repeat the startup command above.
Changes under `docker/exporter` require rebuilding `components-exporter`; the
startup command includes `--build` so its copied scripts stay up to date.

## Credentials

`setup.sh` uses `docker/.env` and `docker/docker-compose-base.yml`. Additional
arguments are forwarded to Compose; for example, use `-p PROJECT` for your
deployment's project name. It connects to the running Compose `mysql:3306` and
`minio:9000` services, so both must be running. This is a one-time initialization
command for those two services. Prepare credentials separately for external services.

### MySQL

Set `MYSQL_EXPORTER_USER` and `MYSQL_EXPORTER_PASSWORD` in `.env`. The script uses
`MYSQL_PASSWORD` to connect as root, creates the monitoring account if absent,
then verifies its password and grants `PROCESS`, `REPLICATION CLIENT`, and
`SELECT ON performance_schema.*`. Existing passwords are never reset.

### MinIO/Silo

Every run generates a v3 token using `MINIO_USER` and `MINIO_PASSWORD` and saves
it to `secrets/minio.token` with owner UID/GID 65534 and mode 600, using `sudo`
when needed. Token contents are ignored by Git. A failed generation leaves the
existing token file in place. After running setup, restart an already running
Prometheus to read the generated token:

```bash
docker compose --env-file docker/.env -f docker/docker-compose-base.yml restart prometheus
```

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
docker compose --env-file docker/.env -f docker/docker-compose-base.yml ps components-exporter prometheus

# Reload rule changes.
docker compose --env-file docker/.env -f docker/docker-compose-base.yml kill -s SIGHUP prometheus

# Stop monitoring.
docker compose --env-file docker/.env -f docker/docker-compose-base.yml stop components-exporter prometheus

# Validate configuration and rules using the image's promtool.
docker compose --env-file docker/.env -f docker/docker-compose-base.yml run --rm --no-deps prometheus --check-config
docker compose --env-file docker/.env -f docker/docker-compose-base.yml --profile prometheus config --quiet
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
