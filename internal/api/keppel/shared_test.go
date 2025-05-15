// SPDX-FileCopyrightText: 2024 SAP SE
// SPDX-License-Identifier: Apache-2.0

package keppelv1_test

import (
	"testing"

	"github.com/sapcc/go-bits/easypg"
)

func TestMain(m *testing.M) {
	easypg.WithTestDB(m, func() int { return m.Run() })
}
