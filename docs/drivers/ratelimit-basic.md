### Rate limit driver: `basic`

A rate limit driver with fixed rate limits that are the same across all accounts and auth tenants.

| Variable | Default | Explanation |
| -------- | ------- | ----------- |
| `KEPPEL_RATELIMIT_BLOB_PULLS` | *(required)* | Rate limit per account for GET requests on blobs. |
| `KEPPEL_RATELIMIT_BLOB_PUSHES` | *(required)* | Rate limit per account for POST requests on blobs and blob uploads. |
| `KEPPEL_RATELIMIT_MANIFEST_PULLS` | *(required)* | Rate limit per account for GET requests on manifests. |
| `KEPPEL_RATELIMIT_MANIFEST_PUSHES` | *(required)* | Rate limit per account for PUT requests on manifests. |

Values for rate limits must be specified in the format `<value> <unit>` where `<unit>` is `r/s` (requests per second), `r/m` (requests per minute) or `r/h` (requests per hour). For example, `100 r/m` allows 100 requests per minute (and account).
