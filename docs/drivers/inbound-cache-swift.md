<!--
SPDX-FileCopyrightText: 2025 SAP SE
SPDX-License-Identifier: Apache-2.0
-->

# Inbound cache driver: `swift`

A full-featured inbound cache driver that caches manifests in an OpenStack Swift container. The container is safe to be
shared by multiple Keppel instances to increase the cache's effectiveness. Cache entries expire through the use of
Swift's built-in object expiration, with a lifetime of 3 hours for tags and 48 hours for manifests.

## Server-side configuration

```sh
export KEPPEL_DRIVER_INBOUND_CACHE='{"type":"swift","params":{...}}'
```

The following parameters may be supplied in `$KEPPEL_DRIVER_INBOUND_CACHE` (in JSON format):

| Field | Type | Explanation |
| ----- | ---- | ----------- |
| `env_prefix` | string | An optional common prefix for all environment variables checked by this driver. |
| `container_name` | string | *(required)* Name of the Swift container where cache entries are stored. |
| `only_hosts` | string | If given, the cache will be skipped for external registries whose hostname does not match this regex. |
| `except_hosts` | string | If given, the cache will be skipped for external registries whose hostname matches this regex. |

Regexes must use [the Go syntax](https://pkg.go.dev/regexp/syntax).
A leading `^` and trailing `$` are automatically added to each regex.

Connection credentials for OpenStack must be supplied via environment variables in the usual style; see [documentation for openstackclient][os-env] for details.
Usually, these credentials are shared between multiple Keppel instances and thus will differ from the credentials used by the `keystone` auth driver.
In this case, set e.g. `params.env_prefix = "KEPPEL_INBOUND_CACHE_"` to check `$KEPPEL_INBOUND_CACHE_OS_AUTH_URL` instead of `$OS_AUTH_URL`, and so on.

[os-env]: https://docs.openstack.org/python-openstackclient/latest/cli/man/openstack.html
