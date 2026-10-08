// SPDX-FileCopyrightText: 2026 Stefan Majewsky <majewsky@gmx.net>
// SPDX-License-Identifier: Apache-2.0

package pathrouter

import (
	"net/http"
	"strings"

	. "go.xyrillian.de/gg/option"
)

// CatchAllVariable is a [Matcher] that accepts subpaths with an arbitrary number of path elements.
// The value of any amount of leading path elements will be collected
// (to be retrieved inside the request handler using [VariableValue]),
// such that the remainder is accepted by the next matcher.
// At least one element must be collected.
//
// CatchAllVariable() may appear at any point within the routing tree,
// but it may not contain another CatchAllVariable() anywhere within it.
func CatchAllVariable(name string, matcher Matcher) Matcher {
	return catchAllVariable(name, nil, matcher.downcast())
}

// CatchAllVariableIf is like [CatchAllVariable], but only matches if the collected path elements are accepted by the given predicate.
// The predicate will receive the unescaped form of the collected path elements, matching the return value of later [VariableValue] calls.
func CatchAllVariableIf(name string, predicate func(string) bool, matcher Matcher) Matcher {
	return catchAllVariable(name, predicate, matcher.downcast())
}

func catchAllVariable(name string, predicate func(string) bool, matcher realMatcher) Matcher {
	// NOTE: The specific behavior of CatchAllVariable() is why this package exists in the first place.
	//       I wanted to replace gorilla/mux with something more performant in Keppel,
	//       but all the fast routers do not accept catch-all variables in the middle of a path
	//       like the OCI Distribution API requires (e.g. "/v2/*repo/manifests/:reference" with "repo" being a full path).

	innerMinLength := matcher.minLength
	innerMaxLength, ok := matcher.maxLength.Unpack()
	if !ok {
		panic("matcher within CatchAllVariable() may not accept unlimited path lengths")
	}

	accept := func(path []string, rc routingContext) http.HandlerFunc {
		for length := innerMinLength; length <= innerMaxLength; length++ {
			if length > len(path) {
				break
			}
			caughtPath, subpath := path[0:len(path)-length], path[len(path)-length:]
			if len(caughtPath) == 0 {
				continue
			}
			variableValue := pathUnescape(strings.Join(caughtPath, "/"))
			if predicate != nil && !predicate(variableValue) {
				continue
			}

			handlerFunc := matcher.accept(subpath, rc)
			if handlerFunc == nil {
				continue
			}
			rc.vars[name] = variableValue
			return handlerFunc
		}
		return nil
	}

	return realMatcher{
		minLength: innerMinLength + 1,
		maxLength: None[int](),
		accept:    accept,
	}
}
