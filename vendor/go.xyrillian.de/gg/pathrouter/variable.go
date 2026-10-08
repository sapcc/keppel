// SPDX-FileCopyrightText: 2026 Stefan Majewsky <majewsky@gmx.net>
// SPDX-License-Identifier: Apache-2.0

package pathrouter

import (
	"net/http"

	"go.xyrillian.de/gg/options"
)

// Variable is a [Matcher] that accepts subpaths with at least one path element.
// The value of the first path element will be collected
// (to be retrieved inside the request handler using [VariableValue]),
// and the remaining subpath will have to be accepted by the next matcher.
func Variable(name string, matcher Matcher) Matcher {
	return variable(name, nil, matcher.downcast())
}

// VariableIf is like [Variable], but only matches if the path element is accepted by the given predicate.
// The predicate will receive the unescaped form of the path element, matching the return value of later [VariableValue] calls.
func VariableIf(name string, predicate func(string) bool, matcher Matcher) Matcher {
	return variable(name, predicate, matcher.downcast())
}

func variable(name string, predicate func(string) bool, matcher realMatcher) Matcher {
	return realMatcher{
		minLength: matcher.minLength + 1,
		maxLength: options.Map(matcher.maxLength, increment),
		accept: func(path []string, rc routingContext) http.HandlerFunc {
			if len(path) == 0 || path[0] == "" {
				return nil
			}
			variableValue := pathUnescape(path[0])
			if predicate != nil && !predicate(variableValue) {
				return nil
			}
			handlerFunc := matcher.accept(path[1:], rc)
			if handlerFunc == nil {
				return nil
			}
			rc.vars[name] = variableValue
			return handlerFunc
		},
	}
}
