#!/bin/sh
# shellcheck shell=ash
set -euo pipefail

# Darwin compatibility
if hash greadlink >/dev/null 2>/dev/null; then
  readlink() { greadlink "$@"; }
fi

# set working directory to repo root
cd "$(dirname "$(dirname "$(readlink -f "$0")")")"

step() {
  printf '\x1B[1;36m>>\x1B[0;36m %s...\x1B[0m\n' "$1"
}

if [ ! -d testing/postgresql-data/ ]; then
  step "First-time setup: Creating PostgreSQL database for testing"
  initdb -A trust -U postgres testing/postgresql-data/
fi
mkdir -p testing/postgresql-run/

step "Configuring PostgreSQL"
sed -ie '/^#\?\(external_pid_file\|unix_socket_directories\|port\)\b/d' testing/postgresql-data/postgresql.conf
(
  echo "external_pid_file = '${PWD}/testing/postgresql-run/pid'"
  echo "unix_socket_directories = '${PWD}/testing/postgresql-run'"
  echo "port = 54321"
) >> testing/postgresql-data/postgresql.conf

# usage in trap is not recognized
# shellcheck disable=SC2317
stop_postgres() {
  EXIT_CODE=$?
  step "Stopping PostgreSQL"
  pg_ctl stop -D testing/postgresql-data/ -w -s
  exit "${EXIT_CODE}"
}

step "Starting PostgreSQL"
rm -f -- testing/postgresql.log
trap stop_postgres EXIT INT TERM
pg_ctl start -D testing/postgresql-data/ -l testing/postgresql.log -w -s

step "Running command: $*"
set +e
"$@"
EXIT_CODE=$?
set -e

exit "${EXIT_CODE}"
