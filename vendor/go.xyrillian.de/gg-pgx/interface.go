// SPDX-FileCopyrightText: 2026 Stefan Majewsky <majewsky@gmx.net>
// SPDX-License-Identifier: Apache-2.0

// Package gg_pgx contains integration for using [gg/gsql] (and libraries based on it, such as [Oblast]) with the PostgreSQL driver library [pgx].
//
// [gg/gsql]: https://pkg.go.dev/go.xyrillian.de/gg/gsql
// [Oblast]: https://pkg.go.dev/go.xyrillian.de/oblast
// [pgx]: https://github.com/jackc/pgx
package gg_pgx

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.xyrillian.de/gg/gsql"
)

// NOTE: The internal structure of these types follows the pattern established
//       by (and explained in) `std.go` of `go.xyrillian.de/gg/gsql`.

// Conn wraps [*pgx.Conn] into a [gsql.Handle].
//
// Because this type has [*pgx.Conn] as an embedded field,
// all methods from that type work on this type as well.
type Conn struct {
	*pgx.Conn
	gsql.ConnectionHandle
}

// NewConn wraps an instance of [*pgx.Conn] into the [Conn] type that implements [gsql.Handle].
func NewConn(conn *pgx.Conn) *Conn {
	return &Conn{conn, connHandle{conn}}
}

// Begin is like [pgx.Conn.Begin], but wraps the resulting transaction into a [gsql.Handle].
func (conn *Conn) Begin(ctx context.Context) (*Tx, error) {
	tx, err := conn.Conn.Begin(ctx)
	return maybeNewTx(tx), err
}

// BeginTx is like [pgx.Conn.BeginTx], but wraps the resulting transaction into a [gsql.Handle].
func (conn *Conn) BeginTx(ctx context.Context, opts pgx.TxOptions) (*Tx, error) {
	tx, err := conn.Conn.BeginTx(ctx, opts)
	return maybeNewTx(tx), err
}

// WithinTransaction executes an action within a database transaction.
// The transaction will be committed if the callback returns successfully, or rolled back otherwise.
//
// This is equivalent to the GSQLTransact() method of conn's [gsql.ConnectionHandle] implementation,
// but the callback receives the concrete type [*Tx] instead of a generic [gsql.Handle].
func (conn *Conn) WithinTransaction(ctx context.Context, action func(*Tx) error) error {
	return withinTransactionOfConn(ctx, conn.Conn, action)
}

// Pool wraps [*pgxpool.Pool] into a [gsql.Handle].
//
// Because this type has [*pgxpool.Pool] as an embedded field,
// all methods from that type work on this type as well.
type Pool struct {
	*pgxpool.Pool
	gsql.ConnectionHandle
}

// NewPool wraps an instance of [*pgxpool.Pool] into the [Pool] type that implements [gsql.Handle].
func NewPool(pool *pgxpool.Pool) *Pool {
	return &Pool{pool, poolHandle{pool}}
}

// Acquire is like [pgxpool.Pool.Acquire], but wraps the resulting connection into a [gsql.Handle].
func (pool *Pool) Acquire(ctx context.Context) (*PoolConn, error) {
	conn, err := pool.Pool.Acquire(ctx)
	return maybe(NewPoolConn, conn), err
}

// AcquireAllIdle is like [pgxpool.Pool.AcquireAllIdle], but wraps the resulting connections into [gsql.Handle] instances.
func (pool *Pool) AcquireAllIdle(ctx context.Context) []*PoolConn {
	conns := pool.Pool.AcquireAllIdle(ctx)
	result := make([]*PoolConn, len(conns))
	for idx, conn := range conns {
		result[idx] = maybe(NewPoolConn, conn)
	}
	return result
}

// AcquireFunc is like [pgxpool.Pool.AcquireFunc], but wraps the resulting connection into a [gsql.Handle].
func (pool *Pool) AcquireFunc(ctx context.Context, f func(*PoolConn) error) error {
	return pool.Pool.AcquireFunc(ctx, func(conn *pgxpool.Conn) error {
		return f(maybe(NewPoolConn, conn))
	})
}

// Begin is like [pgxpool.Pool.Begin], but wraps the resulting transaction into a [gsql.Handle].
func (pool *Pool) Begin(ctx context.Context) (*Tx, error) {
	tx, err := pool.Pool.Begin(ctx)
	return maybeNewTx(tx), err
}

// BeginTx is like [pgxpool.Pool.BeginTx], but wraps the resulting transaction into a [gsql.Handle].
func (pool *Pool) BeginTx(ctx context.Context, opts pgx.TxOptions) (*Tx, error) {
	tx, err := pool.Pool.BeginTx(ctx, opts)
	return maybeNewTx(tx), err
}

// WithinTransaction executes an action within a database transaction.
// The transaction will be committed if the callback returns successfully, or rolled back otherwise.
//
// This is equivalent to the GSQLTransact() method of pool's [gsql.ConnectionHandle] implementation,
// but the callback receives the concrete type [*Tx] instead of a generic [gsql.Handle].
func (pool *Pool) WithinTransaction(ctx context.Context, action func(*Tx) error) error {
	return withinTransactionOfPool(ctx, pool.Pool, action)
}

// PoolConn wraps [*pgxpool.Conn] into a [gsql.Handle].
//
// Because this type has [*pgxpool.Conn] as an embedded field,
// all methods from that type work on this type as well.
type PoolConn struct {
	*pgxpool.Conn
	gsql.ConnectionHandle
}

// NewPoolConn wraps an instance of [*pgxpool.Conn] into the [PoolConn] type that implements [gsql.Handle].
func NewPoolConn(pool *pgxpool.Conn) *PoolConn {
	return &PoolConn{pool, poolConnHandle{pool}}
}

// Begin is like [pgxpool.Conn.Begin], but wraps the resulting transaction into a [gsql.Handle].
func (conn *PoolConn) Begin(ctx context.Context) (*Tx, error) {
	tx, err := conn.Conn.Begin(ctx)
	return maybeNewTx(tx), err
}

// BeginTx is like [pgxpool.Conn.BeginTx], but wraps the resulting transaction into a [gsql.Handle].
func (conn *PoolConn) BeginTx(ctx context.Context, opts pgx.TxOptions) (*Tx, error) {
	tx, err := conn.Conn.BeginTx(ctx, opts)
	return maybeNewTx(tx), err
}

// GetConn is like [pgxpool.Conn.Conn], but wraps the resulting connection into a [gsql.Handle].
//
// This method should be called "Conn", but one of the embedded fields of this type blocks that name.
func (conn *PoolConn) GetConn() *Conn {
	return maybe(NewConn, conn.Conn.Conn())
}

// Hijack is like [pgxpool.Conn.Conn], but wraps the resulting connection into a [gsql.Handle].
func (conn *PoolConn) Hijack() *Conn {
	return maybe(NewConn, conn.Conn.Hijack())
}

// WithinTransaction executes an action within a database transaction.
// The transaction will be committed if the callback returns successfully, or rolled back otherwise.
//
// This is equivalent to the GSQLTransact() method of conn's [gsql.ConnectionHandle] implementation,
// but the callback receives the concrete type [*Tx] instead of a generic [gsql.Handle].
func (conn *PoolConn) WithinTransaction(ctx context.Context, action func(*Tx) error) error {
	return withinTransactionOfPoolConn(ctx, conn.Conn, action)
}

// Tx wraps [pgx.Tx] into a [gsql.Handle].
//
// Because this type has [pgx.Tx] as an embedded field,
// all methods from that type work on this type as well.
type Tx struct {
	pgx.Tx
	gsql.Handle
}

// NewTx wraps an instance of [pgx.Tx] into the [Tx] type that implements [gsql.Handle].
func NewTx(tx pgx.Tx) *Tx {
	return &Tx{tx, txHandle{tx}}
}

// Conn is like the Conn() method of [pgx.Tx], but wraps the resulting connection into a [gsql.Handle].
func (t *Tx) Conn() *Conn {
	return maybe(NewConn, t.Tx.Conn())
}

func maybeNewTx(tx pgx.Tx) *Tx {
	if tx == nil {
		return nil
	}
	return NewTx(tx)
}

func maybe[T, U any](wrap func(*T) *U, value *T) *U {
	if value == nil {
		return nil
	}
	return wrap(value)
}
