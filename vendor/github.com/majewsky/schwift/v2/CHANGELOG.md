# v2.0.0 (2024-07-08)

Breaking changes:

- All methods that make HTTP requests now take a leading `context.Context` argument.
  The `RequestOptions.Context` field has been removed because it is made redundant
  by these extra arguments.

# v1.3.0 (2023-10-25)

New features:

- The error message of `type UnexpectedStatusCodeError` now includes the
  request method and target object name. This will make those errors more
  plausible when returned from complex operations that involve several Swift
  API calls.

# v1.2.0 (2022-10-28)

New features:

- Digest signing now uses sha256 and sha512 (preference in that order) if
  enabled by Swift.

Changes:

- Added golangci-lint to `make test`. All new errors and lints were addressed.

# v1.1.0 (2022-02-07)

Bugfixes:

- Fix request URL when object name is not a well-formed path. For example, an
  object name like "a///b" is not wrongly normalized into "a/b" anymore. If
  your application relies on object names being normalized paths, consider
  passing your object names through `path.Clean()` before giving them to
  `Container.Object()`.

# v1.0.0 (2021-05-28)

Initial release. The library had been mostly feature-complete since 2018, but I
never got around to actually tagging a 1.0.0 since a few less-used features are
missing in the API (mostly object versioning). The 1.0.0 release was overdue,
though, given that this library was already used in many prod deployments.
