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

- [GET /v1/keppel/accounts](#get-keppelv1accounts)
- [GET /v1/keppel/accounts/:name](#get-keppelv1accountsname)
- [PUT /v1/keppel/accounts/:name](#put-keppelv1accountsname)
- [GET /v1/keppel/auth](#get-keppelv1auth)

## GET /v1/keppel/accounts

Lists all accounts that the user has access to.
On success, returns 200 and a JSON response body like this:

```json
{
  "accounts": [
    {
      "name": "firstaccount",
      "auth_tenant_id": "firsttenant"
    },
    {
      "name": "secondaccount",
      "auth_tenant_id": "secondtenant"
    }
  ]
}
```

The following fields may be returned:

| Field | Type | Explanation |
| ----- | ---- | ----------- |
| `accounts[].name` | string | Name of this account. |
| `accounts[].auth_tenant_id` | string | ID of auth tenant that regulates access to this account. |

## GET /v1/keppel/accounts/:name

Shows information about an individual account.
Returns 404 if no account with the given name exists, or if the user does not have access to it.
Otherwise returns 200 and a JSON response body like this:

```json
{
  "account": {
    "name": "firstaccount",
    "auth_tenant_id": "firsttenant"
  }
}
```

The `.account` object's contents are equivalent to the corresponding entry in `.accounts[]` as returned by
`GET /keppel/v1/accounts`.

## PUT /v1/keppel/accounts/:name

Creates or updates the account with the given name. The request body must be a JSON document following the same schema
as the response from the corresponding GET endpoint, except that:

- `account.name` may not be present (the name is already given in the URL), and
- `account.auth_tenant_id` may not be changed for existing accounts.

On success, returns 200 and a JSON response body like from the corresponding GET endpoint.

## GET /v1/keppel/auth

This endpoint is reserved for the authentication workflow of the [OCI Distribution API][oci-dist].
