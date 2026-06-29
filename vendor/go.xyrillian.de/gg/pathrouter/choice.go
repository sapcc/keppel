// SPDX-FileCopyrightText: 2026 Stefan Majewsky <majewsky@gmx.net>
// SPDX-License-Identifier: Apache-2.0

package pathrouter

import (
	"slices"

	. "go.xyrillian.de/gg/option"
	"go.xyrillian.de/gg/options"
)

// Choice is a [Matcher] that accepts all subpaths that are accepted by any of its contained matchers.
// If multiple matchers accept the requested subpath, the first match wins.
//
// Choice is used to specify different matchers for different subpaths,
// as illustrated in the example in the package docstring.
func Choice(matchers ...Matcher) Matcher {
	downcasted := make([]realMatcher, len(matchers))
	for idx, matcher := range matchers {
		downcasted[idx] = matcher.downcast()
	}
	return choice(downcasted)
}

func choice(matchers []realMatcher) Matcher {
	if len(matchers) == 0 {
		panic("Choice() called without any matchers")
	}

	var (
		minLengths     = make([]int, len(matchers))
		maxLengths     = make([]Option[int], len(matchers))
		maxLengthIsInf = false
	)
	for idx, m := range matchers {
		minLengths[idx] = m.minLength
		maxLengths[idx] = m.maxLength
		if m.maxLength.IsNone() {
			maxLengthIsInf = true
		}
	}
	maxLength := None[int]()
	if !maxLengthIsInf {
		maxLength = options.Max(maxLengths...)
	}

	return realMatcher{
		minLength: slices.Min(minLengths),
		maxLength: maxLength,
		accept: func(path []string, vars map[string]string) HandlerFunc {
			for _, m := range matchers {
				hf := m.accept(path, vars)
				if hf != nil {
					return hf
				}
			}
			return nil
		},
	}
}
