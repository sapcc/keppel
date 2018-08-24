/*******************************************************************************
*
* Copyright 2017-2018 SAP SE
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

package postlite

import (
	"database/sql"
	"database/sql/driver"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/majewsky/sqlproxy"
	sqlite "github.com/mattn/go-sqlite3"
	"github.com/sapcc/go-bits/logg"
)

func init() {
	//provide SQL functions that the sqlite3 driver needs to consume Postgres queries successfully
	toTimestamp := func(i int64) int64 {
		return i
	}

	//build a database/sql driver for SQLite that behaves more like Postgres does
	sql.Register("sqlite3-postlite-base", &sqlite.SQLiteDriver{
		ConnectHook: func(conn *sqlite.SQLiteConn) error {
			//need to enable foreign-key support (so that stuff like "ON DELETE CASCADE" works)
			_, err := conn.Exec("PRAGMA foreign_keys = ON", []driver.Value{})
			if err != nil {
				return err
			}
			return conn.RegisterFunc("to_timestamp", toTimestamp, true)
		},
	})

	sql.Register("sqlite3-postlite", &sqlproxy.Driver{
		ProxiedDriverName: "sqlite3-postlite-base", //see above
		BeforeQueryHook:   traceQuery,
		BeforePrepareHook: func(query string) (string, error) {
			//rewrite Postgres function name into SQLite function name
			query = regexp.MustCompile(`\bGREATEST\b`).ReplaceAllString(query, "MAX")
			//Postgres is okay with a no-op "WHERE TRUE" clause, but SQLite does not know the TRUE literal
			query = regexp.MustCompile(`\bWHERE TRUE\s*(GROUP|LIMIT|ORDER|$)`).ReplaceAllString(query, "$1")
			query = regexp.MustCompile(`\bWHERE TRUE AND\b`).ReplaceAllString(query, "WHERE")
			return query, nil
		},
	})
}

var sqlWhitespaceRx = regexp.MustCompile(`(?:\s|--.*)+`) // `.*` matches until end of line!

func traceQuery(query string, args []interface{}) {
	//simplify query string - remove comments and reduce whitespace
	//(This logic assumes that there are no arbitrary strings in the SQL
	//statement, which is okay since values should be given as args anyway.)
	query = strings.TrimSpace(sqlWhitespaceRx.ReplaceAllString(query, " "))

	//early exit for easy option
	if len(args) == 0 {
		logg.Debug(query)
		return
	}

	//if args contains time.Time objects, pretty-print these; use
	//fmt.Sprintf("%#v") for all other types of values
	argStrings := make([]string, len(args))
	for idx, argument := range args {
		switch arg := argument.(type) {
		case time.Time:
			argStrings[idx] = "time.Time [" + arg.Local().String() + "]"
		default:
			argStrings[idx] = fmt.Sprintf("%#v", arg)
		}
	}
	logg.Debug(query + " [" + strings.Join(argStrings, ", ") + "]")
}

var skipInSqliteRx = regexp.MustCompile(`(?ms)^\s*--\s*BEGIN\s+skip\s+in\s+sqlite\s*?$.*^\s*--\s*END\s+skip\s+in\s+sqlite\s*?$`)
var bigserialNotNullPkRx = regexp.MustCompile(`(?i)BIGSERIAL\s+NOT\s+NULL\s+PRIMARY\s+KEY`)

func translatePostgresDDLToSQLite(migrations map[string]string) map[string]string {
	result := make(map[string]string, len(migrations))
	for k, v := range migrations {
		v = skipInSqliteRx.ReplaceAllString(v, "")
		v = bigserialNotNullPkRx.ReplaceAllString(v, "INTEGER PRIMARY KEY")
		result[k] = v
	}
	return result
}
