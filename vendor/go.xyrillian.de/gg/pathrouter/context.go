// SPDX-FileCopyrightText: 2026 Stefan Majewsky <majewsky@gmx.net>
// SPDX-License-Identifier: Apache-2.0

package pathrouter

// Context contains information about an HTTP request that has been processed by matchers from this package.
// It appears as an argument in [HandlerFunc].
type Context struct {
	vars map[string]string
}

func newContext() Context {
	return Context{
		vars: make(map[string]string),
	}
}

// Variable returns the path element(s) collected by a [Variable] or [CatchAllVariable] matcher with the given name,
// with any URL escaping removed.
// If no matcher with this name appeared in the path leading up to the handler, the empty string is returned.
func (rc Context) Variable(name string) string {
	return rc.vars[name]
}
