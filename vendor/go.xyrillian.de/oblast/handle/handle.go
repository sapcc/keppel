// SPDX-FileCopyrightText: 2026 Stefan Majewsky <majewsky@gmx.net>
// SPDX-License-Identifier: Apache-2.0

// Package handle contains type definitions for connecting non-std database drivers to Oblast.
// Since most database drivers use the standard interface from database/sql, the Wrap() function from the main package covers the needs of most users.
package handle

import (
	"context"
	"database/sql"
)

// Handle contains behavior that database handles must offer to Oblast.
// The standard-library types [*sql.DB] and [*sql.Tx] can satisfy this interface through the Wrap() function from the main package.
// Custom implementations of this interface can be used to connect non-std database drivers to Oblast.
//
// The method names are deliberately clunky to avoid name clashes with well-known methods like [sql.DB.Prepare] or [sql.DB.Query].
type Handle interface {
	// OblastPrepare prepares to execute a certain SQL query one or multiple times.
	//
	// The "repeated" flag is a hint to the implementation whether the same statement is going to be run many times.
	// If false, the implementation shall choose to forego the additional effort of a full statement preparation if possible,
	// and execute one-off queries instead.
	OblastPrepare(ctx context.Context, query string, repeated bool) (Statement, error)

	// OblastQuery works like db.QueryContext(ctx, query, args...).
	OblastQuery(ctx context.Context, query string, args []any) (Rows, error)
}

// Statement represents a prepared statement returned from [Handle.Prepare].
// The Exec and QueryRow methods shall work similarly to the respective functions on [*sql.Tx], as indicated in the comments.
//
// You will not need to interact with this type except when implementing your own [Handle].
type Statement interface {
	Close() error

	// Exec works like stmt.ExecContext(ctx, args...).
	Exec(ctx context.Context, args []any) (sql.Result, error)

	// QueryRow works like stmt.QueryRow(ctx, args...).Scan(slots...).
	QueryRow(ctx context.Context, args []any, slots []any) error
}

// Rows represents a set of rows returned from [Handle.Query] in response to a DB query.
// All methods shall behave like on the [*sql.Rows] type from std.
//
// You will not need to interact with this type except when implementing your own [Handle].
type Rows interface {
	Columns() ([]string, error)
	Close() error
	Err() error
	Next() bool
	Scan(slots ...any) error
}
