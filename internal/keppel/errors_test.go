// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package keppel

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func BenchmarkWriteAsRegistryV2ResponseTo(b *testing.B) {
	b.ReportAllocs()

	errObj := ErrUnauthorized.
		With("authentication required").
		WithDetail(map[string]any{"scope": "repository:test1/foo:pull"}).
		WithHeader("WWW-Authenticate", `Bearer realm="https://registry.example.org/keppel/v1/auth"`)

	req := httptest.NewRequest(http.MethodGet, "/v2/test1/foo/manifests/latest", http.NoBody)

	b.ResetTimer()
	for b.Loop() {
		rec := httptest.NewRecorder()
		errObj.WriteAsRegistryV2ResponseTo(rec, req)
		if rec.Code == 0 {
			b.Fatal("no status code written")
		}
	}
}
