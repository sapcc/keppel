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

package test

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
	"testing"

	"github.com/sapcc/go-bits/logg"
	"github.com/sapcc/keppel/internal/keppel"
)

//SetupOptions contains optional arguments for test.Setup().
type SetupOptions struct {
	IsSecondary bool //if true, configures registry-secondary.example.org instead of registry.example.org
	WithAnycast bool
}

//Setup sets up a keppel.Configuration and database connection for a unit test.
func Setup(t *testing.T, optsPtr *SetupOptions) (keppel.Configuration, *keppel.DB) {
	t.Helper()
	logg.ShowDebug, _ = strconv.ParseBool(os.Getenv("KEPPEL_DEBUG"))

	var opts SetupOptions
	if optsPtr != nil {
		opts = *optsPtr
	}

	var (
		dbName          string
		apiPublicURLStr string
	)
	if opts.IsSecondary {
		dbName = "keppel_secondary"
		apiPublicURLStr = "https://registry-secondary.example.org"
	} else {
		dbName = "keppel"
		apiPublicURLStr = "https://registry.example.org"
	}

	var postgresURL string
	if os.Getenv("TRAVIS") == "true" {
		//cf. https://docs.travis-ci.com/user/database-setup/#postgresql
		postgresURL = fmt.Sprintf("postgres://postgres@localhost/%s?sslmode=disable", dbName)
	} else {
		//suitable for use with ./testing/with-postgres-db.sh
		postgresURL = fmt.Sprintf("postgres://postgres@localhost:54321/%s?sslmode=disable", dbName)
	}

	dbURL, err := url.Parse(postgresURL)
	if err != nil {
		t.Fatal(err.Error())
	}
	apiPublicURL, err := url.Parse(apiPublicURLStr)
	if err != nil {
		t.Fatal(err.Error())
	}
	cfg := keppel.Configuration{
		APIPublicURL: *apiPublicURL,
		DatabaseURL:  *dbURL,
	}

	db, err := keppel.InitDB(cfg.DatabaseURL)
	if err != nil {
		t.Error(err)
		t.Log("Try prepending ./testing/with-postgres-db.sh to your command.")
		t.FailNow()
	}

	//wipe the DB clean if there are any leftovers from the previous test run,
	//starting with the manifest_manifest_refs table (this table's foreign-key
	//constraints are so entangled that any attempt to cascade a deletion from
	//higher up in the hierarchy will run into some ON DELETE RESTRICT
	//constraints and fail)
	for {
		result, err := db.Exec(`DELETE FROM manifest_manifest_refs WHERE parent_digest NOT IN (SELECT child_digest FROM manifest_manifest_refs)`)
		if err != nil {
			t.Fatal(err.Error())
		}
		rowsDeleted, err := result.RowsAffected()
		if err != nil {
			t.Fatal(err.Error())
		}
		if rowsDeleted == 0 {
			break
		}
	}

	//wipe the DB clean if there are any leftovers from the previous test run
	for _, tableName := range []string{"manifest_blob_refs", "accounts", "peers", "quotas"} {
		//NOTE: All tables not mentioned above are cleared via ON DELETE CASCADE.
		//
		//NOTE 2: `manifest_blob_refs` is technically not necessary because it
		//would be cleared when `accounts` is cleared, but if we clear `accounts`
		//directly, the deletions cascade down in the wrong order and trigger
		//ON DELETE RESTRICT constraints.
		_, err := db.Exec("DELETE FROM " + tableName)
		if err != nil {
			t.Fatal(err.Error())
		}
	}

	//reset all primary key sequences for reproducible row IDs
	for _, tableName := range []string{"blobs", "repos"} {
		nextID, err := db.SelectInt(fmt.Sprintf(
			"SELECT 1 + COALESCE(MAX(id), 0) FROM %s", tableName,
		))
		if err != nil {
			t.Fatal(err.Error())
		}
		query := fmt.Sprintf(`ALTER SEQUENCE %s_id_seq RESTART WITH %d`, tableName, nextID)
		_, err = db.Exec(query)
		if err != nil {
			t.Fatal(err.Error())
		}
	}

	cfg.JWTIssuerKey, err = keppel.ParseIssuerKey(UnitTestIssuerPrivateKey)
	if err != nil {
		t.Fatal(err.Error())
	}

	if opts.WithAnycast {
		anycastAPIPublicURL, err := url.Parse("https://registry-global.example.org")
		if err != nil {
			t.Fatal(err.Error())
		}
		cfg.AnycastAPIPublicURL = anycastAPIPublicURL

		anycastJWTIssuerKey, err := keppel.ParseIssuerKey(UnitTestAnycastIssuerPrivateKey)
		if err != nil {
			t.Fatal(err.Error())
		}
		cfg.AnycastJWTIssuerKey = &anycastJWTIssuerKey
	}

	return cfg, db
}

//UnitTestIssuerPrivateKey is an RSA private key that can be used as
//KEPPEL_ISSUER_KEY in unit tests. DO NOT USE IN PRODUCTION.
const UnitTestIssuerPrivateKey = `-----BEGIN RSA PRIVATE KEY-----
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
-----END RSA PRIVATE KEY-----`

//UnitTestAnycastIssuerPrivateKey is an RSA private key that can be used as
//KEPPEL_ANYCAST_ISSUER_KEY in unit tests. DO NOT USE IN PRODUCTION.
const UnitTestAnycastIssuerPrivateKey = `-----BEGIN RSA PRIVATE KEY-----
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
-----END RSA PRIVATE KEY-----`
