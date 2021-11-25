/*******************************************************************************
*
* Copyright 2021 SAP SE
*
* Licensed under the Apache License, Version 2.0 (the "License");
* you may not use this file except in compliance with the License.
* You should have received a copy of the License along with this
* program. If not, you may obtain a copy of the License at
*
*     http://www.apache.org/licenses/LICENSE-2.0
*
* Unless required by applicable law or agreed to in writing, software
* distributed under the License is distributed on an "AS IS" BASIS,
* WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
* See the License for the specific language governing permissions and
* limitations under the License.
*
*******************************************************************************/

package api

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/sapcc/keppel/internal/keppel"
)

//AddDomainRemapMiddleware adds to the handler chain a middleware that implements domain remapping, i.e. rewriting of URLs like
//
//    https://<account>.<public-hostname>/v2/<path>
//
//into
//
//    https://<public-hostname>/v2/<account>/<path>
//
func AddDomainRemapMiddleware(cfg keppel.Configuration, h http.Handler) http.Handler {
	return domainRemapMiddleware{cfg, h}
}

type domainRemapMiddleware struct {
	cfg  keppel.Configuration
	next http.Handler
}

//ServeHTTP implements the http.Handler interface.
func (h domainRemapMiddleware) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	//if request does not use domain remapping, forward to the next handler unchanged
	rewrittenURL, accountName, matches := h.tryRewriteURL(keppel.OriginalRequestURL(r))
	if !matches {
		h.next.ServeHTTP(w, r)
		return
	}

	//if the request is for a domain-remapped hostname, but we don't know how to
	//rewrite it, we reject it entirely
	if rewrittenURL == nil {
		http.Error(w, "request path invalid for this hostname", http.StatusBadRequest)
		return
	}

	//otherwise, rewrite request object for the next handler (with a new Request
	//object to ensure that previous handlers work properly after we return)
	reqCloned := *r
	reqCloned.URL = &url.URL{
		Path:     rewrittenURL.Path,
		RawQuery: rewrittenURL.RawQuery,
	}
	reqCloned.Header = make(http.Header, len(r.Header)+2)
	for k, v := range r.Header {
		reqCloned.Header[k] = v
	}
	reqCloned.Header.Set("X-Forwarded-Host", rewrittenURL.Host)
	reqCloned.Header.Set("X-Forwarded-Proto", rewrittenURL.Scheme)

	//we also need to intercept the ResponseWriter to rewrite URLs in the Location response header
	wWrapped := domainRemapResponseWriter{w, accountName, false}
	h.next.ServeHTTP(&wWrapped, &reqCloned)
}

func (h domainRemapMiddleware) tryRewriteURL(u url.URL) (rewrittenURL *url.URL, accountName string, matches bool) {
	//hostname must look like "<account>.<rest>"
	hostParts := strings.SplitN(u.Host, ".", 2)
	if len(hostParts) != 2 {
		return nil, "", false
	}

	//head must look like an account name
	if !keppel.RepoPathComponentRx.MatchString(hostParts[0]) {
		return nil, "", false
	}

	//tail must be one of our public URL hostnames
	switch {
	case hostParts[1] == h.cfg.APIPublicURL.Host:
		//acceptable
	case h.cfg.AnycastAPIPublicURL != nil && hostParts[1] == h.cfg.AnycastAPIPublicURL.Host:
		//acceptable
	default:
		//nope
		return nil, "", false
	}

	//can only rewrite requests for the Registry API, otherwise the request is completely invalid
	if !strings.HasPrefix(u.Path, "/v2/") {
		return nil, hostParts[0], true
	}

	//perform rewrite (except for GET /v2/ which needs to be served as-is)
	result := u
	result.Host = hostParts[1]
	if u.Path != "/v2/" {
		result.Path = fmt.Sprintf("/v2/%s/%s", hostParts[0], strings.TrimPrefix(u.Path, "/v2/"))
	}
	return &result, hostParts[0], true
}

type domainRemapResponseWriter struct {
	inner         http.ResponseWriter
	accountName   string
	headerWritten bool
}

//Header implements the http.ResponseWriter interface.
func (w *domainRemapResponseWriter) Header() http.Header {
	return w.inner.Header()
}

//WriteHeader implements the http.ResponseWriter interface.
func (w *domainRemapResponseWriter) WriteHeader(statusCode int) {
	if w.headerWritten {
		return
	}

	//if the API generated a Location header with a relative URL, we need to
	//rewrite its path to match the path structure of the remapped domain
	locHeader := w.inner.Header().Get("Location")
	accountPrefix := fmt.Sprintf("/v2/%s/", w.accountName)
	if strings.HasPrefix(locHeader, accountPrefix) {
		locHeader = "/v2/" + strings.TrimPrefix(locHeader, accountPrefix)
		w.inner.Header().Set("Location", locHeader)
	}

	w.inner.WriteHeader(statusCode)
	w.headerWritten = true
}

//Write implements the http.ResponseWriter interface.
func (w *domainRemapResponseWriter) Write(buf []byte) (int, error) {
	if !w.headerWritten {
		w.WriteHeader(http.StatusOK)
	}
	return w.inner.Write(buf)
}
