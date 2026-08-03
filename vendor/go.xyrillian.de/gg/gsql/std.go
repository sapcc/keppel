// SPDX-FileCopyrightText: 2026 Stefan Majewsky <majewsky@gmx.net>
// SPDX-License-Identifier: Apache-2.0

package gsql

import (
	"context"
	"database/sql"
	"fmt"

	"go.xyrillian.de/gg/errext"
)

// NOTE: The internal structure of these types looks weird at first glance, with
// the pointer to the underlying instance duplicated, but of course that's deliberate.
//
// If our types implemented [Handle] directly, every function call taking them as an argument
// of type [Handle] would allocate a new fat pointer when converting from e.g. [*DB]
// at the callsite to [Handle] in the argument value.
//
// To circumvent this, our types only _have_ [Handle] instances within them within them
// as an embedded field, thus implementing [Handle] indirectly instead of directly.

// DB wraps [*sql.DB] into a [Handle].
//
// Because this type has [*sql.DB] as an embedded field,
// all methods from that type work on this type as well.
type DB struct {
	*sql.DB
	ConnectionHandle
}

// NewDB wraps an instance of [*sql.DB] into the [DB] type that implements [Handle].
func NewDB(db *sql.DB) *DB {
	return &DB{db, sqlConnectionHandle[*sql.DB]{sqlHandle[*sql.DB]{db}}}
}

// Begin is like [sql.DB.Begin], but wraps the resulting transaction into a [Handle].
func (db *DB) Begin() (*Tx, error) {
	tx, err := db.DB.Begin()
	return maybe(NewTx, tx), err
}

// BeginTx is like [sql.DB.BeginTx], but wraps the resulting transaction into a [Handle].
func (db *DB) BeginTx(ctx context.Context, opts *sql.TxOptions) (*Tx, error) {
	tx, err := db.DB.BeginTx(ctx, opts)
	return maybe(NewTx, tx), err
}

// Conn is like [sql.DB.Conn], but wraps the resulting connection into a [Handle].
func (db *DB) Conn(ctx context.Context) (*Conn, error) {
	conn, err := db.DB.Conn(ctx)
	return maybe(NewConn, conn), err
}

// WithinTransaction executes an action within a database transaction.
// The transaction will be committed if the callback returns successfully, or rolled back otherwise.
//
// This is equivalent to the GSQLTransact() method of the DB's [ConnectionHandle] implementation,
// but the callback receives the concrete type [*Tx] instead of a generic [Handle].
func (db *DB) WithinTransaction(ctx context.Context, action func(*Tx) error) error {
	return withinTransaction(ctx, db.DB, action)
}

// Conn wraps [*sql.Conn] into a [Handle].
//
// Because this type has [*sql.Conn] as an embedded field,
// all methods from that type work on this type as well.
type Conn struct {
	*sql.Conn
	ConnectionHandle
}

// NewConn wraps an instance of [*sql.Conn] into the [Conn] type that implements [Handle].
func NewConn(db *sql.Conn) *Conn {
	return &Conn{db, sqlConnectionHandle[*sql.Conn]{sqlHandle[*sql.Conn]{db}}}
}

// BeginTx is like [sql.DB.BeginTx], but wraps the resulting transaction into a [Handle].
func (conn *Conn) BeginTx(ctx context.Context, opts *sql.TxOptions) (*Tx, error) {
	tx, err := conn.Conn.BeginTx(ctx, opts)
	return maybe(NewTx, tx), err
}

// WithinTransaction executes an action within a database transaction.
// The transaction will be committed if the callback returns successfully, or rolled back otherwise.
//
// This is equivalent to the GSQLTransact() method of conn's [ConnectionHandle] implementation,
// but the callback receives the concrete type [*Tx] instead of a generic [Handle].
func (conn *Conn) WithinTransaction(ctx context.Context, action func(*Tx) error) error {
	return withinTransaction(ctx, conn.Conn, action)
}

// Tx wraps [*sql.Tx] into a [Handle].
//
// Because this type has [*sql.Tx] as an embedded field,
// all methods from that type work on this type as well.
type Tx struct {
	*sql.Tx
	Handle
}

// NewTx wraps an instance of [*sql.Tx] into the [Tx] type that implements [Handle].
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

	_ ConnectionHandle = &DB{}
	_ ConnectionHandle = &Conn{}
)

////////////////////////////////////////////////////////////////////////////////
// Handle implementation

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

// GSQLPrepare implements the [Handle] interface.
func (h sqlHandle[T]) GSQLPrepare(ctx context.Context, query string, repeated bool) (Statement, error) {
	if !repeated {
		return wrappedStatement{h.Base, query, nil}, nil
	}
	stmt, err := h.Base.PrepareContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("during Prepare(): %w", err)
	}
	return wrappedStatement{h.Base, query, stmt}, nil
}

// GSQLQuery implements the [Handle] interface.
func (h sqlHandle[T]) GSQLQuery(ctx context.Context, query string, args []any) (Rows, error) {
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

////////////////////////////////////////////////////////////////////////////////
// ConnectionHandle implementation

// sqlConnection is an interface covered by both [*sql.DB] and [*sql.Conn].Tx].
type sqlConnection interface {
	sqlExecutor
	Close() error
	BeginTx(ctx context.Context, opts *sql.TxOptions) (*sql.Tx, error)
}

// sqlConnectionHandle provides the [ConnectionHandle] implementation for any type that implements [sqlConnection].
type sqlConnectionHandle[T sqlConnection] struct {
	sqlHandle[T]
}

// GSQLClose implements the [ConnectionHandle] interface.
func (h sqlConnectionHandle[T]) GSQLClose(ctx context.Context) error {
	return h.Base.Close()
}

// GSQLTransact implements the [ConnectionHandle] interface.
func (h sqlConnectionHandle[T]) GSQLTransact(ctx context.Context, action func(tx Handle) error) error {
	return withinTransaction(ctx, h.Base, func(tx *Tx) error {
		return action(tx)
	})
}

// withinTransaction implements the method of that name that exists on all types based on [sqlConnectionHandle].
func withinTransaction(ctx context.Context, conn sqlConnection, action func(*Tx) error) error {
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	err = action(NewTx(tx))
	if err == nil {
		return errext.WithCleanup(nil, "tx.Commit", tx.Commit())
	} else {
		return errext.WithCleanup(err, "tx.Rollback", tx.Rollback())
	}
}
