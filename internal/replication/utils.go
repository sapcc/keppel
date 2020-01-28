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

package replication

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/sapcc/keppel/internal/keppel"
)

//Replicator contains several tools that are required for replication.
type Replicator struct {
	cfg keppel.Configuration
	db  *keppel.DB
	od  keppel.OrchestrationDriver
}

//NewReplicator creates a new Replicator instance.
func NewReplicator(cfg keppel.Configuration, db *keppel.DB, od keppel.OrchestrationDriver) Replicator {
	return Replicator{cfg, db, od}
}

func (r Replicator) getPeerToken(peer keppel.Peer, repoFullName string) (string, error) {
	reqURL := fmt.Sprintf(
		"https://%[1]s/keppel/v1/auth?service=%[1]s&scope=repository:%[2]s:pull",
		peer.HostName, repoFullName)

	req, err := http.NewRequest("GET", reqURL, nil)
	if err != nil {
		return "", err
	}

	req.Header.Set("Authorization", keppel.BuildBasicAuthHeader(
		"replication@"+r.cfg.APIPublicHostname(),
		peer.OurPassword,
	))

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", unexpectedStatusCodeError{req, http.StatusOK, resp.Status}
	}

	var respData struct {
		Token string `json:"token"`
	}
	err = json.NewDecoder(resp.Body).Decode(&respData)
	if err != nil {
		return "", err
	}

	if respData.Token == "" {
		return "", errors.New("peer authentication did not yield a token")
	}
	return respData.Token, nil
}

////////////////////////////////////////////////////////////////////////////////

type unexpectedStatusCodeError struct {
	req            *http.Request
	expectedStatus int
	actualStatus   string
}

func (e unexpectedStatusCodeError) Error() string {
	return fmt.Sprintf("during %s %s: expected status %d, but got %s",
		e.req.Method, e.req.URL.String(), e.expectedStatus, e.actualStatus,
	)
}
