// SPDX-FileCopyrightText: 2026 Stefan Majewsky <majewsky@gmx.net>
// SPDX-License-Identifier: Apache-2.0

package gg_pgx

import (
	"database/sql"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"go.xyrillian.de/gg/gsql"
)

type wrappedRows struct {
	inner pgx.Rows
}

var _ gsql.Rows = wrappedRows{}

// Columns implements the [gsql.Rows] interface.
func (r wrappedRows) Columns() ([]string, error) {
	descriptions := r.inner.FieldDescriptions()
	result := make([]string, len(descriptions))
	for idx, desc := range descriptions {
		result[idx] = desc.Name
	}
	return result, nil
}

// Close implements the [gsql.Rows] interface.
func (r wrappedRows) Close() error {
	r.inner.Close()
	return r.inner.Err()
}

// Err implements the [gsql.Rows] interface.
func (r wrappedRows) Err() error {
	return r.inner.Err()
}

// Next implements the [gsql.Rows] interface.
func (r wrappedRows) Next() bool {
	return r.inner.Next()
}

// Scan implements the [gsql.Rows] interface.
func (r wrappedRows) Scan(args ...any) error {
	return r.inner.Scan(args...)
}

type wrappedResult struct {
	inner pgconn.CommandTag
}

var _ sql.Result = wrappedResult{}

// LastInsertId implements the [sql.Result] interface.
func (r wrappedResult) LastInsertId() (int64, error) {
	return 0, errors.New("PostgreSQL does not support LastInsertId()")
}

// LastInsertId implements the [sql.Result] interface.
func (r wrappedResult) RowsAffected() (int64, error) {
	return r.inner.RowsAffected(), nil
}
