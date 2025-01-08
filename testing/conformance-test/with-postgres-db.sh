#!/bin/sh

# Copyright 2022 SAP SE
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.
#
# SPDX-License-Identifier: Apache-2.0

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

if [ ! -d conformance-test/postgresql-data/ ]; then
  step "First-time setup: Creating PostgreSQL database for testing"
  initdb -A trust -U postgres conformance-test/postgresql-data/
fi
mkdir -p conformance-test/postgresql-run/

step "Configuring PostgreSQL"
sed -ie '/^#\?\(external_pid_file\|unix_socket_directories\|port\)\b/d' conformance-test/postgresql-data/postgresql.conf
(
  echo "external_pid_file = '${PWD}/conformance-test/postgresql-run/pid'"
  echo "unix_socket_directories = '${PWD}/conformance-test/postgresql-run'"
  echo "port = 54321"
) >> conformance-test/postgresql-data/postgresql.conf

# usage in trap is not recognized
# shellcheck disable=SC2317
stop_postgres() {
  EXIT_CODE=$?
  step "Stopping PostgreSQL"
  pg_ctl stop -D conformance-test/postgresql-data/ -w -s
  exit "${EXIT_CODE}"
}

step "Starting PostgreSQL"
rm -f -- conformance-test/postgresql.log
trap stop_postgres EXIT INT TERM
pg_ctl start -D conformance-test/postgresql-data/ -l conformance-test/postgresql.log -w -s

step "Running command: $*"
set +e
"$@"
EXIT_CODE=$?
set -e

exit "${EXIT_CODE}"
