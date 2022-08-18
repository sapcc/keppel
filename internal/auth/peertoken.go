/*******************************************************************************
*
* Copyright 2021 SAP SE
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

package auth

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/sapcc/keppel/internal/keppel"
)

// GetPeerToken returns a token that can be used for the Peer API (or any other API endpoint that accepts the
func GetPeerToken(cfg keppel.Configuration, peer keppel.Peer, scope Scope) (string, error) {
	reqURL := fmt.Sprintf("https://%[1]s/keppel/v1/auth?service=%[1]s&scope=%[2]s", peer.HostName, scope.String())
	req, err := http.NewRequest(http.MethodGet, reqURL, http.NoBody)
	if err != nil {
		return "", err
	}
	ourUserName := "replication@" + cfg.APIPublicHostname
	req.Header.Set("Authorization", keppel.BuildBasicAuthHeader(ourUserName, peer.OurPassword))

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	respBodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	err = resp.Body.Close()
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("could not get peer token: expected 200 OK, but got %s: %s", resp.Status, strings.TrimSpace(string(respBodyBytes)))
	}

	var data struct {
		Token string `json:"token"`
	}
	err = json.Unmarshal(respBodyBytes, &data)
	if err != nil {
		return "", fmt.Errorf("could not get peer token: %w", err)
	}
	return data.Token, nil
}
