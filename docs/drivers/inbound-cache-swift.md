### Inbound cache driver: `swift`

A full-featured inbound cache driver that caches manifests in an OpenStack Swift container. The container is safe to be
shared by multiple Keppel instances to increase the cache's effectiveness. Cache entries expire through the use of
Swift's built-in object expiration, with a lifetime of 3 hours for tags and 48 hours for manifests.

| Variable | Default | Explanation |
| -------- | ------- | ----------- |
| `KEPPEL_INBOUND_CACHE_OS_...` | *(required)* | A full set of OpenStack auth environment variables for Keppel's service user. See [documentation for openstackclient][os-env] for details. Each variable name gets an additional `KEPPEL_INBOUND_CACHE_` prefix (e.g. `KEPPEL_INBOUND_CACHE_OS_AUTH_URL`) to disambiguate from the `OS_...` variables used by the `keystone` auth driver. |
| `KEPPEL_INBOUND_CACHE_SWIFT_CONTAINER` | *(required)* | Name of the Swift container where cache entries are stored. |
| `KEPPEL_INBOUND_CACHE_ONLY_HOSTS` | *(optional)* | If given, the cache will be skipped for external registries whose hostname does not match the given regex. A leading `^` and trailing `$` is implied. |
| `KEPPEL_INBOUND_CACHE_EXPECT_HOSTS` | *(optional)* | If given, the cache will be skipped for external registries whose hostname matches the given regex. A leading `^` and trailing `$` is implied. |
