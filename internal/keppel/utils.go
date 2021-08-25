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
	"time"
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

//MaybeTimeToUnix casts a time.Time instance into its UNIX timestamp while preserving nil-ness.
func MaybeTimeToUnix(t *time.Time) *int64 {
	if t == nil {
		return nil
	}
	val := t.Unix()
	return &val
}
