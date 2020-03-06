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
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/sapcc/keppel/internal/keppel"
)

//Replicator contains several tools that are required for replication.
type Replicator struct {
	cfg keppel.Configuration
	db  *keppel.DB
	sd  keppel.StorageDriver
	//non-pure functions that can be replaced by deterministic doubles for unit tests
	timeNow           func() time.Time
	generateStorageID func() string
}

//NewReplicator creates a new Replicator instance.
func NewReplicator(cfg keppel.Configuration, db *keppel.DB, sd keppel.StorageDriver) *Replicator {
	return &Replicator{cfg, db, sd, time.Now, keppel.GenerateStorageID}
}

//OverrideTimeNow replaces time.Now with a test double.
func (r *Replicator) OverrideTimeNow(timeNow func() time.Time) *Replicator {
	r.timeNow = timeNow
	return r
}

//OverrideGenerateStorageID replaces keppel.GenerateStorageID with a test double.
func (r *Replicator) OverrideGenerateStorageID(generateStorageID func() string) *Replicator {
	r.generateStorageID = generateStorageID
	return r
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

func (r Replicator) fetchFromUpstream(repo keppel.Repository, method, path string, peer keppel.Peer, peerToken string) (body io.ReadCloser, bodyLengthBytes uint64, contentType string, returnErr error) {
	reqURL := fmt.Sprintf(
		"https://%s/v2/%s/%s",
		peer.HostName, repo.FullName(), path)

	req, err := http.NewRequest(method, reqURL, nil)
	if err != nil {
		return nil, 0, "", err
	}
	req.Header.Set("Authorization", "Bearer "+peerToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, 0, "", err
	}
	defer func() {
		//close resp.Body only if we're not passing it to the caller
		if body == nil {
			resp.Body.Close()
		}
	}()

	//on success, just return the response body
	if resp.StatusCode == http.StatusOK {
		blobLengthBytes, err := strconv.ParseUint(resp.Header.Get("Content-Length"), 10, 64)
		return resp.Body, blobLengthBytes, resp.Header.Get("Content-Type"), err
	}

	//on error, try to parse the upstream RegistryV2Error so that we can proxy it
	//through to the client correctly
	//
	//NOTE: We use HasPrefix here because the actual Content-Type is usually
	//"application/json; charset=utf-8".
	if strings.HasPrefix(resp.Header.Get("Content-Type"), "application/json") {
		var respData struct {
			Errors []*keppel.RegistryV2Error `json:"errors"`
		}
		err := json.NewDecoder(resp.Body).Decode(&respData)
		if err == nil && len(respData.Errors) > 0 {
			return nil, 0, "", respData.Errors[0].WithStatus(resp.StatusCode)
		}
	}
	return nil, 0, "", unexpectedStatusCodeError{req, http.StatusOK, resp.Status}
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
