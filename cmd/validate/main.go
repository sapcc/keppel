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

package validatecmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"

	"github.com/docker/distribution"
	"github.com/opencontainers/go-digest"
	"github.com/sapcc/go-bits/logg"
	"github.com/sapcc/keppel/internal/client"
	"github.com/sapcc/keppel/internal/keppel"
	"github.com/spf13/cobra"
)

var (
	authUserName string
	authPassword string
)

//AddCommandTo mounts this command into the command hierarchy.
func AddCommandTo(parent *cobra.Command) {
	cmd := &cobra.Command{
		Use:     "validate <image>",
		Example: "  keppel validate registry.example.org/library/alpine:3.9",
		Short:   "Pulls an image and validates that its contents are intact.",
		Long: `Pulls an image and validates that its contents are intact.
If the image is in a Keppel replica account, this ensures that the image is replicated as a side effect.`,
		Args: cobra.ExactArgs(1),
		Run:  run,
	}
	cmd.PersistentFlags().StringVar(&authUserName, "username", "", "User name (only required for non-public images).")
	cmd.PersistentFlags().StringVar(&authUserName, "password", "", "Password (only required for non-public images).")
	parent.AddCommand(cmd)
}

func run(cmd *cobra.Command, args []string) {
	ref, interpretation, err := client.ParseImageReference(args[0])
	logg.Info("interpreting %s as %s", args[0], interpretation)
	if err != nil {
		logg.Fatal(err.Error())
	}

	var token string
	var manifestsToCheck []digest.Digest
	var blobsToCheck []digest.Digest

	manifestBytes, manifestContentType, err := getManifestContents(ref, ref.Reference, &token)
	if err != nil {
		logg.Fatal(err.Error())
	}
	manifest, manifestDesc, err := distribution.UnmarshalManifest(manifestContentType, manifestBytes)
	if err != nil {
		logg.Fatal("error decoding %s manifest: %s", manifestContentType, err.Error())
	}
	for _, desc := range manifest.References() {
		if isManifestMediaType(desc.MediaType) {
			manifestsToCheck = append(manifestsToCheck, desc.Digest)
		} else {
			blobsToCheck = append(blobsToCheck, desc.Digest)
		}
	}
	logg.Info("manifest %s looks good, references %d manifests and %d blobs", manifestDesc.Digest, len(manifestsToCheck), len(blobsToCheck))

	for len(manifestsToCheck) > 0 {
		manifestDigest := manifestsToCheck[0]
		manifestsToCheck = manifestsToCheck[1:]

		manifestBytes, manifestContentType, err := getManifestContents(ref, manifestDigest.String(), &token)
		if err != nil {
			logg.Fatal(err.Error())
		}
		manifest, manifestDesc, err := distribution.UnmarshalManifest(manifestContentType, manifestBytes)
		if err != nil {
			logg.Fatal("error decoding %s manifest: %s", manifestContentType, err.Error())
		}
		newManifestCount, newBlobCount := 0, 0
		for _, desc := range manifest.References() {
			if isManifestMediaType(desc.MediaType) {
				manifestsToCheck = append(manifestsToCheck, desc.Digest)
				newManifestCount++
			} else {
				blobsToCheck = append(blobsToCheck, desc.Digest)
				newBlobCount++
			}
		}
		logg.Info("manifest %s looks good, references %d manifests and %d blobs", manifestDesc.Digest, newManifestCount, newBlobCount)
	}

	for _, blobDigest := range blobsToCheck {
		err := verifyBlobContents(ref, blobDigest, token)
		if err == nil {
			logg.Info("blob %s looks good", blobDigest)
		} else {
			logg.Fatal("error verifying blob %s: %s", blobDigest, err.Error())
		}
	}
}

func getManifestContents(ref client.ImageReference, reference string, token *string) ([]byte, string, error) {
	uri := fmt.Sprintf("https://%s/v2/%s/manifests/%s",
		ref.Host, ref.RepoName, reference)

	//send GET request for manifest
	req, err := http.NewRequest("GET", uri, nil)
	if err != nil {
		return nil, "", err
	}
	req.Header["Accept"] = distribution.ManifestMediaTypes()
	if *token != "" {
		req.Header.Set("Authorization", "Bearer "+*token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, "", err
	}
	respBytes, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, "", err
	}
	err = resp.Body.Close()
	if err != nil {
		return nil, "", err
	}

	//if it's a 401, do the auth challenge...
	if resp.StatusCode == http.StatusUnauthorized {
		authChallenge, err := client.ParseAuthChallenge(resp.Header)
		if err != nil {
			return nil, "", fmt.Errorf("cannot parse auth challenge from 401 response to GET %s: %s", uri, err.Error())
		}
		*token, err = authChallenge.GetToken(os.Getenv("DOCKER_USERNAME"), os.Getenv("DOCKER_PASSWORD"))
		if err != nil {
			return nil, "", fmt.Errorf("authentication failed: %s", err.Error())
		}

		//...then resend the GET request with the token
		req, err := http.NewRequest("GET", uri, nil)
		if err != nil {
			return nil, "", err
		}
		req.Header["Accept"] = distribution.ManifestMediaTypes()
		req.Header.Set("Authorization", "Bearer "+*token)
		resp, err = http.DefaultClient.Do(req)
		if err != nil {
			return nil, "", err
		}
		respBytes, err = ioutil.ReadAll(resp.Body)
		if err != nil {
			return nil, "", err
		}
		err = resp.Body.Close()
		if err != nil {
			return nil, "", err
		}
	}

	if resp.StatusCode != http.StatusOK {
		return nil, "", parseRegistryAPIError(respBytes)
	}
	return respBytes, resp.Header.Get("Content-Type"), nil
}

func verifyBlobContents(ref client.ImageReference, blobDigest digest.Digest, token string) (returnErr error) {
	uri := fmt.Sprintf("https://%s/v2/%s/blobs/%s",
		ref.Host, ref.RepoName, blobDigest)

	req, err := http.NewRequest("GET", uri, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}

	defer func() {
		if returnErr == nil {
			returnErr = resp.Body.Close()
		} else {
			resp.Body.Close()
		}
	}()

	if resp.StatusCode != http.StatusOK {
		respBytes, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			return err
		}
		return parseRegistryAPIError(respBytes)
	}

	hash := blobDigest.Algorithm().Hash()
	_, err = io.Copy(hash, resp.Body)
	if err != nil {
		return err
	}
	actualDigest := digest.NewDigest(blobDigest.Algorithm(), hash)
	if actualDigest != blobDigest {
		return fmt.Errorf("actual digest is %s", actualDigest)
	}
	return nil
}

func parseRegistryAPIError(respBytes []byte) error {
	var data struct {
		Errors []*keppel.RegistryV2Error `json:"errors"`
	}
	err := json.Unmarshal(respBytes, &data)
	if err == nil {
		return data.Errors[0]
	}
	return errors.New(string(respBytes))
}

func isManifestMediaType(contentType string) bool {
	for _, mt := range distribution.ManifestMediaTypes() {
		if mt == contentType {
			return true
		}
	}
	return false
}
