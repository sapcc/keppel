// SPDX-FileCopyrightText: 2026 Stefan Majewsky <majewsky@gmx.net>
// SPDX-License-Identifier: Apache-2.0

// Package pathrouter contains an HTTP router that differentiates endpoints based on paths without any regex matching.
//
// # Comparison to other HTTP router libraries
//
// In contrast to most other router implementations, pathrouter does not take its routes as a list of declarations.
// Instead, the routing table is declared as a nested structure describing the decision tree that the router follows:
//
//	// not like this (this is sometimes called "Sinatra style" after a popular framework using this approach)
//	r := otherlibrary.NewRouter()
//	r.Handle(http.MethodGet, "/v1/objects", api.ListObjects)
//	r.Handle(http.MethodPost, "/v1/objects/new", api.CreateObject)
//	r.Handle(http.MethodDelete, "/v1/objects/:id", api.DeleteObject)
//	r.Handle(http.MethodGet, "/v1/objects/:id", api.GetObject)
//	r.Handle(http.MethodPatch, "/v1/objects/:id", api.PatchObject)
//
//	// but like this
//	import pr "go.xyrillian.de/gg/pathrouter"
//	r := pr.Element("v1",
//		pr.Element("objects",
//			pr.Choice(
//				pr.Here(pr.Handlers(pr.ByMethod{
//					http.MethodGet: api.ListObjects,
//				})),
//				pr.Element("new", pr.Here(pr.Handlers(pr.ByMethod{
//					http.MethodPost: api.CreateObject,
//				}))),
//				pr.Variable("id", pr.Here(pr.Handlers(pr.ByMethod{
//					http.MethodDelete: api.DeleteObject,
//					http.MethodGet:    api.GetObject,
//					http.MethodPatch:  api.PatchObject,
//				}))),
//			),
//		),
//	)
//
// Compared to other common router libraries like [gorilla/mux],
// pathrouter does not use regular expressions in its implementation,
// thus making it extremely fast at the expense of reducing flexibility in what can be matched.
// For instance, in the example above, requests for /v1/objects/:id will accept any non-empty string for the "id" variable.
// Package pathrouter expects that request handlers will perform additional format checks on extracted path variables as required.
//
// Unlike other fast HTTP router libraries such as [httprouter] or [httptreemux], pathrouter can match a catch-all path (i.e. a variable extending over multiple path elements) anywhere in the path, not just at the end.
// The only limitation with catch-all paths is that only one catch-all path may be matched per route, e.g. "/v1/objects/*path/relations" can be matched, but "v1/objects/*path/compare/*otherpath" cannot.
//
// # Handling of escape sequences in paths
//
// Path matching is performed on the escaped form of the URL path, as returned by [url.URL.EscapedPath],
// so any slashes that were encoded as %2F in the URL path will be considered part of a path element instead of a boundary.
//
// For instance, using the example above, the URL path "/v1/objects/42/23" would not match and generate a 404 response,
// but the URL path "/v1/objects/42%2F23" would match and invoke (e.g. with method GET) the GetObject handler with vars["id"] = "42/23".
// Like in this example, [HandlerFunc] will receive unescaped values in its vars argument.
//
// # Implicit normalizations
//
// If the request path contains consecutive runs of unescaped slashes, they will be normalized to behave like a single slash.
// For example, "http://localhost//foo%2F//bar//" is identical to "http://localhost/foo%2F/bar/".
//
// [gorilla/mux]: https://github.com/gorilla/mux
// [httprouter]: https://github.com/julienschmidt/httprouter
// [httptreemux]: https://github.com/dimfeld/httptreemux
package pathrouter

import (
	"context"
	"net/http"
	"net/url"
	"strings"

	. "go.xyrillian.de/gg/option"
)

var (
	// force imports that are necessary to make docstring links work
	_ context.Context = nil
	_ url.URL
)

// Matcher is the common type for any actor that can inspect the path of a request URL (or a suffix of it)
// and either accept or decline to handle the request.
//
// Matcher implements the ServeHTTP method of [http.Handler] and can thus be used with any net/http facility like [http.ListenAndServe].
//
// Alternatively, the TryServeHTTP method behaves like ServeHTTP, but will not render a "404 Not Found" response
// when the matcher is not capable of handling requests with the given path, instead only returning false without touching the ResponseWriter.
// This method may be useful when composing e.g. multiple [Matcher] instances that each implement a different API with different endpoints.
type Matcher interface {
	http.Handler
	TryServeHTTP(w http.ResponseWriter, r *http.Request) bool

	// Casts a Matcher into type realMatcher, which is the only type that actually implements this interface.
	// This allows eliminating fat pointers in the matcher tree.
	downcast() realMatcher
}

type realMatcher struct {
	accept func(path []string, vars map[string]string) HandlerFunc

	// The smallest len(path) that accept() can accept.
	minLength int
	// The largest len(path) that accept() can accept, or None if accept() can handle arbitrarily long paths.
	maxLength Option[int]
}

// ServeHTTP implements the [Matcher] interface.
func (m realMatcher) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !m.TryServeHTTP(w, r) {
		http.NotFound(w, r)
	}
}

// TryServeHTTP implements the [Matcher] interface.
func (m realMatcher) TryServeHTTP(w http.ResponseWriter, r *http.Request) bool {
	path := extractPath(r.URL)
	vars := make(map[string]string)
	handlerFunc := m.accept(path, vars)
	if handlerFunc == nil {
		return false
	} else {
		handlerFunc(w, r, vars)
		return true
	}
}

// realMatcher implements the [Matcher] interface.
func (m realMatcher) downcast() realMatcher {
	return m
}

func extractPath(u *url.URL) []string {
	// e.g. u.Path = "//foo//bar//" becomes ["", "", "foo", "", "", "bar", "", ""]
	path := strings.Split(u.EscapedPath(), "/")

	// sequences of slashes are supposed to behave like single slashes
	// e.g. u.Path = "//foo//bar//" becomes ["foo", "bar", ""] here
	for idx := 0; idx < len(path)-1; idx++ {
		if path[idx] == "" {
			copy(path[idx:], path[idx+1:])
			path = path[0 : len(path)-1]
			idx--
		}
	}

	return path
}
