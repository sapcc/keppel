// SPDX-FileCopyrightText: 2020 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package tasks

import (
	"database/sql"
	"testing"
	"time"

	"github.com/sapcc/go-bits/must"
	"go.xyrillian.de/gg/assert"
	. "go.xyrillian.de/gg/option"

	"github.com/sapcc/keppel/internal/keppel"
	"github.com/sapcc/keppel/internal/models"
	"github.com/sapcc/keppel/internal/test"
)

func TestAnnounceAccountsToFederation(t *testing.T) {
	j, s := setup(t)
	ctx := t.Context()

	s.FD.RecordedAccounts = nil
	s.Clock.StepBy(1 * time.Hour)

	account1 := must.ReturnT(keppel.FindReducedAccount(ctx, s.DB, "test1"))(t)

	accountJob := j.AccountFederationAnnouncementJob(s.Registry)

	// with just one account set up, AnnounceNextAccountToFederation should
	// announce that account, then start doing nothing
	assert.ErrEqual(t, accountJob.ProcessOne(s.Ctx), nil)
	expectAccountsAnnouncedJustNow(t, s, account1)
	assert.ErrEqual(t, accountJob.ProcessOne(s.Ctx), sql.ErrNoRows)
	expectAccountsAnnouncedJustNow(t, s /*, nothing */)

	// setup another account; only that one should need announcing initially
	s.Clock.StepBy(5 * time.Minute)
	account2 := models.Account{Name: "test2", AuthTenantID: "test2authtenant"}
	must.SucceedT(t, models.AccountStore.Insert(ctx, s.DB, &account2))
	assert.ErrEqual(t, accountJob.ProcessOne(s.Ctx), nil)
	expectAccountsAnnouncedJustNow(t, s, account2.Reduced())
	assert.ErrEqual(t, accountJob.ProcessOne(s.Ctx), sql.ErrNoRows)
	expectAccountsAnnouncedJustNow(t, s /*, nothing */)

	// do another full round of announcements
	s.Clock.StepBy(65 * time.Minute)
	assert.ErrEqual(t, accountJob.ProcessOne(s.Ctx), nil)
	expectAccountsAnnouncedJustNow(t, s, account1)
	assert.ErrEqual(t, accountJob.ProcessOne(s.Ctx), nil)
	expectAccountsAnnouncedJustNow(t, s, account2.Reduced())
	assert.ErrEqual(t, accountJob.ProcessOne(s.Ctx), sql.ErrNoRows)
	expectAccountsAnnouncedJustNow(t, s /*, nothing */)
}

func TestAccountPlatformFilterSync(t *testing.T) {
	test.WithRoundTripper(func(_ *test.RoundTripper) {
		_, s1 := setup(t)
		j2, s2 := setupReplica(t, s1, "on_first_use")
		ctx := t.Context()

		s2.Clock.StepBy(1 * time.Hour)

		syncJob := j2.AccountPlatformFilterSyncJob(s2.Registry)

		assert.ErrEqual(t, syncJob.ProcessOne(s2.Ctx), nil)
		account := must.ReturnT(keppel.FindAccount(ctx, s2.DB, "test1"))(t)
		assert.Equal(t, account.PlatformFilter, nil)
		assert.ErrEqual(t, syncJob.ProcessOne(s2.Ctx), sql.ErrNoRows)

		// set up another replica account
		s2.Clock.StepBy(65 * time.Minute)
		account2 := models.Account{
			Name:                     "test2",
			AuthTenantID:             "test2authtenant",
			UpstreamPeerHostName:     "registry.example.org",
			NextPlatformFilterSyncAt: Some(s2.Clock.Now().Add(1 * time.Hour)),
		}
		must.SucceedT(t, models.AccountStore.Insert(ctx, s2.DB, &account2))
		must.SucceedT(t, models.AccountStore.Insert(ctx, s1.DB, &models.Account{
			Name:         "test2",
			AuthTenantID: "test2authtenant",
		}))
		assert.ErrEqual(t, syncJob.ProcessOne(s2.Ctx), nil)
		assert.ErrEqual(t, syncJob.ProcessOne(s2.Ctx), sql.ErrNoRows)

		// change the primary's platform filter for test1
		s2.Clock.StepBy(65 * time.Minute)
		newFilter := models.PlatformFilter{
			{OS: "linux", Architecture: "amd64"},
			{OS: "linux", Architecture: "arm64", Variant: "v8"},
		}
		primary1 := must.ReturnT(keppel.FindAccount(ctx, s1.DB, "test1"))(t)
		primary1.PlatformFilter = newFilter
		must.SucceedT(t, models.AccountStore.Update(ctx, s1.DB, primary1))

		// do another full round of syncs
		assert.ErrEqual(t, syncJob.ProcessOne(s2.Ctx), nil)
		assert.ErrEqual(t, syncJob.ProcessOne(s2.Ctx), nil)
		assert.ErrEqual(t, syncJob.ProcessOne(s2.Ctx), sql.ErrNoRows)

		account = must.ReturnT(keppel.FindAccount(ctx, s2.DB, "test1"))(t)
		assert.Equal(t, account.PlatformFilter, newFilter)
		account = must.ReturnT(keppel.FindAccount(ctx, s2.DB, "test2"))(t)
		assert.Equal(t, account.PlatformFilter, nil)
	})
}

func expectAccountsAnnouncedJustNow(t *testing.T, s test.Setup, accounts ...models.ReducedAccount) {
	t.Helper()
	var expected []test.AccountRecordedByFederationDriver
	for _, a := range accounts {
		expected = append(expected, test.AccountRecordedByFederationDriver{
			Account:    a,
			RecordedAt: s.Clock.Now(),
		})
	}
	assert.Equal(t, s.FD.RecordedAccounts, expected)

	// reset for next test step
	s.FD.RecordedAccounts = nil
}
