# Keppel, a multi-tenant container image registry

In this document:

- [Overview](#overview)
- [Terminology](#terminology)
- [Building and running Keppel](#building-and-running-keppel)

In other documents:

- [Supported drivers](./docs/drivers/)
- [API specification](./docs/api-spec.md)
- [Notes for developers/contributors](./CONTRIBUTING.md)

## Overview

When working with the container ecosystem (Docker, Kubernetes, etc.), you need a place to store your container images.
The default choice is the [Docker Registry](https://github.com/docker/distribution), but a Docker Registry alone is not
enough in productive deployments because you also need a compatible OAuth2 provider. A popular choice is
[Harbor](https://goharbor.io), which bundles a Docker Registry, an auth provider, and some other tools. Another choice
is [Quay](https://github.com/quay/quay), which implements the registry protocol itself, but is otherwise very similar to
Harbor.

However, Harbor's architecture is limited by its use of a single registry that is shared between all users. Most
importantly, by putting the images of all users in the same storage, quota and usage tracking need to be implemented
from scratch to ensure accurate controlling and billing. Keppel uses a different approach: It orchestrates a fleet of
Docker registry instances that all have separate storage spaces. Storage quota and usage can therefore be tracked by the
backing storage. This orchestration is completely transparent to the user: A unified API endpoint is provided that
multiplexes requests to their respective registry instances.

Keppel provides a full implementation of the [OCI Distribution
API](https://github.com/opencontainers/distribution-spec), the standard API for container image registries.
It also provides a [custom API](docs/api-spec.md) to control the multitenancy added by Keppel and to expose additional
metadata about repositories, manifests and tags.

## Terminology

Within Keppel, an **account** is a namespace that gets its own registry instance, and therefore, its own backing
storage. The account name is the first path element in the name of a repository. For example, consider the image
`keppel.example.com/foo/bar:latest`. It's repository name is `foo/bar`, which means it's located in the Keppel account
`foo`.

Access is controlled by the account's **auth tenant** or just **tenant**. Note that tenants and accounts are separate
concepts: An account corresponds to one backing storage, and a tenant corresponds to an authentication/authorization
scope. Each account is linked to exactly one auth tenant, but there can be multiple accounts linked to the same auth
tenant.

Keppel provides several interfaces for pluggable **drivers**, and it is up to the Keppel operator to choose the
appropriate drivers for their environment:

- The **auth driver** accesses an external auth service and translates users and permissions in that auth service into
  permissions in Keppel. The choice of auth driver therefore defines which auth tenants exist.

- The **storage driver** accesses an external storage service and chooses the backing storage for each Keppel account.
  The choice of storage driver is usually linked to the choice of auth driver because the auth driver needs to set up
  permissions for Keppel to access the storage service.

- The **name claim driver** decides which account names a given user and auth tenant is allowed to claim. In a
  single-region deployment, the "trivial" name claim driver allows everyone to claim any unused name. In a multi-region
  deployment, an appropriate name claim driver could access a central service that manages account name claims. As for
  storage drivers, the choice of name claim driver may be linked to the choice of auth driver.

- The **orchestration driver** manages the fleet of Docker registry instances, and proxies requests to them.

## Building and running Keppel

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
| `KEPPEL_AUDIT_RABBITMQ_URI` | *(optional)* | RabbitMQ URI as per the [AMQP URI format](https://www.rabbitmq.com/uri-spec.html). If this variable is configured then Keppel will send audit events to the respective RabbitMQ server. |
| `KEPPEL_AUDIT_RABBITMQ_QUEUE_NAME` | *(required if `KEPPEL_RABBITMQ_URI` is configured)* | Name for the queue that will hold the audit events. The events are published to the default exchange. |
| `KEPPEL_AUDIT_SILENT` | *(optional)* | Whether to disable audit event logging to standard output. |
| `KEPPEL_DB_URI` | *(required)* | A [libpq connection URI][pq-uri] that locates the Keppel database. The non-URI "connection string" format is not allowed; it must be a URI. |
| `KEPPEL_DRIVER_AUTH` | *(required)* | The name of an auth driver. |
| `KEPPEL_DRIVER_NAMECLAIM` | *(required)* | The name of a name claim driver. For single-region deployments, the correct choice is probably `trivial`. |
| `KEPPEL_DRIVER_ORCHESTRATION` | *(required)* | The name of an orchestration driver. |
| `KEPPEL_DRIVER_STORAGE` | *(required)* | The name of a storage driver. |
| `KEPPEL_ISSUER_KEY` | *(required)* | The private key (in PEM format, or given as a path to a PEM file) that keppel-api uses to sign auth tokens for Docker clients. |
| `KEPPEL_ISSUER_CERT` | *(required)* | The certificate (in PEM format, or given as a path to a PEM file) belonging to the key above. keppel-registry verifies client tokens using this certificate. |
| `KEPPEL_PEERS` | *(optional)* | A comma-separated list of hostnames where our peer keppel-api instances are running. This is the set of instances that this keppel-api can replicate from. |

To choose drivers, refer to the [documentation for drivers](./docs/drivers). Note that some drivers require additional
configuration as mentioned in their respective documentation.

For `KEPPEL_ISSUER_KEY` and `KEPPEL_ISSUER_CERT`, the Subject Public Key of the certificate must be the public
counterpart of the private issuer key. Here's an example of how to generate such a pair of private key and certificate:

```bash
openssl genrsa -out privkey.pem 4096 2>/dev/null
openssl req -x509 -sha256 -days 365 -subj "/CN=keppel" -key privkey.pem -out cert.pem
```

[pq-uri]: https://www.postgresql.org/docs/9.6/static/libpq-connect.html#LIBPQ-CONNSTRING
