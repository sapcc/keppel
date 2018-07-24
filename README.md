# keppel

Federated multi-tenant container image registry

## Usage

Run with

```bash
make && PATH=$PWD/build:$PATH keppel-api
```

`keppel-api` expects that `keppel-registry` exists in `$PATH`, hence the manipulation of `$PATH` in this example.
`keppel-api` requires the following environment variables:

- `OS_AUTH_URL`, `OS_USERNAME` etc.: the conventional OpenStack auth vars for Keppel's own service user (only Identity API v3 is supported)
- `KEPPEL_POSTGRES_URI`: a [libpq connection URI](https://www.postgresql.org/docs/9.6/static/libpq-connect.html#LIBPQ-CONNSTRING)
- `KEPPEL_LISTEN_ADDRESS`: listen address for HTTP server of keppel-api (defaults to `:8080`)
- `KEPPEL_LOCAL_ROLE`: a Keystone role name that enables read-write access to a project's Swift account when assigned at the project level (usually `swiftoperator`)

The Postgres user must have permission to create additional databases. For each registry account, the database name is derived by concatenating the original database name in `KEPPEL_POSTGRES_URI` with the project ID using a dash, i.e. `account_db_name = keppel_db_name + "-" + project_id`.
