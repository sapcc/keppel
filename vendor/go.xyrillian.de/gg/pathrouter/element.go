// SPDX-FileCopyrightText: 2026 Stefan Majewsky <majewsky@gmx.net>
// SPDX-License-Identifier: Apache-2.0

package pathrouter

import (
	"net/http"
	"slices"
	"strings"

	"go.xyrillian.de/gg/options"
)

// Element is a [Matcher] that accepts subpaths with at least one path element.
// The first path element must be equal to the given value,
// and the remaining subpath will have to be accepted by the next matcher.
//
// To match a trailing slash in the request path, Element can be used with "/" as its argument:
//
//	pathrouter.Element("/", pathrouter.Handlers(...))
//
// But usually, when matching request paths with trailing slashes is desired, [Here] should be used instead.
func Element(value string, matcher Matcher) Matcher {
	return element(value, matcher.downcast())
}

func element(value string, matcher realMatcher) Matcher {
	switch value {
	case "":
		panic(`Element() called with value = ""`)
	case "/":
		value = "" // trailing slash will lead to an empty element in `path`, e.g. strings.Split("foo/bar/") == []string{"foo","bar",""}
	default:
	}

	return realMatcher{
		minLength: matcher.minLength + 1,
		maxLength: options.Map(matcher.maxLength, increment),
		accept: func(path []string, rc routingContext) http.HandlerFunc {
			if len(path) == 0 || path[0] != value {
				return nil
			}
			return matcher.accept(path[1:], rc)
		},
	}
}

// Elements is a shorthand for a chain of [Element] calls. For example,
//
//	pathrouter.Element("v1", pathrouter.Element("admin", pathrouter.Element("warnings", innerMatcher)))
//
// can be shortened to
//
//	pathrouter.Elements("v1/admin/warnings", innerMatcher)))
//
// Slashes in the provided value will be treated as delimiting separate path elements, so escaped slashes will not count.
// For instance, the example above might match a request path of "/v1/admin/warnings/253", but not "/v1/admin%2Fwarnings/253".
func Elements(value string, matcher Matcher) Matcher {
	values := strings.Split(value, "/")
	for _, value := range slices.Backward(values) {
		matcher = Element(value, matcher)
	}
	return matcher
}
