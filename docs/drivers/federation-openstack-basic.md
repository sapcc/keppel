<!--
SPDX-FileCopyrightText: 2025 SAP SE
SPDX-License-Identifier: Apache-2.0
-->

# Federation driver: `openstack-basic`

A simple federation driver for use with the [`keystone` auth driver](./auth-keystone.md). Claims are checked against a
hardcoded allowlist. This driver is OpenStack-specific since it translates auth tenant IDs (i.e., project IDs) into
project names before checking.

## Server-side configuration

```sh
export KEPPEL_DRIVER_FEDERATION='{"type":"openstack-basic","params":{...}}'
```

The following parameters may be supplied in `$KEPPEL_DRIVER_FEDERATION` (in JSON format):

| Field | Type | Explanation |
| ----- | ---- | ----------- |
| `allowlist` | array of objects | A list of rules to allow certain account names to be created within certain projects. |
| `allowlist[].project` | string | A regex matching Keystone project names, in the form `projectName@domainName`. |
| `allowlist[].account` | string | A regex matching Keppel account names. |

Regexes must use [the Go syntax](https://pkg.go.dev/regexp/syntax).
A leading `^` and trailing `$` are automatically added to each regex.
For example, the allowlist entry `{"project":"foo.*@bar","account":"qux.*"}` will allow all projects in the domain `bar` whose name starts with `foo` to claim account names starting with `qux`.
