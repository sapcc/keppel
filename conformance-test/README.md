<!--
SPDX-FileCopyrightText: 2025 SAP SE

SPDX-License-Identifier: Apache-2.0
-->

# How to run?

In the first terminal run `make run-api-for-conformance-test`

In a second terminal clone https://github.com/opencontainers/distribution-spec.git
and then run `cd conformance/` and `go test -c`.

Extract and export the envs from `.github/workflows/oci-distribution-conformance.yml`, replace `OCI_ROOT_URL=http://127.0.0.1:8080`
and then run the test suite `./conformance.test`.
