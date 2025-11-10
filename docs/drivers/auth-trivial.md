<!--
SPDX-FileCopyrightText: 2025 SAP SE
SPDX-License-Identifier: Apache-2.0
-->

# Auth driver: `trivial`

An auth driver that accepts exactly one username/password combination, which grants universal access.
It is intended only for use in isolated test setups, e.g. when running the OCI conformance test suite.

## Server-side configuration

```sh
export KEPPEL_DRIVER_AUTH='{"type":"trivial","params":{...}}'
```

The following parameters may be supplied in `$KEPPEL_DRIVER_AUTH` (in JSON format):

| Field | Type | Explanation |
| ----- | ---- | ----------- |
| `username`<br>`password` | string | *(required)* The credentials that will grant access. |
