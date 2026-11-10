// SPDX-FileCopyrightText: 2026 Stefan Majewsky <majewsky@gmx.net>
// SPDX-License-Identifier: Apache-2.0

package pgruntime

import (
	"strings"
)

// Convenience function for preparing an identifier that needs to be inserted into a query verbatim
// (e.g. a database name for CREATE DATABASE).
func quoteIdentifier(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}
