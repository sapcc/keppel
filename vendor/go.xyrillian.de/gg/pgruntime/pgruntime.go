// SPDX-FileCopyrightText: 2026 Stefan Majewsky <majewsky@gmx.net>
// SPDX-License-Identifier: Apache-2.0

// Package pgruntime provides connection handling for PostgreSQL databases,
// including optional support for database migrations and self-contained test DB instances.
// It can be used with any PostgreSQL data driver, including [lib/pq] and [pgx].
//
// # Basic usage
//
// In productive scenarios, build a [ConnectionTarget] instance from configuration values or command-line arguments, and call [Connector.Connect].
// In test scenarios, call [Connector.ConnectForTest].
//
// Both situations require a [Connector] instance:
// When using an std-compatible driver like [lib/pq], use [StdConnector].
// Otherwise, a custom [Connector] implementation must be supplied.
// For [pgx], the connectors from [gg-pgx] may be used.
//
// # Legacy
//
// This is a clean-room reimplementation of one half of [easypg] with several interface improvements and cleanups, most notably:
//   - The hard dependency on lib/pq has been removed.
//   - Support for creating databases on first use has been removed (except in ConnectForTest).
//   - The reset logic in ConnectForTest is completely reworked and massively simplified.
//
// [easypg]: https://pkg.go.dev/github.com/sapcc/go-bits/easypg
// [lib/pq]: https://pkg.go.dev/github.com/lib/pq
// [pgx]: https://pkg.go.dev/github.com/jackc/pgx/v5
// [gg-pgx]: https://pkg.go.dev/go.xyrillian.de/gg-pgx
package pgruntime
