#!/bin/sh
set -eu

. "$(dirname "$0")/monitoring-config.sh"
monitoring_init
export MYSQL_EXPORTER_USER="${MYSQL_EXPORTER_USER:-exporter}"
case "$monitoring_doc_engine" in
    elasticsearch)
        export ES_USERNAME="${ES_EXPORTER_USER:-elastic}" ES_PASSWORD="${ES_EXPORTER_PASSWORD:-}"
        export ES_URI="$ELASTICSEARCH_EXPORTER_URL"
        ;;
    opensearch)
        export ES_USERNAME="${OPENSEARCH_EXPORTER_USER:-admin}" ES_PASSWORD="${OPENSEARCH_EXPORTER_PASSWORD:-}"
        export ES_URI="$OPENSEARCH_EXPORTER_URL"
        ;;
esac
exec supervisord -c /etc/supervisord.conf
