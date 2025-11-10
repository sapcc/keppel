<!--
SPDX-FileCopyrightText: 2025 SAP SE
SPDX-License-Identifier: Apache-2.0
-->

# Storage driver: `swift`

This driver only works with the [`keystone` auth driver](auth-keystone.md). For a given Keppel account, it stores image
data in the Swift container `keppel-$ACCOUNT_NAME` in the OpenStack project that is this account's auth tenant.

## Server-side configuration

```sh
export KEPPEL_DRIVER_STORAGE='{"type":"swift"}'
```

The service user must have permissions to switch to every Swift account. Such access is usually provided by the `swiftreseller` role.
