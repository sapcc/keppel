// SPDX-FileCopyrightText: 2026 Stefan Majewsky <majewsky@gmx.net>
// SPDX-License-Identifier: Apache-2.0

// Package oblast has moved to https://pkg.go.dev/go.xyrillian.de/gg/oblast (except for type [RuntimeIndex]).
package oblast // import "go.xyrillian.de/oblast"

import (
	"context"

	"go.xyrillian.de/gg/gsql"
	gg_oblast "go.xyrillian.de/gg/oblast"
)

// Dialect has moved to gg/oblast (follow the link below).
type Dialect = gg_oblast.Dialect

// MariaDBDialect has moved to gg/oblast (follow the link below).
var MariaDBDialect = gg_oblast.MariaDBDialect

// PostgresDialect has moved to gg/oblast (follow the link below).
var PostgresDialect = gg_oblast.PostgresDialect

// SqliteDialect has moved to gg/oblast (follow the link below).
var SqliteDialect = gg_oblast.SqliteDialect

// MissingRecordError has moved to gg/oblast (follow the link below).
type MissingRecordError[R any] = gg_oblast.MissingRecordError[R]

// PlanOption has moved to gg/oblast (follow the link below).
type PlanOption = gg_oblast.PlanOption

// TableNameIs has moved to gg/oblast (follow the link below).
var TableNameIs = gg_oblast.TableNameIs

// PrimaryKeyIs has moved to gg/oblast (follow the link below).
var PrimaryKeyIs = gg_oblast.PrimaryKeyIs

// StructTagKeyIs has moved to gg/oblast (follow the link below).
var StructTagKeyIs = gg_oblast.StructTagKeyIs

// ReadOnly has moved to gg/oblast (follow the link below).
var ReadOnly = gg_oblast.ReadOnly

// Store has moved to gg/oblast (follow the link below).
type Store[R any] = gg_oblast.Store[R]

// NewStore has moved to gg/oblast (follow the link below).
func NewStore[R any](dialect Dialect, opts ...PlanOption) (Store[R], error) {
	return gg_oblast.NewStore[R](dialect, opts...)
}

// MustNewStore has moved to gg/oblast (follow the link below).
func MustNewStore[R any](dialect Dialect, opts ...PlanOption) Store[R] {
	return gg_oblast.MustNewStore[R](dialect, opts...)
}

// PreparedSelectQuery has moved to gg/oblast (follow the link below).
type PreparedSelectQuery[R any] = gg_oblast.PreparedSelectQuery[R]

// Selection has moved to gg/oblast (follow the link below).
type Selection[R any] = gg_oblast.Selection[R]

// Select has moved to gg/oblast (follow the link below).
func Select[T any](ctx context.Context, db gsql.Handle, query string, args ...any) Selection[T] {
	return gg_oblast.Select[T](ctx, db, query, args...)
}

// SelectOne has moved to gg/oblast (follow the link below).
func SelectOne[T any](ctx context.Context, db gsql.Handle, query string, args ...any) (T, error) {
	return gg_oblast.SelectOne[T](ctx, db, query, args...)
}

// TupleSelect has moved to gg/oblast (follow the link below).
func TupleSelect[R any](ctx context.Context, db gsql.Handle, query string, args ...any) Selection[R] {
	return gg_oblast.TupleSelect[R](ctx, db, query, args...)
}

// TupleSelectOne has moved to gg/oblast (follow the link below).
func TupleSelectOne[R any](ctx context.Context, db gsql.Handle, query string, args ...any) (R, error) {
	return gg_oblast.TupleSelectOne[R](ctx, db, query, args...)
}
