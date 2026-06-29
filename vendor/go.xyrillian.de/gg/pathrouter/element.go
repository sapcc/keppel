// SPDX-FileCopyrightText: 2026 Stefan Majewsky <majewsky@gmx.net>
// SPDX-License-Identifier: Apache-2.0

package pathrouter

import "go.xyrillian.de/gg/options"

// Element is a [Matcher] that accepts subpaths with at least one path element.
// The first path element must be equal to the given value,
// and the remaining subpath will have to be accepted by the next matcher.
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
		accept: func(path []string, vars map[string]string) HandlerFunc {
			if len(path) == 0 || path[0] != value {
				return nil
			}
			return matcher.accept(path[1:], vars)
		},
	}
}
