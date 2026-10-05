// SPDX-FileCopyrightText: 2026 Stefan Majewsky <majewsky@gmx.net>
// SPDX-License-Identifier: Apache-2.0

package pathrouter

import "go.xyrillian.de/gg/options"

// Variable is a [Matcher] that accepts subpaths with at least one path element.
// The value of the first path element will be collected into the [Context],
// and the remaining subpath will have to be accepted by the next matcher.
// The collected path element can be retrieved with [Context.Variable].
func Variable(name string, matcher Matcher) Matcher {
	return variable(name, matcher.downcast())
}

func variable(name string, matcher realMatcher) Matcher {
	return realMatcher{
		minLength: matcher.minLength + 1,
		maxLength: options.Map(matcher.maxLength, increment),
		accept: func(path []string, rc Context) HandlerFunc {
			if len(path) == 0 || path[0] == "" {
				return nil
			}
			handlerFunc := matcher.accept(path[1:], rc)
			if handlerFunc == nil {
				return nil
			}
			rc.vars[name] = pathUnescape(path[0])
			return handlerFunc
		},
	}
}
