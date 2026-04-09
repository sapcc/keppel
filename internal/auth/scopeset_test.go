// SPDX-FileCopyrightText: 2020 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"testing"
)

func BenchmarkNewScopeSet(b *testing.B) {
	b.ReportAllocs()

	scopes := [][]Scope{
		{
			{
				ResourceType: "repository",
				ResourceName: "example/app",
				Actions:      []string{"pull"},
			},
		},
		{
			{
				ResourceType: "repository",
				ResourceName: "example/app",
				Actions:      []string{"pull"},
			},
			{
				ResourceType: "repository",
				ResourceName: "example/app",
				Actions:      []string{"push"},
			},
		},
		{
			{
				ResourceType: "repository",
				ResourceName: "example/app",
				Actions:      []string{"pull"},
			},
			{
				ResourceType: "repository",
				ResourceName: "example/sidecar",
				Actions:      []string{"pull"},
			},
		},
	}

	b.ResetTimer()
	for i := range b.N {
		_ = NewScopeSet(scopes[i%len(scopes)]...)
	}
}
