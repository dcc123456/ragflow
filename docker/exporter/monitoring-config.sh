#!/bin/sh
# Deployment selection and listener validation shared by both monitoring containers.
monitoring_valid_port() {
    case "$1" in ''|*[!0-9]*|0*) return 1 ;; esac
    [ "${#1}" -le 5 ] && [ "$1" -ge "$2" ] && [ "$1" -le 65535 ]
}

monitoring_claim_port() {
    if ! monitoring_valid_port "$2" 1024; then
        echo "$1 exporter configuration invalid: listener port must be 1024..65535" >&2
        return 1
    fi
    case "$monitoring_ports" in
        *" $2 "*) echo "$1 exporter configuration invalid: listener port is already reserved" >&2; return 1 ;;
    esac
    monitoring_ports="$monitoring_ports$2 "
}

monitoring_init() {
    MYSQL_EXPORTER_PORT="${MYSQL_EXPORTER_PORT:-9104}"
    KVROCKS_EXPORTER_PORT="${KVROCKS_EXPORTER_PORT:-9121}"
    NATS_EXPORTER_PORT="${NATS_EXPORTER_PORT:-7777}"
    ES_EXPORTER_PORT="${ES_EXPORTER_PORT:-9114}"
    MYSQL_EXPORTER_SELECTED=0 ES_EXPORTER_SELECTED=0
    MYSQL_EXPORTER_ENABLED=false KVROCKS_EXPORTER_ENABLED=false
    NATS_EXPORTER_ENABLED=false ES_EXPORTER_ENABLED=false
    monitoring_ports=" "
    monitoring_db_type="$(printf '%s' "${DB_TYPE:-mysql}" | tr '[:upper:]' '[:lower:]')"
    monitoring_doc_engine="$(printf '%s' "${DOC_ENGINE:-elasticsearch}" | tr '[:upper:]' '[:lower:]')"
    if [ "$monitoring_db_type" = mysql ]; then
        MYSQL_EXPORTER_SELECTED=1
        if monitoring_claim_port mysql "$MYSQL_EXPORTER_PORT"; then MYSQL_EXPORTER_ENABLED=true; fi
    fi
    if monitoring_claim_port kvrocks "$KVROCKS_EXPORTER_PORT"; then KVROCKS_EXPORTER_ENABLED=true; fi
    if monitoring_claim_port nats "$NATS_EXPORTER_PORT"; then NATS_EXPORTER_ENABLED=true; fi
    case "$monitoring_doc_engine" in
        elasticsearch|opensearch)
            ES_EXPORTER_SELECTED=1
            if monitoring_claim_port elasticsearch "$ES_EXPORTER_PORT"; then ES_EXPORTER_ENABLED=true; fi
            ;;
    esac
    export MYSQL_EXPORTER_ENABLED KVROCKS_EXPORTER_ENABLED NATS_EXPORTER_ENABLED ES_EXPORTER_ENABLED
    export MYSQL_EXPORTER_PORT KVROCKS_EXPORTER_PORT NATS_EXPORTER_PORT ES_EXPORTER_PORT
}
