FROM registry-1.docker.io/prom/mysqld-exporter:v0.21.0 AS mysql
FROM registry-1.docker.io/kvrocks/kvrocks-exporter:1.0.11 AS kvrocks
FROM registry-1.docker.io/natsio/prometheus-nats-exporter:0.20.3 AS nats
FROM quay.io/prometheuscommunity/elasticsearch-exporter:v1.11.0 AS elasticsearch

FROM registry-1.docker.io/library/alpine:3.24.3
RUN apk add --no-cache ca-certificates supervisor \
    && addgroup -g 1000 exporters \
    && adduser -D -u 1000 -G exporters exporter
COPY --from=mysql /bin/mysqld_exporter /usr/local/bin/
COPY --from=kvrocks /ko-app/kvrocks_exporter /usr/local/bin/kvrocks_exporter
COPY --from=nats /prometheus-nats-exporter /usr/local/bin/
COPY --from=elasticsearch /bin/elasticsearch_exporter /usr/local/bin/
COPY entrypoint.sh monitoring-config.sh /usr/local/bin/
COPY supervisord.conf /etc/supervisord.conf
RUN chmod 755 /usr/local/bin/entrypoint.sh
USER 1000:1000
HEALTHCHECK --interval=15s --timeout=5s --start-period=10s --retries=3 \
    CMD ["supervisorctl", "-c", "/etc/supervisord.conf", "pid"]
ENTRYPOINT ["/usr/local/bin/entrypoint.sh"]
