# Overview for developers/contributors

You should have read the entire [README.md](./README.md) once before starting
to work on Keppel's codebase. This document assumes that you did that already.

## Testing methodology

### Core implementation

Run the full test suite with:

```sh
$ ./testing/with-postgres-db.sh make check
```

This will produce a coverage report at `build/cover.html`.

## Assumptions about docker-registry internals

Our implementation relies on some assumptions about the registry's internal
business logic which have been validated at the time of this writing, but may
change in future releases of the registry since they are not part of an API
contract. **When upgrading docker/distribution to newer versions, check
carefully that these assumptions still hold.** Otherwise we venture into
undefined-behavior territory.

### Transparent manifest translation

When tracking tags that exist in a registry, we rely on the fact that the
registry only allows to manipulate a manifest through its tag name or through
the original digest of the manifest (i.e. the Docker-Content-Digest that was
returned the manifest was initially pushed into the registry), not through the
digest of any other representations of the same manifest.

As of now (docker/distribution v2.7.1), the registry *can* translate manifests
that were originally pushed as [Image Manifest V2, Schema 2][im2-s2] into
[Image Manifest V2, Schema 1][im2-s1] to ensure compatibility with older
clients. This leads to a different content digest (since the digest is computed
over a canonical serialization of the manifest, which differs between schema
versions), but this is not a problem because this translation only happens for
read requests (HEAD and GET) and only when the manifest is identified by tag
name, not by digest.

[im2-s1]: https://docs.docker.com/registry/spec/manifest-v2-1/
[im2-s2]: https://docs.docker.com/registry/spec/manifest-v2-2/

If it were possible for a user to issue write requests (DELETE) on a manifest
identified by its digest, and the digest was that of a version of the manifest
in a different format than the one that was originally pushed, `keppel-api`
would not be able to connect the dots and understand which digest the client is
referring to. This would cause a divergence between the manifests in Keppel's
DB and the actual set of manifests in the registry.

As of now, OCI manifests are unaffected by all of this since no transparent
translation is offered for this type of manifests at all.

## Working on the `kubernetes` orchestration driver

When running `keppel-api` for testing (preferably through `make run-api`), you
should use the `local-processes` orchestration driver if possible. If you need
to test the `kubernetes` orchestration driver, observe the special procedures
noted in its documentation. Our own helm chart has a `dev-toolbox` to help with
testing the `kubernetes` driver, see documentation over there.
