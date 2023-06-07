# Keppel API specification

Besides the [OCI Distribution API][oci-dist] that is used e.g. by `docker pull/push`, Keppel provides its own REST API
for managing Keppel accounts.

[oci-dist]: https://github.com/opencontainers/distribution-spec

## Concepts

This document uses the terminology defined in the [README.md](../README.md#terminology).

- Error responses always have `Content-Type: text/plain`.
- Account names must conform to the regex `^[a-z0-9-]{1,48}$`, that is, they may not be longer than 48 chars and may
  only contain lowercase letters, digits and dashes.

### Authentication

The authentication method for this API depends on which auth driver is used by the Keppel instance in question:

- When the auth driver is `keystone`, all endpoints require a Keystone token to be present in the `X-Auth-Token` header.
  Only Keystone v3 is supported.
- When the auth driver is `keystone`, Keppel's service URL can be found in the Keystone service catalog under the
  service type `keppel`.

The OCI Distribution API usually uses OAuth-like bearer tokens, but in Keppel, it can also be made to use the same
authentication method as the API specified in this document. To do so, add the request header `Authorization: keppel`
and the same request headers as on this API to a request for the OCI Distribution API. Conversely, the Keppel API can be
used with the bearer token auth scheme prescribed by the OCI Distribution API. The Keppel API will render the respective
auth challenges when API requests are made without any form of authentication.

### Domain remapping

By default, the OCI Distribution API is structured such that the account name is prepended to all repository names. For
instance, a Docker image reference like `registry.example.com/foo/bar:latest` refers to the repository called `bar`
within the account called `foo`.

However, if the Keppel instance has been set up thusly, the OCI Distribution API is also offered for each account
individually. This is called a **domain-remapped API** in Keppel, because it is inspired by a similar concept of the
same name in OpenStack Swift. The domain-remapped analog of the example image reference above would be
`foo.registry.example.com/bar:latest`, i.e. the account name has become part of the domain name.

This is particularly useful if you have an external replica account of Docker Hub, since dockerd only accepts plain
hostnames in its `registry-mirrors` option. Therefore something like `registry.example.com/dockerhubmirror` would not
work, but `dockerhubmirror.registry.example.com` does work.

The domain-remapped domain names only offer the OCI Distribution API and the `GET /keppel/v1/auth` endpoint. The Keppel
API itself can only be accessed through the respective Keppel instance's main domain name.

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
      ],
      "gc_policies": [
        {
          "match_repository": ".*",
          "time_constraint": {
            "on": "last_pulled_at",
            "newer_than": {
              "value": 7,
              "unit": "d"
            }
          },
          "action": "protect"
        },
        {
          "match_repository": ".*/webapp",
          "except_repository": "example/webapp",
          "match_untagged": true,
          "action": "delete"
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
| `accounts[].gc_policies` | list of objects or omitted | Policies for garbage collection (automated deletion of images) for repositories in this account. GC policies apply in addition to the regular garbage collection runs performed by Keppel that clean up unreferenced objects of all kinds. GC policies are ordered by priority: Earlier policies take precedence over later policies. |
| `accounts[].gc_policies[].match_repository` | string | Required. The GC policy applies to all repositories in this account whose name matches this regex. The leading account name and slash is stripped from the repository name before matching. The notes on regexes below apply. |
| `accounts[].gc_policies[].except_repository` | string or omitted | If given, matching repositories will be excluded from this GC policy, even if they match the `match_repository` regex. The syntax and mechanics of matching are otherwise identical to `match_repository` above. |
| `accounts[].gc_policies[].match_tag` | string or omitted | The GC policy applies to all images in matching repositories that have a tag whose name matches this regex. The notes on regexes below apply. |
| `accounts[].gc_policies[].except_tag` | string or omitted | If given, images with matching tag names will be excluded from this GC policy, even if they match the `match_tag` regex. The syntax and mechanics of matching are otherwise identical to `match_tag` above. |
| `accounts[].gc_policies[].only_untagged` | bool or omitted | If true, the GC policy applies only to those images that do not have any tags. |
| `accounts[].gc_policies[].time_constraint` | object | If given, the GC policy only applies to images matching the time constraint specified herein. |
| `accounts[].gc_policies[].time_constraint.on` | string | The timestamp attribute on each image on which this time constraint operates. Either `pushed_at` or `last_pulled_at`. For the purposes of GC policy evaluation, if an image has never been pulled, its `last_pulled_at` timestamp will be set to the UNIX epoch (1970-01-01 00:00:00 UTC). |
| `accounts[].gc_policies[].time_constraint.oldest`<br>`accounts[].gc_policies[].time_constraint.newest` | integer or omitted | If set, the GC policy only applies to at most that many images within each repository, specifically to those that are oldest/newest ones when ordered by the timestamp attribute specified in the `time_constraint.on` key. These constraints are forbidden for policies with action "delete" to ensure that GC runs are idempotent. |
| `accounts[].gc_policies[].time_constraint.older_than`<br>`accounts[].gc_policies[].time_constraint.newer_than` | duration or omitted | If set, the GC policy only applies to at most images whose timestamp (as selected by the `time_constraint.on` key) is older/newer than the given age. Durations are given as a JSON object with the keys `value` (integer) and `unit` (string), e.g. `{"value": 4, "unit": "d"}` for 4 days. The units `s` (second), `m` (minute), `h` (hour), `d` (day), `w` (7 days) and `y` (365 days) are understood. |
| `accounts[].gc_policies[].action` | string | One of: `delete` (to delete matching images) or `protect` (to not delete matching images, even if another policy with a lower priority would want to). |
| `accounts[].in_maintenance` | bool | Whether this account is in maintenance mode. [See below](#maintenance-mode) for details. |
| `accounts[].metadata` | object of strings | Free-form metadata maintained by the user. The contents of this field are not interpreted by Keppel, but may trigger special behavior in applications using this API. |
| `accounts[].rbac_policies` | list of objects | Policies for rule-based access control (RBAC) to repositories in this account. RBAC policies are evaluated in addition to the permissions granted by the auth tenant. |
| `accounts[].rbac_policies[].match_cidr` | string | The RBAC policy applies to requests which originate from an IP address that matches the CIDR. |
| `accounts[].rbac_policies[].match_repository` | string | The RBAC policy applies to all repositories in this account whose name matches this regex. The leading account name and slash is stripped from the repository name before matching. The notes on regexes below apply. |
| `accounts[].rbac_policies[].match_username` | string | The RBAC policy applies to all users whose name matches this regex. Refer to the [documentation of your auth driver](./drivers/) for the syntax of usernames. The notes on regexes below apply. |
| `accounts[].rbac_policies[].permissions` | list of strings | The permissions granted by the RBAC policy. Acceptable values include `pull`, `push`, `delete`, `anonymous_pull` and `anonymous_first_pull`. When `pull`, `push` or `delete` are included, `match_username` is not empty. When `anonymous_pull` or `anonymous_first_pull` is included, `match_username` is empty. `anonymous_first_pull` is only relevant for external replica accounts and allows unauthenticated users to replicate tags. It should always be combined with an appropriate `match_*` rule. |
| `accounts[].replication` | object or omitted | Replication configuration for this account, if any. [See below](#replication-strategies) for details. |
| `accounts[].platform_filter` | list of objects or omitted | Only allowed for replica accounts. If not empty, when replicating an image list manifest (i.e. a multi-architecture image), only submanifests matching one of the given platforms will be replicated. Each entry must have the same format as the `manifests[].platform` field in the [OCI Image Index Specification](https://github.com/opencontainers/image-spec/blob/master/image-index.md). |
| `accounts[].validation` | object or omitted | Validation rules for this account. When included, pushing blobs and manifests not satisfying these validation rules may be rejected. |
| `accounts[].validation.required_labels` | list of strings | When non-empty, image manifests must include all these labels. (Labels can be set on an image using the Dockerfile's `LABEL` command.) |

The values of fields with names like `match_...` and `except_...` are regular expressions, using the
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

#### Strategy: `from_external_on_first_use`

This behaves mostly identically to `on_first_use`, but can pull from any registry implementing the OCI Distribution
Spec, including public registries like Docker Hub or GCR. Since there is no way for Keppel to negotiate service users
with these registries, the user must supply pull credentials (or else anonymous access is used for pulling, meaning that
only publicly accessible images can be replicated). Note that:

- Accounts with this strategy can be replicated from by other peer registries. For instance, an account with
  `on_first_use` in a peer registry can pull from an account with `from_external_on_first_use` in this registry.
- Pulling an image from this account requires a non-anonymous token when the image is pulled for the first time. This is
  a safety measure to prevent external users from leeching off some other team who configured their account to pull from
  a popular public registry and enabled anonymous pulling. In this scenario, only the team members of the team hosting
  the account can decide to host images in the account by explicitly pulling them for the first time.

The following fields are shown on accounts configured with this strategy:

| Field | Type | Explanation |
| ----- | ---- | ----------- |
| `accounts[].replication.strategy` | string | The string `from_external_on_first_use`. |
| `accounts[].replication.upstream.url` | string | The URL from which images are pulled. This may refer to either a public registry's domain name (e.g. `registry-1.docker.io` for Docker Hub) or a subpath below its domain name (e.g. `gcr.io/google_containers`). |
| `accounts[].replication.upstream.username`<br>`accounts[].replication.upstream.password` | string, optional | The credentials that this registry logs in with to replicate images from upstream. If not given, anonymous login is used. |

Note that the `accounts[].replication.upstream.password` field is omitted from GET responses for security reasons.

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

```json
{
  "sublease_token": "oingoojei6aejab0Too4"
}
```

The sublease token mechanism is optional. If the `.sublease_token` field comes back empty, it means that no sublease
token needs to be presented when creating a replica of this primary account.

Sublease tokens can only be issued for primary accounts. If the account in question is a replica account, 400 (Bad
Request) is returned.

## GET /keppel/v1/accounts/:name/security\_scan\_policies

If this Keppel is configured to use its bundled [Trivy security scanner](https://aquasecurity.github.io/trivy), this
endpoint returns a list of user-defined policies for how Keppel processes reports generated by Trivy for images in the
respective account. On success, returns 200 and a JSON response body like this:

```json
{
  "policies": [
    {
      "match_repository": ".*",
      "match_vulnerability_id": ".*",
      "except_fix_released": true,
      "action": {
        "ignore": true,
        "assessment": "risk accepted: vulnerabilities without an available fix are not actionable"
      }
    },
    {
      "managed_by_user": "mytechnicaluser@mydomain",
      "match_repository": "my-python-app|my-other-image",
      "match_vulnerability_id": "CVE-2022-40897",
      "action": {
        "severity": "Low",
        "assessment": "adjusted severity: python-setuptools cannot be invoked through user requests"
      }
    }
  ]
}
```

The following fields may be returned:

| Field | Type | Explanation |
| ----- | ---- | ----------- |
| `policies` | array of objects | Each policy matches a certain set of vulnerabilities on images in a certain set of repositories. Policies can be used to adjust the severity of vulnerabilities or ignore them altogether in certain situations. |
| `policies[].managed_by_user` | string or omitted | If shown, the policy can only be edited by the user account with the given name. |
| `policies[].match_repository` | string | The policy applies to all repositories in this account whose name matches this regex. The leading account name and slash is stripped from the repository name before matching. The notes on regexes below apply. |
| `policies[].except_repository` | string or omitted | If given, matching repositories will be excluded from this policy, even if they match the `match_repository` regex. The syntax and mechanics of matching are otherwise identical to `match_repository` above. |
| `policies[].match_vulnerability_id` | string | The policy applies to all vulnerabilities whose ID (as shown in the `VulnerabilityID` field of Trivy's JSON report format) matches this regex. In most cases, the vulnerability ID is a CVE number like `CVE-2014-0160`. The notes on regexes below apply. |
| `policies[].except_vulnerability_id` | string or omitted | If given, matching vulnerabilities will be excluded from this policy, even if they match the `match_vulnerability_id` regex. The syntax and mechanics of matching are otherwise identical to `match_vulnerability_id` above. |
| `policies[].except_fix_released` | bool or omitted | If true, the policy applies only to those vulnerabilities for which no fixed version has been released to the distribution repository (that is, the `FixedVersion` field is missing in Trivy's JSON report format). |
| `policies[].action` | object | The effect that this policy will have on matching vulnerabilities reported for images in matching repositories.  |
| `policies[].action.assessment` | string | A human-readable description of the reasoning behind this policy (maximum 1 KiB). |
| `policies[].action.ignore` | bool or omitted | If true, matching vulnerabilities will be ignored when computing the aggregated vulnerability status of the respective image manifest. This is the same effect as if `action.severity` was set to `Clean`, but the intent is clearer. |
| `policies[].action.severity` | string or omitted | If present, matching vulnerabilities will be treated as having the given severity when computing the aggregated vulnerability status of the respective image manifest. Acceptable values include `Low`, `Medium`, `High` and `Critical`. |

The values of string fields with names like `match_...` and `except_...` are regular expressions, using the
[syntax defined by Go's stdlib regex parser](https://golang.org/pkg/regexp/syntax/). The anchors `^` and `$` are implied
at both ends of the regex, and need not be added explicitly.

If multiple policies match the same vulnerability, only the action of the first policy (in order of appearance in the
`policies` list) will take effect.

## PUT /keppel/v1/accounts/:name/security\_scan\_policies

If this Keppel is configured to use its bundled [Trivy security scanner](https://aquasecurity.github.io/trivy), this
endpoint replaces the list of user-defined policies for how Keppel processes reports generated by Trivy for images in
the respective account. The request body must be a JSON document following the same schema as the response from the
corresponding GET endpoint, except that:

- `policies[].managed_by_user` may contain the special value `$REQUESTER` to indicate the requesting user. This value
  will be replaced with the actual name of the requesting user.

On success, returns 200 and a JSON response body like from the corresponding GET endpoint.

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
| `repositories[].size_bytes` | integer | Size sum for all blobs in this repository. This correctly deduplicates layers shared between multiple manifests, but does not count the manifest's own size (only the blobs referenced therein). |
| `repositories[].pushed_at` | UNIX timestamp | When a manifest was pushed into the registry most recently. |
| `truncated` | boolean | Indicates whether [marker-based pagination](#marker-based-pagination) must be used to retrieve the rest of the result. |

### Marker-based pagination

Because an account may contain a potentially large number of repos, the implementation may employ **marker-based
pagination**. If the `.truncated` field is present and true, only a partial result is shown. The next page of results
can be obtained by resending the GET request with the query parameter `marker` set to the name of the last repository in
the current result list, for instance

```
GET /keppel/v1/accounts/$ACCOUNT_NAME/repositories?marker=foo1000
```

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
      ],
      "labels": {
        "maintainers": "Stefan"
      },
      "gc_status": {
        "protected_by_recent_upload": true
      },
      "vulnerability_status": "Clean",
      "trivy_vulnerability_status": "Low"
    },
    {
      "digest": "sha256:5891b5b522d5df086d0ff0b110fbd9d21bb4fc7163af34d08286a2e846f6be03",
      "media_type": "application/vnd.oci.image.manifest.v1+json",
      "size_bytes": 2791084,
      "pushed_at": 1575467980,
      "last_pulled_at": null,
      "vulnerability_status": "High",
      "trivy_vulnerability_status": "Critical"
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
| `manifests[].labels` | object of strings | Free-form labels maintained by the user (labels are set on an image using the Dockerfile's `LABEL` command). The contents of this field may be interpreted by Keppel and might trigger special behavior, e.g. when `validation.required_labels` is configured for an account. |
| `manifests[].gc_status` | object or omitted | Omitted if policy-guided garbage collection has not encountered this manifest yet. Otherwise contains a status report from the last GC run. If this object is shown, it will contain exactly one of the following attributes. |
| `manifests[].gc_status.protected_by_recent_upload` | true or omitted | If true, this manifest was protected from deletion during the last GC run because it was uploaded too recently (within 10 minutes of the GC run). |
| `manifests[].gc_status.protected_by_parent` | string or omitted | If shown, this manifest was protected from deletion during the last GC run because there is a parent manifest that references it. The field contains the parent manifest's digest. If the manifest is referenced by multiple parent manifests, it is not defined which parent manifest's digest will be shown. |
| `manifests[].gc_status.protected_by_policy` | object or omitted | If shown, this manifest was protected from deletion during the last GC run because of a matching policy with the "protect" action. The object will contain the policy definition in the same format as described above for `accounts[].gc_policies[]`. |
| `manifests[].gc_status.relevant_policies` | array of objects or omitted | If shown, this manifest was not protected from deletion during the last GC run, but no deleting policy matched either. The array will contain the definitions of all deleting policies that could apply to this manifest, in the same format as described above for `accounts[].gc_policies[]`. |
| `manifests[].vulnerability_status` | string | Either `Clean` (no vulnerabilities have been found in this image), `Pending` (vulnerability scanning is not enabled on this server or is still in progress for this image or has failed for this image), `Error` (vulnerability scanning failed for this image or an image referenced in this manifest), or any of the severity strings defined by Clair (`Unknown`, `Negligible`, `Low`, `Medium`, `High`, `Critical`, `Defcon1`). The full vulnerability report can be retrieved with [a separate API call](#delete-keppelv1accountsnamerepositoriesname_manifestsdigestvulnerability_report). |
| `manifests[].trivy_vulnerability_status` | string | Either `Clean` (no vulnerabilities have been found in this image), `Pending` (vulnerability scanning is not enabled on this server or is still in progress for this image or has failed for this image), `Error` (vulnerability scanning failed for this image or an image referenced in this manifest), or any of the severity strings defined by Clair (`Unknown`, `Low`, `Medium`, `High`, `Critical`). The full vulnerability report can be retrieved with [a separate API call](#delete-keppelv1accountsnamerepositoriesname_manifestsdigesttrivy_report). |
| `manifests[].vulnerability_scan_error` | string | Only shown if `vulnerability_status` is `Error`. Contains the error message from Clair that explains why this image could not be scanned. When `vulnerability_status` is `Error` because scanning failed for an image referenced in this manifest, the error message will be shown on the referenced manifest instead of on this manifest. |
| `truncated` | boolean | Indicates whether [marker-based pagination](#marker-based-pagination) must be used to retrieve the rest of the result. |

## DELETE /keppel/v1/accounts/:name/repositories/:name/\_manifests/:digest

Deletes the specified manifest and all tags pointing to it. Returns 204 (No Content) on success.
The digest that identifies the manifest must be that manifest's canonical digest, otherwise 404 is returned.

## GET /keppel/v1/accounts/:name/repositories/:name/\_manifests/:digest/vulnerability\_report

Retrieves the vulnerability report for the specified manifest. If the manifest exists and a vulnerability report is available for it, returns 200 (OK) and a JSON response body containing the vulnerability report in the [format defined by Clair](https://quay.github.io/clair/reference/api.html#schemavulnerabilityreport).

Returns 404 (Not Found) if the specified manifest does not exist.

Otherwise, returns 204 (No Content) if the manifest does not directly reference any image layers and thus cannot be scanned for vulnerabilities itself.

Otherwise, returns 405 (Method Not Allowed) if the manifest exists, but its vulnerability status (see above) is either `Pending` or `Error`. (This case should technically also be a 404, but the different status code allows clients to disambiguate the nonexistence of the manifest from the nonexistence of the vulnerability report.)

Note that, when manifests reference other manifests (the most common case being multi-arch images referencing their constituent single-arch images), the vulnerability status of the parent manifest aggregates over the vulnerability statuses of its child manifests, but its vulnerability report only covers image layers directly referenced by the parent manifest. Clients displaying the vulnerability report for a multi-arch image manifest or any other manifest referencing child manifests should recursively fetch the vulnerability reports of all child manifests and show a merged representation as appropriate for their use case.

## GET /keppel/v1/accounts/:name/repositories/:name/\_manifests/:digest/trivy\_report

If this Keppel is configured to use its bundled [Trivy security scanner](https://aquasecurity.github.io/trivy), this
endpoint retrieves a report for the specified manifest from Trivy. If the manifest exists and a vulnerability report is
available for it, returns 200 (OK) and a JSON response body containing the vulnerability report in the [format defined
by Trivy](https://aquasecurity.github.io/trivy/latest/docs/configuration/reporting/#json), possibly enriched as
described below.

The output format can be selected with the `format` query parameter. Supported values include:

- [`json`](https://aquasecurity.github.io/trivy/latest/docs/configuration/reporting/#json) (default) for Trivy's default vulnerability report format, and
- [`spdx-json`](https://aquasecurity.github.io/trivy/latest/docs/target/sbom/#spdx) for the image's SBOM in the SPDX-compliant JSON format.

Returns 404 (Not Found) if the specified manifest does not exist.

Otherwise, returns 204 (No Content) if the manifest does not directly reference any image layers and thus cannot be scanned for vulnerabilities itself.

Otherwise, returns 405 (Method Not Allowed) if the manifest exists, but its vulnerability status (see above) is either `Pending` or `Error`.
(This case should technically also be a 404, but the different status code allows clients to disambiguate the nonexistence of the manifest from the nonexistence of the vulnerability report.)

Note that, when manifests reference other manifests (the most common case being multi-arch images referencing their constituent single-arch images), the vulnerability
status of the parent manifest aggregates over the vulnerability statuses of its child manifests, but its vulnerability report only covers image layers directly referenced
by the parent manifest. Clients displaying the vulnerability report for a multi-arch image manifest or any other manifest referencing child manifests should recursively
fetch the vulnerability reports of all child manifests and show a merged representation as appropriate for their use case.

### Report enrichment

Reports in the format `json` can be enriched by Keppel by adding the top-level key `X-Keppel-Applicable-Policies`. This
key appears if security scan policies are maintained on the account containing the image manifest, and at least one
policy applies to at least one vulnerability that exists in the report. If the key exists, it contains an object whose
keys are vulnerability IDs, with each respective value being the policy that applies to the vulnerabilities with this
ID. This information can be used by user agents to understand how Keppel computed the vulnerability status of the full
image manifest from the individual vulnerabilities.

## DELETE /keppel/v1/accounts/:name/repositories/:name/\_tags/:name

Deletes the specified tag, without deleting the manifest it points to. Returns 204 (No Content) on success.

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
  "password": "EaxiYoo8ju7Ohvukooch"
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
```

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

## GET /clair/:path

When Keppel is set up with a Clair instance for vulnerability scanning, all GET/HEAD requests for paths under `/clair/`
get reverse-proxied into the Clair API. The user must have administrative access to Keppel. This reverse-proxying is
useful because a Clair instance associated with Keppel usually only responds to requests with tokens issued by Keppel,
and Keppel does not hand out those tokens at all.
