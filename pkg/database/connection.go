/*******************************************************************************
*
* Copyright 2018 SAP SE
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

package database

import (
	"database/sql"
	"errors"
	"fmt"
	net_url "net/url"
	"os"
	"regexp"

	//enable postgres driver for database/sql
	_ "github.com/lib/pq"
	"github.com/mattes/migrate"
	"github.com/mattes/migrate/database/postgres"
	bindata "github.com/mattes/migrate/source/go-bindata"
	"github.com/sapcc/go-bits/logg"
	gorp "gopkg.in/gorp.v2"
)

//DB provides additional methods on top of gorp.DBMap.
type DB struct {
	gorp.DbMap
}

//Init initializes the Postgres connection.
func Init(url *net_url.URL) (*DB, error) {
	db, err := connectToDatabase(url)
	if err != nil {
		return nil, fmt.Errorf("cannot connect to Postgres: %s", err.Error())
	}

	err = migrateSchema(db)
	if err != nil {
		return nil, fmt.Errorf("cannot apply database schema: %s", err.Error())
	}

	result := &DB{DbMap: gorp.DbMap{Db: db, Dialect: gorp.PostgresDialect{}}}
	initModels(&result.DbMap)
	return result, nil
}

var dbNotExistErrRx = regexp.MustCompile(`^pq: database "([^"]+)" does not exist$`)

func connectToDatabase(url *net_url.URL) (*sql.DB, error) {
	db, err := sql.Open("postgres", url.String())
	if err == nil {
		//apparently the "database does not exist" error only occurs when trying to issue the first statement
		_, err = db.Exec("SELECT 1")
	}
	if err == nil {
		//success
		return db, nil
	}
	match := dbNotExistErrRx.FindStringSubmatch(err.Error())
	if match == nil {
		//unexpected error
		return nil, err
	}
	dbName := match[1]

	//connect to Postgres without the database name specified, so that we can
	//execute CREATE DATABASE
	urlWithoutDB := *url
	urlWithoutDB.Path = "/"
	db2, err := sql.Open("postgres", urlWithoutDB.String())
	if err == nil {
		_, err = db2.Exec("CREATE DATABASE " + dbName)
	}
	if err == nil {
		err = db2.Close()
	} else {
		db2.Close()
	}
	if err != nil {
		return nil, err
	}

	//now the actual database is there and we can connect to it
	return sql.Open("postgres", url.String())
}

func migrateSchema(db *sql.DB) error {
	//use the "go-bindata" driver for github.com/mattes/migrate, but without
	//actually using go-bindata (go-bindata stubbornly insists on making its
	//generated functions public, but I don't want to pollute the API)
	var assetNames []string
	for name := range sqlMigrations {
		assetNames = append(assetNames, name)
	}
	asset := func(name string) ([]byte, error) {
		data, ok := sqlMigrations[name]
		if ok {
			return []byte(data), nil
		}
		return nil, &os.PathError{Op: "open", Path: "<keppel>/builtin-sql/" + name, Err: errors.New("not found")}
	}

	sourceDriver, err := bindata.WithInstance(bindata.Resource(assetNames, asset))
	if err != nil {
		return err
	}
	dbDriver, err := postgres.WithInstance(db, &postgres.Config{})
	if err != nil {
		return err
	}
	m, err := migrate.NewWithInstance("go-bindata", sourceDriver, "postgres", dbDriver)
	if err != nil {
		return err
	}
	err = m.Up()
	if err == migrate.ErrNoChange {
		//no idea why this is an error
		return nil
	}
	return err
}

//RollbackUnlessCommitted calls Rollback() on a transaction if it hasn't been
//committed or rolled back yet. Use this with the defer keyword to make sure
//that a transaction is automatically rolled back when a function fails.
func RollbackUnlessCommitted(tx *gorp.Transaction) {
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
