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

- [GET /keppel/v1/accounts](#get-keppelv1accounts)
- [GET /keppel/v1/accounts/:name](#get-keppelv1accountsname)
- [PUT /keppel/v1/accounts/:name](#put-keppelv1accountsname)
- [GET /keppel/v1/auth](#get-keppelv1auth)
- [GET /keppel/v1/accounts/:name/repositories](#get-keppelv1accountsnamerepositories)
- [GET /keppel/v1/accounts/:name/repositories/:name/\_manifests](#get-keppelv1accountsnamerepositoriesnamemanifests)

## GET /keppel/v1/accounts

Lists all accounts that the user has access to.
On success, returns 200 and a JSON response body like this:

```json
{
  "accounts": [
    {
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
    },
    {
      "name": "secondaccount",
      "auth_tenant_id": "secondtenant",
      "rbac_policies": []
    }
  ]
}
```

The following fields may be returned:

| Field | Type | Explanation |
| ----- | ---- | ----------- |
| `accounts[].name` | string | Name of this account. |
| `accounts[].auth_tenant_id` | string | ID of auth tenant that regulates access to this account. |
| `accounts[].rbac_policies` | list of objects | Policies for rule-based access control (RBAC) to repositories in this account. RBAC policies are evaluated in addition to the permissions granted by the auth tenant. |
| `accounts[].rbac_policies[].match_repository` | string | The RBAC policy applies to all repositories in this account whose name matches this regex. The leading account name and slash is stripped from the repository name before matching. The notes on regexes below apply. |
| `accounts[].rbac_policies[].match_username` | string | The RBAC policy applies to all users whose name matches this regex. Refer to the [documentation of your auth driver](./drivers/) for the syntax of usernames. The notes on regexes below apply. |
| `accounts[].rbac_policies[].permissions` | list of strings | The permissions granted by the RBAC policy. Acceptable values include `pull`, `push`, `delete` and `anonymous_pull`. When `pull`, `push` or `delete` are included, `match_username` is not empty. When `anonymous_pull` is included, `match_username` is empty. |

The values of the `match_repository` and `match_username` fields are regular expressions, using the
[syntax defined by Go's stdlib regex parser](https://golang.org/pkg/regexp/syntax/). The anchors `^` and `$` are implied
at both ends of the regex, and need not be added explicitly.

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
- `account.auth_tenant_id` may not be changed for existing accounts.

On success, returns 200 and a JSON response body like from the corresponding GET endpoint.

## GET /keppel/v1/accounts/:name/repositories

Lists repositories within the account with the given name. On success, returns 200 and a JSON response body like this:

```json
{
  "repositories": [
    {
      "name": "foo0001",
      "manifest_count": 23,
      "tag_count": 2
    },
    ...,
    {
      "name": "foo1000",
      "manifest_count": 10,
      "tag_count": 0
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
| `truncated` | boolean | Indicates whether [marker-based pagination](#marker-based-pagination) must be used to retrieve the rest of the result. |

### Marker-based pagination

Because an account may contain a potentially large number of repos, the implementation may employ **marker-based
pagination**. If the `.truncated` field is present and true, only a partial result is shown. The next page of results
can be obtained by resending the GET request with the query parameter `marker` set to the name of the last repository in
the current result list, for instance

    GET /keppel/v1/accounts/$ACCOUNT_NAME/repositories?marker=foo1000

for the example response shown above. The last page of results will have `truncated` omitted or set to false.

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
      "tags": [
        {
          "name": "latest",
          "pushed_at": 1575468024
        }
      ]
    },
    {
      "digest": "sha256:5891b5b522d5df086d0ff0b110fbd9d21bb4fc7163af34d08286a2e846f6be03",
      "media_type": "application/vnd.oci.image.manifest.v1+json",
      "size_bytes": 2791084,
      "pushed_at": 1575467980
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
| `manifests[].tags` | array | All tags that currently resolve to this manifest. |
| `manifests[].tags[].name` | string | The name of this tag. |
| `manifests[].tags[].pushed_at` | string | When this tag was last updated in the registry. |
| `truncated` | boolean | Indicates whether [marker-based pagination](#marker-based-pagination) must be used to retrieve the rest of the result. |

## GET /keppel/v1/auth

This endpoint is reserved for the authentication workflow of the [OCI Distribution API][oci-dist].
