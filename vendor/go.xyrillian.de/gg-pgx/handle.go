// SPDX-FileCopyrightText: 2026 Stefan Majewsky <majewsky@gmx.net>
// SPDX-License-Identifier: Apache-2.0

package gg_pgx

import (
	"context"
	"strconv"
	"sync/atomic"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.xyrillian.de/gg/errext"
	"go.xyrillian.de/gg/gsql"
)

type connHandle struct {
	inner *pgx.Conn
}

// GSQLPrepare implements the [gsql.Handle] interface.
func (h connHandle) GSQLPrepare(ctx context.Context, query string, repeated bool) (gsql.Statement, error) {
	if !repeated {
		return wrappedUnpreparedStatement{query, h.inner}, nil
	}
	name := getPreparedStatementName()
	stmt, err := h.inner.Prepare(ctx, name, query)
	return wrappedPreparedStatement{ctx, stmt, h.inner, h.inner.Deallocate}, err
}

// GSQLQuery implements the [gsql.Handle] interface.
func (h connHandle) GSQLQuery(ctx context.Context, query string, args []any) (gsql.Rows, error) {
	rows, err := h.inner.Query(ctx, query, args...)
	return wrappedRows{rows}, err
}

// GSQLClose implements the [gsql.ConnectionHandle] interface.
func (h connHandle) GSQLClose(ctx context.Context) error {
	return h.inner.Close(ctx)
}

// GSQLTransact implements the [gsql.ConnectionHandle] interface.
func (h connHandle) GSQLTransact(ctx context.Context, action func(tx gsql.Handle) error) error {
	return withinTransactionOfConn(ctx, h.inner, func(tx *Tx) error {
		return action(tx)
	})
}

func withinTransactionOfConn(ctx context.Context, conn *pgx.Conn, action func(tx *Tx) error) error {
	tx, err := conn.Begin(ctx)
	if err != nil {
		return err
	}
	return transact(ctx, tx, action)
}

////////////////////////////////////////////////////////////////////////////////

type poolHandle struct {
	inner *pgxpool.Pool
}

// GSQLPrepare implements the [gsql.Handle] interface.
func (h poolHandle) GSQLPrepare(ctx context.Context, query string, repeated bool) (gsql.Statement, error) {
	if !repeated {
		return wrappedUnpreparedStatement{query, h.inner}, nil
	}
	name := getPreparedStatementName()

	// while we have a prepared statement, we also need to hold an acquired connection
	conn, err := h.inner.Acquire(ctx)
	if err != nil {
		return nil, err
	}
	stmt, err := conn.Conn().Prepare(ctx, name, query)
	if err != nil {
		conn.Release()
		return nil, err
	}
	deallocate := func(ctx context.Context, name string) error {
		err := conn.Conn().Deallocate(ctx, name)
		conn.Release()
		return err
	}
	return wrappedPreparedStatement{ctx, stmt, conn, deallocate}, nil
}

// GSQLQuery implements the [gsql.Handle] interface.
func (h poolHandle) GSQLQuery(ctx context.Context, query string, args []any) (gsql.Rows, error) {
	rows, err := h.inner.Query(ctx, query, args...)
	return wrappedRows{rows}, err
}

// GSQLClose implements the [gsql.ConnectionHandle] interface.
func (h poolHandle) GSQLClose(ctx context.Context) error {
	h.inner.Close()
	return nil
}

// GSQLTransact implements the [gsql.ConnectionHandle] interface.
func (h poolHandle) GSQLTransact(ctx context.Context, action func(tx gsql.Handle) error) error {
	return withinTransactionOfPool(ctx, h.inner, func(tx *Tx) error {
		return action(tx)
	})
}

func withinTransactionOfPool(ctx context.Context, pool *pgxpool.Pool, action func(tx *Tx) error) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	return transact(ctx, tx, action)
}

////////////////////////////////////////////////////////////////////////////////

type poolConnHandle struct {
	inner *pgxpool.Conn
}

// GSQLPrepare implements the [gsql.Handle] interface.
func (h poolConnHandle) GSQLPrepare(ctx context.Context, query string, repeated bool) (gsql.Statement, error) {
	if !repeated {
		return wrappedUnpreparedStatement{query, h.inner}, nil
	}
	name := getPreparedStatementName()
	stmt, err := h.inner.Conn().Prepare(ctx, name, query)
	return wrappedPreparedStatement{ctx, stmt, h.inner, h.inner.Conn().Deallocate}, err
}

// GSQLQuery implements the [gsql.Handle] interface.
func (h poolConnHandle) GSQLQuery(ctx context.Context, query string, args []any) (gsql.Rows, error) {
	rows, err := h.inner.Query(ctx, query, args...)
	return wrappedRows{rows}, err
}

// GSQLClose implements the [gsql.ConnectionHandle] interface.
func (h poolConnHandle) GSQLClose(ctx context.Context) error {
	h.inner.Release()
	return nil
}

// GSQLTransact implements the [gsql.ConnectionHandle] interface.
func (h poolConnHandle) GSQLTransact(ctx context.Context, action func(tx gsql.Handle) error) error {
	return withinTransactionOfPoolConn(ctx, h.inner, func(tx *Tx) error {
		return action(tx)
	})
}

func withinTransactionOfPoolConn(ctx context.Context, conn *pgxpool.Conn, action func(tx *Tx) error) error {
	tx, err := conn.Begin(ctx)
	if err != nil {
		return err
	}
	return transact(ctx, tx, action)
}

////////////////////////////////////////////////////////////////////////////////

type txHandle struct {
	inner pgx.Tx
}

// GSQLPrepare implements the [gsql.Handle] interface.
func (h txHandle) GSQLPrepare(ctx context.Context, query string, repeated bool) (gsql.Statement, error) {
	if !repeated {
		return wrappedUnpreparedStatement{query, h.inner}, nil
	}
	name := getPreparedStatementName()
	stmt, err := h.inner.Conn().Prepare(ctx, name, query)
	return wrappedPreparedStatement{ctx, stmt, h.inner, h.inner.Conn().Deallocate}, err
}

// GSQLQuery implements the [gsql.Handle] interface.
func (h txHandle) GSQLQuery(ctx context.Context, query string, args []any) (gsql.Rows, error) {
	rows, err := h.inner.Query(ctx, query, args...)
	return wrappedRows{rows}, err
}

////////////////////////////////////////////////////////////////////////////////

var preparedStatementId atomic.Uint64

func getPreparedStatementName() string {
	return "oblast_pgx_" + strconv.FormatUint(preparedStatementId.Add(1), 10)
}

func transact(ctx context.Context, tx pgx.Tx, action func(tx *Tx) error) error {
	err := action(NewTx(tx))
	if err == nil {
		return errext.WithCleanup(nil, "tx.Commit", tx.Commit(ctx))
	} else {
		return errext.WithCleanup(err, "tx.Rollback", tx.Rollback(ctx))
	}
}
