// SPDX-FileCopyrightText: 2026 Stefan Majewsky <majewsky@gmx.net>
// SPDX-License-Identifier: Apache-2.0

// Package oblast is an ORM library for Go, focusing specifically on just the loading and storing of records in the most efficient manner possible.
// No utilities are provided for generating DDL or managing schema migrations, or for building complex OLAP queries.
//
// # Usage pattern
//
// Oblast can load or store any struct type by matching individual fields to column names (on load) or query arguments (on store).
// Struct types that are suitable for this kind of mapping are called "record types" throughout this package documentation.
//
// To use this library, first declare a record type, and create a [Store] for it once to analyze the type and prepare the respective OLTP queries:
//
//	type LogEntry struct {
//		ID        int64     `db:"id,auto"`
//		CreatedAt time.Time `db:"created_at"`
//		Message   string    `db:"message"`
//	}
//	var logEntryStore = oblast.NewStore[LogEntry](
//		oblast.PostgresDialect(),
//		oblast.TableNameIs("log_entries"),
//		oblast.PrimaryKeyIs("id"),
//	)
//
// Then use it many times to perform load and store operations:
//
//	func doStuff(db *oblast.DB) error {
//		newEntry := LogEntry{
//			CreatedAt: time.Now(),
//			Message: "Hello World.",
//		}
//		err := logEntryStore.Insert(dbh, &newEntry)
//		if err != nil {
//			return err
//		}
//		fmt.Printf("created log entry %d", newEntry.ID)
//
//		allEntries, err := logEntryStore.SelectWhere(dbh, `created_at < NOW()`)
//		if err != nil {
//		  return err
//		}
//		fmt.Printf("there are %d log entries so far", len(allEntries))
//	}
//
// In this example, "oblast.DB" is a thin wrapper around [*sql.DB], which can be obtained with the [NewDB] function.
// A [*DB] can be used in the same way as an [*sql.DB], but if Oblast is only to be used for specific functions,
// then individual [*sql.Conn] or [*sql.Tx] instances can also be wrapped with the [NewConn] and [NewTx] functions.
//
// # Mapping rules for record types
//
// If the database column has a different name (or casing, e.g. "id" vs. "ID") than the field name, provide it in the field tag "db".
// The field tag may also contain additional options, separated from the column name by commas.
// To have Oblast ignore a field, either make it private or declare its column name as "-".
// For example:
//
//	type Example struct {
//		FirstValue  string         `db:"first_value"` // maps to DB column "first_value"
//		SecondValue string         // maps to DB column "SecondValue"
//		ThirdValue  string         `db:"third_value,auto"` // maps to DB column "third_value" with "auto" option
//		FourthValue string         `db:",auto"`            // maps to DB column "FourthValue" with "auto" option
//		Cache       map[string]any `db:"-"`                // ignored by Oblast because of column name "-"
//		action      func()         // ignored by Oblast because field is private
//	}
//
// The following field options are understood:
//   - "auto": During [Store.Insert], do not store this field's value. Instead, the database will auto-generate a value, which will be read back into the record. In SQL dialects that use [sql.Result.LastInsertId] for this (as opposed to a RETURNING clause), only at most one field per record type may have this option, and it must be of an integer type.
//
// It is possible to place mapped fields within sub-structs, including within embedded types.
// This is useful e.g. to avoid code duplication for database columns that are repeated across multiple types:
//
//	type Timestamps struct {
//		CreatedAt time.Time  `db:"created_at"`
//		UpdatedAt *time.Time `db:"updated_at"`
//		DeletedAt *time.Time `db:"deleted_at"`
//	}
//
//	type FooRecord struct {
//		ID         int64  `db:"id,auto"`
//		Name       string `db:"name"`
//		Timestamps Timestamps
//	}
//	// ... and other struct types that use type Timestamps ...
//
// This behavior may be undesirable on custom struct types that implement [sql.Scanner] and/or [driver.Valuer], or are understood by a [driver.NamedValueChecker] set up by your SQL driver.
// To keep Oblast from recursing into struct types and mapping their fields, provide an explicit `db:"..."` tag on them:
//
//	type GeoPoint struct {
//		Longitude, Latitude int
//	}
//	func (p *GeoPoint) Scan(src any) error {...}
//	func (p GeoPoint) Value() (driver.Value, error) {...}
//
//	type Event struct {
//		ID          int64 `db:",auto"`
//		Description string
//		Time        time.Time
//		// explicit tag ensures that Location.Longitude and Location.Latitude are not mapped individually
//		Location    GeoPoint `db:"Location"`
//	}
package oblast // import "go.xyrillian.de/oblast"

import (
	"database/sql"
	"database/sql/driver"
	"fmt"
	"reflect"
)

var (
	// the following types appear in docstring links
	_ sql.Scanner              = nil
	_ driver.NamedValueChecker = nil
)

// PlanOption is an option that can be given to [NewStore] to influence query planning for a certain type of record.
type PlanOption func(*planOpts)

// TableNameIs is a PlanOption for record types that correspond to exactly one database table (as opposed to a join of multiple tables).
// This option is required to enable any of the methods of [Store] that use partially or fully auto-generated query strings.
func TableNameIs(name string) PlanOption {
	return func(opts *planOpts) { opts.TableName = name }
}

// PrimaryKeyIs is a PlanOption for record types that correspond to a database table with a primary key.
// This option is required to enable use of the [Store.Update] and [Store.Delete] methods.
func PrimaryKeyIs(columnNames ...string) PlanOption {
	return func(opts *planOpts) { opts.PrimaryKeyColumnNames = columnNames }
}

// StructTagKeyIs is a PlanOption for record types that allows renaming the struct tag key that Oblast inspects from its default value of "db".
// For example, providing StructTagKeyIs("oblast") means that a struct tag like `db:",auto"` must be written as `oblast:",auto"` instead.
//
// This is useful when migrating from or to another ORM library that uses the same `db:"..."` tag as Oblast, but with conflicting semantics.
func StructTagKeyIs(key string) PlanOption {
	return func(opts *planOpts) { opts.StructTagKey = key }
}

// Store holds information on how to read and write data into record type R,
// and can also be used to execute autogenerated queries if the respective [PlanOption] values were provided during [NewStore].
type Store[R any] struct {
	plan plan
}

// NewStore initializes a store for record type R.
// Returns an error if R is not a struct type.
//
// In most situations, the intended usage pattern is to call NewStore (or [MustNewStore]) once per record type,
// and hold the result in a global variable.
//
// When dealing with private one-off record types that are declared within the function or method using them,
// NewStore (or [MustNewStore]) may also be called once per function call.
// NewStore will internally cache its results and return a cheap copy on subsequent calls with the same arguments,
// only incurring the cost of a read lock on a mutex.
func NewStore[R any](dialect Dialect, opts ...PlanOption) (Store[R], error) {
	plan, err := getOrBuildPlan(reflect.TypeFor[R](), dialect, collectPlanOptions(opts))
	if err != nil {
		var zero R
		return Store[R]{}, fmt.Errorf("cannot use type %T for queries: %w", zero, err)
	}
	return Store[R]{plan}, err
}

// MustNewStore is like [NewStore], but panics on error.
func MustNewStore[R any](dialect Dialect, opts ...PlanOption) Store[R] {
	store, err := NewStore[R](dialect, opts...)
	if err != nil {
		panic(err.Error())
	}
	return store
}
