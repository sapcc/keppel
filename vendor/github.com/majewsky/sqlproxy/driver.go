/*******************************************************************************
*
* Copyright 2017 Stefan Majewsky <majewsky@gmx.net>
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

/*

Package sqlproxy provides a database/sql driver that adds hooks to an existing
SQL driver. For example, to augment a PostgreSQL driver with statement logging:

	//this assumes that a "postgresql" driver is already registered
	sql.Register("postgres-with-logging", &sqlproxy.Driver {
		ProxiedDriverName: "postgresql",
		BeforeQueryHook: func(query string, args[]interface{}) {
			log.Printf("SQL: %s %#v", query, args)
		},
	})

There's also a BeforePrepareHook that can be used to reject or edit query
strings.

Caveats

Do not use this code on production databases. This package is intended for
development purposes only, and access to it should remain behind a debugging
switch. It only implements the bare necessities of the database/sql driver
interface and hides optimizations and advanced features of the proxied SQL
driver.

*/
package sqlproxy

import (
	"database/sql"
	"database/sql/driver"
	"io"
	"time"
)

////////////////////////////////////////////////////////////////////////////////
// driver

//Driver implements sql.Driver. See package documentation for details.
type Driver struct {
	//ProxiedDriverName identifies the SQL driver which will be used to actually
	//perform SQL queries.
	ProxiedDriverName string
	//BeforePrepareHook (optional) runs just before a query is prepared (both for
	//explicit Prepare() calls and one-off queries). The return value will be
	//substituted for the original query string, allowing the hook to rewrite
	//queries arbitrarily. If an error is returned, it will be propagated to the
	//caller of db.Prepare() or tx.Prepare() etc.
	BeforePrepareHook func(query string) (string, error)
	//BeforeQueryHook (optional) runs just before a query is executed, e.g. by
	//the Exec(), Query() or QueryRows() methods of sql.DB, sql.Tx and sql.Stmt.
	BeforeQueryHook func(query string, args []interface{})
}

//Open implements the Driver interface.
func (d *Driver) Open(dataSource string) (driver.Conn, error) {
	db, err := sql.Open(d.ProxiedDriverName, dataSource)
	if err != nil {
		return nil, err
	}
	return &connection{d, db}, nil
}

////////////////////////////////////////////////////////////////////////////////
// connection

type connection struct {
	driver *Driver
	db     *sql.DB
}

//Prepare implements the driver.Conn interface.
func (c *connection) Prepare(query string) (driver.Stmt, error) {
	var err error
	if c.driver.BeforePrepareHook != nil {
		query, err = c.driver.BeforePrepareHook(query)
		if err != nil {
			return nil, err
		}
	}
	stmt, err := c.db.Prepare(query)
	return &statement{c.driver, stmt, query}, err
}

//Close implements the driver.Conn interface.
func (c *connection) Close() error {
	return c.db.Close()
}

//Begin implements the driver.Conn interface.
func (c *connection) Begin() (driver.Tx, error) {
	tx, err := c.db.Begin()
	return tx, err
}

////////////////////////////////////////////////////////////////////////////////
// statement

type statement struct {
	driver *Driver
	stmt   *sql.Stmt
	query  string
}

//Close implements the driver.Stmt interface.
func (s *statement) Close() error {
	return s.stmt.Close()
}

//NumInput implements the driver.Stmt interface.
func (s *statement) NumInput() int {
	//FIXME: the public API of sql.Stmt does not offer that information
	return -1
}

//Exec implements the driver.Stmt interface.
func (s *statement) Exec(values []driver.Value) (driver.Result, error) {
	args := castValues(values)
	s.driver.execBeforeQueryHook(s.query, args)
	return s.stmt.Exec(args...)
}

//Query implements the driver.Stmt interface.
func (s *statement) Query(values []driver.Value) (driver.Rows, error) {
	args := castValues(values)
	s.driver.execBeforeQueryHook(s.query, args)
	rows, err := s.stmt.Query(args...)
	return &resultRows{rows}, err
}

func (d *Driver) execBeforeQueryHook(query string, args []interface{}) {
	if d.BeforeQueryHook != nil {
		d.BeforeQueryHook(query, args)
	}
}

////////////////////////////////////////////////////////////////////////////////
// rows

type resultRows struct {
	rows *sql.Rows
}

//Columns implements the driver.Rows interface.
func (r *resultRows) Columns() []string {
	result, err := r.rows.Columns()
	if err != nil {
		panic(err)
	}
	return result
}

//Close implements the driver.Rows interface.
func (r *resultRows) Close() error {
	return r.rows.Close()
}

//Next implements the driver.Rows interface.
func (r *resultRows) Next(dest []driver.Value) error {
	if !r.rows.Next() {
		return io.EOF
	}

	buffer := make([]interface{}, len(dest))
	for idx := range buffer {
		buffer[idx] = &union{Current: 0}
	}

	err := r.rows.Scan(buffer...)
	if err != nil {
		return err
	}

	for idx, val := range buffer {
		dest[idx], _ = val.(*union).Value()
	}
	return nil
}

////////////////////////////////////////////////////////////////////////////////
// utils

//Union is a type that implements sql.Scanner and driver.Valuer, and can store
//all scannable values.
type union struct {
	Current uint //0 for nil values
	Option1 int64
	Option2 float64
	Option3 bool
	Option4 []byte
	Option5 string
	Option6 time.Time
}

//Scan implements the sql.Scanner interface. It places a value in the union.
func (u *union) Scan(src interface{}) error {
	switch s := src.(type) {
	case int64:
		u.Current = 1
		u.Option1 = s
	case float64:
		u.Current = 2
		u.Option2 = s
	case bool:
		u.Current = 3
		u.Option3 = s
	case []byte:
		u.Current = 4
		u.Option4 = s
	case string:
		u.Current = 5
		u.Option5 = s
	case time.Time:
		u.Current = 6
		u.Option6 = s
	default:
		u.Current = 0
	}
	return nil
}

//Value implements the driver.Valuer interface.
func (u *union) Value() (driver.Value, error) {
	switch u.Current {
	case 1:
		return u.Option1, nil
	case 2:
		return u.Option2, nil
	case 3:
		return u.Option3, nil
	case 4:
		return u.Option4, nil
	case 5:
		return u.Option5, nil
	case 6:
		return u.Option6, nil
	default:
		return nil, nil
	}
}

func castValues(values []driver.Value) []interface{} {
	result := make([]interface{}, len(values))
	for idx, arg := range values {
		result[idx] = arg
	}
	return result
}
