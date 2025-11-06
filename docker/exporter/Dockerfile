FROM alpine:3.18 AS builder

RUN apk add --no-cache wget tar

RUN wget -O /tmp/redis_exporter.tar.gz https://github.com/oliver006/redis_exporter/releases/download/v1.57.0/redis_exporter-v1.57.0.linux-amd64.tar.gz && \
    tar xzf /tmp/redis_exporter.tar.gz -C /tmp/ && \
    mv /tmp/redis_exporter-v1.57.0.linux-amd64/redis_exporter /usr/local/bin/redis_exporter

RUN wget -O /tmp/mysqld_exporter.tar.gz https://github.com/prometheus/mysqld_exporter/releases/download/v0.15.0/mysqld_exporter-0.15.0.linux-amd64.tar.gz && \
    tar xzf /tmp/mysqld_exporter.tar.gz -C /tmp/ && \
    mv /tmp/mysqld_exporter-0.15.0.linux-amd64/mysqld_exporter /usr/local/bin/mysqld_exporter

RUN wget -O /tmp/elasticsearch_exporter.tar.gz https://github.com/prometheus-community/elasticsearch_exporter/releases/download/v1.6.0/elasticsearch_exporter-1.6.0.linux-amd64.tar.gz && \
    tar xzf /tmp/elasticsearch_exporter.tar.gz -C /tmp/ && \
    mv /tmp/elasticsearch_exporter-1.6.0.linux-amd64/elasticsearch_exporter /usr/local/bin/elasticsearch_exporter

FROM alpine:3.18

RUN apk add --no-cache supervisor

RUN addgroup -g 1000 exporters && \
    adduser -D -u 1000 -G exporters exporter

COPY --from=builder /usr/local/bin/redis_exporter /usr/local/bin/
COPY --from=builder /usr/local/bin/mysqld_exporter /usr/local/bin/
COPY --from=builder /usr/local/bin/elasticsearch_exporter /usr/local/bin/

RUN mkdir -p /etc/supervisor.d /etc/exporters && \
    chown -R exporter:exporters /etc/exporters

COPY supervisord.conf /etc/supervisord.conf
COPY exporter-configs/ /etc/exporters/

RUN chmod +x /usr/local/bin/*_exporter && \
    chown -R exporter:exporters /usr/local/bin/*_exporter

USER exporter

EXPOSE 9121 9104 9114

CMD ["/usr/bin/supervisord", "-c", "/etc/supervisord.conf"]