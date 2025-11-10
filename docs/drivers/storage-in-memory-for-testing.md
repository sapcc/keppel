<!--
SPDX-FileCopyrightText: 2025 SAP SE
SPDX-License-Identifier: Apache-2.0
-->

# Storage driver: `in-memory-for-testing`

This driver works with any auth driver. Each keppel-registry stores its
contents in RAM only, without using any persistent storage. This driver is
useful for test suite runs.

## Server-side configuration

```sh
export KEPPEL_DRIVER_STORAGE='{"type":"in-memory-for-testing"}'
```
