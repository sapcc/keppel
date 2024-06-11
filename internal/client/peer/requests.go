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
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/sapcc/keppel/internal/keppel"
	"github.com/sapcc/keppel/internal/models"
)

// DownloadManifestViaPullDelegation asks the peer to download a manifest from
// an external registry for us. This gets used when the external registry
// denies the pull to us because we hit our rate limit.
func (c Client) DownloadManifestViaPullDelegation(ctx context.Context, imageRef models.ImageReference, userName, password string) (respBodyBytes []byte, contentType string, err error) {
	reqURL := c.buildRequestURL(fmt.Sprintf(
		"peer/v1/delegatedpull/%s/v2/%s/manifests/%s",
		imageRef.Host, imageRef.RepoName, imageRef.Reference,
	))
	reqHeaders := map[string]string{
		"X-Keppel-Delegated-Pull-Username": userName,
		"X-Keppel-Delegated-Pull-Password": password,
	}

	respBodyBytes, respStatusCode, respHeader, err := c.doRequest(ctx, http.MethodGet, reqURL, http.NoBody, reqHeaders)
	if err != nil {
		return nil, "", err
	}
	if respStatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("during GET %s: expected 200, got %d with response: %s",
			reqURL, respStatusCode, string(respBodyBytes))
	}
	return respBodyBytes, respHeader.Get("Content-Type"), nil
}

// GetForeignAccountConfiguration asks the peer for the configuration of the
// specified account on its side. We use this to match certain account
// attributes with the primary account when creating a replica.
//
// The configuration is deserialized into `target`, which must have the type
// `*keppelv1.Account`. We cannot return this type explicitly because that
// would create an import cycle between this package and package keppelv1.
func (c Client) GetForeignAccountConfigurationInto(ctx context.Context, target any, accountName string) error {
	reqURL := c.buildRequestURL("keppel/v1/accounts/" + accountName)

	respBodyBytes, respStatusCode, _, err := c.doRequest(ctx, http.MethodGet, reqURL, http.NoBody, nil)
	if err != nil {
		return err
	}
	if respStatusCode != http.StatusOK {
		return fmt.Errorf("during GET %s: expected 200, got %d with response: %s",
			reqURL, respStatusCode, string(respBodyBytes))
	}

	data := struct {
		Target any `json:"account"`
	}{target}
	err = jsonUnmarshalStrict(respBodyBytes, &data)
	if err != nil {
		return fmt.Errorf("while parsing response for GET %s: %w", reqURL, err)
	}
	return nil
}

// GetSubleaseToken asks the peer for a sublease token for this account to replicate it on another Keppel instance.
// Only the primary instance of an account can be asked for a sublease token.
func (c Client) GetSubleaseToken(ctx context.Context, accountName string) (string, error) {
	reqURL := c.buildRequestURL("keppel/v1/accounts/" + accountName + "/sublease")

	respBodyBytes, respStatusCode, _, err := c.doRequest(ctx, http.MethodPost, reqURL, http.NoBody, nil)
	if err != nil {
		return "", err
	}
	if respStatusCode != http.StatusOK {
		return "", fmt.Errorf("during POST %s: expected 200, got %d with response: %s",
			reqURL, respStatusCode, string(respBodyBytes))
	}

	data := struct {
		SubleaseToken string `json:"sublease_token"`
	}{}
	err = jsonUnmarshalStrict(respBodyBytes, &data)
	if err != nil {
		return "", fmt.Errorf("while parsing response for POST %s: %w", reqURL, err)
	}
	return data.SubleaseToken, nil
}

// PerformReplicaSync uses the replica-sync API to perform an optimized
// manifest/tag sync with an upstream repo that is managed by one of our peers.
//
// If the repo is deleted on upstream (i.e. 404 is returned), this function
// will return (nil, nil) to signal to the caller that a detailed deletion
// check should be performed.
func (c Client) PerformReplicaSync(ctx context.Context, fullRepoName string, payload keppel.ReplicaSyncPayload) (*keppel.ReplicaSyncPayload, error) {
	reqURL := c.buildRequestURL("peer/v1/sync-replica/" + fullRepoName)
	reqBodyBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("during POST %s: %w", reqURL, err)
	}

	respBodyBytes, respStatusCode, _, err := c.doRequest(ctx, http.MethodPost, reqURL, bytes.NewReader(reqBodyBytes), nil)
	if err != nil {
		return nil, err
	}
	if respStatusCode == http.StatusNotFound {
		// 404 can occur when the repo has been deleted on primary; in this case,
		// fall back to verifying the deletion explicitly using the normal API
		return nil, nil
	}
	if respStatusCode != http.StatusOK {
		return nil, fmt.Errorf("during POST %s: expected 200, got %d with response: %s",
			reqURL, respStatusCode, string(respBodyBytes))
	}

	var respPayload keppel.ReplicaSyncPayload
	err = jsonUnmarshalStrict(respBodyBytes, &respPayload)
	if err != nil {
		return nil, fmt.Errorf("while parsing response for POST %s: %w", reqURL, err)
	}
	return &respPayload, nil
}

// Like yaml.UnmarshalStrict(), but for JSON.
func jsonUnmarshalStrict(buf []byte, target any) error {
	dec := json.NewDecoder(bytes.NewReader(buf))
	dec.DisallowUnknownFields()
	return dec.Decode(target)
}
