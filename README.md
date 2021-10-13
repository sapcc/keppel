# Keppel, a multi-tenant container image registry

[![CI](https://github.com/sapcc/keppel/actions/workflows/ci.yaml/badge.svg)](https://github.com/sapcc/keppel/actions/workflows/ci.yaml)
[![Coverage Status](https://coveralls.io/repos/github/sapcc/keppel/badge.svg?branch=master)](https://coveralls.io/github/sapcc/keppel?branch=master)
[![Go Report Card](https://goreportcard.com/badge/github.com/sapcc/keppel)](https://goreportcard.com/report/github.com/sapcc/keppel)

In this document:

- [Overview](#overview)
- [History](#history)
- [Terminology](#terminology)
- [Usage](#usage)

In other documents:

- [Operator guide](./docs/operator-guide.md)
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
importantly, by putting the images of all users in the same storage, quota and usage tracking gets unnecessarily
complicated. Keppel instead uses multi-tenant-aware storage drivers so that each customer gets their own separate
storage space. Storage quota and usage can therefore be tracked by the backing storage. This orchestration is completely
transparent to the user: A unified API endpoint is provided that multiplexes requests to their respective registry
instances.

Keppel fully implements the [OCI Distribution API][dist-api], the standard API for container image registries. It also
provides a [custom API](docs/api-spec.md) to control the multitenancy added by Keppel and to expose additional metadata
about repositories, manifests and tags. Other unique features of Keppel include:

- **cross-regional federation**: Keppel instances running in different geographical regions or different network
  segments can share their account name space and provide seamless replication between accounts of the same name on
  different instances.
- **online garbage collection**: Unlike Docker Registry, Keppel can perform all garbage collection tasks without
  scheduled downtime or any other form of operator intervention.
- **vulnerability scanning**: Keppel can use [Clair](https://quay.github.io/clair/) to perform vulnerability scans on
  its contents.

[dist-api]: https://github.com/opencontainers/distribution-spec

## History

In its first year, leading up to 1.0, Keppel used to orchestrate a fleet of docker-registry instances to provide the
OCI Distribution API. We hoped to save ourselves the work of reimplementing the full Distribution API, since Keppel
would only have to reverse-proxy customer requests into their respective docker-registry. Over time, as Keppel's feature
set grew, more and more API requests were intercepted to track metadata, validate requests and so forth. We ended up
scrapping the docker-registry fleet entirely to make Keppel much easier to deploy and manage. It's now conceptually more
similar to Quay than to Harbor, but retains its unique multi-tenant storage architecture.

## Terminology

Within Keppel, an **account** is a namespace that gets its own backing storage. The account name is the first path
element in the name of a repository. For example, consider the image `keppel.example.com/foo/bar:latest`. It's
repository name is `foo/bar`, which means it's located in the Keppel account `foo`.

Access is controlled by the account's **auth tenant** or just **tenant**. Note that tenants and accounts are separate
concepts: An account corresponds to one backing storage, and a tenant corresponds to an authentication/authorization
scope. Each account is linked to exactly one auth tenant, but there can be multiple accounts linked to the same auth
tenant.

Inside an account, you will find **repositories** containing **blobs**, **manifests** and **tags**. The meaning of those
terms is the same as for any other Docker registry or OCI image registry, and is defined in the [OCI Distribution API
specification][dist-api].

## Usage

Build with `make`, install with `make install` or `docker build`. The resulting `keppel` binary contains client commands
as well as server commands.

- For how to use the client commands, run `keppel --help`.
- For how to deploy the server components, please refer to the [operator guide](./docs/operator-guide.md).
