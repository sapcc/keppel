# Overview for developers/contributors

You should have read the entire [README.md](./README.md) once before starting
to work on Keppel's codebase. This document assumes that you did that already.

## Testing methodology

### Core implementation

Run the full test suite with:

```sh
$ ./testing/with-postgres-db.sh make check
```

This will produce a coverage report at `build/cover.html`.

## Working on the `kubernetes` orchestration driver

When running `keppel-api` for testing (preferably through `make run-api`), you
should use the `local-processes` orchestration driver if possible. If you need
to test the `kubernetes` orchestration driver, observe the special procedures
noted in its documentation. Our own helm chart has a `dev-toolbox` to help with
testing the `kubernetes` driver, see documentation over there.
