<!--
SPDX-FileCopyrightText: 2025 SAP SE
SPDX-License-Identifier: Apache-2.0
-->

# Federation driver: `swift`

A full-featured federation driver that keeps track of Keppel accounts in an OpenStack Swift container that is shared
between all participating Keppel instances. We use this driver at SAP Converged Cloud since we already use Swift for
storage anyway, hence also using it for federation reduces complexity. Since Swift is not strongly consistent, there is
a small risk of ending up in inconsistent situations when two Keppels write the configuration for the same account name
at the same time, but the driver includes some protections to make these errors more unlikely, so the risk is remote
enough to be acceptable for us.

## Server-side configuration

```sh
export KEPPEL_DRIVER_FEDERATION='{"type":"swift","params":{...}}'
```

The following parameters may be supplied in `$KEPPEL_DRIVER_FEDERATION` (in JSON format):

| Field | Type | Explanation |
| ----- | ---- | ----------- |
| `env_prefix` | string | An optional common prefix for all environment variables checked by this driver. |
| `container_name` | string | *(required)* Name of the Swift container where account registrations are stored. |

Connection credentials for OpenStack must be supplied via environment variables in the usual style; see [documentation for openstackclient][os-env] for details.
Usually, these credentials are shared between multiple Keppel instances and thus will differ from the credentials used by the `keystone` auth driver.
In this case, set e.g. `params.env_prefix = "KEPPEL_FEDERATION_"` to check `$KEPPEL_FEDERATION_OS_AUTH_URL` instead of `$OS_AUTH_URL`, and so on.

[os-env]: https://docs.openstack.org/python-openstackclient/latest/cli/man/openstack.html
