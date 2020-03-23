# Overview for developers/contributors

You should have read the entire [README.md](./README.md) once before starting
to work on Keppel's codebase. This document assumes that you did that already.

## Testing methodology

Run the full test suite with:

```sh
$ ./testing/with-postgres-db.sh make check
```

This will produce a coverage report at `build/cover.html`.

## Code structure

Once compiled, Limes is only a single binary containing subcommands for the various components. This reduces the size of
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
| `internal/auth` | no | reusable parts of the authentication workflow |
| `internal/api/auth` | yes | implementation of the authentication workflow of the Distribution API |
| `internal/api/keppel` | yes | implementation of the Keppel API |
| `internal/api/registry` | yes | implementation of the Distribution API |
| `internal/drivers` | no | productive driver implementations (not covered by unit tests) |
