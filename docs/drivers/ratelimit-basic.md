<!--
SPDX-FileCopyrightText: 2025 SAP SE

SPDX-License-Identifier: Apache-2.0
-->

### Rate limit driver: `basic`

A rate limit driver with fixed rate limits that are the same across all accounts and auth tenants.

| Variable | Default | Explanation |
| -------- | ------- | ----------- |
| `KEPPEL_RATELIMIT_BLOB_PULLS` | *(required)* | Rate limit per account for GET requests on blobs. |
| `KEPPEL_RATELIMIT_BLOB_PUSHES` | *(required)* | Rate limit per account for POST requests on blobs and blob uploads. |
| `KEPPEL_RATELIMIT_MANIFEST_PULLS` | *(required)* | Rate limit per account for GET requests on manifests. |
| `KEPPEL_RATELIMIT_MANIFEST_PUSHES` | *(required)* | Rate limit per account for PUT requests on manifests. |
| `KEPPEL_RATELIMIT_TRIVY_REPORT_RETRIEVALS` | *(required)* | Rate limit per account for GET requests on Trivy reports. |
| `KEPPEL_BURST_BLOB_PULLS`<br>`KEPPEL_BURST_BLOB_PUSHES`<br>`KEPPEL_BURST_MANIFEST_PULLS`<br>`KEPPEL_BURST_MANIFEST_PUSHES`<br>`KEPPEL_BURST_TRIVY_REPORT_RETRIEVALS` | `5` | Burst budget for each of these rate limits. When starting from a completely unused rate limit, this many requests are always allowed before first being rate-limited. This number should be generous especially for blob pulls since pulling a single manifest usually leads to pulling a lot of blobs. |

Values for these rate limits (except bursts) must be specified in the format `<value> <unit>` where `<unit>` is `r/s` (requests per second), `r/m` (requests per minute) or `r/h` (requests per hour). For example, `100 r/m` allows 100 requests per minute (and account).

| Variable | Default | Explanation |
| -------- | ------- | ----------- |
| `KEPPEL_RATELIMIT_ANYCAST_BLOB_PULL_BYTES` | *(optional)* | Rate limit per account for anycast GET requests on blobs that are served across regions. If not set, this rate limit is not enforced. |
| `KEPPEL_BURST_ANYCAST_BLOB_PULL_BYTES` | `0` | Burst budget for the above rate limit. (See above for explanation.) |

Values for this rate limits must be specified in the format `<value> <unit>` where `<unit>` is `B/s` (bytes per second), `B/m` (bytes per minute) or `B/h` (bytes per hour). For example, `10737418240 B/m` allows 10 GiB per minute (and account). Units other than bytes are not understood as of now.
