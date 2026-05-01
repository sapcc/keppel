// SPDX-FileCopyrightText: 2026 Stefan Majewsky <majewsky@gmx.net>
// SPDX-License-Identifier: Apache-2.0

package oblast

import (
	"fmt"
	"strconv"
	"strings"
)

// Dialect accounts for differences between different SQL dialects
// that are relevant to query generation within Oblast.
//
// # Compatibility notice
//
// This interface may be extended, even within minor versions, when doing so is
// required to add support for new DB dialects that differ from previously
// supported dialects in unexpected ways.
type Dialect interface {
	// Placeholder returns the placeholder for the i-th query argument.
	// Most dialects use "?", but e.g. PostgreSQL uses "$1", "$2" and so on.
	// The argument numbers from 0 like a slice index.
	Placeholder(i int) string

	// QuoteIdentifier wraps the name of a column or table in quotes,
	// in order to avoid the name from being interpreted as a keyword.
	QuoteIdentifier(name string) string

	// UpsertClause generates an "ON CONFLICT" or similar clause
	// that can be appended to an INSERT query to make it fall back to
	// behave like UPDATE if a record with the same primary key already exists.
	// This is only used for record types that have a primary key.
	UpsertClause(pkColumns, otherColumns []string) string
}

// MariaDBDialect is the dialect of MariaDB 10.5+ databases.
//
// This dialect does NOT support MySQL, as well as ancient MariaDB versions (10.5 was released 2020-06-24),
// because those do not understand the "INSERT ... RETURNING" syntax.
func MariaDBDialect() Dialect {
	return mariadbDialect{}
}

type mariadbDialect struct{}

func (mariadbDialect) Placeholder(_ int) string           { return "?" }
func (mariadbDialect) QuoteIdentifier(name string) string { return "`" + name + "`" }

func (d mariadbDialect) UpsertClause(pkColumns, otherColumns []string) string {
	clauses := make([]string, max(1, len(otherColumns)))
	if len(otherColumns) == 0 {
		// we need at least one UPDATE clause; if there are no non-PK columns,
		// we can just use one of the PK columns, updating those is a safe no-op
		clauses[0] = fmt.Sprintf(`%[1]s = VALUES(%[1]s)`, d.QuoteIdentifier(pkColumns[0]))
	} else {
		for idx, name := range otherColumns {
			clauses[idx] = fmt.Sprintf(`%[1]s = VALUES(%[1]s)`, d.QuoteIdentifier(name))
		}
	}
	return ` ON DUPLICATE KEY UPDATE ` + strings.Join(clauses, ", ")
}

// PostgresDialect is the dialect of PostgreSQL databases.
func PostgresDialect() Dialect {
	return postgresDialect{}
}

type postgresDialect struct{}

func (postgresDialect) Placeholder(i int) string           { return "$" + strconv.Itoa(i+1) }
func (postgresDialect) QuoteIdentifier(name string) string { return `"` + name + `"` }

func (d postgresDialect) UpsertClause(pkColumns, otherColumns []string) string {
	quotedPkColumns := make([]string, len(pkColumns))
	for idx, name := range pkColumns {
		quotedPkColumns[idx] = d.QuoteIdentifier(name)
	}
	clauses := make([]string, len(otherColumns))
	for idx, name := range otherColumns {
		clauses[idx] = fmt.Sprintf(`%[1]s = EXCLUDED.%[1]s`, d.QuoteIdentifier(name))
	}
	if len(otherColumns) == 0 {
		return fmt.Sprintf(` ON CONFLICT (%s) DO NOTHING`, strings.Join(quotedPkColumns, ", "))
	} else {
		return fmt.Sprintf(` ON CONFLICT (%s) DO UPDATE SET %s`,
			strings.Join(quotedPkColumns, ", "), strings.Join(clauses, ", "))
	}
}

// SqliteDialect is the dialect of SQLite 3.24.0+ databases.
//
// This dialect does NOT support ancient SQLite versions (3.24.0 was released 2018-06-04)
// that do not understand the "INSERT ... RETURNING" syntax.
func SqliteDialect() Dialect {
	return sqliteDialect{}
}

type sqliteDialect struct{}

func (sqliteDialect) Placeholder(_ int) string           { return "?" }
func (sqliteDialect) QuoteIdentifier(name string) string { return `"` + name + `"` }
func (sqliteDialect) UpsertClause(pkColumns, otherColumns []string) string {
	return postgresDialect{}.UpsertClause(pkColumns, otherColumns)
}
