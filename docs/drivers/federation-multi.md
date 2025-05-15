<!--
SPDX-FileCopyrightText: 2025 SAP SE

SPDX-License-Identifier: Apache-2.0
-->

### Federation driver: `multi`

Runs multiple federation drivers in parallel. The first driver in the list is
used for read operations, but all write operations fan out into each driver.

This is useful when switching to a different federation driver
(`old -> multi[old,new] -> new`). The old driver is set up as the primary
source of truth, while the new driver runs passively in the background and has
its storage populated. Once all accounts have run through a federation
announcement cycle, the configuration can be updated to only use the new
driver.

Because of the risk of accidental inconsistencies between the state of the
individual federation drivers, it is not recommended to run a
multi-federation-driver setup in production for extended periods of time.

| Variable | Default | Explanation |
| -------- | ------- | ----------- |
| `KEPPEL_FEDERATION_MULTI_DRIVERS` | *(required)* | Comma-separated list of driver names that are run in parallel. Each of these drivers is configured with its usual set of environment variables. |
