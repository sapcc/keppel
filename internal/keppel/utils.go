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
	"net/http"
	"net/url"
	"strings"

	"github.com/opencontainers/go-digest"
)

//OriginalRequestURL returns the URL that the original requester used when
//sending an HTTP request. This inspects the X-Forwarded-* set of headers to
//identify reverse proxying.
func OriginalRequestURL(r *http.Request) url.URL {
	u := url.URL{
		Path:     r.URL.Path,
		RawQuery: r.URL.RawQuery,
	}

	//case 1: we are behind a reverse proxy
	u.Host = r.Header.Get("X-Forwarded-Host")
	if u.Host != "" {
		u.Scheme = r.Header.Get("X-Forwarded-Proto")
		if u.Scheme == "" {
			u.Scheme = "http"
		}
		return u
	}

	//case 2: we are not behind a reverse proxy, but the Host header indicates how the user reached us
	if r.Host != "" {
		u.Host = r.Host
		u.Scheme = "http"
		return u
	}

	//case 3: no idea how the user got here - don't include any guesses in the URL
	return u
}

//AppendQuery adds additional query parameters to an existing unparsed URL.
func AppendQuery(url string, query url.Values) string {
	if strings.Contains(url, "?") {
		return url + "&" + query.Encode()
	}
	return url + "?" + query.Encode()
}

////////////////////////////////////////////////////////////////////////////////

//ManifestReference is a reference to a manifest as encountered in a URL on the
//Registry v2 API. Exactly one of the members will be non-empty.
type ManifestReference struct {
	Digest digest.Digest
	Tag    string
}

//ParseManifestReference parses a manifest reference. If `reference` parses as
//a digest, it will be interpreted as a digest. Otherwise it will be
//interpreted as a tag name.
func ParseManifestReference(reference string) ManifestReference {
	digest, err := digest.Parse(reference)
	if err == nil {
		return ManifestReference{Digest: digest}
	}
	return ManifestReference{Tag: reference}
}

//String returns the original string representation of this reference.
func (r ManifestReference) String() string {
	if r.Digest != "" {
		return r.Digest.String()
	}
	return r.Tag
}

//IsDigest returns whether this reference is to a specific digest, rather than to a tag.
func (r ManifestReference) IsDigest() bool {
	return r.Digest != ""
}

//IsTag returns whether this reference is to a tag, rather than to a specific digest.
func (r ManifestReference) IsTag() bool {
	return r.Digest == ""
}
