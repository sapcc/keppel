// SPDX-FileCopyrightText: 2026 Stefan Majewsky <majewsky@gmx.net>
// SPDX-License-Identifier: Apache-2.0

package pathrouter

import (
	"strings"

	. "go.xyrillian.de/gg/option"
)

// CatchAllVariable is a [Matcher] that accepts subpaths with an arbitrary number of path elements.
// The value of any amount of leading path elements will be collected into vars[name],
// such that the remainder is accepted by the next matcher.
// At least one element must be collected into vars[name].
//
// CatchAllVariable() may appear at any point within the routing tree,
// but it may not contain another CatchAllVariable() anywhere within it.
func CatchAllVariable(name string, matcher Matcher) Matcher {
	return catchAllVariable(name, matcher.downcast())
}

func catchAllVariable(name string, matcher realMatcher) Matcher {
	// NOTE: The specific behavior of CatchAllVariable() is why this package exists in the first place.
	//       I wanted to replace gorilla/mux with something more performance in Keppel,
	//       but all the fast routers do not accept catch-all variables in the middle of a path
	//       like the OCI Distribution API requires (e.g. "/v2/*repo/manifests/:reference" with "repo" being a full path).

	innerMinLength := matcher.minLength
	innerMaxLength, ok := matcher.maxLength.Unpack()
	if !ok {
		panic("matcher within CatchAllVariable() may not accept unlimited path lengths")
	}

	accept := func(path []string, vars map[string]string) HandlerFunc {
		for length := innerMinLength; length <= innerMaxLength; length++ {
			if length > len(path) {
				break
			}
			caughtPath, subpath := path[0:len(path)-length], path[len(path)-length:]
			if len(caughtPath) == 0 {
				continue
			}

			handlerFunc := matcher.accept(subpath, vars)
			if handlerFunc == nil {
				continue
			}
			vars[name] = pathUnescape(strings.Join(caughtPath, "/"))
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
