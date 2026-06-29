// SPDX-FileCopyrightText: 2026 Stefan Majewsky <majewsky@gmx.net>
// SPDX-License-Identifier: Apache-2.0

package pathrouter

import (
	"fmt"
	"net/url"
)

func pathUnescape(in string) string {
	out, err := url.PathUnescape(in)
	if err != nil {
		// defense in depth: should not fail because `in` was indirectly obtained from r.URL.EscapedPath()
		panic(fmt.Sprintf("PathUnescape failed on %q: %s", in, err.Error()))
	}
	return out
}

func increment(x int) int {
	return x + 1
}
