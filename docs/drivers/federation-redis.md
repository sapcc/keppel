### Federation driver: `redis`

A full-featured federation driver that keeps track of Keppel accounts in a Redis that's shared between all participating
Keppel instances. You probably want a clustered Redis setup like [Dynomite](https://github.com/Netflix/dynomite) to
avoid a single point of failure, but a single Redis instance also works fine as long as all Keppels can reach it. The
Redis is only read from and written when creating or deleting accounts and when issuing sublease tokens.

| Variable | Default | Explanation |
| -------- | ------- | ----------- |
| `KEPPEL_FEDERATION_REDIS_HOSTNAME` | *(required)* | Hostname identifying the location of the shared Redis instance. This is separate from `KEPPEL_REDIS_HOSTNAME` since that one is usually local to the current Keppel instance whereas the federation Redis is shared among all Keppel instances in your deployment. |
| `KEPPEL_FEDERATION_REDIS_PORT` | `6379` | Port on which the shared Redis instance is running on. |
| `KEPPEL_FEDERATION_REDIS_DB_NUM` | `0` | Database number. |
| `KEPPEL_FEDERATION_REDIS_PASSWORD` | *(optional)* | Password for the authentication. |
| `KEPPEL_FEDERATION_REDIS_PREFIX` | `keppel` | A prefix string that is prepended to all keys that this driver accesses in the Redis. This is useful for separating QA from productive deployments etc. |

In Redis, the following keys are accessed by this driver:

| Key | Type | Explanation |
| --- | ---- | ----------- |
| `${PREFIX}-primary-${NAME}` | string | The hostname of the keppel-api hosting the primary account with that name. |
| `${PREFIX}-replicas-${NAME}` | array of strings | The hostnames of the keppel-apis hosting replica accounts with that name. |
| `${PREFIX}-sublease-token-${NAME}` | string | The sublease token that was most recently issued by the keppel-api hosting the primary account with that name. Will be replaced with the empty string when the token is redeemed to create a replica account. |
