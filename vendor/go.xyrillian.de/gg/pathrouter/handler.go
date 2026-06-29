// SPDX-FileCopyrightText: 2026 Stefan Majewsky <majewsky@gmx.net>
// SPDX-License-Identifier: Apache-2.0

package pathrouter

import (
	"fmt"
	"net/http"
	"slices"
	"strings"

	. "go.xyrillian.de/gg/option"
)

// HandlerFunc is like [http.HandlerFunc], but also receives variable values that were extracted from the URL path.
//
// Package pathrouter passes variables explicitly like this, instead of via [context.Context.WithValue],
// because doing so imposes a performance penalty for every usage of the respective context.
type HandlerFunc = func(w http.ResponseWriter, r *http.Request, vars map[string]string)

// ByMethod is a set of request handlers matching the same request path, keyed on request method.
// It is commonly constructed as a literal using the respective constants from the net/http package, such as [http.MethodGet].
// This type appears in the signature of func [Handlers], see documentation over there for details.
type ByMethod map[string]HandlerFunc

// Handlers is a [Matcher] that accepts empty subpaths only.
// It appears at the leaf nodes of a [Matcher] tree, which correspond to specific endpoint paths,
// and contains the actual request handlers for one endpoint path:
//
//	import pr "go.xyrillian.de/gg/pathrouter"
//	handler := pr.Element("v1", pr.Choice(
//		pr.Element("teams", pr.Variable("id", pr.Handlers(pr.ByMethod{
//			http.MethodGet:    handleGetTeam,    // GET /v1/teams/:id
//			http.MethodPut:    handlePutTeam,    // PUT /v1/teams/:id
//			http.MethodDelete: handleDeleteTeam, // DELETE /v1/teams/:id
//		}))),
//		pr.Element("employees", pr.Variable("id", pr.Handlers(pr.ByMethod{
//			http.MethodGet: handleGetEmployee, // GET /v1/employees/:id
//		}))),
//	))
//
// If there is a handler for [http.MethodGet], but none for [http.MethodHead], the GET handler will be called for HEAD as well.
// To have a GET handler, but no HEAD handler, set the handler for [http.MethodHead] to nil.
// Any other use of a nil [HandlerFunc] is invalid and will cause a panic.
func Handlers(m ByMethod) Matcher {
	// reuse GET handler for HEAD if not overridden
	if handler, exists := m[http.MethodGet]; exists {
		if _, exists := m[http.MethodHead]; !exists {
			m[http.MethodHead] = handler
		}
		if m[http.MethodHead] == nil {
			delete(m, http.MethodHead)
		}
	}

	// check that all handlers are valid
	for method, handler := range m {
		if handler == nil {
			panic(fmt.Sprintf("handler is nil for method %q", method))
		}
	}

	// precomputations for accept()
	allowHeader := m.buildAllowHeader()
	serve := func(w http.ResponseWriter, r *http.Request, vars map[string]string) {
		handler, ok := m[r.Method]
		if ok {
			handler(w, r, vars)
			return
		}

		w.Header().Set("Allow", allowHeader)
		if r.Method == http.MethodOptions {
			http.Error(w, "", http.StatusOK)
		} else {
			http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		}
	}

	return realMatcher{
		minLength: 0,
		maxLength: Some(0),
		accept: func(path []string, vars map[string]string) HandlerFunc {
			if len(path) != 0 {
				return nil
			}
			return serve
		},
	}
}

func (m ByMethod) buildAllowHeader() string {
	allowedMethods := make([]string, 0, len(m))
	for method := range m {
		allowedMethods = append(allowedMethods, method)
	}
	slices.Sort(allowedMethods)
	return strings.Join(allowedMethods, ", ")
}
