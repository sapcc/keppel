<!--
SPDX-FileCopyrightText: 2025 SAP SE
SPDX-License-Identifier: Apache-2.0
-->

# Federation driver: `trivial`

Allows every user and auth tenant to claim any account name that's not already in use.

## Server-side configuration

```sh
export KEPPEL_DRIVER_FEDERATION='{"type":"trivial"}'
```
