// SPDX-FileCopyrightText: 2020 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package tasks

import (
	"database/sql"
	"testing"
	"time"

	"github.com/sapcc/go-bits/assert"
	"github.com/sapcc/go-bits/must"

	"github.com/sapcc/keppel/internal/models"
	"github.com/sapcc/keppel/internal/test"
)

func TestAnnounceAccountsToFederation(t *testing.T) {
	j, s := setup(t)
	s.FD.RecordedAccounts = nil
	s.Clock.StepBy(1 * time.Hour)

	var account1 models.Account
	must.SucceedT(t, s.DB.SelectOne(&account1, `SELECT * FROM accounts`))

	accountJob := j.AccountFederationAnnouncementJob(s.Registry)

	// with just one account set up, AnnounceNextAccountToFederation should
	// announce that account, then start doing nothing
	assert.ErrEqual(t, accountJob.ProcessOne(s.Ctx), nil)
	expectAccountsAnnouncedJustNow(t, s, account1)
	assert.ErrEqual(t, accountJob.ProcessOne(s.Ctx), sql.ErrNoRows)
	expectAccountsAnnouncedJustNow(t, s /*, nothing */)

	// setup another account; only that one should need announcing initially
	s.Clock.StepBy(5 * time.Minute)
	account2 := models.Account{Name: "test2", AuthTenantID: "test2authtenant", GCPoliciesJSON: "[]"}
	must.SucceedT(t, s.DB.Insert(&account2))
	assert.ErrEqual(t, accountJob.ProcessOne(s.Ctx), nil)
	expectAccountsAnnouncedJustNow(t, s, account2)
	assert.ErrEqual(t, accountJob.ProcessOne(s.Ctx), sql.ErrNoRows)
	expectAccountsAnnouncedJustNow(t, s /*, nothing */)

	// do another full round of announcements
	s.Clock.StepBy(65 * time.Minute)
	assert.ErrEqual(t, accountJob.ProcessOne(s.Ctx), nil)
	expectAccountsAnnouncedJustNow(t, s, account1)
	assert.ErrEqual(t, accountJob.ProcessOne(s.Ctx), nil)
	expectAccountsAnnouncedJustNow(t, s, account2)
	assert.ErrEqual(t, accountJob.ProcessOne(s.Ctx), sql.ErrNoRows)
	expectAccountsAnnouncedJustNow(t, s /*, nothing */)
}

func expectAccountsAnnouncedJustNow(t *testing.T, s test.Setup, accounts ...models.Account) {
	t.Helper()
	var expected []test.AccountRecordedByFederationDriver
	for _, a := range accounts {
		expected = append(expected, test.AccountRecordedByFederationDriver{
			Account:    a,
			RecordedAt: s.Clock.Now(),
		})
	}
	assert.DeepEqual(t, "accounts announced to federation",
		s.FD.RecordedAccounts, expected)

	// reset for next test step
	s.FD.RecordedAccounts = nil
}
