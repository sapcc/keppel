<!--
SPDX-FileCopyrightText: 2025 SAP SE

SPDX-License-Identifier: Apache-2.0
-->

### Federation driver: `swift`

A full-featured federation driver that keeps track of Keppel accounts in an OpenStack Swift container that is shared
between all participating Keppel instances. We use this driver at SAP Converged Cloud since we already use Swift for
storage anyway, hence also using it for federation reduces complexity. Since Swift is not strongly consistent, there is
a small risk of ending up in inconsistent situations when two Keppels write the configuration for the same account name
at the same time, but the driver includes some protections to make these errors more unlikely, so the risk is remote
enough to be acceptable for us.

| Variable | Default | Explanation |
| -------- | ------- | ----------- |
| `KEPPEL_FEDERATION_OS_...` | *(required)* | A full set of OpenStack auth environment variables for Keppel's service user. See [documentation for openstackclient][os-env] for details. Each variable name gets an additional `KEPPEL_FEDERATION_` prefix (e.g. `KEPPEL_FEDERATION_OS_AUTH_URL`) to disambiguate from the `OS_...` variables used by the `keystone` auth driver. |
| `KEPPEL_FEDERATION_SWIFT_CONTAINER` | *(required)* | Name of the Swift container where account registrations are stored. |
