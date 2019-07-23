#!/bin/sh
set -euo pipefail

# set working directory to repo root
cd "$(dirname "$(dirname "$(readlink -f "$0")")")"

step() {
  echo -e "\e[1;36m>>\e[0;36m $@...\e[0m"
}

if [ ! -d testing/postgresql-data/ ]; then
  step "First-time setup: Creating PostgreSQL database for testing"
  initdb -A trust -U postgres testing/postgresql-data/
fi
mkdir -p testing/postgresql-run/

step "Configuring PostgreSQL"
sed -i '/^#\?\(external_pid_file\|unix_socket_directories\|port\)\b/d' testing/postgresql-data/postgresql.conf
(
  echo "external_pid_file = '${PWD}/testing/postgresql-run/pid'"
  echo "unix_socket_directories = '${PWD}/testing/postgresql-run'"
  echo "port = 54321"
) >> testing/postgresql-data/postgresql.conf

step "Starting PostgreSQL"
rm -f -- testing/postgresql.log
pg_ctl start -D testing/postgresql-data/ -l testing/postgresql.log -w -s

step "Running command: $@"
set +e
"$@"
EXIT_CODE=$?
set -e

step "Stopping PostgreSQL"
pg_ctl stop -D testing/postgresql-data/ -w -s

exit "${EXIT_CODE}"
