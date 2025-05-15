<!--
SPDX-FileCopyrightText: 2025 SAP SE

SPDX-License-Identifier: Apache-2.0
-->

### Storage driver: `filesystem`

This driver works with any auth driver. With this driver, manifest and blob contents are stored on a regular file
system. If Keppel is deployed across multiple nodes, a network file system must be used to ensure consistency. This
driver is intended for development and validation purposes. In productive environments, a driver for a proper
distributed storage should be used instead.

## Server-side configuration

| Variable | Default | Explanation |
| -------- | ------- | ----------- |
| `KEPPEL_FILESYSTEM_PATH` | *(required)* | The directory in which this storage driver will store all payloads. |
