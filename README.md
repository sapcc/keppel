# keppel

Federated multi-tenant container image registry

TODO: explain more (esp. introduce the terms "account" and "tenant")

## Usage

Run with

```bash
make && PATH=$PWD/build:$PATH keppel-api
```

`keppel-api` expects that `keppel-registry` exists in `$PATH`, hence the manipulation of `$PATH` in this example.
`keppel-api` expects no command-line arguments, and takes configuration from the following environment variables:

| Variable | Default | Explanation |
| -------- | ------- | ----------- |
| `KEPPEL_API_LISTEN_ADDRESS` | :8080 | Listen address for HTTP server. |
| `KEPPEL_API_PUBLIC_URL` | *(required)* | URL where users reach keppel-api. |
| `KEPPEL_DB_URI` | *(required)* | A [libpq connection URI][pq-uri] that locates the Keppel database. The non-URI "connection string" format is not allowed; it must be a URI. |
| `KEPPEL_DRIVER_AUTH` | *(required)* | The name of an auth driver (TODO link to appropriate section). |
| `KEPPEL_DRIVER_ORCHESTRATION` | *(required)* | The name of an orchestration driver (TODO link to appropriate section). |
| `KEPPEL_DRIVER_STORAGE` | *(required)* | The name of a storage driver (TODO link to appropriate section). |
| `KEPPEL_ISSUER_KEY` | *(required)* | The private key (in PEM format) that keppel-api uses to sign auth tokens for Docker clients. |
| `KEPPEL_ISSUER_CERT` | *(required)* | The certificate (in PEM format) belonging to the key above. keppel-registry verifies client tokens using this certificate. |

Notes on `KEPPEL_ISSUER_KEY` and `KEPPEL_ISSUER_CERT`:

- Instead of a PEM-encoded key or cert, the variables may also contain the paths to the key or cert, respectively.
- The Subject Public Key of the certificate must be the public counterpart of the private issuer key. You can generate a suitable `trust` section by running `bash ./util/generate_trust.sh` in the repo root directory. Note that certificates expire! `util/generate_trust.sh` will generate a certificate with a validity of 1 year.

## Supported auth drivers

Auth drivers are responsible for authenticating users and authorizing their access to Keppel accounts.

### `keystone`

An auth driver using the Keystone V3 API of an OpenStack cluster. With this
driver, Keppel auth tenants correspond to Keystone projects. Incoming HTTP requests
are authenticated by reading a Keystone token from the X-Auth-Token request
header.

| Variable | Default | Explanation |
| -------- | ------- | ----------- |
| `OS_...` | *(required)* | A full set of OpenStack auth environment variables for Keppel's service user. See [documentation for openstackclient][os-env] for details. |
| `KEPPEL_AUTH_LOCAL_ROLE` | *(required)* | A Keystone role name that will be assigned to Keppel's service user in a project when creating a Keppel tenant there, in order to enable access to this project for the storage driver. For the `swift` storage driver, this will usually be `swiftoperator`. |
| `KEPPEL_OSLO_POLICY_PATH` | *(required)* | Path to the `policy.json` file for this service. |

Keppel understands access rules in the [`oslo.policy` JSON format][os-pol]. An example can be seen at
[`docs/example-policy.json`](./docs/example-policy.json). The following rules are expected:

- `account:list` is required for any non-anonymous access to the API.
- `account:show` enables read access to repository and tag listings.
- `account:pull` allows to `docker pull` images.
- `account:push` allows to `docker push` images.
- `account:edit` enables write access to an account's configuration.

## Supported storage drivers

Storage drivers are responsible for choosing the storage backend for the `keppel-registry` process for each individual Keppel account.

### `swift`

This driver only works with the `keystone` auth driver. For a given Keppel
account, it stores image data in the Swift container `keppel-$ACCOUNT_NAME` in
the OpenStack project that is this account's auth tenant.

## Supported orchestration drivers

Orchestration drivers are responsible for running the `keppel-registry`
processes for each individual Keppel account and reverse-proxing API requests to them as necessary.

### `local-processes`

Runs one `keppel-registry` process per Keppel account as a direct child process
of the `keppel-api` process. This orchestration driver is useful for
development purposes and not intended for usage in production.

[pq-uri]: https://www.postgresql.org/docs/9.6/static/libpq-connect.html#LIBPQ-CONNSTRING
[os-env]: https://docs.openstack.org/python-openstackclient/latest/cli/man/openstack.html
[os-pol]: https://docs.openstack.org/oslo.policy/latest/admin/policy-json-file.html
