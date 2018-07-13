# keppel

Federated multi-tenant container image registry

## Usage

Run with 

```bash
make && PATH=$PWD/build:$PATH keppel-api
```

`keppel-api` expects that `keppel-registry` is reachable in `$PATH`, hence the manipulation of `$PATH` in this example.
`keppel-api` requires the following environment variables:

- `OS_AUTH_URL`, `OS_USERNAME` etc.: the conventional OpenStack auth vars (only Identity API v3 is supported)
- `KEPPEL_POSTGRES_URI`: a [libpq connection URI](https://www.postgresql.org/docs/9.6/static/libpq-connect.html#LIBPQ-CONNSTRING)
- `KEPPEL_LOCAL_ROLE`: a Keystone role name that enables read-write access to a project's Swift account when assigned at the project level (usually `swiftoperator`)
