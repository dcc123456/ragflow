#!/bin/bash
set -euo pipefail

# External MySQL servers manage their own monitoring accounts.
[[ "$MYSQL_EXPORTER_ADDRESS" == mysql:3306 ]] || exit 0

die() { printf 'MySQL monitoring: %s\n' "$*" >&2; exit 1; }

user="$MYSQL_EXPORTER_USER"
[[ "$user" =~ ^[a-zA-Z0-9_-]{1,32}$ && "$user" != root ]] || die 'Set MYSQL_EXPORTER_USER to a dedicated monitoring account.'
[[ -n "$MYSQL_PASSWORD" && -n "$MYSQL_EXPORTER_PASSWORD" ]] || die 'Set MYSQL_PASSWORD and MYSQL_EXPORTER_PASSWORD.'
[[ "$MYSQL_EXPORTER_PASSWORD" != *$'\n'* && "$MYSQL_EXPORTER_PASSWORD" != *$'\r'* ]] || die 'MYSQL_EXPORTER_PASSWORD must be a single-line value.'

mysql_query() {
    local username="$1"
    local connection_password="$2"
    shift 2

    MYSQL_PWD="$connection_password" timeout 8s mysql --protocol=TCP --host=mysql --port=3306 --user="$username" \
        --connect-timeout=3 --batch --skip-column-names --raw --binary-mode "$@"
}

password="${MYSQL_EXPORTER_PASSWORD//\'/\'\'}"
mysql_query root "$MYSQL_PASSWORD" >/dev/null 2>&1 <<SQL || die 'Cannot create the monitoring account. Check root access from the Compose network.'
SET SESSION sql_mode='NO_BACKSLASH_ESCAPES';
CREATE USER IF NOT EXISTS '$user'@'%' IDENTIFIED BY '$password' WITH MAX_USER_CONNECTIONS 3;
SQL
unset password

identity="$(mysql_query "$user" "$MYSQL_EXPORTER_PASSWORD" --execute 'SELECT CURRENT_USER()' 2>/dev/null)" || die 'The monitoring account password does not match MYSQL_EXPORTER_PASSWORD.'
[[ "$identity" == "$user@%" ]] || die 'A more specific MySQL account matched. Check the monitoring account host restrictions.'
mysql_query root "$MYSQL_PASSWORD" >/dev/null 2>&1 <<SQL || die 'Cannot grant the monitoring account permissions.'
GRANT PROCESS, REPLICATION CLIENT ON *.* TO '$user'@'%';
GRANT SELECT ON performance_schema.* TO '$user'@'%';
SQL
echo 'MySQL monitoring account ready.'
