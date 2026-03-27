// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package keppelv1_test

import (
	"testing"

	"github.com/sapcc/go-bits/easypg"
	"github.com/sapcc/go-bits/httptest"
)

func TestMain(m *testing.M) {
	easypg.WithTestDB(m, func() int { return m.Run() })
}

// Shorthand for setting the X-Test-Perms header in a test request.
func withPerms(testPermsHeader string) httptest.RequestOption {
	return httptest.WithHeader("X-Test-Perms", testPermsHeader)
}
