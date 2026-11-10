# Native service monitoring

Prometheus scrapes the existing MinIO/Silo and ClickHouse services directly.
It starts independently; it does not start or initialize the monitored services.
API/Admin and MySQL metrics are outside this setup.

| Job | Native endpoint | Metrics |
| --- | --- | --- |
| `minio-job` | `localhost:9000/minio/metrics/v3` | Cluster capacity/health, objects, S3 requests/errors/traffic, process CPU/memory/I/O and drive metrics. |
| `clickhouse` | `localhost:9363/metrics` | Query/connection/memory gauges, cumulative events, asynchronous metrics and errors. |
| `prometheus` | `localhost:9090/metrics` | Prometheus itself. |

Prometheus uses Linux host networking. These default targets assume that the
services publish their ports on the same host. For other deployments, change the
targets in `prometheus.yml` to the actual reachable addresses. Changing
`MINIO_PORT` or `CLICKHOUSE_PROMETHEUS_PORT` also requires updating those targets.

The MinIO/Silo job uses the v3 root endpoint without a trailing slash. The pinned
Silo image already supports it; no exporter or image upgrade is needed.

Run the commands below from the repository root. They use Compose's normal project
name selection. If the existing deployment uses a custom project name, set
`COMPOSE_PROJECT_NAME` to that same name, or add `-p PROJECT_NAME` to each Compose
command. Use the existing deployment's project name consistently.

## Prepare the MinIO/Silo token once

If a valid token file is already present, proceed directly to Start Prometheus.

For a new deployment, generate a token manually using the existing MinIO/Silo
credentials. For MinIO managed by this Compose project, enter the running service:

```bash
docker compose --env-file docker/.env -f docker/docker-compose-base.yml exec minio /bin/sh
mc alias set monitor http://localhost:9000
mc admin prometheus generate monitor --api-version v3
exit
```

For a separately deployed MinIO/Silo service, run the same `mc` commands in its
running container and use the API address reachable from there.

When prompted, enter the deployment's existing access key and secret key. With
this Compose configuration, they come from `MINIO_USER` and `MINIO_PASSWORD`
in `docker/.env`, unless overridden at deployment time. Use the actual deployed
credentials rather than copying example passwords.

Copy only the generated `bearer_token` value. For the first deployment, create
its file with the permissions required by the pinned Prometheus image:

```bash
install -d -m 700 docker/prometheus/secrets
sudo install -m 600 -o 65534 -g 65534 /dev/null docker/prometheus/secrets/minio.token
sudo nano docker/prometheus/secrets/minio.token
```

Paste the token as a single line, without quotes or `bearer_token:`. In nano,
press Ctrl+O, Enter, then Ctrl+X to save and exit. Another editor can also be used.
To update an existing file, run only the editor command.

The token file is ignored by Git and mounted read-only at
`/run/secrets/minio.token`. A valid token remains usable across normal restarts.
After replacing an existing token file, restart only Prometheus to refresh its
single-file bind mount:

```bash
docker compose --env-file docker/.env -f docker/docker-compose-base.yml --profile prometheus restart prometheus
```

## Prepare ClickHouse

The Compose ClickHouse service mounts `docker/clickhouse/prometheus.xml` to expose
its native `/metrics` endpoint on container port 9363. Docker publishes that port
only on the host's IPv4 loopback address; the scrape target uses `localhost`.
`CLICKHOUSE_PROMETHEUS_PORT` selects the published host port (default 9363).
Use distinct host ports for deployments sharing a Docker host.
Apply this configuration through the normal ClickHouse deployment. Adding the
mount and port to an existing container requires recreating that container and
briefly interrupts connections; its existing data volume is retained.

## Start Prometheus

Once its token file is present, start only the optional monitoring service:

```bash
docker compose --env-file docker/.env -f docker/docker-compose-base.yml --profile prometheus up -d --no-deps --wait prometheus
```

Prometheus has no dependent initialization service. The monitored services may
be deployed separately; an unreachable target is shown as DOWN. Configuration and
rule files are mounted read-only, and historical metrics persist in `prometheus_data`.

## Rules and verification

Open <http://localhost:9090/targets>. All three configured jobs should be UP when
their services are running. The scrape and alert evaluation intervals are 60
seconds. The recording group also runs every 60 seconds to match the scrape interval.

Rules for other services remain in the repository and are not loaded until their
scrape jobs are configured. Four MinIO rule files are enabled:

| File | Purpose |
| --- | --- |
| `minio_alerts.yml` | Metrics availability, offline nodes, free capacity, request error rate, read/write/delete latency and predicted growth. |
| `minio_throughput_recording.yml` | Network and process storage I/O rates, daily totals and I/O efficiency ratios. |
| `minio_throughput_alerts.yml` | Network throughput alerts. |
| `minio_disk_throughput_alerts.yml` | Silo process storage I/O throughput alerts. |

All MinIO metrics come from one v3 scrape, avoiding duplicate accounting.
Rules use v3 names such as `minio_api_requests_traffic_received_bytes`,
`minio_system_process_io_write_bytes` and
`minio_system_drive_api_latency_micros`. S3 request counters use the `name`
label for the API operation, while drive latency still uses `api`; latency is
in microseconds and is converted to milliseconds by the alert rules.

This Silo release omits zero-valued metrics. Four recording rules normalize the
network and process I/O byte counters before the throughput and daily-total rules
apply `rate` or `increase`. Filling missing counters before calculating changes
includes the first nonzero traffic observed after an idle period. These rules run
in that order in the same 60-second group. Missing counters are filled with zero
only when the target is UP and its process uptime metric is present. The S3 `type`
label is removed so network and process I/O recordings have matching node labels.
A failed scrape does not record counter values or fallback zeros.

The performance alert group also records `minio:api:errors_total`, filling absent
error counters with zero for APIs whose S3 request counter is present and whose
target is UP. The error-rate alert evaluates this counter after it is recorded in
the same group, preserving the first error burst after successful requests. API
labels remain separate, and counter resets are handled by `rate`.

Capacity alerts use actual usable free space, treating an omitted free-space
gauge as zero when usable total capacity is present. This preserves full-disk
alerts. Offline-node metrics being absent while the cluster is healthy is normal.
Percentages are scaled to 0–100 before formatting. Storage I/O counters describe
the Silo process, not all activity on the host's disks.

Throughput values use MiB/s and daily totals use GiB. Efficiency ratios are
absent while their denominator is zero. Calculating changes requires at least
two recorded counter samples. Daily totals use the counter samples recorded since
these rules started, up to the last 24 hours; earlier samples are not backfilled.
Object counts and object usage follow the background scanner.

Examples:

```promql
up{job=~"minio-job|clickhouse"}
minio_cluster_health_capacity_usable_free_bytes{job="minio-job"}
rate(minio_api_requests_total{job="minio-job",type="s3"}[5m])
minio_system_process_resident_memory_bytes{job="minio-job"}
minio:network:write_throughput_mb_sec
minio:disk:total_throughput_mb_sec
ClickHouseMetrics_Query{job="clickhouse"}
ClickHouseMetrics_MemoryTracking{job="clickhouse"}
```

Rate queries need multiple scrapes. Alert thresholds retain the previous values.
Rules are evaluated in Prometheus; sending notifications requires a separately
configured Alertmanager.

## Manage

```bash
# Inspect only Prometheus.
docker compose --env-file docker/.env -f docker/docker-compose-base.yml --profile prometheus ps prometheus

# Reload scrape and rule files without restarting Prometheus.
docker compose --env-file docker/.env -f docker/docker-compose-base.yml --profile prometheus kill -s SIGHUP prometheus

# Stop monitoring without stopping the monitored services.
docker compose --env-file docker/.env -f docker/docker-compose-base.yml --profile prometheus stop prometheus
```

Silo source references for the pinned release: [v3 collectors](https://github.com/pgsty/silo/blob/RELEASE.2026-08-06T00-00-00Z/cmd/metrics-v3.go), [metric value emission](https://github.com/pgsty/silo/blob/RELEASE.2026-08-06T00-00-00Z/cmd/metrics-v3-types.go).
