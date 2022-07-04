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
	"fmt"
	"time"

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

//AnnounceNextAccountToFederation finds the next account that has not been
//announced to the FederationDriver in more than an hour, and announces it. If
//no accounts need to be announced, sql.ErrNoRows is returned to instruct the
//caller to slow down.
func (j *Janitor) AnnounceNextAccountToFederation() (returnErr error) {
	var account keppel.Account
	defer func() {
		if returnErr == nil {
			announceAccountToFederationSuccessCounter.Inc()
		} else if returnErr != sql.ErrNoRows {
			announceAccountToFederationFailedCounter.Inc()
			returnErr = fmt.Errorf("while announcing account %q to federation: %s",
				account.Name, returnErr.Error())
		}
	}()

	//find account to announce
	err := j.db.SelectOne(&account, accountAnnouncementSearchQuery, j.timeNow())
	if err != nil {
		if err == sql.ErrNoRows {
			logg.Debug("no accounts to announce to federation - slowing down...")
			return sql.ErrNoRows
		}
		return err
	}

	err = j.fd.RecordExistingAccount(account, j.timeNow())
	if err != nil {
		//since the announcement is not critical for day-to-day operation, we
		//accept that it can fail and move on regardless
		logg.Error("cannot announce account %q to federation: %s", account.Name, err.Error())
	}

	_, err = j.db.Exec(accountAnnouncementDoneQuery, account.Name, j.timeNow().Add(1*time.Hour))
	return err
}
