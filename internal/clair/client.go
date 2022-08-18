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

package clair

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"time"

	"github.com/golang-jwt/jwt/v4"
)

// Client is a client for accessing the Clair vulnerability scanning service.
type Client struct {
	//BaseURL is where the Clair API is running.
	BaseURL url.URL
	//PresharedKey is used to sign auth tokens for use with Clair.
	PresharedKey []byte
	//isEmptyManifest tracks when we did not submit a manifest because it does
	//not contain any actual layers.
	isEmptyManifest map[string]bool
}

func (c *Client) requestURL(pathElements ...string) string {
	requestURL := c.BaseURL
	requestURL.Path = path.Join(c.BaseURL.Path, path.Join(pathElements...))
	return requestURL.String()
}

func (c *Client) doRequest(req *http.Request, respBody interface{}) error {
	//add auth token to request
	now := time.Now()
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.RegisteredClaims{
		Audience:  jwt.ClaimStrings{c.BaseURL.Host},
		Issuer:    "keppel",
		IssuedAt:  jwt.NewNumericDate(now),
		NotBefore: jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(now.Add(1 * time.Hour)),
	})
	tokenStr, err := token.SignedString(c.PresharedKey)
	if err != nil {
		return fmt.Errorf("cannot issue token for %s %s: %w", req.Method, req.URL.String(), err)
	}
	req.Header.Set("Authorization", "Bearer "+tokenStr)

	//add additional headers to request
	if req.Body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")

	//run request
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("cannot %s %s: %w", req.Method, req.URL.String(), err)
	}
	respBodyBytes, err := io.ReadAll(resp.Body)
	if err == nil {
		err = resp.Body.Close()
	} else {
		resp.Body.Close()
	}
	if err != nil {
		return fmt.Errorf("cannot %s %s: %w", req.Method, req.URL.String(), err)
	}

	//expect 2xx response
	if resp.StatusCode >= 299 {
		return fmt.Errorf("cannot %s %s: got %d response: %q", req.Method, req.URL.String(), resp.StatusCode, string(respBodyBytes))
	}

	err = json.Unmarshal(respBodyBytes, &respBody)
	if err != nil {
		return fmt.Errorf("cannot %s %s: cannot decode response body: %w", req.Method, req.URL.String(), err)
	}
	return nil
}

// SendRequest sends an arbitrary request without request body or special
// headers (so probably only GET or HEAD) to Clair with proper auth. This
// interface is only used by the Clair API proxy.
func (c *Client) SendRequest(method, urlPath string, responseBody interface{}) error {
	req, err := http.NewRequest(method, c.requestURL(urlPath), http.NoBody)
	if err != nil {
		return err
	}
	return c.doRequest(req, responseBody)
}
