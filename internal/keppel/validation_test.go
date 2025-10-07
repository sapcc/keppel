// SPDX-FileCopyrightText: 2025 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package keppel

import (
	"testing"

	"github.com/sapcc/go-bits/assert"
)

func TestCelExpressionRegexs(t *testing.T) {
	testCases := []struct {
		expression string
		matches    bool
		labels     []string
	}{
		// positive cases:
		{`'foo' in labels`, true, []string{"foo"}},
		{`'foo' in labels && 'bar' in labels`, true, []string{"foo", "bar"}},
		{`'foo' in labels&&'bar' in labels`, true, []string{"foo", "bar"}},
		{`'foo' in labels && 'bar' in labels && 'baz' in labels`, true, []string{"foo", "bar", "baz"}},
		{`'foo' in labels && 'bar' in labels && 'baz' in labels && 'quux' in labels`, true, []string{"foo", "bar", "baz", "quux"}},
		{`'foo bar' in labels`, true, []string{"foo bar"}},
		{`'foo-bar_baz.123' in labels`, true, []string{"foo-bar_baz.123"}},
		{`'FOO' in labels && 'BAR' in labels && 'BAZ' in labels`, true, []string{"FOO", "BAR", "BAZ"}},
		{`'foo' in labels && 'bar' in labels && ' ' in labels`, true, []string{"foo", "bar", " "}},

		// negative cases:
		{`'foo' in labels || 'bar' in labels`, false, nil},
		{`'foo' in labels && ('bar' in labels || 'baz' in labels)`, false, nil},
		{`'foo' in labels && ! ('bar' in labels)`, false, nil},
		{`'foo' in labels && 'bar' not in labels`, false, nil},
		{`'foo' in labels && 'bar' in labels && 'baz' not in labels`, false, nil},
		{`'' in labels`, false, nil},
		{`'foo' not in labels`, false, nil},
		{`foo in labels`, false, nil},
		{`'foo' in label`, false, nil},
		{`'	foo' in labels && 'bar' in label`, false, nil},
		{`'foo' in labels && 'bar' in labels && '' in labels`, false, nil},

		// do not allow single quotes to make regex' easier
		{`'foo 'bar'' in labels`, false, nil},
		{`'foo' in labels && ''' in labels`, false, nil},
	}

	for _, tc := range testCases {
		t.Run(tc.expression, func(t *testing.T) {
			matches := celExpressionRx.MatchString(tc.expression)
			assert.Equal(t, matches, tc.matches)

			if matches {
				labels := extractRequiredLabelsFromCEL(tc.expression)
				assert.DeepEqual(t, tc.expression, labels, tc.labels)
			} else {
				assert.DeepEqual(t, tc.expression, nil, tc.labels)
			}
		})
	}
}
