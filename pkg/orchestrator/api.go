/******************************************************************************
*
*  Copyright 2018 SAP SE
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

package orchestrator

import (
	"fmt"
	"time"

	"github.com/sapcc/go-bits/logg"
	"github.com/sapcc/keppel/pkg/database"
)

//API is used by other threads to communicate with the Orchestrator
//which manages the keppel-registry processes.
type API struct {
	getPortRequestChan chan<- getPortRequest
}

//GetHostPortForAccount instructs the orchestrator to start this account's
//keppel-registry process if it is not running yet, and returns the host+port
//where the keppel-registry is running (e.g. "localhost:12345").
func (api *API) GetHostPortForAccount(account database.Account) string {
	resultChan := make(chan uint16, 1)
	api.getPortRequestChan <- getPortRequest{
		Account: account,
		Result:  resultChan,
	}
	return fmt.Sprintf("localhost:%d", <-resultChan)
}

//EnsureAllRegistriesAreRunning polls the orchestrator every few minutes to
//ensure that the keppel-registry processes for all known accounts are running.
//This method does not return, so it should be run in a separate goroutine.
func (api *API) EnsureAllRegistriesAreRunning(db *database.DB) {
	for {
		var accounts []database.Account
		_, err := db.Select(&accounts, `SELECT * FROM accounts`)
		if err != nil {
			logg.Error("failed to enumerate accounts: " + err.Error())
			accounts = nil
		}
		for _, account := range accounts {
			//this starts the keppel-registry process for the account if not yet running
			api.getPortRequestChan <- getPortRequest{Account: account}
		}

		//polling interval
		time.Sleep(1 * time.Minute)
	}
}
