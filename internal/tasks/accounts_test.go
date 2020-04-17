/******************************************************************************
*
*  Copyright 2020 SAP SE
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

package tasks

import (
	"database/sql"
	"testing"
	"time"

	"github.com/sapcc/go-bits/assert"
	"github.com/sapcc/keppel/internal/keppel"
	"github.com/sapcc/keppel/internal/test"
)

func TestAnnounceAccountsToFederation(t *testing.T) {
	j, _, db, fd, _, clock, _ := setup(t)
	clock.StepBy(1 * time.Hour)

	var account1 keppel.Account
	must(t, db.SelectOne(&account1, `SELECT * FROM accounts`))

	//with just one account set up, AnnounceNextAccountToFederation should
	//announce that account, then start doing nothing
	expectSuccess(t, j.AnnounceNextAccountToFederation())
	expectAccountsAnnouncedJustNow(t, fd, clock, account1)
	expectError(t, sql.ErrNoRows.Error(), j.AnnounceNextAccountToFederation())
	expectAccountsAnnouncedJustNow(t, fd, clock /*, nothing */)

	//setup another account; only that one should need announcing initially
	clock.StepBy(5 * time.Minute)
	account2 := keppel.Account{Name: "test2", AuthTenantID: "test2authtenant"}
	must(t, db.Insert(&account2))
	expectSuccess(t, j.AnnounceNextAccountToFederation())
	expectAccountsAnnouncedJustNow(t, fd, clock, account2)
	expectError(t, sql.ErrNoRows.Error(), j.AnnounceNextAccountToFederation())
	expectAccountsAnnouncedJustNow(t, fd, clock /*, nothing */)

	//do another full round of announcements
	clock.StepBy(65 * time.Minute)
	expectSuccess(t, j.AnnounceNextAccountToFederation())
	expectAccountsAnnouncedJustNow(t, fd, clock, account1)
	expectSuccess(t, j.AnnounceNextAccountToFederation())
	expectAccountsAnnouncedJustNow(t, fd, clock, account2)
	expectError(t, sql.ErrNoRows.Error(), j.AnnounceNextAccountToFederation())
	expectAccountsAnnouncedJustNow(t, fd, clock /*, nothing */)

}

func expectAccountsAnnouncedJustNow(t *testing.T, fd *test.FederationDriver, clock *test.Clock, accounts ...keppel.Account) {
	var expected []test.AccountRecordedByFederationDriver
	for _, a := range accounts {
		expected = append(expected, test.AccountRecordedByFederationDriver{
			Account:    a,
			RecordedAt: clock.Now(),
		})
	}
	assert.DeepEqual(t, "accounts announced to federation",
		fd.RecordedAccounts, expected)

	//reset for next test step
	fd.RecordedAccounts = nil
}
