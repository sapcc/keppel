# Keppel Operator Guide

In this document:

- [Terminology and data model](#terminology-and-data-model)
  - [Drivers](#drivers)
- [Building and running Keppel](#building-and-running-keppel)

In other documents:

- [Supported drivers](./docs/drivers/)

## Terminology and data model

This document assumes that you have already read and understood the [general README](../README.md). If not, start
reading there.

TODO explain repos, blobs, blob mounts, manifests, tags

### Drivers

Keppel provides several interfaces for pluggable **drivers**, and it is up to you to choose the appropriate drivers for
your environment:

- The **auth driver** accesses an external auth service and translates users and permissions in that auth service into
  permissions in Keppel. The choice of auth driver therefore defines which auth tenants exist.

- The **storage driver** accesses an external storage service and chooses the backing storage for each Keppel account.
  The choice of storage driver is usually linked to the choice of auth driver because the auth driver needs to set up
  permissions for Keppel to access the storage service.

- The **name claim driver** decides which account names a given user and auth tenant is allowed to claim. In a
  single-region deployment, the "trivial" name claim driver allows everyone to claim any unused name. In a multi-region
  deployment, an appropriate name claim driver could access a central service that manages account name claims. As for
  storage drivers, the choice of name claim driver may be linked to the choice of auth driver.

## Building and running Keppel

Build Keppel with `make`, install with `make install` or `docker build`. This is the same as in the general README,
since the server and client components are all combined in one single binary. For a complete Keppel deployment, you need
to run:

- as many instances of `keppel server api` as you want, and
- exactly one instance of `keppel server janitor`.

Both commands take configuration from environment variables.

### Common configuration options

The following configuration options are understood by both the API server and the janitor:

| Variable | Default | Explanation |
| -------- | ------- | ----------- |
| `KEPPEL_API_PUBLIC_URL` | *(required)* | URL where users reach keppel-api. |
| `KEPPEL_DB_URI` | *(required)* | A [libpq connection URI][pq-uri] that locates the Keppel database. The non-URI "connection string" format is not allowed; it must be a URI. |
| `KEPPEL_DRIVER_AUTH` | *(required)* | The name of an auth driver. |
| `KEPPEL_DRIVER_NAMECLAIM` | *(required)* | The name of a name claim driver. For single-region deployments, the correct choice is probably `trivial`. |
| `KEPPEL_DRIVER_STORAGE` | *(required)* | The name of a storage driver. |
| `KEPPEL_ISSUER_KEY` | *(required)* | The private key (in PEM format, or given as a path to a PEM file) that keppel-api uses to sign auth tokens for Docker clients. Can be generated with `openssl genrsa -out privkey.pem 4096`. |

To choose drivers, refer to the [documentation for drivers](./docs/drivers/). Note that some drivers require additional
configuration as mentioned in their respective documentation.

[pq-uri]: https://www.postgresql.org/docs/9.6/static/libpq-connect.html#LIBPQ-CONNSTRING

### API server configuration options

These options are only understood by the API server.

| Variable | Default | Explanation |
| -------- | ------- | ----------- |
| `KEPPEL_API_LISTEN_ADDRESS` | :8080 | Listen address for HTTP server. |
| `KEPPEL_AUDIT_RABBITMQ_URI` | *(optional)* | RabbitMQ URI as per the [AMQP URI format](https://www.rabbitmq.com/uri-spec.html). If this variable is configured then Keppel will send audit events to the respective RabbitMQ server. |
| `KEPPEL_AUDIT_RABBITMQ_QUEUE_NAME` | *(required if `KEPPEL_AUDIT_RABBITMQ_URI` is configured)* | Name for the queue that will hold the audit events. The events are published to the default exchange. |
| `KEPPEL_AUDIT_SILENT` | *(optional)* | Whether to disable audit event logging to standard output. |
| `KEPPEL_PEERS` | *(optional)* | A comma-separated list of hostnames where our peer keppel-api instances are running. This is the set of instances that this keppel-api can replicate from. |

### Janitor configuration options

These options are only understood by the janitor.

| Variable | Default | Explanation |
| -------- | ------- | ----------- |
| `KEPPEL_JANITOR_LISTEN_ADDRESS` | :8080 | Listen address for janitor process (provides HTTP endpoint for Prometheus metrics). |
