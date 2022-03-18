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

package client

import (
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"strings"

	"github.com/sapcc/keppel/internal/keppel"
)

//RepoClient contains methods for interacting with a repository on a registry server.
type RepoClient struct {
	Scheme   string //either "http" or "https"
	Host     string //either a plain hostname or a host:port like "example.org:443"
	RepoName string

	//credentials (only needed for non-public repos)
	UserName string
	Password string

	//auth state
	token string
}

type repoRequest struct {
	Method       string
	Path         string
	Headers      http.Header
	Body         io.ReadSeeker
	ExpectStatus int
}

func (c *RepoClient) doRequest(r repoRequest) (*http.Response, error) {
	if c.Scheme == "" {
		c.Scheme = "https"
	}

	uri := fmt.Sprintf("%s://%s/v2/%s/%s",
		c.Scheme, c.Host, c.RepoName, r.Path)

	//send GET request for manifest
	req, err := http.NewRequest(r.Method, uri, r.Body)
	if err != nil {
		return nil, err
	}
	for k, v := range r.Headers {
		req.Header[k] = v
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, keppel.ErrUnavailable.With(err.Error())
	}

	//if it's a 401, do the auth challenge...
	if resp.StatusCode == http.StatusUnauthorized {
		authChallenge, err := ParseAuthChallenge(resp.Header)
		if err != nil {
			return nil, fmt.Errorf("cannot parse auth challenge from 401 response to %s %s: %s", r.Method, uri, err.Error())
		}
		c.token, err = authChallenge.GetToken(c.UserName, c.Password)
		if err != nil {
			return nil, fmt.Errorf("authentication failed: %s", err.Error())
		}
		if c.token == "" {
			return nil, errors.New("authentication failed: no token was returned")
		}

		//...then resend the GET request with the token
		if r.Body != nil {
			_, err = r.Body.Seek(0, io.SeekStart)
			if err != nil {
				return nil, err
			}
		}
		req, err := http.NewRequest(r.Method, uri, r.Body)
		if err != nil {
			return nil, err
		}
		for k, v := range r.Headers {
			req.Header[k] = v
		}
		req.Header.Set("Authorization", "Bearer "+c.token)
		resp, err = http.DefaultClient.Do(req)
		if err != nil {
			return nil, keppel.ErrUnavailable.With(err.Error())
		}
	}

	if resp.StatusCode != r.ExpectStatus {
		//on error, try to parse the upstream RegistryV2Error so that we can proxy it
		//through to the client correctly
		//
		//NOTE: We use HasPrefix here because the actual Content-Type is usually
		//"application/json; charset=utf-8".
		if r.Method != "HEAD" && strings.HasPrefix(resp.Header.Get("Content-Type"), "application/json") {
			var respData struct {
				Errors []*keppel.RegistryV2Error `json:"errors"`
			}
			err := json.NewDecoder(resp.Body).Decode(&respData)
			if err == nil {
				err = resp.Body.Close()
			} else {
				resp.Body.Close()
			}
			if err == nil && len(respData.Errors) > 0 {
				return nil, respData.Errors[0].WithStatus(resp.StatusCode)
			}
		}
		resp.Body.Close()
		return nil, unexpectedStatusCodeError{req, http.StatusOK, resp.Status}
	}

	return resp, nil
}

////////////////////////////////////////////////////////////////////////////////

type unexpectedStatusCodeError struct {
	req            *http.Request
	expectedStatus int
	actualStatus   string
}

func (e unexpectedStatusCodeError) Error() string {
	return fmt.Sprintf("during %s %s: expected status %d, but got %s",
		e.req.Method, html.EscapeString(e.req.URL.String()), e.expectedStatus, e.actualStatus,
	)
}
