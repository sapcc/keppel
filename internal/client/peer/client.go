// SPDX-FileCopyrightText: 2023 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package peerclient

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/sapcc/keppel/internal/auth"
	"github.com/sapcc/keppel/internal/keppel"
	"github.com/sapcc/keppel/internal/models"
)

// Client can be used for API access to one of our peers (using our peering
// credentials).
type Client struct {
	peer  models.Peer
	token string
}

// New obtains a token for API access to the given peer (using our peering
// credentials), and wraps it into a Client instance.
func New(ctx context.Context, cfg keppel.Configuration, peer models.Peer, scope auth.Scope) (Client, error) {
	c := Client{peer, ""}
	err := c.initToken(ctx, cfg, scope)
	if err != nil {
		return Client{}, fmt.Errorf("while trying to obtain a peer token for %s in scope %s: %w", peer.HostName, scope, err)
	}
	return c, nil
}

func (c *Client) initToken(ctx context.Context, cfg keppel.Configuration, scope auth.Scope) error {
	reqURL := c.buildRequestURL(fmt.Sprintf("keppel/v1/auth?service=%[1]s&scope=%[2]s", c.peer.HostName, scope))
	ourUserName := "replication@" + cfg.APIPublicHostname
	authHeader := map[string]string{"Authorization": keppel.BuildBasicAuthHeader(ourUserName, c.peer.OurPassword)}

	respBody, respStatusCode, _, err := c.doRequest(ctx, http.MethodGet, reqURL, http.NoBody, authHeader)
	if err != nil {
		return err
	}
	defer respBody.Close()
	if respStatusCode != http.StatusOK {
		return fmt.Errorf("expected 200 OK, but got %d: %s", respStatusCode, tryReadAllAndTrimSpace(respBody))
	}

	var data struct {
		Token string `json:"token"`
	}
	err = json.NewDecoder(respBody).Decode(&data)
	if err != nil {
		return err
	}
	c.token = data.Token
	return nil
}

func (c Client) buildRequestURL(path string) string {
	return fmt.Sprintf("https://%s/%s", c.peer.HostName, path)
}

// The caller is responsible for closing respBodyBytes if it's non-nil.
func (c Client) doRequest(ctx context.Context, method, url string, body io.Reader, headers map[string]string) (respBodyBytes io.ReadCloser, respStatusCode int, respHeader http.Header, err error) {
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, 0, nil, fmt.Errorf("during %s %s: %w", method, url, err)
	}
	if c.token != "" { // empty token occurs only during initToken()
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, 0, nil, fmt.Errorf("during %s %s: %w", method, url, err)
	}

	return resp.Body, resp.StatusCode, resp.Header, nil
}
