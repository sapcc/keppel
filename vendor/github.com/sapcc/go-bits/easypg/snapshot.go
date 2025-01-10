/*******************************************************************************
*
* Copyright 2021 SAP SE
*
* Licensed under the Apache License, Version 2.0 (the "License");
* you may not use this file except in compliance with the License.
* You should have received a copy of the License along with this
* program. If not, you may obtain a copy of the License at
*
*     http://www.apache.org/licenses/LICENSE-2.0
*
* Unless required by applicable law or agreed to in writing, software
* distributed under the License is distributed on an "AS IS" BASIS,
* WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
* See the License for the specific language governing permissions and
* limitations under the License.
*
*******************************************************************************/

package easypg

import (
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"time"
)

//NOTE: This file contains various private types for taking and diffing
// database snapshots and serializing them into SQL statements. The public API
// for these types is in `testhelpers.go`.

////////////////////////////////////////////////////////////////////////////////
// type dbSnapshot

// dbSnapshot is a map with table names as keys and table snapshots as values.
type dbSnapshot map[string]tableSnapshot

const (
	listAllTablesQuery = `
		SELECT table_name FROM information_schema.tables
		WHERE table_schema = 'public' AND table_name != 'schema_migrations'
		ORDER BY table_name COLLATE "C"
	`
	listKeyColumnsQuery = `
		SELECT table_name, column_name FROM information_schema.key_column_usage
		WHERE table_schema = 'public' AND table_name != 'schema_migrations' AND position_in_unique_constraint IS NULL
	`
	listColumnDefaultsQuery = `
		SELECT table_name, column_name, is_nullable, column_default FROM information_schema.columns
		WHERE table_schema = 'public' AND table_name != 'schema_migrations'
	`
)

func newDBSnapshot(t TestingT, db *sql.DB) dbSnapshot {
	t.Helper()

	// list all tables
	var tableNames []string
	rows, err := db.Query(listAllTablesQuery)
	failOnErr(t, err)
	for rows.Next() {
		var name string
		failOnErr(t, rows.Scan(&name))
		tableNames = append(tableNames, name)
	}
	failOnErr(t, rows.Err())
	failOnErr(t, rows.Close()) //nolint:sqlclosecheck

	// list key columns for all tables
	keyColumnNames := make(map[string][]string)
	rows, err = db.Query(listKeyColumnsQuery)
	failOnErr(t, err)
	for rows.Next() {
		var (
			tableName  string
			columnName string
		)
		failOnErr(t, rows.Scan(&tableName, &columnName))
		keyColumnNames[tableName] = append(keyColumnNames[tableName], columnName)
	}
	failOnErr(t, rows.Err())
	failOnErr(t, rows.Close()) //nolint:sqlclosecheck

	// list column default values for all tables
	columnDefaults := make(map[string]map[string]string)
	rows, err = db.Query(listColumnDefaultsQuery)
	failOnErr(t, err)
	for rows.Next() {
		var (
			tableName     string
			columnName    string
			isNullableStr string // either "NO" or "YES"
			defaultExpr   *string
		)
		failOnErr(t, rows.Scan(&tableName, &columnName, &isNullableStr, &defaultExpr))
		if columnDefaults[tableName] == nil {
			columnDefaults[tableName] = make(map[string]string)
		}

		switch {
		case defaultExpr == nil || *defaultExpr == "":
			if isNullableStr == "YES" {
				columnDefaults[tableName][columnName] = "NULL"
			} else {
				// no default value and NOT NULL, i.e. a value must always be supplied explicitly
				continue
			}
		case strings.Contains(*defaultExpr, "nextval"):
			// default value is computed from a sequence object and must always be snapshotted
			continue
		default:
			columnDefaults[tableName][columnName] = computeValueOfSQLExpression(t, db, *defaultExpr).Serialized
		}
	}
	failOnErr(t, rows.Err())
	failOnErr(t, rows.Close()) //nolint:sqlclosecheck

	// snapshot all tables
	result := make(dbSnapshot, len(tableNames))
	for _, tableName := range tableNames {
		result[tableName] = newTableSnapshot(t, db, tableName, keyColumnNames[tableName], columnDefaults[tableName])
	}
	return result
}

// ToSQL returns a set of SQL statements that reproduce this snapshot when
// starting from `prev`. If `prev` is nil, only INSERT statements will be
// returned.
func (d dbSnapshot) ToSQL(prev dbSnapshot) string {
	tableNames := make([]string, len(d))
	for tableName := range d {
		tableNames = append(tableNames, tableName)
	}
	sort.Strings(tableNames)

	results := make([]string, 0, len(tableNames))
	for _, tableName := range tableNames {
		var result string
		tPrev, exists := prev[tableName]
		if exists {
			result = d[tableName].ToSQL(tableName, &tPrev)
		} else {
			result = d[tableName].ToSQL(tableName, nil)
		}
		if result != "" {
			results = append(results, result)
		}
	}
	return strings.Join(results, "\n")
}

////////////////////////////////////////////////////////////////////////////////
// type tableSnapshot

// tableSnapshot contains the state of a table.
type tableSnapshot struct {
	ColumnNames    []string
	KeyColumnNames []string
	ColumnDefaults map[string]string
	// The map key is computed by rowSnapshot.Key().
	Rows map[string]rowSnapshot
}

func newTableSnapshot(t TestingT, db *sql.DB, tableName string, keyColumnNames []string, columnDefaults map[string]string) tableSnapshot {
	t.Helper()

	rows, err := db.Query(`SELECT * FROM ` + tableName) //nolint:gosec // cannot provide tableName as bind parameter
	failOnErr(t, err)
	columnNames, err := rows.Columns()
	failOnErr(t, err)

	// if there is no primary key or uniqueness constraint, use all columns as key
	// (this means that diffs will only ever show INSERT and DELETE, not UPDATE)
	if len(keyColumnNames) == 0 {
		keyColumnNames = columnNames
	}
	// sort key columns in the same order as the columns themselves (this plays
	// into sorting of keys and thus sorting of rows later on)
	idxOfColumnName := make(map[string]int, len(columnNames))
	for idx, columnName := range columnNames {
		idxOfColumnName[columnName] = idx
	}
	sort.Slice(keyColumnNames, func(i, j int) bool {
		return idxOfColumnName[keyColumnNames[i]] < idxOfColumnName[keyColumnNames[j]]
	})

	result := tableSnapshot{
		ColumnNames:    columnNames,
		KeyColumnNames: keyColumnNames,
		ColumnDefaults: columnDefaults,
		Rows:           make(map[string]rowSnapshot),
	}

	scanTarget := make([]any, len(columnNames))
	for idx := range scanTarget {
		scanTarget[idx] = &sqlValueSerializer{}
	}

	for rows.Next() {
		failOnErr(t, rows.Scan(scanTarget...))
		row := make(rowSnapshot, len(columnNames))
		for idx, columnName := range columnNames {
			row[columnName] = scanTarget[idx].(*sqlValueSerializer).Serialized
		}
		result.Rows[row.Key(result.KeyColumnNames)] = row
	}
	failOnErr(t, rows.Err())
	failOnErr(t, rows.Close()) //nolint:sqlclosecheck

	return result
}

// ToSQL returns a set of SQL statements that reproduce this snapshot when
// starting from `prev`. If `prev` is nil, only INSERT statements will be
// returned.
func (t tableSnapshot) ToSQL(tableName string, prev *tableSnapshot) string {
	allRowKeys := make([]string, 0, len(t.Rows))
	for key := range t.Rows {
		allRowKeys = append(allRowKeys, key)
	}
	if prev != nil {
		for key := range prev.Rows {
			if _, exists := t.Rows[key]; !exists {
				allRowKeys = append(allRowKeys, key)
			}
		}
	}
	sort.Strings(allRowKeys)

	results := make([]string, len(allRowKeys))
	for idx, key := range allRowKeys {
		if prev == nil || prev.Rows[key] == nil {
			results[idx] = t.Rows[key].ToSQLInsert(tableName, t.ColumnNames, t.ColumnDefaults)
			continue
		}
		currRow := t.Rows[key]
		if currRow == nil {
			results[idx] = fmt.Sprintf("DELETE FROM %s WHERE %s;\n", tableName, key)
			continue
		}
		columnDiff := currRow.ToSQLUpdateSet(t.ColumnNames, prev.Rows[key])
		if columnDiff != "" {
			results[idx] = fmt.Sprintf("UPDATE %s SET %s WHERE %s;\n", tableName, columnDiff, key)
		}
	}

	return strings.Join(results, "")
}

////////////////////////////////////////////////////////////////////////////////
// type rowSnapshot

// rowSnapshot is a map with column names as keys and SQL literals as values.
type rowSnapshot map[string]string

// Key returns a serialization of this row's key column values as a SQL
// condition (e.g. `foo_id = 1 AND name = 'bar'`).
func (r rowSnapshot) Key(keyColumnNames []string) string {
	exprs := make([]string, len(keyColumnNames))
	for idx, columnName := range keyColumnNames {
		exprs[idx] = fmt.Sprintf("%s = %s", columnName, r[columnName])
	}
	return strings.Join(exprs, " AND ")
}

// ToSQLInsert renders an INSERT statement that reproduces this row.
func (r rowSnapshot) ToSQLInsert(tableName string, columnNames []string, columnDefaults map[string]string) string {
	displayColumnNames := make([]string, 0, len(columnNames))
	displayValues := make([]string, 0, len(columnNames))
	for _, columnName := range columnNames {
		// values are only displayed if they deviate from the column's default value (or if there is no default value)
		if columnDefaults[columnName] == "" || columnDefaults[columnName] != r[columnName] {
			displayColumnNames = append(displayColumnNames, columnName)
			displayValues = append(displayValues, r[columnName])
		}
	}

	return fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s);\n",
		tableName,
		strings.Join(displayColumnNames, ", "),
		strings.Join(displayValues, ", "),
	)
}

// ToSQLUpdateSet renders the SET part of an UPDATE statement that produces this row out of `prev`.
func (r rowSnapshot) ToSQLUpdateSet(columnNames []string, prev rowSnapshot) string {
	var results []string
	for _, columnName := range columnNames {
		value := r[columnName]
		if prev[columnName] != value {
			results = append(results, fmt.Sprintf("%s = %s", columnName, value))
		}
	}
	return strings.Join(results, ", ")
}

////////////////////////////////////////////////////////////////////////////////
// type sqlValueSerializer

type sqlValueSerializer struct {
	Serialized string
}

// Scan implements the sql.Scanner interface.
func (s *sqlValueSerializer) Scan(src any) error {
	switch val := src.(type) {
	case int64:
		s.Serialized = fmt.Sprintf("%#v", val)
	case float64:
		s.Serialized = fmt.Sprintf("%#v", val)
	case bool:
		s.Serialized = "FALSE"
		if val {
			s.Serialized = "TRUE"
		}
	case []byte:
		s.Serialized = fmt.Sprintf("'%s'", string(val))
	case string:
		s.Serialized = fmt.Sprintf("'%s'", val)
	case time.Time:
		s.Serialized = fmt.Sprintf("%#v", val.Unix())
	default:
		s.Serialized = "NULL"
	}
	return nil
}

////////////////////////////////////////////////////////////////////////////////
// func computeValueOfSQLExpression

var sqlExpressionCache = make(map[string]sqlValueSerializer)

func computeValueOfSQLExpression(t TestingT, db *sql.DB, expr string) (result sqlValueSerializer) {
	t.Helper()

	result, exists := sqlExpressionCache[expr]
	if !exists {
		failOnErr(t, db.QueryRow("SELECT "+expr).Scan(&result))
		sqlExpressionCache[expr] = result
	}
	return result
}
