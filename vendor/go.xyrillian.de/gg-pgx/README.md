<!--
SPDX-FileCopyrightText: 2025 Stefan Majewsky <majewsky@gmx.net>
SPDX-License-Identifier: Apache-2.0
-->

# gg-pgx

Integration for using [gg/gsql](https://pkg.go.dev/go.xyrillian.de/gg/gsql) (and libraries based on it, such as [Oblast](https://pkg.go.dev/go.xyrillian.de/oblast)) with the PostgreSQL driver library [pgx](https://github.com/jackc/pgx).

## How to contribute

This repository accepts contributions as follows:

- For bugfixes or dependency updates, feel free to submit a PR right away.
- For entirely new types, or when adding library dependencies, please send a mail or open a GitHub issue first to discuss the design.
- Generally, we dislike library dependencies in this house because every dependency means extra work managing dependency upgrades.

Before sending a patch, please ensure that `make check` does not report any problems.

To contribute to the primary repository at <https://git.xyrillian.de/go-gg-pgx>, please use `git format-patch` in the usual manner and send patches to the maintainer's mail address (which can be found in the copyright notice headers on each file).
Alternatively, if you are still using GitHub, you can submit issues and pull requests at the mirror repository <https://github.com/majewsky/gg-pgx>.
