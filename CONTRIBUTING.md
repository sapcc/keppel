# Overview for developers/contributors

You should have read the entire [README.md](./README.md) once before starting
to work on Castellum's codebase. This document assumes that you did that already.

## Testing methodology

### Core implementation

Run the full test suite with:

```sh
$ ./testing/with-postgres-db.sh make check
```

This will produce a coverage report at `build/cover.html`.
