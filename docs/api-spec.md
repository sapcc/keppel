# Keppel API specification

Besides the [OCI Distribution API][oci-dist] that is used e.g. by `docker pull/push`, Keppel provides its own REST API
for managing Keppel accounts.

[oci-dist]: https://github.com/opencontainers/distribution-spec

- Error responses always have `Content-Type: text/plain`.
- Account names must conform to the regex `^[a-z0-9-]{1,48}$`, that is, they may not be longer than 48 chars and may
  only contain lowercase letters, digits and dashes.
- When the auth driver is `keystone`, Keppel's service URL can be found in the Keystone service catalog under the
  service type `keppel`.
- When the auth driver is `keystone`, all endpoints require a Keystone token to be present in the `X-Auth-Token` header.
  Only Keystone v3 is supported.

This document uses the terminology defined in the [README.md](../README.md#terminology).

- [GET /keppel/v1](#get-keppelv1)
- [GET /keppel/v1/accounts](#get-keppelv1accounts)
- [GET /keppel/v1/accounts/:name](#get-keppelv1accountsname)
- [PUT /keppel/v1/accounts/:name](#put-keppelv1accountsname)
- [DELETE /keppel/v1/accounts/:name](#delete-keppelv1accountsname)
- [POST /keppel/v1/accounts/:name/sublease](#post-keppelv1accountsnamesublease)
- [GET /keppel/v1/accounts/:name/repositories](#get-keppelv1accountsnamerepositories)
- [DELETE /keppel/v1/accounts/:name/repositories/:name](#delete-keppelv1accountsnamerepositoriesname)
- [GET /keppel/v1/accounts/:name/repositories/:name/\_manifests](#get-keppelv1accountsnamerepositoriesnamemanifests)
- [DELETE /keppel/v1/accounts/:name/repositories/:name/\_manifests/:digest](#delete-keppelv1accountsnamerepositoriesnamemanifestsdigest)
- [GET /keppel/v1/auth](#get-keppelv1auth)
- [POST /keppel/v1/auth/peering](#post-keppelv1authpeering)
- [GET /keppel/v1/peers](#get-keppelv1peers)
- [GET /keppel/v1/quotas/:auth\_tenant\_id](#get-keppelv1quotasauthtenantid)
- [PUT /keppel/v1/quotas/:auth\_tenant\_id](#put-keppelv1quotasauthtenantid)

## GET /keppel/v1

Shows information about this Keppel API. Authentication is not required.
On success, returns 200 and a JSON response like this:

```json
{
  "auth_driver": "keystone"
}
```

The following fields may be returned:

| Field | Type | Explanation |
| ----- | ---- | ----------- |
| `auth_driver` | string | The authentication driver used by this Keppel instance. This is important to know for clients using the Keppel API to decide how to obtain an authorization for the API. |

## GET /keppel/v1/accounts

Lists all accounts that the user has access to.
On success, returns 200 and a JSON response body like this:

```json
{
  "accounts": [
    {
      "name": "firstaccount",
      "auth_tenant_id": "firsttenant",
      "metadata": {},
      "rbac_policies": [
        {
          "match_repository": "library/.*",
          "permissions": [ "anonymous_pull" ]
        },
        {
          "match_repository": "library/alpine",
          "match_username": "exampleuser@secondtenant",
          "permissions": [ "pull", "push" ]
        }
      ]
    },
    {
      "name": "secondaccount",
      "auth_tenant_id": "secondtenant",
      "metadata": {
        "priority": "just an example"
      },
      "rbac_policies": [],
      "replication": {
        "strategy": "on_first_use",
        "upstream": "keppel.example.com"
      },
      "validation": {
        "required_labels": [ "maintainers", "source_repo" ]
      }
    }
  ]
}
```

The following fields may be returned:

| Field | Type | Explanation |
| ----- | ---- | ----------- |
| `accounts[].name` | string | Name of this account. |
| `accounts[].auth_tenant_id` | string | ID of auth tenant that regulates access to this account. |
| `accounts[].in_maintenance` | bool | Whether this account is in maintenance mode. [See below](#maintenance-mode) for details. |
| `accounts[].metadata` | object of strings | Free-form metadata maintained by the user. The contents of this field are not interpreted by Keppel, but may trigger special behavior in applications using this API. |
| `accounts[].rbac_policies` | list of objects | Policies for rule-based access control (RBAC) to repositories in this account. RBAC policies are evaluated in addition to the permissions granted by the auth tenant. |
| `accounts[].rbac_policies[].match_repository` | string | The RBAC policy applies to all repositories in this account whose name matches this regex. The leading account name and slash is stripped from the repository name before matching. The notes on regexes below apply. |
| `accounts[].rbac_policies[].match_username` | string | The RBAC policy applies to all users whose name matches this regex. Refer to the [documentation of your auth driver](./drivers/) for the syntax of usernames. The notes on regexes below apply. |
| `accounts[].rbac_policies[].permissions` | list of strings | The permissions granted by the RBAC policy. Acceptable values include `pull`, `push`, `delete` and `anonymous_pull`. When `pull`, `push` or `delete` are included, `match_username` is not empty. When `anonymous_pull` is included, `match_username` is empty. |
| `accounts[].replication` | object or omitted | Replication configuration for this account, if any. [See below](#replication-strategies) for details. |
| `accounts[].validation` | object or omitted | Validation rules for this account. When included, pushing blobs and manifests not satisfying these validation rules may be rejected. |
| `accounts[].validation.required_labels` | list of strings | When non-empty, image manifests must include all these labels. (Labels can be set on an image using the Dockerfile's `LABEL` command.) |

The values of the `match_repository` and `match_username` fields are regular expressions, using the
[syntax defined by Go's stdlib regex parser](https://golang.org/pkg/regexp/syntax/). The anchors `^` and `$` are implied
at both ends of the regex, and need not be added explicitly.

### Replication strategies

This section describes the different possible configurations for `accounts[].replication`.

#### Strategy: `on_first_use`

When an authorized user pulls a manifest which does not exist in this registry yet, the same manifest will be queried in
the respective primary account. The primary account must have the same name as this account and must be located in one
of the upstream registries configured by the Keppel operator. If this query returns a result, the manifest and all blobs
referenced by it will be pulled from the upstream registry into the local one. Note that:

- Manifests and blobs can not be deleted directly, but will be cleaned up once they disappear from the upstream registry.
- Accounts with this replication strategy will not allow direct push access. Images can only be added to these accounts
  through replication.

The following fields are shown on accounts configured with this strategy:

| Field | Type | Explanation |
| ----- | ---- | ----------- |
| `accounts[].replication.strategy` | string | The string `on_first_use`. |
| `accounts[].replication.upstream` | string | The hostname of the upstream registry. Must be one of the peers configured for this registry by its operator. |

### Maintenance mode

When `accounts[].in_maintenance` is true, the following differences in behavior apply to this account:

- For primary accounts (i.e. accounts that are not replicas), no new blobs or manifests may be pushed. Only pulling and
  deleting are allowed.
- For replica accounts, no new blobs or manifests will be replicated. Pulling is still allowed, but it becomes possible
  to delete blobs and manifests.

Maintenance mode is a significant part of the account deletion workflow: Sending a DELETE request on an account is only
allowed while the account is in maintenance mode, and the caller must have deleted all manifests from the account before
attempting to DELETE it.

## GET /keppel/v1/accounts/:name

Shows information about an individual account.
Returns 404 if no account with the given name exists, or if the user does not have access to it.
Otherwise returns 200 and a JSON response body like this:

```json
{
  "account": {
    "name": "firstaccount",
    "auth_tenant_id": "firsttenant",
    "rbac_policies": [
      {
        "match_repository": "library/.*",
        "permissions": [ "anonymous_pull" ]
      },
      {
        "match_repository": "library/alpine",
        "match_username": "exampleuser@secondtenant",
        "permissions": [ "pull", "push" ]
      }
    ]
  }
}
```

The `.account` object's contents are equivalent to the corresponding entry in `.accounts[]` as returned by
`GET /keppel/v1/accounts`.

## PUT /keppel/v1/accounts/:name

Creates or updates the account with the given name. The request body must be a JSON document following the same schema
as the response from the corresponding GET endpoint, except that:

- `account.name` may not be present (the name is already given in the URL), and
- `account.auth_tenant_id` and `account.replication` may not be changed for existing accounts.

On success, returns 200 and a JSON response body like from the corresponding GET endpoint.

When creating a replica account, it may be necessary to supply a **sublease token** in the `X-Keppel-Sublease-Token`
header. The sublease token must have been issued by the Keppel instance hosting the corresponding primary account, via
the [POST /keppel/v1/accounts/:name/sublease](#post-keppelv1accountsnamesublease) endpoint. If a sublease token is
required, but the correct one was not supplied, 403 (Forbidden) will be returned.

## DELETE /keppel/v1/accounts/:name

Deletes the given account. On success, returns 204 (No Content).

Accounts can only be deleted after all manifests and blobs have been deleted from the account and its backing storage.
If these requirements are not met, 409 (Conflict) will be returned along with a JSON response body like this:

```json
{
  "remaining_manifests": {
    "count": 23,
    "next": [
      {
        "repository": "library/alpine",
        "digest": "sha256:54c5b3dd459d5ef778bb2fa1e23a5fb0e1b62ae66970bcb436e8f81a1a1a8e41",
      },
      {
        "repository": "library/alpine",
        "digest": "sha256:721fe5d2ca0c3f66b596df049b23619d14b9912f88344dea3b5335ad007f11a3",
      },
      ...
    ]
  },
  "remaining_blobs": {
    "count": 42
  },
  "error": "cannot delete this primary account because replicas are still attached to it"
}
```

The following fields may be returned:

| Field | Type | Explanation |
| ----- | ---- | ----------- |
| `remaining_manifests` | object | If this field is included, there are still manifests left in the account that the client has to delete before the account deletion can proceed. A list of manifests can be found in the `remaining_manifests.next` field. |
| `remaining_manifests.count` | integer | The total number of manifests that are still stored in this account. This value can be used to present a progress indication to the user. |
| `remaining_manifests.next` | array of objects | A list of manifests that are still stored in this account. To proceed with the account deletion, the client shall delete these manifests first, then restart the DELETE request on the account. The length of this list is capped to prevent excessive response sizes, so the number of entries in this list may be less than `remaining_manifests.count`. In this case, the next DELETE request on the account will show the next set of manifests that needs to be deleted. |
| `remaining_manifests.next[].repository` | string | The repository (within this account) where this manifest is stored. |
| `remaining_manifests.next[].digest` | string | The digest of this manifest. |
| `remaining_blobs` | object | If this field is included, there are still blobs left in the account. There is no API for deleting blobs, but when this field is included, it indicates that Keppel has scheduled a garbage collection to cleanup these blobs. The client shall restart the DELETE request on the account after some time (e.g. 15 seconds) to observe whether garbage collection is finished. |
| `remaining_blobs.count` | integer | The total number of blobs that are still stored in this account. This value can be used to present a progress indication to the user. |
| `error` | string | If this field is included, the account deletion was attempted, but failed. This field contains a human-readable error message describing the problem. |

Unlike in the artificial example above, usually only one of the top-level fields will be present at a time. Each
top-level field represents a distinct phase of the account deletion process: First all manifests need to be deleted (so
only `remaining_manifests` would be shown), then all blobs need to be garbage-collected (so only `remaining_blobs` would
be shown), then the account itself can be deleted (so only `error` would be shown if necessary).

## POST /keppel/v1/accounts/:name/sublease

Issues a **sublease token** for the given account. A sublease token can be redeemed exactly once to create a replica
account connected to this account in another Keppel instance. On success, returns 200 and a JSON response body like
this:

```
{
  "sublease_token": "oingoojei6aejab0Too4"
}
```

The sublease token mechanism is optional. If the `.sublease_token` field comes back empty, it means that no sublease
token needs to be presented when creating a replica of this primary account.

Sublease tokens can only be issued for primary accounts. If the account in question is a replica account, 400 (Bad
Request) is returned.

## GET /keppel/v1/accounts/:name/repositories

Lists repositories within the account with the given name. On success, returns 200 and a JSON response body like this:

```json
{
  "repositories": [
    {
      "name": "foo0001",
      "manifest_count": 23,
      "tag_count": 2,
      "size_bytes": 103876423,
      "pushed_at": 1575467980
    },
    ...,
    {
      "name": "foo1000",
      "manifest_count": 10,
      "tag_count": 0,
      "size_bytes": 29862877,
      "pushed_at": 1575468024
    }
  ],
  "truncated": true
}
```

The following fields may be returned:

| Field | Type | Explanation |
| ----- | ---- | ----------- |
| `repositories[].name` | string | Name of this repository. |
| `repositories[].manifest_count` | integer | Number of manifests that are stored in this repository. |
| `repositories[].tag_count` | integer | Number of tags that exist in this repository. |
| `repositories[].size_bytes` | integer | Sum of all manifests in this repository and layers referenced therein. This may be higher than the actual storage usage because layer sharing is not considered in this sum. |
| `repositories[].pushed_at` | UNIX timestamp | When a manifest was pushed into the registry most recently. |
| `truncated` | boolean | Indicates whether [marker-based pagination](#marker-based-pagination) must be used to retrieve the rest of the result. |

### Marker-based pagination

Because an account may contain a potentially large number of repos, the implementation may employ **marker-based
pagination**. If the `.truncated` field is present and true, only a partial result is shown. The next page of results
can be obtained by resending the GET request with the query parameter `marker` set to the name of the last repository in
the current result list, for instance

    GET /keppel/v1/accounts/$ACCOUNT_NAME/repositories?marker=foo1000

for the example response shown above. The last page of results will have `truncated` omitted or set to false.

## DELETE /keppel/v1/accounts/:name/repositories/:name

Deletes the specified repository and all manifests in it. Returns 204 (No Content) on success.

Returns 409 (Conflict) if the repository still contains manifests. All manifests in the repository must be deleted
before the repository can be deleted.

## GET /keppel/v1/accounts/:name/repositories/:name/\_manifests

*Note the underscore in the last path element. Since repository names may contain slashes themselves, the underscore is necessary to distinguish the reserved word `_manifests` from a path component in the repository name.*

Lists manifests (and, indirectly, tags) in the given repository in the given account. On success, returns 200 and a JSON
response body like this:

```json
{
  "manifests": [
    {
      "digest": "sha256:622cb3371c1a08096eaac564fb59acccda1fcdbe13a9dd10b486e6463c8c2525",
      "media_type": "application/vnd.docker.distribution.manifest.v2+json",
      "size_bytes": 10518718,
      "pushed_at": 1575468024,
      "last_pulled_at": 1575554424,
      "tags": [
        {
          "name": "latest",
          "pushed_at": 1575468024,
          "last_pulled_at": 1575550824
        }
      ]
    },
    {
      "digest": "sha256:5891b5b522d5df086d0ff0b110fbd9d21bb4fc7163af34d08286a2e846f6be03",
      "media_type": "application/vnd.oci.image.manifest.v1+json",
      "size_bytes": 2791084,
      "pushed_at": 1575467980,
      "last_pulled_at": null
    }
  ]
}
```

The following fields may be returned:

| Field | Type | Explanation |
| ----- | ---- | ----------- |
| `manifests[].digest` | string | The canonical digest of this manifest. |
| `manifests[].media_type` | string | The MIME type of the canonical form of this manifest. |
| `manifests[].size_bytes` | integer | Total size of this manifest and all layers referenced by it in the backing storage. |
| `manifests[].pushed_at` | UNIX timestamp | When this manifest was pushed into the registry. |
| `manifests[].last_pulled_at` | UNIX timestamp or null | When this manifest was last pulled from the registry (or null if it was never pulled). |
| `manifests[].tags` | array | All tags that currently resolve to this manifest. |
| `manifests[].tags[].name` | string | The name of this tag. |
| `manifests[].tags[].pushed_at` | string | When this tag was last updated in the registry. |
| `manifests[].tags[].last_pulled_at` | UNIX timestamp or null | When this manifest was last pulled from the registry using this tag name (or null if it was never pulled from this tag). |
| `truncated` | boolean | Indicates whether [marker-based pagination](#marker-based-pagination) must be used to retrieve the rest of the result. |

## DELETE /keppel/v1/accounts/:name/repositories/:name/_manifests/:digest

Deletes the specified manifest and all tags pointing to it. Returns 204 (No Content) on success.
The digest that identifies the manifest must be that manifest's canonical digest, otherwise 404 is returned.

## GET /keppel/v1/auth

This endpoint is reserved for the authentication workflow of the [OCI Distribution API][oci-dist].

## POST /keppel/v1/auth/peering

*This endpoint is only used for internal communication between Keppel registries and cannot be used by outside users.*

An upstream registry can send this request to a downstream registry to issue a username and password for it. These
credentials allow pull access to the entire upstream registry via the usual Registry V2 auth process. The downstream
registry is expected to store these credentials for use in content replication. On success, returns 204 (No Content).
The request body must be a JSON document like this:

```json
{
  "peer": "keppel.example.org",
  "username": "replication@keppel.example.com",
  "password": "EaxiYoo8ju7Ohvukooch",
}
```

The following fields must be included:

| Field | Type | Explanation |
| ----- | ---- | ----------- |
| `peer` | string | The hostname of the registry for which those credentials are valid. |
| `username`<br />`password` | string | Credentials granting global pull access to that registry. |

## GET /keppel/v1/peers

Shows information about the peers known to this registry. This information is vital for users who want to create a
replica account, since they have to know which peers are eligible.
On success, returns 200 and a JSON response body like this:

```json
{
  "peers": [
    { "hostname": "keppel.example.org" },
    { "hostname": "keppel.example.com" }
  ]
}

The following fields may be returned:

| Field | Type | Explanation |
| ----- | ---- | ----------- |
| `peers` | list of objects | List of peers known to this registry. |
| `peers[].hostname` | string | Hostname of this peer. |

## GET /keppel/v1/quotas/:auth\_tenant\_id

Shows information about resource usage and limits for the given auth tenant.
On success, returns 200 and a JSON response body like this:

```json
{
  "manifests": {
    "quota": 1000,
    "usage": 42
  }
}
```

The following fields may be returned:

| Field | Type | Explanation |
| ----- | ---- | ----------- |
| `manifests.quota` | integer | Maximum number of manifests that can be pushed to repositories in accounts belonging to this auth tenant. |
| `manifests.usage` | integer | How many manifests exist in repositories in accounts belonging to this auth tenant. |

## PUT /keppel/v1/quotas/:auth\_tenant\_id

Updates the configuration for this auth tenant. The request body must be a JSON document following the same schema
as the response from the corresponding GET endpoint, except that the `.usage` fields may not be present.

On success, returns 200 and a JSON response body like from the corresponding GET endpoint.
