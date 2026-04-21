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
	"github.com/sapcc/go-bits/httptest"

	"github.com/sapcc/keppel/internal/keppel"
	"github.com/sapcc/keppel/internal/test"
)

func TestListTags(t *testing.T) {
	ctx := t.Context()

	testWithPrimary(t, nil, func(s test.Setup) {
		readOnlyTokenHeaders := s.GetTokenHeaders(t, "repository:test1/foo:pull")

		// test tag list for missing repo
		s.RespondTo(ctx, "GET /v2/test1/foo/tags/list",
			httptest.WithHeaders(readOnlyTokenHeaders),
		).ExpectJSON(t, http.StatusNotFound, test.ErrorCode(keppel.ErrNameUnknown))

		// upload a test image without tagging it
		image := test.GenerateImage( /* no layers */ )
		image.MustUpload(t, s, fooRepoRef, "")

		// test empty tag list for existing repo (query parameters do not influence this result)
		for _, query := range []string{"", "?n=10", "?n=10&last=foo"} {
			s.RespondTo(ctx, "GET /v2/test1/foo/tags/list"+query,
				httptest.WithHeaders(readOnlyTokenHeaders),
			).ExpectJSON(t, http.StatusOK, jsonmatch.Object{
				"name": "test1/foo",
				"tags": []string{},
			})
		}

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
		s.RespondTo(ctx, "GET /v2/test1/foo/tags/list",
			httptest.WithHeaders(readOnlyTokenHeaders),
		).ExpectJSON(t, http.StatusOK, jsonmatch.Object{
			"name": "test1/foo",
			"tags": allTagNames,
		})

		// test paginated
		for offset := range allTagNames {
			for length := 1; length <= len(allTagNames)+1; length++ {
				expectedPage := allTagNames[offset:]
				expectedHeaders := http.Header{
					"Content-Type": {"application/json"},
				}

				if len(expectedPage) > length {
					expectedPage = expectedPage[:length]
					lastRepoName := expectedPage[len(expectedPage)-1]
					expectedHeaders.Set("Link", fmt.Sprintf(`</v2/test1/foo/tags/list?last=%s&n=%d>; rel="next"`,
						strings.ReplaceAll(lastRepoName, "/", "%2F"), length,
					))
				}

				methodAndPath := fmt.Sprintf(`GET /v2/test1/foo/tags/list?n=%d`, length)
				if offset > 0 {
					methodAndPath += `&last=` + allTagNames[offset-1]
				}

				s.RespondTo(ctx, methodAndPath, httptest.WithHeaders(readOnlyTokenHeaders)).
					ExpectHeaders(t, expectedHeaders).
					ExpectJSON(t, http.StatusOK, jsonmatch.Object{
						"name": "test1/foo",
						"tags": expectedPage,
					})
			}
		}

		// test error cases for pagination query params
		s.RespondTo(ctx, "GET /v2/test1/foo/tags/list?n=-1", httptest.WithHeaders(readOnlyTokenHeaders)).
			ExpectText(t, http.StatusBadRequest, `invalid value for "n": strconv.ParseUint: parsing "-1": invalid syntax`+"\n")
		s.RespondTo(ctx, "GET /v2/test1/foo/tags/list?n=0", httptest.WithHeaders(readOnlyTokenHeaders)).
			ExpectText(t, http.StatusBadRequest, `invalid value for "n": must not be 0`+"\n")

		// test anycast tag listing
		if currentlyWithAnycast {
			testWithReplica(t, s, "on_first_use", func(firstPass bool, s2 test.Setup) {
				testAnycast(t, firstPass, s2.DB, func() {
					anycastTokenHeaders := s.GetAnycastTokenHeaders(t, "repository:test1/foo:pull")
					for _, setup := range []test.Setup{s, s2} {
						setup.RespondTo(ctx, "GET /v2/test1/foo/tags/list",
							httptest.WithHeaders(anycastTokenHeaders),
						).ExpectJSON(t, http.StatusOK, jsonmatch.Object{
							"name": "test1/foo",
							"tags": allTagNames,
						})
					}
				})
			})
		}
	})
}
