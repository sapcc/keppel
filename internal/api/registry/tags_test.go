// SPDX-FileCopyrightText: 2020 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package registryv2_test

import (
	"fmt"
	"math/rand"
	"net/http"
	"sort"
	"strings"
	"testing"

	"github.com/majewsky/gg/jsonmatch"
	"github.com/sapcc/go-bits/assert"
	"github.com/sapcc/go-bits/httptest"

	"github.com/sapcc/keppel/internal/keppel"
	"github.com/sapcc/keppel/internal/test"
)

func TestListTags(t *testing.T) {
	testWithPrimary(t, nil, func(s test.Setup) {
		ctx := t.Context()
		h := s.Handler
		readOnlyToken := s.GetToken(t, "repository:test1/foo:pull")

		// test tag list for missing repo
		resp := h.RespondTo(ctx, "GET /v2/test1/foo/tags/list",
			httptest.WithHeader("Authorization", "Bearer "+readOnlyToken))
		resp.ExpectJSON(t, http.StatusNotFound, test.ErrorCode(keppel.ErrNameUnknown))
		assert.Equal(t, resp.Header().Get(test.VersionHeaderKey), test.VersionHeaderValue)

		// upload a test image without tagging it
		image := test.GenerateImage( /* no layers */ )
		image.MustUpload(t, s, fooRepoRef, "")

		// test empty tag list for existing repo
		doEmptyTagList := func(path string) {
			resp := h.RespondTo(ctx, "GET "+path,
				httptest.WithHeader("Authorization", "Bearer "+readOnlyToken))
			resp.ExpectJSON(t, http.StatusOK, jsonmatch.Object{"name": "test1/foo", "tags": jsonmatch.Array{}})
			assert.Equal(t, resp.Header().Get(test.VersionHeaderKey), test.VersionHeaderValue)
		}
		doEmptyTagList("/v2/test1/foo/tags/list")

		// query parameters do not influence this result
		doEmptyTagList("/v2/test1/foo/tags/list?n=10")
		doEmptyTagList("/v2/test1/foo/tags/list?n=10&last=foo")

		// generate pseudo-random, but deterministic tag names
		allTagNames := make([]string, 10)
		sidGen := test.StorageIDGenerator{}
		for idx := range allTagNames {
			allTagNames[idx] = sidGen.Next()
		}

		// upload test image under all of them (in randomized order!)
		rand.Shuffle(len(allTagNames), func(i, j int) {
			allTagNames[i], allTagNames[j] = allTagNames[j], allTagNames[i]
		})
		for _, tagName := range allTagNames {
			image.MustUpload(t, s, fooRepoRef, tagName)
		}
		// but when listing tags, we expect them in sorted order
		sort.Strings(allTagNames)

		// test unpaginated
		resp2 := h.RespondTo(ctx, "GET /v2/test1/foo/tags/list",
			httptest.WithHeader("Authorization", "Bearer "+readOnlyToken))
		resp2.ExpectJSON(t, http.StatusOK, jsonmatch.Object{"name": "test1/foo", "tags": allTagNames})
		assert.Equal(t, resp2.Header().Get(test.VersionHeaderKey), test.VersionHeaderValue)

		// test paginated
		for offset := range allTagNames {
			for length := 1; length <= len(allTagNames)+1; length++ {
				expectedPage := allTagNames[offset:]
				var linkHeader string

				if len(expectedPage) > length {
					expectedPage = expectedPage[:length]
					lastRepoName := expectedPage[len(expectedPage)-1]
					linkHeader = fmt.Sprintf(`</v2/test1/foo/tags/list?last=%s&n=%d>; rel="next"`,
						strings.ReplaceAll(lastRepoName, "/", "%2F"), length,
					)
				}

				path := fmt.Sprintf(`/v2/test1/foo/tags/list?n=%d`, length)
				if offset > 0 {
					path += `&last=` + allTagNames[offset-1]
				}

				resp := h.RespondTo(ctx, "GET "+path, httptest.WithHeader("Authorization", "Bearer "+readOnlyToken))
				resp.ExpectJSON(t, http.StatusOK, jsonmatch.Object{"name": "test1/foo", "tags": expectedPage})
				assert.Equal(t, resp.Header().Get(test.VersionHeaderKey), test.VersionHeaderValue)
				assert.Equal(t, resp.Header().Get("Content-Type"), "application/json")
				if linkHeader != "" {
					assert.Equal(t, resp.Header().Get("Link"), linkHeader)
				}
			}
		}

		// test error cases for pagination query params
		resp3 := h.RespondTo(ctx, "GET /v2/test1/foo/tags/list?n=-1",
			httptest.WithHeader("Authorization", "Bearer "+readOnlyToken))
		resp3.ExpectText(t, http.StatusBadRequest, "invalid value for \"n\": strconv.ParseUint: parsing \"-1\": invalid syntax\n")
		assert.Equal(t, resp3.Header().Get(test.VersionHeaderKey), test.VersionHeaderValue)

		resp4 := h.RespondTo(ctx, "GET /v2/test1/foo/tags/list?n=0",
			httptest.WithHeader("Authorization", "Bearer "+readOnlyToken))
		resp4.ExpectText(t, http.StatusBadRequest, "invalid value for \"n\": must not be 0\n")
		assert.Equal(t, resp4.Header().Get(test.VersionHeaderKey), test.VersionHeaderValue)

		// test anycast tag listing
		if currentlyWithAnycast {
			testWithReplica(t, s, "on_first_use", func(firstPass bool, s2 test.Setup) {
				h2 := s2.Handler
				testAnycast(t, firstPass, s2.DB, func() {
					anycastToken := s.GetAnycastToken(t, "repository:test1/foo:pull")
					anycastHeaders := []httptest.RequestOption{
						httptest.WithHeader("Authorization", "Bearer "+anycastToken),
						httptest.WithHeader("X-Forwarded-Host", s.Config.AnycastAPIPublicHostname),
						httptest.WithHeader("X-Forwarded-Proto", "https"),
					}
					resp := h.RespondTo(ctx, "GET /v2/test1/foo/tags/list", anycastHeaders...)
					resp.ExpectJSON(t, http.StatusOK, jsonmatch.Object{"name": "test1/foo", "tags": allTagNames})
					assert.Equal(t, resp.Header().Get(test.VersionHeaderKey), test.VersionHeaderValue)

					resp2 := h2.RespondTo(ctx, "GET /v2/test1/foo/tags/list", anycastHeaders...)
					resp2.ExpectJSON(t, http.StatusOK, jsonmatch.Object{"name": "test1/foo", "tags": allTagNames})
					assert.Equal(t, resp2.Header().Get(test.VersionHeaderKey), test.VersionHeaderValue)
				})
			})
		}
	})
}
