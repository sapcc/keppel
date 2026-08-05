// SPDX-FileCopyrightText: 2026 Stefan Majewsky <majewsky@gmx.net>
// SPDX-License-Identifier: Apache-2.0

package pgruntime

import (
	"context"
	"database/sql"
	"strings"

	"go.xyrillian.de/gg/errext"
	"go.xyrillian.de/gg/gsql"
)

// Convenience function for executing a one-off SQL query returning no rows.
// TODO: move to gsql
func execQuery(ctx context.Context, db gsql.Handle, query string, args []any) (sql.Result, error) {
	stmt, err := db.GSQLPrepare(ctx, query, false)
	if err != nil {
		return nil, err
	}
	result, err := stmt.Exec(ctx, args)
	return result, errext.WithCleanup(err, "stmt.Close", stmt.Close())
}

// Convenience function for executing a one-off SQL query returning one row.
func queryRow(ctx context.Context, db gsql.Handle, query string, args, slots []any) error {
	stmt, err := db.GSQLPrepare(ctx, query, false)
	if err != nil {
		return err
	}
	err = stmt.QueryRow(ctx, args, slots)
	return errext.WithCleanup(err, "stmt.Close", stmt.Close())
}

// Convenience function for executing a one-off SQL query returning one value.
// TODO: move to gsql
func selectOneValue[T any](ctx context.Context, db gsql.Handle, query string, args ...any) (T, error) {
	stmt, err := db.GSQLPrepare(ctx, query, false)
	if err != nil {
		var none T
		return none, err
	}

	var result T
	err = stmt.QueryRow(ctx, args, []any{&result})
	return result, errext.WithCleanup(err, "stmt.Close", stmt.Close())
}

// Convenience function for executing a one-off SQL query returning several single-column rows.
// TODO: move to gsql (and also add ForeachValue with a callback instead of a slice return, maybe even ForeachPair and ForeachTriple)
func selectSeveralValues[T any](ctx context.Context, db gsql.Handle, query string, args ...any) ([]T, error) {
	rows, err := db.GSQLQuery(ctx, query, args)
	if err != nil {
		return nil, err
	}
	var result []T
	for rows.Next() {
		// TODO: this should share growRecordSlice() from Oblast to optimize allocations
		var value T
		err := rows.Scan(&value)
		if err != nil {
			return nil, errext.WithCleanup(err, "rows.Close", rows.Close())
		}
		result = append(result, value)
	}
	return result, errext.WithCleanup(nil, "rows.Err", rows.Err())
}

// Convenience function for preparing an identifier that needs to be inserted into a query verbatim
// (e.g. a database name for CREATE DATABASE).
func quoteIdentifier(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}
