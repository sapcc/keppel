// SPDX-FileCopyrightText: 2026 Stefan Majewsky <majewsky@gmx.net>
// SPDX-License-Identifier: Apache-2.0

package pathrouter

import (
	. "go.xyrillian.de/gg/option"
)

// Here is a [Matcher] that matches effectively empty subpaths, that is:
// subpaths that are either empty or only contain a heretofore unmatched trailing slash.
// It can be used to opt-in to allowing trailing slashes at the ends of URL paths:
//
//	// only matches "/foo/bar"
//	pr.Element("foo", pr.Element("bar", pr.Handlers(...))
//
//	// also matches "/foo/bar/"
//	pr.Element("foo", pr.Element("bar", pr.Here(pr.Handlers(...)))
//
// The next matcher must match only the empty path, so currently only [Handlers] is allowed.
//
// Here is provided as a useful shorthand, but could be implemented using the other matchers like so:
//
//	// this...
//	m := pr.Here(pr.Handlers(byMethod))
//
//	// ...is equivalent to this
//	h := pr.Handlers(byMethod)
//	m := pr.Choice(h, pr.Element("/", h))
//
// The name "Here" makes sense when Here() appears as one of several options within Choice(),
// where the other options would match subpaths, while Here() matches, well, "here".
// The example in the package docstring illustrates this.
func Here(matcher Matcher) Matcher {
	return here(matcher.downcast())
}

func here(matcher realMatcher) Matcher {
	if matcher.minLength != 0 || matcher.maxLength != Some(0) {
		panic("matcher within Here() must be Handlers()")
	}

	return realMatcher{
		minLength: 0,
		maxLength: Some(1),
		accept: func(path []string, vars map[string]string) HandlerFunc {
			if len(path) == 0 || (len(path) == 1 && path[0] == "") {
				return matcher.accept(nil, vars)
			}
			return nil
		},
	}
}
