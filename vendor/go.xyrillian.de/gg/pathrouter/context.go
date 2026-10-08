// SPDX-FileCopyrightText: 2026 Stefan Majewsky <majewsky@gmx.net>
// SPDX-License-Identifier: Apache-2.0

package pathrouter

import (
	"context"
	"maps"
	"net/http"
)

type routingContextKeyType struct{}

var routingContextKey = any(routingContextKeyType{})

// routingContext contains information about an HTTP request that has been processed by matchers from this package.
// It is stored in `r.Body` of the HTTP request by wrapping the original response body's behavior.
type routingContext struct {
	vars map[string]string
}

func injectContext(r *http.Request) (*http.Request, routingContext) {
	// NOTE: Using context.WithValue() and thus extending the chain within r.Context by another element
	//       incurs a performance penalty whenever `r.Context` is used, but it's the option that sucks the least.
	//       We initially had `type HandlerFunc = func(http.ResponseWriter, *http.Request, pathrouter.Context)`
	//       to avoid this performance penalty, but having another thing that's called "Context" is confusing.
	//       The final API implementation accepts a slight performance decrease to prioritize developer ergonomics.
	//
	//       The only other option that does not add an extra argument to the HandlerFunc signature is to stash
	//       `type routingContext` within `r.Body`, but that would incur the performance penalty on every use of
	//       `r.Body` instead of `r.Context()`, which is usually much worse.

	rc := routingContext{
		vars: make(map[string]string),
	}
	ctx := context.WithValue(r.Context(), routingContextKey, rc)
	return r.WithContext(ctx), rc
}

// VariableValue returns the path element(s) collected by a [Variable] or [CatchAllVariable] matcher of the same name,
// with any URL escaping removed.
// If no matcher with this name appeared in the path leading up to the handler, the empty string is returned.
//
// Panics if r is not a request that was routed by a [Matcher] from this package.
func VariableValue(r *http.Request, name string) string {
	rc, ok := r.Context().Value(routingContextKey).(routingContext)
	if !ok {
		panic("request was not routed by gg/pathrouter")
	}
	return rc.vars[name]
}

// VariableValues returns all path element(s) collected by [Variable] or [CatchAllVariable] matchers, keyed on the name of the matcher.
// The result is the set of all possible values that [VariableValue] can return.
//
// Panics if r is not a request that was routed by a [Matcher] from this package.
func VariableValues(r *http.Request) map[string]string {
	rc, ok := r.Context().Value(routingContextKey).(routingContext)
	if !ok {
		panic("request was not routed by gg/pathrouter")
	}
	return maps.Clone(rc.vars)
}
