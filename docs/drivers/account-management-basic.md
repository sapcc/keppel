# Account management driver: `basic`

This driver sources managed accounts from a static JSON configuration file.

## Server-side configuration

| Variable | Default | Explanation |
| -------- | ------- | ----------- |
| `KEPPEL_ACCOUNT_MANAGEMENT_CONFIG_PATH` | *(required)* | The path to the configuration file. |

The driver will reload this configuration file for every work cycle of the account management job, so it is a viable
strategy to update the configuration file without restarting the janitor process (e.g. in Kubernetes, by having the file mounted
from a ConfigMap).

## Configuration file syntax

The configuration file must be formatted in JSON.
The following fields are valid inside the configuration file:

| Field | Explanation |
| ----- | ----------- |
| `accounts` | list of objects | A list of objects, one for each managed account. Any managed accounts that exists in the database, but is not included in this list will be deleted. |
| `accounts[].name`<br>`accounts[].auth_tenant_id`<br>`accounts[].gc_policies`<br>`accounts[].rbac_policies`<br>`accounts[].replication`<br>`accounts[].validation` | These fields have the same structure and meaning as on `{GET,PUT} /keppel/v1/accounts/:name`; see [API spec](../api-spec.md) for details. |
| `accounts[].security_scan_policies` | This field has the same structure and meaning as `policies` on `{GET,PUT} /keppel/v1/accounts/:name/security_scan_policies`; see [API spec](../api-spec.md) for details. |
