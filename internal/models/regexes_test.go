// SPDX-FileCopyrightText: 2025 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package models

import (
	"fmt"
	"testing"

	"github.com/sapcc/go-bits/assert"
)

func TestRepoNameWithLeadingSlashRx(t *testing.T) {
	type testCase struct {
		input    string
		expected bool
	}

	cases := []testCase{
		// from the examples
		{
			input:    "/library/alpine",
			expected: true,
		},
		{
			input:    "/library/alpine/nonsense",
			expected: true,
		},
		// some extra cases
		{
			input:    "/",
			expected: false,
		},
		{
			input:    "library/alpine",
			expected: false,
		},
		{
			input:    "/library/alpine/",
			expected: false,
		},
		{
			input:    "/library//alpine",
			expected: false,
		},
	}

	for _, tc := range cases {
		matches := RepoNameWithLeadingSlashRx.MatchString(tc.input)
		assert.DeepEqual(t, fmt.Sprintf("matches %q", tc.input), matches, tc.expected)
	}
}

func TestImageReferenceRx(t *testing.T) {
	type testCase struct {
		input    string
		expected []string
	}

	cases := []testCase{
		// from the examples
		{
			input:    "/library/ubuntu:latest",
			expected: []string{"/library/ubuntu:latest", "/library/ubuntu", "latest", ""},
		},
		{
			input:    "/repo/image:nonsense",
			expected: []string{"/repo/image:nonsense", "/repo/image", "nonsense", ""},
		},
		{
			input:    "/myrepo/myimage:e9707504ad0d4c119036b6d41ace4a33596139d3feb9ccb6617813ce48c3eeef",
			expected: []string{"/myrepo/myimage:e9707504ad0d4c119036b6d41ace4a33596139d3feb9ccb6617813ce48c3eeef", "/myrepo/myimage", "e9707504ad0d4c119036b6d41ace4a33596139d3feb9ccb6617813ce48c3eeef", ""},
		},
		{
			input:    "/myrepo/myimage@sha256:e9707504ad0d4c119036b6d41ace4a33596139d3feb9ccb6617813ce48c3eeef",
			expected: []string{"/myrepo/myimage@sha256:e9707504ad0d4c119036b6d41ace4a33596139d3feb9ccb6617813ce48c3eeef", "/myrepo/myimage", "", "sha256:e9707504ad0d4c119036b6d41ace4a33596139d3feb9ccb6617813ce48c3eeef"},
		},
		{
			input:    "/myrepo/myimage:nonsense@sha256:e9707504ad0d4c119036b6d41ace4a33596139d3feb9ccb6617813ce48c3eeef",
			expected: []string{"/myrepo/myimage:nonsense@sha256:e9707504ad0d4c119036b6d41ace4a33596139d3feb9ccb6617813ce48c3eeef", "/myrepo/myimage", "nonsense", "sha256:e9707504ad0d4c119036b6d41ace4a33596139d3feb9ccb6617813ce48c3eeef"},
		},

		// some extra cases
		{
			input:    "/repo/image",
			expected: []string{"/repo/image", "/repo/image", "", ""},
		},
		{
			input:    "/image:tag",
			expected: []string{"/image:tag", "/image", "tag", ""},
		},
		{
			input:    "/image",
			expected: []string{"/image", "/image", "", ""},
		},
		{
			input:    "/library/ubuntu:latest:extra",
			expected: nil,
		},
		{
			input:    "/myrepo//myimage",
			expected: nil,
		},
		{
			input:    "/repo/image@sha256:",
			expected: nil,
		},
		{
			input:    "/repo/image@sha256:short",
			expected: nil,
		},
		{
			input:    "image",
			expected: nil,
		},
		{
			input:    "/:tag",
			expected: nil,
		},
		{
			input:    "",
			expected: nil,
		},
	}

	for _, tc := range cases {
		submatches := ImageReferenceRx.FindStringSubmatch(tc.input)
		assert.DeepEqual(t, fmt.Sprintf("submatches %q", tc.input), submatches, tc.expected)
	}
}
