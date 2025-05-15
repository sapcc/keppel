<!--
SPDX-FileCopyrightText: 2025 SAP SE

SPDX-License-Identifier: Apache-2.0
-->

# Peer API specification

Besides the [OCI Distribution API][oci-dist] that is used e.g. by `docker pull/push`, and
[Keppel's main API](./api-spec.md), there is a REST API exclusively for internal use by peered Keppel instances.

[oci-dist]: https://github.com/opencontainers/distribution-spec

Authentication uses the "basic" scheme of the HTTP standard header "Authorization". The supplied credentials must be for
a replication user: The user name must be `replication@${HOSTNAME}`, where `${HOSTNAME}` is the hostname of a registered
peer.

This document uses the terminology defined in the [README.md](../README.md#terminology).

- [GET /peer/v1/delegatedpull/:hostname/v2/:repo/manifests/:reference](#get-peerv1delegatedpullhostnamev2repomanifestsreference)
- [POST /peer/v1/sync-replica/:account/:repository](#post-peerv1sync-replicaaccountrepository)

## GET /peer/v1/delegatedpull/:hostname/v2/:repo/manifests/:reference

Pulls the manifest identified by the URL path (everything after `delegatedpull`) using the
[OCI Distribution API][oci-dist], and returns the response to the caller.

This endpoint is used by peers who want to pull from an external registry, but have exhausted their rate limit with that
external registry.

## POST /peer/v1/sync-replica/:account/:repository

Keppels hosting a replica account periodically call this endpoint on the peer hosting the respective primary account, in
order to sync manifest/tag metadata for all contained repositories between the primary and replica account:

- The replica uses the response to delete all replicated images that have been deleted on the primary side; and to
  update tags to point to the same manifests as on the primary side (replicating missing tagged manifests as needed).
- The primary uses the request payload to update the `last_pulled` attribute on all manifests and tags, such that the
  `last_pulled` attribute on a manifest or tag reflects the time of the last pull on the primary or any of its replicas.

The request body must be a JSON document that includes the following fields:

| Field | Type | Explanation |
| ----- | ---- | ----------- |
| `manifests` | array of objects | A list of all manifests that currently exist in this repo on the replica side. |
| `manifests[].digest` | string | The canonical digest of this manifest. |
| `manifests[].last_pulled_at` | UNIX timestamp or null | When this manifest was last pulled from the registry (or null if it was never pulled). |
| `manifests[].tags` | array | All tags that currently resolve to this manifest. |
| `manifests[].tags[].name` | string | The name of this tag. |
| `manifests[].tags[].last_pulled_at` | UNIX timestamp or null | When this tag was last pulled from the registry (or null if it was never pulled). |

On success, returns 200 (OK) and a JSON response with the following fields:

| Field | Type | Explanation |
| ----- | ---- | ----------- |
| `manifests` | array of objects | A list of all manifests that currently exist in this repo on the primary side. |
| `manifests[].digest` | string | The canonical digest of this manifest. |
| `manifests[].tags` | array | All tags that currently resolve to this manifest. |
| `manifests[].tags[].name` | string | The name of this tag. |
