<!--
SPDX-FileCopyrightText: 2025 SAP SE
SPDX-License-Identifier: Apache-2.0
-->

# Account management driver: `trivial`

A driver for deployments without any managed accounts.

- This driver advertises no managed accounts.
- When switching from a different account management driver to this one,
  any managed accounts that exist in the database will be deleted.

## Server-side configuration

```sh
export KEPPEL_DRIVER_ACCOUNT_MANAGEMENT='{"type":"trivial"}'
```
