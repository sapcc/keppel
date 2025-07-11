# SPDX-FileCopyrightText: 2025 SAP SE
# SPDX-License-Identifier: Apache-2.0
#
# shellcheck shell=sh

export KEPPEL_API_PUBLIC_FQDN=localhost
export KEPPEL_ISSUER_KEY=./conformance-test/privkey.pem
export KEPPEL_DB_CONNECTION_OPTIONS=sslmode=disable
export KEPPEL_DB_PASSWORD=
export KEPPEL_DB_PORT=54321
export KEPPEL_USERNAME=johndoe
export KEPPEL_PASSWORD=SuperSecret

export KEPPEL_DRIVER_AUTH=trivial
export KEPPEL_DRIVER_FEDERATION=trivial
export KEPPEL_DRIVER_INBOUND_CACHE=trivial
export KEPPEL_DRIVER_STORAGE=filesystem
export KEPPEL_FILESYSTEM_PATH=./conformance-test/storage

# clean out the backing storage from the previous run (the `test -d` is a
# safety net to ensure that we don't delete something unintended by accident)
if [ -d "${KEPPEL_FILESYSTEM_PATH}/bogus/conformance-test" ]; then
  rm -rf -- "${KEPPEL_FILESYSTEM_PATH}"
fi

export KEPPEL_RUN_DB_SETUP_FOR_CONFORMANCE_TEST=true

# export KEPPEL_OSLO_POLICY_PATH=docs/example-policy.yaml
