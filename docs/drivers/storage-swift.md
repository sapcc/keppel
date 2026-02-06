<!--
SPDX-FileCopyrightText: 2025 SAP SE
SPDX-License-Identifier: Apache-2.0
-->

# Storage driver: `swift`

This driver only works with the [`keystone` auth driver](auth-keystone.md). For a given Keppel account, it stores image
data in the Swift container `keppel-$ACCOUNT_NAME` in the OpenStack project that is this account's auth tenant.

## Server-side configuration

```sh
export KEPPEL_DRIVER_STORAGE='{"type":"swift","params":{...}}'
```

The service user must have permissions to switch to every Swift account. Such access is usually provided by the `swiftreseller` role.

The following parameters may be supplied in `$KEPPEL_DRIVER_STORAGE`:

| Field | Type | Explanation |
| ----- | ---- | ----------- |
| `use_service_user_project` | bool | *(optional)* When set to `true`, stores all payload in the service user's own project instead of in the project owning the respective Keppel account. Defaults to `false`. |
