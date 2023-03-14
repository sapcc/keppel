/******************************************************************************
*
*  Copyright 2023 SAP SE
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

package peerclient

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/sapcc/keppel/internal/auth"
	"github.com/sapcc/keppel/internal/keppel"
)

// Client can be used for API access to one of our peers (using our peering
// credentials).
type Client struct {
	peer  keppel.Peer
	token string
}

// New obtains a token for API access to the given peer (using our peering
// credentials), and wraps it into a Client instance.
func New(cfg keppel.Configuration, peer keppel.Peer, scope auth.Scope) (Client, error) {
	token, err := getToken(cfg, peer, scope)
	return Client{peer, token}, err
}

func getToken(cfg keppel.Configuration, peer keppel.Peer, scope auth.Scope) (string, error) {
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

func (c Client) buildRequestURL(path string) string {
	return fmt.Sprintf("https://%s/%s", c.peer.HostName, path)
}

func (c Client) doRequest(method, url string, body io.Reader, headers map[string]string) (respBodyBytes []byte, respStatusCode int, respHeader http.Header, err error) {
	req, err := http.NewRequest(method, url, body)
	if err != nil {
		return nil, 0, nil, fmt.Errorf("during %s %s: %w", method, url, err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, 0, nil, fmt.Errorf("during %s %s: %w", method, url, err)
	}
	defer resp.Body.Close()
	respBodyBytes, err = io.ReadAll(resp.Body)
	if err != nil {
		return nil, 0, nil, fmt.Errorf("during %s %s: %w", method, url, err)
	}

	return respBodyBytes, resp.StatusCode, resp.Header, nil
}
