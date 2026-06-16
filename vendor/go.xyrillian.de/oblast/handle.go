// SPDX-FileCopyrightText: 2026 Stefan Majewsky <majewsky@gmx.net>
// SPDX-License-Identifier: Apache-2.0

package oblast

import (
	"context"
	"database/sql"
	"fmt"

	"go.xyrillian.de/oblast/handle"
)

// Handle contains behavior that database handles must offer to Oblast.
// Custom implementations of this interface can be used to connect non-std database drivers to Oblast.
type Handle = handle.Handle

////////////////////////////////////////////////////////////////////////////////
// public API for database/sql compatibility
//
// NOTE: The internal structure of these types looks weird at first glance, with
// the pointer to the underlying instance duplicated, but of course that's deliberate.
//
// If our types implemented [Handle] directly, every function call taking them as an argument
// of type [Handle] (e.g. any of the methods on [Store]) would allocate a new fat pointer
// when converting from e.g. [*DB] at the callsite to [Handle] in the argument value.
//
// To circumvent this, our types only _have_ [Handle] instances within them within them
// as an embedded field, thus implementing [Handle] indirectly instead of directly.

// DB wraps [*sql.DB] into a [Handle] that can be used with Oblast.
//
// Because this type has [*sql.DB] as an embedded field,
// all methods from that type work on this type as well.
type DB struct {
	*sql.DB
	Handle
}

// NewDB wraps an instance of [*sql.DB] into Oblast's own [DB] type.
func NewDB(db *sql.DB) *DB {
	return &DB{db, sqlHandle[*sql.DB]{db}}
}

// Begin is like [sql.DB.Begin], but wraps the resulting transaction for use with Oblast.
func (db *DB) Begin() (*Tx, error) {
	tx, err := db.DB.Begin()
	return maybe(NewTx, tx), err
}

// BeginTx is like [sql.DB.BeginTx], but wraps the resulting transaction for use with Oblast.
func (db *DB) BeginTx(ctx context.Context, opts *sql.TxOptions) (*Tx, error) {
	tx, err := db.DB.BeginTx(ctx, opts)
	return maybe(NewTx, tx), err
}

// Conn is like [sql.DB.Conn], but wraps the resulting connection for use with Oblast.
func (db *DB) Conn(ctx context.Context) (*Conn, error) {
	conn, err := db.DB.Conn(ctx)
	return maybe(NewConn, conn), err
}

// Conn wraps [*sql.Conn] into a [Handle] that can be used with Oblast.
//
// Because this type has [*sql.Conn] as an embedded field,
// all methods from that type work on this type as well.
type Conn struct {
	*sql.Conn
	Handle
}

// NewConn wraps an instance of [*sql.Conn] into Oblast's own [Conn] type.
func NewConn(db *sql.Conn) *Conn {
	return &Conn{db, sqlHandle[*sql.Conn]{db}}
}

// BeginTx is like [sql.DB.BeginTx], but wraps the resulting transaction for use with Oblast.
func (conn *Conn) BeginTx(ctx context.Context, opts *sql.TxOptions) (*Tx, error) {
	tx, err := conn.Conn.BeginTx(ctx, opts)
	return maybe(NewTx, tx), err
}

// Tx wraps [*sql.Tx] into a [Handle] that can be used with Oblast.
//
// Because this type has [*sql.Tx] as an embedded field,
// all methods from that type work on this type as well.
type Tx struct {
	*sql.Tx
	Handle
}

// NewTx wraps an instance of [*sql.Tx] into Oblast's own [Tx] type.
func NewTx(db *sql.Tx) *Tx {
	return &Tx{db, sqlHandle[*sql.Tx]{db}}
}

func maybe[T, U any](wrap func(*T) *U, value *T) *U {
	if value == nil {
		return nil
	}
	return wrap(value)
}

// prove that we implement the interfaces that we claim
var (
	_ Handle = &DB{}
	_ Handle = &Conn{}
	_ Handle = &Tx{}
)

////////////////////////////////////////////////////////////////////////////////
// Handle implementation for database/sql types

// sqlExecutor is an interface covered by both [*sql.DB], [*sql.Conn] and [*sql.Tx].
type sqlExecutor interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	PrepareContext(ctx context.Context, query string) (*sql.Stmt, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// sqlHandle provides the [Handle] implementation for any type that implements [sqlExecutor].
type sqlHandle[T sqlExecutor] struct {
	Base T
}

// OblastPrepare implements the [Handle] interface.
func (h sqlHandle[T]) OblastPrepare(ctx context.Context, query string, repeated bool) (handle.Statement, error) {
	if !repeated {
		return wrappedStatement{h.Base, query, nil}, nil
	}
	stmt, err := h.Base.PrepareContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("during Prepare(): %w", err)
	}
	return wrappedStatement{h.Base, query, stmt}, nil
}

// OblastQuery implements the [Handle] interface.
func (h sqlHandle[T]) OblastQuery(ctx context.Context, query string, args []any) (handle.Rows, error) {
	return h.Base.QueryContext(ctx, query, args...) //nolint:rowserrcheck // the caller does the check
}

type wrappedStatement struct {
	db    sqlExecutor
	query string
	stmt  *sql.Stmt // nil if repeated = false
}

// Close implements the [Statement] interface.
func (s wrappedStatement) Close() error {
	if s.stmt == nil {
		return nil
	}
	return s.stmt.Close()
}

// Exec implements the [Statement] interface.
func (s wrappedStatement) Exec(ctx context.Context, args []any) (sql.Result, error) {
	if s.stmt == nil {
		return s.db.ExecContext(ctx, s.query, args...)
	} else {
		return s.stmt.ExecContext(ctx, args...)
	}
}

// QueryRow implements the [Statement] interface.
func (s wrappedStatement) QueryRow(ctx context.Context, args, slots []any) error {
	if s.stmt == nil {
		return s.db.QueryRowContext(ctx, s.query, args...).Scan(slots...)
	} else {
		return s.stmt.QueryRowContext(ctx, args...).Scan(slots...)
	}
}
