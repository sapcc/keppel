// SPDX-FileCopyrightText: 2020 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package keppel

import (
	"net/http"
	"net/url"
	"strings"
	"time"

	. "github.com/majewsky/gg/option"
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
func MaybeTimeToUnix(t Option[time.Time]) Option[int64] {
	tt, ok := t.Unpack()
	if !ok {
		return None[int64]()
	}
	return Some(tt.Unix())
}

// MinMaybeTime returns the earlier of two Option[time.Time], or None if they are both None.
func MinMaybeTime(t1, t2 Option[time.Time]) Option[time.Time] {
	tt1, ok := t1.Unpack()
	if !ok {
		return t2
	}
	tt2, ok := t2.Unpack()
	if !ok {
		return t1
	}

	if tt1.Before(tt2) {
		return t1
	} else {
		return t2
	}
}

// MaxMaybeTime returns the later of two Option[time.Time], or None if they are both None.
func MaxMaybeTime(t1, t2 Option[time.Time]) Option[time.Time] {
	tt1, ok := t1.Unpack()
	if !ok {
		return t2
	}
	tt2, ok := t2.Unpack()
	if !ok {
		return t1
	}

	if tt1.After(tt2) {
		return t1
	} else {
		return t2
	}
}
