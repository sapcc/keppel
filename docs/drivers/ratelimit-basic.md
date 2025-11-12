<!--
SPDX-FileCopyrightText: 2025 SAP SE
SPDX-License-Identifier: Apache-2.0
-->

# Rate limit driver: `basic`

A rate limit driver with fixed rate limits that are the same across all accounts and auth tenants.

## Server-side configuration

```sh
export KEPPEL_DRIVER_RATELIMIT='{"type":"basic","params":{...}}'
```

The following parameters may be supplied in `$KEPPEL_DRIVER_RATELIMIT` (in JSON format):

| Field | Type | Explanation |
| ----- | ---- | ----------- |
| `anycast_blob_pull_bytes` | object | Rate limit per account for anycast GET requests on blobs that are served across regions. Note that this rate limit counts bytes transferred, not requests. |
| `blob_pulls` | object | Rate limit per account for GET requests on blobs. |
| `blob_pushes` | object | Rate limit per account for POST requests on blobs and blob uploads. |
| `manifest_pulls` | object | Rate limit per account for GET requests on manifests. |
| `manifest_pushes` | object | Rate limit per account for PUT requests on manifests. |
| `trivy_report_retrievals` | object | Rate limit per account for GET requests on Trivy reports. |

If any of those rate limits is not set, it will not be enforced.
Each rate limit is specified as an object with the following fields:

| Field | Type | Explanation |
| ----- | ---- | ----------- |
| `limit`<br>`window` | integer<br>string | The actual rate limit. For example, `{"limit":10,"window":"s"}` is a limit of 10 per second. Acceptable values for `window` are `s` (1 second), `m` (1 minute) or `h` (1 hour). |
| `burst` | integer | Burst budget. When starting from a completely unused rate limit, this many requests/bytes may always be made/transferred before first being rate-limited. (From an algorithmic perspective, the rate limit describes how quickly this budget replenishes.) This number should be generous especially for blob pulls since pulling a single manifest usually leads to pulling a lot of blobs. |
