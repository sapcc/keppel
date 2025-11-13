<!--
SPDX-FileCopyrightText: 2025 SAP SE
SPDX-License-Identifier: Apache-2.0
-->

# Inbound cache driver: `trivial`

Does not actually cache anything. Every access is a cache miss and goes through to the real external source.

## Server-side configuration

```sh
export KEPPEL_DRIVER_INBOUND_CACHE='{"type":"trivial"}'
```
