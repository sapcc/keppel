<!--
SPDX-FileCopyrightText: 2025 SAP SE
SPDX-License-Identifier: Apache-2.0
-->

# Storage driver: `multi`

This driver implementation facilitates a zero-downtime migration from one storage backend to another.
The migration takes place in multiple phases:

1. _(old status quo)_ Just the old storage driver is used.
2. The "multi" storage driver is used, with its `phase` set to `copy`. During this phase:
   - Reads hit the old backend.
   - Writes and deletes hit both the old and new storage backend.
   - When validating blobs or manifests, the janitor will copy the object in question to the new backend if not done yet.
   - No explicit copy process exists for Trivy reports exist, but the janitor rewrites them during every security check on the respective manifest, which ensures they are available in the new backend.
3. The "multi" storage driver is used, with its `phase` set to `cleanup`. During this phase:
   - Reads hit the new backend.
   - Writes hit the new backend, and (except for blobs) try to clean up the respective object on the old backend if it still exists there.
   - When the janitor performs blob validations, it will clean up the blob on the old backend if it still exists there.
   - Deletes hit both the old and new storage backend.
4. The "multi" storage driver is used, with its `phase` set to `finalize`. During this phase:
   - Reads, writes and deletes only hit the new backend.
   - When the janitor performs a storage sweep on an account, it will delete the account on the old backend if it still exists there.
5. _(new status quo)_ Just the new storage driver is used.

To allow operators to decide when to move to the next phase, Prometheus metrics should be inspected:

- During phase `copy`:
  - Wait for copies to finish: The counter metrics `keppel_multi_storage_driver_copied_{blobs,manifests}` should not be increasing anymore. Note that blob validation only happens once a week for each blob, so this can take a while. If you need to speed this up, use the SQL query `UPDATE blobs SET next_validation_at = NOW()`.
  - Ensure that copies do not fail: The counter metrics `keppel_{blob,manifest}_validations{task_outcome="failed"}` should not be increasing.
  - If Trivy is used, ensure that Trivy reports get written: The counter metric `keppel_trivy_security_status_checks{task_outcome="failed"}` should not be increasing.
- During phase `cleanup`:
  - Wait for cleanups to finish: The counter metrics `keppel_multi_storage_driver_cleaned_up` should not be increasing any more. The same note as above on the length of the blob validation cycle applies.
  - Ensure that cleanups do not fail: The counter metrics `keppel_{blob,manifest}_validations{task_outcome="failed"}` should not be increasing.
- During phase `finalize`:
  - Ensure that account cleanups do not fail: The counter metric `keppel_storage_sweeps{task_outcome="failed"}` should not be increasing.

For each of the checks that looks at a metric with `task_outcome="failed"`, it is a good idea to also check that the same metric with `task_outcome="success"` is increasing at a normal pace, to verify that the respective background job did not stall. These are all things for which you probably have set up alerts anyway, but it is especially important during the `multi`-driver-based migration to avoid data loss.

## Server-side configuration

```sh
export KEPPEL_DRIVER_STORAGE='{"type":"multi","params":{...}}'
```

The following parameters may be supplied in `$KEPPEL_DRIVER_STORAGE`:

| Field | Type | Explanation |
| ----- | ---- | ----------- |
| `old` | object | The configuration for the old storage driver, in the same format (an object with `type` and `params` keys) that would be expected in `$KEPPEL_DRIVER_STORAGE` if only this driver was used. |
| `new` | object | The configuration for the new storage driver, in the same format as the `old` parameter. |
| `phase` | string | Must be one of `copy`, `cleanup`, `finalize`. The storage driver must be run using each of these phases in order, and can then finally be replaced with the new storage driver. |

### Example

For a migration from Swift to Ceph object storage, with both storage clusters running side-by-side during the migration:

```json
{
  "type": "multi",
  "params": {
    "old": {
      "type": "swift",
      "params": { "service_type": "object-store" }
    },
    "new": {
      "type": "swift",
      "params": { "service_type": "object-store-ceph" }
    },
    "phase": "copy"
  }
}
```
