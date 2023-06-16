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
	"context"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/sapcc/go-bits/jobloop"
	"github.com/sapcc/go-bits/logg"
	"github.com/sapcc/go-bits/sqlext"

	"github.com/sapcc/keppel/internal/keppel"
)

var accountAnnouncementSearchQuery = sqlext.SimplifyWhitespace(`
	SELECT * FROM accounts
		WHERE next_federation_announcement_at IS NULL OR next_federation_announcement_at < $1
	-- accounts without any announcements first, then sorted by last announcement
	ORDER BY next_federation_announcement_at IS NULL DESC, next_federation_announcement_at ASC
	-- only one account at a time
	LIMIT 1
`)

var accountAnnouncementDoneQuery = sqlext.SimplifyWhitespace(`
	UPDATE accounts SET next_federation_announcement_at = $2 WHERE name = $1
`)

// AccountFederationAnnouncementJob is a job. Each task finds an account that has not been
// announced to the FederationDriver in more than an hour, and announces it. If
// no accounts need to be announced, sql.ErrNoRows is returned to instruct the
// caller to slow down.
func (j *Janitor) AccountFederationAnnouncementJob(registerer prometheus.Registerer) jobloop.Job { //nolint: dupl // interface implementation of different things
	return (&jobloop.ProducerConsumerJob[keppel.Account]{
		Metadata: jobloop.JobMetadata{
			ReadableName: "account federation announcement",
			CounterOpts: prometheus.CounterOpts{
				Name: "keppel_account_federation_announcements",
				Help: "Counter for announcements of existing accounts to the federation driver.",
			},
		},
		DiscoverTask: func(_ context.Context, _ prometheus.Labels) (account keppel.Account, err error) {
			err = j.db.SelectOne(&account, accountAnnouncementSearchQuery, j.timeNow())
			return account, err
		},
		ProcessTask: j.announceAccountToFederation,
	}).Setup(registerer)
}

func (j *Janitor) announceAccountToFederation(ctx context.Context, account keppel.Account, labels prometheus.Labels) error {
	err := j.fd.RecordExistingAccount(ctx, account, j.timeNow())
	if err != nil {
		//since the announcement is not critical for day-to-day operation, we
		//accept that it can fail and move on regardless
		logg.Error("cannot announce account %q to federation: %s", account.Name, err.Error())
	}

	_, err = j.db.Exec(accountAnnouncementDoneQuery, account.Name, j.timeNow().Add(j.addJitter(1*time.Hour)))
	return err
}
