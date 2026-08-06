#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")"

stop_postgres() {
	EXIT_CODE=$?
	pg_ctl stop --wait --silent -D datadir
	exit "${EXIT_CODE}"
}
trap stop_postgres EXIT INT TERM

rm -f -- run/postgresql.log
pg_ctl start --wait --silent -D datadir -l run/postgresql.log
$COMMAND -U postgres -h 127.0.0.1 -p 54320 "$@"
