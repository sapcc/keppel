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

package keppel

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/opencontainers/go-digest"
)

const defaultHostName = "registry-1.docker.io"
const defaultTagName = "latest"

//ImageReference refers to an image that can be pulled from a registry.
type ImageReference struct {
	Host      string //either a plain hostname or a host:port like "example.org:443"
	RepoName  string
	Reference ManifestReference
}

//String returns the most compact string representation of this reference.
func (r ImageReference) String() string {
	var result string
	if r.Reference.IsDigest() {
		//digests are appended with "@"
		result = fmt.Sprintf("%s@%s", r.RepoName, r.Reference.Digest.String())
	} else {
		//tag names are appended with ":"
		if r.Reference.Tag == defaultTagName {
			result = r.RepoName
		} else {
			result = fmt.Sprintf("%s:%s", r.RepoName, r.Reference.Tag)
		}
	}

	if r.Host == defaultHostName {
		//strip leading "library/" from repo name; e.g.
		//"registry-1.docker.io/library/alpine:3.9" becomes just "alpine:3.9"
		return strings.TrimPrefix(result, "library/")
	}
	return fmt.Sprintf("%s/%s", r.Host, result)
}

//ParseImageReference parses an image reference string like
//"registry.example.org/alpine:3.9" into an ImageReference struct.
//Both on success and on error, an additional string is returned indicating how
//the input was interpreted (e.g. which defaults were inferred). This can be
//shown to the user to help them understand how the reference was parsed.
func ParseImageReference(input string) (ImageReference, string, error) {
	//prepend hostname for default registry if input does not include a hostname or host:port
	inputParts := strings.SplitN(input, "/", 2)
	hadNoHostName := false
	if len(inputParts) == 1 || !looksLikeHostName(inputParts[0]) {
		input = fmt.Sprintf("%s/%s", defaultHostName, input)
		hadNoHostName = true
	}

	//reformat into a URL for parsing purposes
	input = "docker-pullable://" + input
	imageURL, err := url.Parse(input)
	if err != nil {
		return ImageReference{}, input, err
	}

	var ref ImageReference
	if strings.Contains(imageURL.Path, "@") {
		//input references a digest
		pathParts := ImageReferenceRx.FindStringSubmatch(imageURL.Path)
		digest, err := digest.Parse(pathParts[len(pathParts)-1])
		if err != nil {
			return ImageReference{}, input, fmt.Errorf("invalid digest: %q", ref.Reference)
		}
		ref = ImageReference{
			Host:      imageURL.Host,
			RepoName:  strings.TrimPrefix(pathParts[1], "/"),
			Reference: ManifestReference{Digest: digest},
		}
	} else if strings.Contains(imageURL.Path, ":") {
		//input references a tag name
		pathParts := strings.SplitN(imageURL.Path, ":", 2)
		ref = ImageReference{
			Host:      imageURL.Host,
			RepoName:  strings.TrimPrefix(pathParts[0], "/"),
			Reference: ManifestReference{Tag: pathParts[1]},
		}
	} else {
		//input references no tag or digest - use default tag
		ref = ImageReference{
			Host:      imageURL.Host,
			RepoName:  strings.TrimPrefix(imageURL.Path, "/"),
			Reference: ManifestReference{Tag: "latest"},
		}
	}

	if !RepoNameWithLeadingSlashRx.MatchString("/" + ref.RepoName) {
		return ImageReference{}, input, fmt.Errorf("invalid repository name: %q", ref.RepoName)
	}

	if hadNoHostName {
		//on the default registry, single-word repo names like "alpine" are
		//actually shorthands for "library/alpine" etc.
		if !strings.Contains(ref.RepoName, "/") {
			ref.RepoName = "library/" + ref.RepoName
		}
	}

	return ref, input, nil
}

func looksLikeHostName(host string) bool {
	if strings.Contains(host, ":") {
		//looks like "host:port"
		return true
	}
	return strings.Contains(host, ".") || host == "localhost"
}
