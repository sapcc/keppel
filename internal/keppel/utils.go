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
	"regexp"
	"strings"
	"time"
)

var (
	RepoNameRx          = `[a-z0-9]+(?:[._-][a-z0-9]+)*`
	RepoPathRx          = regexp.MustCompile(`^` + RepoNameRx + `(?:/` + RepoNameRx + `)*$`)
	RepoPathComponentRx = regexp.MustCompile(`^` + RepoNameRx + `$`)
)

// The "with leading slash" simplifies the regex because we don't need to write the
// regex for a path element twice.
// Examples:
// - /library/alpine
// - /library/alpine:nonsense
var (
	RepoNameWithLeadingSlash   = "(?:/" + RepoNameRx + ")+"
	RepoNameWithLeadingSlashRx = regexp.MustCompile(`^` + RepoNameWithLeadingSlash + `$`)
)

// ImageReferenceRx is used to match repo/account and optional tag and digest combination
// Examples:
// - /library/alpine
// - /library/alpine:nonsense
// - /library/alpine:e9707504ad0d4c119036b6d41ace4a33596139d3feb9ccb6617813ce48c3eeef
// - /library/alpine@sha256:e9707504ad0d4c119036b6d41ace4a33596139d3feb9ccb6617813ce48c3eeef
// - /library/alpine:nonsense@sha256:e9707504ad0d4c119036b6d41ace4a33596139d3feb9ccb6617813ce48c3eeef
var ImageReferenceRx = regexp.MustCompile(`^(` + RepoNameWithLeadingSlash + `)(?::([a-zA-Z0-9_][a-zA-Z0-9._-]{0,127}))?(?:@(sha256:[a-z0-9]{64}))?$`)

// IsAccountName returns whether the given string is a well-formed account name.
// This does not check whether the account actually exists in the DB.
func IsAccountName(input string) bool {
	//account names are historically limited to 48 chars (because we used to put
	//them in Postgres database names which are limited to 64 chars); we might
	//lift this restriction in the future, but there is no immediate need for it
	if len(input) > 48 {
		return false
	}
	return RepoPathComponentRx.MatchString(input)
}

// OriginalRequestURL returns the URL that the original requester used when
// sending an HTTP request. This inspects the X-Forwarded-* set of headers to
// identify reverse proxying.
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

// AppendQuery adds additional query parameters to an existing unparsed URL.
func AppendQuery(urlStr string, query url.Values) string {
	if strings.Contains(urlStr, "?") {
		return urlStr + "&" + query.Encode()
	}
	return urlStr + "?" + query.Encode()
}

// MaybeTimeToUnix casts a time.Time instance into its UNIX timestamp while preserving nil-ness.
func MaybeTimeToUnix(t *time.Time) *int64 {
	if t == nil {
		return nil
	}
	val := t.Unix()
	return &val
}

func MinMaybeTime(t1, t2 *time.Time) *time.Time {
	if t1 == nil {
		return t2
	}
	if t2 == nil {
		return t1
	}

	if t1.Before(*t2) {
		return t1
	} else {
		return t2
	}
}

func MaxMaybeTime(t1, t2 *time.Time) *time.Time {
	if t1 == nil {
		return t2
	}
	if t2 == nil {
		return t1
	}

	if t1.After(*t2) {
		return t1
	} else {
		return t2
	}
}
