/******************************************************************************
*
*  Copyright 2017-2020 SAP SE
*
*  Licensed under the Apache License, Version 2.0 (the "License");
*  you may not use this file except in compliance with the License.
*  You may obtain a copy of the License at
*
*      http://www.apache.org/licenses/LICENSE-2.0
*
*  Unless required by applicable law or agreed to in writing, software
*  distributed under the License is distributed on an "AS IS" BASIS,
*  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
*  See the License for the specific language governing permissions and
*  limitations under the License.
*
******************************************************************************/

package sqlext

import (
	"database/sql"
	"regexp"

	"github.com/sapcc/go-bits/logg"
)

// ForeachRow calls db.Query() with the given query and args, then executes the
// given action one for every row in the result set. It then cleans up the
// result set, and collects any errors that occur during all of this.
//
// Inside the action, you only have to call rows.Scan() and use the values
// obtained from it. For example:
//
//	err := sqlext.ForeachRow(tx,
//	  `SELECT value FROM metadata WHERE key = $1`, []any{"mykey"},
//	  func(rows *sql.Rows) error {
//	    var value string
//	    err := rows.Scan(&value)
//	    if err != nil {
//	      return err
//	    }
//	    logg.Info("value fetched: %q", value)
//	    return nil
//	  },
//	)
func ForeachRow(db Executor, query string, args []any, action func(*sql.Rows) error) (returnedErr error) {
	rows, err := db.Query(query, args...)
	if err != nil {
		return err
	}
	defer func() {
		err := rows.Close()
		if returnedErr == nil {
			returnedErr = err
		}
	}()
	for rows.Next() {
		err = action(rows)
		if err != nil {
			return err
		}
	}
	return rows.Err()
}

// RollbackUnlessCommitted calls Rollback() on a transaction if it hasn't been
// committed or rolled back yet. Use this with the defer keyword to make sure
// that a transaction is automatically rolled back when a function fails.
func RollbackUnlessCommitted(tx Rollbacker) {
	err := tx.Rollback()
	switch err {
	case nil:
		//rolled back successfully
		logg.Info("implicit rollback done")
		return
	case sql.ErrTxDone:
		//already committed or rolled back - nothing to do
		return
	default:
		logg.Error("implicit rollback failed: %s", err.Error())
	}
}

var sqlWhitespaceOrCommentRx = regexp.MustCompile(`\s+(?m:--.*$)?`)

// SimplifyWhitespace takes an SQL query string that's hardcoded in the program
// and simplifies all the whitespaces, esp. ensuring that there are no comments
// and newlines. This makes the database log nicer when queries are logged there
// (e.g. for running too long), while still allowing nice multi-line formatting
// and inline comments in the source code.
func SimplifyWhitespace(query string) string {
	return sqlWhitespaceOrCommentRx.ReplaceAllString(query, " ")
}

// WithPreparedStatement calls db.Prepare() and passes the resulting prepared
// statement into the given action. It then cleans up the prepared statements,
// and it collects any errors that occur during all of this.
//
// Inside the action, you only have to call stmt.Exec() as often as you need.
// For example:
//
//	var someData map[string]string
//	err := sqlext.WithPreparedStatement(tx,
//	  `INSERT INTO datatable (key, value) VALUES ($1, $2)`,
//	  func(stmt *sql.Stmt) error {
//	    for k, v := range someData {
//	      err := stmt.Exec(k, v)
//	      if err != nil {
//	        return err
//	      }
//	    }
//	    return nil
//	  },
//	)
func WithPreparedStatement(db Executor, query string, action func(*sql.Stmt) error) (returnedErr error) {
	stmt, err := db.Prepare(query)
	if err != nil {
		return err
	}
	defer func() {
		err := stmt.Close()
		if returnedErr == nil {
			returnedErr = err
		}
	}()
	return action(stmt)
}
