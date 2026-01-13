// SPDX-FileCopyrightText: 2020 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package client

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"strings"

	"maps"

	"github.com/sapcc/keppel/internal/keppel"
)

// RepoClient contains methods for interacting with a repository on a registry server.
type RepoClient struct {
	Scheme   string // either "http" or "https"
	Host     string // either a plain hostname or a host:port like "example.org:443"
	RepoName string

	// credentials (only needed for non-public repos)
	UserName string
	Password string

	// auth state
	token string
}

type repoRequest struct {
	Method       string
	Path         string
	Headers      http.Header
	Body         io.ReadSeeker
	ExpectStatus int
}

// SetToken can be used in tests to inject a pre-computed token and bypass the
// username/password requirement.
func (c *RepoClient) SetToken(token string) {
	c.token = token
}

func (c *RepoClient) sendRequest(ctx context.Context, r repoRequest, uri string) (*http.Response, *http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, r.Method, uri, r.Body)
	if err != nil {
		return nil, nil, err
	}
	maps.Copy(req.Header, r.Headers)
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, nil, keppel.ErrUnavailable.With(err.Error())
	}

	return resp, req, nil
}

func (c *RepoClient) doRequest(ctx context.Context, r repoRequest) (*http.Response, error) {
	if c.Scheme == "" {
		c.Scheme = "https"
	}

	uri := fmt.Sprintf("%s://%s/v2/%s/%s", c.Scheme, c.Host, c.RepoName, r.Path)

	// send GET request for manifest
	resp, req, err := c.sendRequest(ctx, r, uri)
	if err != nil {
		return nil, fmt.Errorf("during %s %s: %w", r.Method, uri, err)
	}

	// if it's a 401, do the auth challenge...
	if resp.StatusCode == http.StatusUnauthorized {
		authChallenge, err := ParseAuthChallenge(resp.Header)
		if err != nil {
			return nil, fmt.Errorf("cannot parse auth challenge from 401 response to %s %s: %w", r.Method, uri, err)
		}
		var rerr *keppel.RegistryV2Error
		c.token, rerr = authChallenge.GetToken(ctx, c.UserName, c.Password)
		if rerr != nil {
			return nil, fmt.Errorf("authentication failed during %s %s: %w", r.Method, uri, rerr)
		}

		// ...then resend the GET request with the token
		if r.Body != nil {
			_, err = r.Body.Seek(0, io.SeekStart)
			if err != nil {
				return nil, err
			}
		}
		resp, _, err = c.sendRequest(ctx, r, uri)
		if err != nil {
			return nil, fmt.Errorf("during %s %s: %w", r.Method, uri, err)
		}
	}

	if resp.StatusCode != r.ExpectStatus {
		defer resp.Body.Close()

		// on error, try to parse the upstream RegistryV2Error so that we can proxy it
		// through to the client correctly
		//
		//NOTE: We use HasPrefix here because the actual Content-Type is usually
		// "application/json; charset=utf-8".
		if r.Method != http.MethodHead && strings.HasPrefix(resp.Header.Get("Content-Type"), "application/json") {
			var respData struct {
				Errors []*keppel.RegistryV2Error `json:"errors"`
			}
			err := json.NewDecoder(resp.Body).Decode(&respData)
			if err == nil && len(respData.Errors) > 0 {
				return nil, respData.Errors[0].WithStatus(resp.StatusCode)
			}
		}

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
