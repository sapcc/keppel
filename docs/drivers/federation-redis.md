<!--
SPDX-FileCopyrightText: 2025 SAP SE
SPDX-License-Identifier: Apache-2.0
-->

# Federation driver: `redis`

A full-featured federation driver that keeps track of Keppel accounts in a Redis that's shared between all participating
Keppel instances. You probably want a clustered Redis setup like [Dynomite](https://github.com/Netflix/dynomite) to
avoid a single point of failure, but a single Redis instance also works fine as long as all Keppels can reach it. The
Redis is only read from and written when creating or deleting accounts and when issuing sublease tokens.

## Server-side configuration

```sh
export KEPPEL_DRIVER_FEDERATION='{"type":"redis","params":{...}}'
```

The following parameters may be supplied in `$KEPPEL_DRIVER_FEDERATION` (in JSON format):

| Field | Type | Explanation |
| ----- | ---- | ----------- |
| `env_prefix` | string | A common prefix for all environment variables checked by this driver. Defaults to `KEPPEL_FEDERATION_REDIS`. |
| `key_prefix` | string | A prefix string that is prepended to all keys that this driver accesses in the Redis. This is useful for separating QA from productive deployments etc. Defaults to `keppel`. |

The following environment variables may be supplied:

| Variable | Default | Explanation |
| -------- | ------- | ----------- |
| `${params.env_prefix}_HOSTNAME` | *(required)* | Hostname identifying the location of the shared Redis instance. This is separate from `KEPPEL_REDIS_HOSTNAME` since that one is usually local to the current Keppel instance whereas the federation Redis is shared among all Keppel instances in your deployment. |
| `${params.env_prefix}_PORT` | `6379` | Port on which the shared Redis instance is running on. |
| `${params.env_prefix}_DB_NUM` | `0` | Database number. |
| `${params.env_prefix}_PASSWORD` | *(optional)* | Password for the authentication. |
| `${params.env_prefix}_PREFIX` | `keppel` | A prefix string that is prepended to all keys that this driver accesses in the Redis. This is useful for separating QA from productive deployments etc. |

In Redis, the driver accesses the following keys:

| Key | Type | Explanation |
| --- | ---- | ----------- |
| `${params.key_prefix}-primary-${NAME}` | string | The hostname of the keppel-api hosting the primary account with that name. |
| `${params.key_prefix}-replicas-${NAME}` | array of strings | The hostnames of the keppel-apis hosting replica accounts with that name. |
| `${params.key_prefix}-sublease-token-${NAME}` | string | The sublease token that was most recently issued by the keppel-api hosting the primary account with that name. Will be replaced with the empty string when the token is redeemed to create a replica account. |
