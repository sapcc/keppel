# Keppel Operator Guide

In this document:

- [Terminology and data model](#terminology-and-data-model)
  - [Validation and garbage collection](#validation-and-garbage-collection)
- [Building and running Keppel](#building-and-running-keppel)
  - [Drivers](#drivers)
  - [Common configuration options](#common-configuration-options)
  - [API server configuration options](#api-server-configuration-options)
  - [Janitor configuration options](#janitor-configuration-options)
- [Prometheus metrics](#prometheus-metrics)

In other documents:

- [Supported drivers](./docs/drivers/)

## Terminology and data model

This document assumes that you have already read and understood the [general README](../README.md). If not, start
reading there.

As outlined in the general README, Keppel stores data in **accounts** (each of which is tied to exactly one backing
storage) that are associated with **auth tenants** that control access to them.

Accounts are structured into **repositories**. Each repository contains any number of blobs, manifests and tags:

- **Manifests** collect a number of references to other manifests and blobs plus some metadata. Keppel parses manifests
  before storing them, so users are limited to the manifest formats supported by Keppel. Those supported formats are
  [Docker images and image lists](https://docs.docker.com/registry/spec/manifest-v2-2/), [OCI image
  manifests](https://github.com/opencontainers/image-spec/blob/master/image-index.md) and [OCI image
  indexes](https://github.com/opencontainers/image-spec/blob/master/image-index.md). Manifests are identified by their
  SHA-256 digest.

- **Blobs** are binary objects with arbitrary contents. Keppel does not inspect blobs' contents, but because only blobs
  referenced by manifests can be stored long-term (all blobs without a manifest reference are garbage-collected, see
  below), only blob types supported by the aforementioned manifest formats will be seen in practice. Those are mostly
  image layers and image configurations. Blobs, like manifests, are identified by their SHA-256 digest.

- **Tags** are named references to manifests (similar to how Git tags are just nice names for Git commits).

For example, consider the following image reference appearing in a Docker command:

```bash
$ docker pull keppel.example.com/os-images/debian:jessie-slim
```

This is referring to the tag `jessie-slim` within the repository `debian` in the Keppel account `os-images`. The Docker
client (as well as the OCI Distribution API spec) would consider the repository to be `os-images/debian` in this case
since they don't know about Keppel accounts. In general, the Keppel account name is always the first part of
the repository name, up to the first slash. In Keppel, we usually refer to `debian` as the "repository name" and
`os-images/debian` as the "full repository name".

Multiple Keppel deployments can be set up as **peers** of each other, which enables users to setup replication between
accounts of the same name on multiple peers. For example, if `keppel-1.example.com` and `keppel-2.example.com` are peers
of each other, and the account `library` is set up as a primary account on `keppel-1.example.com` and a replica of that
primary account on `keppel-2.example.com`, then the following will work:

```bash
$ docker push keppel-1.example.com/library/myimage:mytag
$ docker pull keppel-2.example.com/library/myimage:mytag # same image!
```

There's one more thing you need to know: In Keppel's data model, blobs are actually not sorted into repositories, but
one level higher, into accounts. This allows us to deduplicate blobs that are referenced by multiple repositories in the
same account. To model which repositories contain which blobs, Keppel's data model has an additional object, the **blob
mount**. A blob mount connects a blob stored within an account with a repo within that account where that blob can be
accessed by the user. All in all, our data model looks like this:

![data model](./data-model.png)

### Validation and garbage collection

TODO explain purpose, rhythm, success/error indicators

## Building and running Keppel

Build Keppel with `make`, install with `make install` or `docker build`. This is the same as in the general README,
since the server and client components are all combined in one single binary. For a complete Keppel deployment, you need
to run:

- as many instances of `keppel server api` as you want, and
- exactly one instance of `keppel server janitor`.

Both commands take configuration from environment variables, as listed below.

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

## Prometheus metrics

TODO
