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
	return domainRemap{cfg, h}
}

type domainRemap struct {
	cfg  keppel.Configuration
	next http.Handler
}

//ServeHTTP implements the http.Handler interface.
func (h domainRemap) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	//if request does not use domain remapping, forward to the next handler unchanged
	rewrittenURL, matches := h.tryRewriteURL(keppel.OriginalRequestURL(r))
	if !matches {
		h.next.ServeHTTP(w, r)
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
	h.next.ServeHTTP(w, &reqCloned)
}

func (h domainRemap) tryRewriteURL(u url.URL) (result url.URL, matches bool) {
	//can only rewrite requests for the Registry API
	if !strings.HasPrefix(u.Path, "/v2/") {
		return url.URL{}, false
	}

	//hostname must look like "<account>.<rest>"
	hostParts := strings.SplitN(u.Host, ".", 2)
	if len(hostParts) != 2 {
		return url.URL{}, false
	}

	//head must look like an account name
	if !keppel.RepoPathComponentRx.MatchString(hostParts[0]) {
		return url.URL{}, false
	}

	//tail must be one of our public URL hostnames
	switch {
	case hostParts[1] == h.cfg.APIPublicURL.Host:
		//acceptable
	case h.cfg.AnycastAPIPublicURL != nil && hostParts[1] == h.cfg.AnycastAPIPublicURL.Host:
		//acceptable
	default:
		//nope
		return url.URL{}, false
	}

	//perform rewrite
	result = u
	result.Host = hostParts[1]
	result.Path = fmt.Sprintf("/v2/%s/%s", hostParts[0], strings.TrimPrefix(u.Path, "/v2/"))
	return result, true
}
