<!--
SPDX-FileCopyrightText: 2025 SAP SE

SPDX-License-Identifier: Apache-2.0
-->

# Overview for developers/contributors

You should have read the entire [README.md](./README.md) once before starting
to work on Keppel's codebase. This document assumes that you did that already.

## Testing methodology

In the following parts is described how to run Keppel locally and the test suite.

### Run Keppel locally

You can start Keppel locally with `make run-api` but without the correct environment variables in the `.env` file it will stop really fast.

The content of an `.env` file to get Keppel up and running is shown below:

```bash
export KEPPEL_API_PUBLIC_FQDN=localhost
export KEPPEL_API_PUBLIC_URL=http://localhost:8080
export KEPPEL_DB_CONNECTION_OPTIONS=sslmode=disable
export KEPPEL_DB_PASSWORD=mysecretpassword
export KEPPEL_DRIVER_AUTH=trivial
export KEPPEL_DRIVER_FEDERATION=trivial
export KEPPEL_DRIVER_INBOUND_CACHE=trivial
export KEPPEL_DRIVER_STORAGE=filesystem
export KEPPEL_FILESYSTEM_PATH=./keppel
export KEPPEL_ISSUER_KEY=./privkey.pem
export KEPPEL_PASSWORD=SuperSecret
export KEPPEL_USERNAME=johndoe
```

In addition to that the following extra steps are required:
- A private key in PEM format is required to sign auth token for Docker clients. It can be generated with `openssl genrsa -out privkey.pem 4096`.
- A local postgresql instance. You can start one in docker with the following command: `docker run --name postgres -e POSTGRES_PASSWORD=mysecretpassword -e POSTGRES_DB=keppel -p 127.0.0.1:5432:5432 -d postgres`
- An ephemeral postgresql instance can also be started with `./testing/with-postgres-db.sh make run-api`. This requires adding `export KEPPEL_DB_PORT=54321` to the `.env` file.

### Run the test suite

Run the full test suite with:

```sh
$ ./testing/with-postgres-db.sh make check
```

This will produce a coverage report at `build/cover.html`.

## Code structure

Once compiled, Keppel is only a single binary containing subcommands for the various components. This reduces the size of
the compiled application dramatically since a lot of code is shared. The main entrypoint is in `main.go`, from which
everything else follows.

The `main.go` is fairly compact. The entrypoints for the respective subcommands are below `cmd/`, and the bulk of the
code is below `internal/`, organized into packages as follows: (listed roughly from the bottom up)

| Package | `go test` | Contents |
| --- | :---: | --- |
| `internal/keppel` | no | base data types, configuration parsing, driver interfaces, various utility methods |
| `internal/test` | no | test doubles etc. |
| `internal/processor` | no | reusable parts of the API server implementation |
| `internal/tasks` | yes | implementation of the janitor tasks |
| `internal/client` | yes | library for client-side access to Keppel's APIs (used by client commands and by replication) |
| `internal/tokenauth` | no | reusable parts of the authentication workflow (mostly regarding the token-based auth workflow used by the Registry API) |
| `internal/api/auth` | yes | implementation of the authentication workflow of the Distribution API |
| `internal/api/keppel` | yes | implementation of the Keppel API |
| `internal/api/registry` | yes | implementation of the Distribution API |
| `internal/drivers` | no | productive driver implementations (not covered by unit tests) |
