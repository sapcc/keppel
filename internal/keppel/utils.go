// SPDX-FileCopyrightText: 2020 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package keppel

import (
	"net/http"
	"net/url"
	"strings"
	"time"
)

// OriginalRequestURL returns the URL that the original requester used when
// sending an HTTP request. This inspects the X-Forwarded-* set of headers to
// identify reverse proxying.
func OriginalRequestURL(r *http.Request) url.URL {
	u := url.URL{
		Path:     r.URL.Path,
		RawQuery: r.URL.RawQuery,
	}

	// case 1: we are behind a reverse proxy
	u.Host = r.Header.Get("X-Forwarded-Host")
	if u.Host != "" {
		u.Scheme = r.Header.Get("X-Forwarded-Proto")
		if u.Scheme == "" {
			u.Scheme = "http"
		}
		return u
	}

	// case 2: we are not behind a reverse proxy, but the Host header indicates how the user reached us
	if r.Host != "" {
		u.Host = r.Host
		u.Scheme = "http"
		return u
	}

	// case 3: no idea how the user got here - don't include any guesses in the URL
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
