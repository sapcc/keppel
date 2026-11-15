// SPDX-FileCopyrightText: 2026 Stefan Majewsky <majewsky@gmx.net>
// SPDX-License-Identifier: Apache-2.0

package gg_pgx

import (
	"context"
	"database/sql"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.xyrillian.de/gg/gsql"
)

type pgxExecutor interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) // TODO: remove after splitting Handle types
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

var (
	_ pgxExecutor = &pgx.Conn{}
	_ pgxExecutor = &pgxpool.Conn{}
	_ pgxExecutor = pgx.Tx(&pgxpool.Tx{})
)

type wrappedPreparedStatement struct {
	ctx        context.Context
	statement  *pgconn.StatementDescription
	executor   pgxExecutor
	deallocate func(ctx context.Context, name string) error
}

type wrappedUnpreparedStatement struct {
	query    string
	executor pgxExecutor
}

var (
	_ gsql.Statement = wrappedPreparedStatement{}
	_ gsql.Statement = wrappedUnpreparedStatement{}
)

// Close implements the [gsql.Statement] interface.
func (s wrappedPreparedStatement) Close() error {
	return s.deallocate(s.ctx, s.statement.Name)
}

// Close implements the [gsql.Statement] interface.
func (s wrappedUnpreparedStatement) Close() error {
	return nil
}

// Exec implements the [gsql.Statement] interface.
func (s wrappedPreparedStatement) Exec(ctx context.Context, args []any) (sql.Result, error) {
	result, err := s.executor.Exec(ctx, s.statement.Name, args...)
	return wrappedResult{result}, err
}

// Exec implements the [gsql.Statement] interface.
func (s wrappedUnpreparedStatement) Exec(ctx context.Context, args []any) (sql.Result, error) {
	result, err := s.executor.Exec(ctx, s.query, args...)
	return wrappedResult{result}, err
}

// QueryRow implements the [gsql.Statement] interface.
func (s wrappedPreparedStatement) QueryRow(ctx context.Context, args, slots []any) error {
	return s.executor.QueryRow(ctx, s.statement.Name, args...).Scan(slots...)
}

// QueryRow implements the [gsql.Statement] interface.
func (s wrappedUnpreparedStatement) QueryRow(ctx context.Context, args, slots []any) error {
	return s.executor.QueryRow(ctx, s.query, args...).Scan(slots...)
}
