<!--
SPDX-FileCopyrightText: 2025 SAP SE

SPDX-License-Identifier: Apache-2.0
-->

# Account management driver: `basic`

This driver sources managed accounts from a static JSON configuration file.

## Server-side configuration

| Variable | Default | Explanation |
| -------- | ------- | ----------- |
| `KEPPEL_ACCOUNT_MANAGEMENT_CONFIG_PATH` | *(required)* | The path to the configuration file. |
| `KEPPEL_ACCOUNT_MANAGEMENT_PROTECTED_ACCOUNTS` | *(optional)* | A space-separated list of account names. If any of these accounts are managed, but do not appear in the configuration file, the driver will rather fail than instruct the janitor to clean them up. This is an extra layer of protection if you want to be super-paranoid about protecting specific high-value accounts from accidental deletion (e.g. in the case of accidentally feeding an empty config file to the driver). |

The driver will reload this configuration file for every work cycle of the account management job, so it is a viable
strategy to update the configuration file without restarting the janitor process (e.g. in Kubernetes, by having the file mounted
from a ConfigMap).

## Configuration file syntax

The configuration file must be formatted in JSON.
The following fields are valid inside the configuration file:

| Field | Explanation |
| ----- | ----------- |
| `accounts` | list of objects | A list of objects, one for each managed account. Any managed accounts that exists in the database, but is not included in this list will be deleted. |
| `accounts[].name`<br>`accounts[].auth_tenant_id`<br>`accounts[].gc_policies`<br>`accounts[].platform_filter`<br>`accounts[].rbac_policies`<br>`accounts[].replication`<br>`accounts[].validation` | These fields have the same structure and meaning as on `{GET,PUT} /keppel/v1/accounts/:name`; see [API spec](../api-spec.md) for details. |
| `accounts[].security_scan_policies` | This field has the same structure and meaning as `policies` on `{GET,PUT} /keppel/v1/accounts/:name/security_scan_policies`; see [API spec](../api-spec.md) for details. |
