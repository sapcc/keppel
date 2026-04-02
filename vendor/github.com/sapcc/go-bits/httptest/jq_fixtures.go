// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package httptest

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/itchyny/gojq"
	"github.com/majewsky/gg/jsonmatch"
)

// JQModifiableJSONFixture represents a JSON fixture file, that can be
// modified with jq syntax before converting it a jsonmatch.Diffable format.
// We do not save it back to a modified fixture file, so that we circumvent
// all the possible problems of JSON-fixture files.
type JQModifiableJSONFixture struct {
	filePath        string
	testIdentifier  string
	jqModifications []string
}

// NewJQModifiableJSONFixture creates a new JQModifiableJSONFixture for the
// given fixture file path. The testIdentifier is used in error messages to
// help identify which test caused a failure.
func NewJQModifiableJSONFixture(filePath, testIdentifier string) JQModifiableJSONFixture {
	return JQModifiableJSONFixture{
		filePath:       filePath,
		testIdentifier: testIdentifier,
	}
}

// Modify appends one or more jq expressions that will be applied to the
// fixture's JSON content when converting it to a jsonmatch.Diffable. Multiple
// modifications are piped together in the order they were added. The receiver
// is returned to allow method chaining.
func (j JQModifiableJSONFixture) Modify(modifications ...string) JQModifiableJSONFixture {
	result := j
	result.jqModifications = append(slices.Clone(j.jqModifications), modifications...)
	return result
}

// ToDiffable reads the JSON fixture file, applies all accumulated jq
// modifications, and converts the result into a jsonmatch.Diffable. It returns
// an error if the file cannot be read, the JSON is malformed, the jq expression
// is invalid, or the expression produces multiple results.
func (j JQModifiableJSONFixture) toDiffable() (diffable jsonmatch.Diffable, err error) {
	originalJSON, err := os.ReadFile(j.filePath)
	if err != nil {
		return diffable, fmt.Errorf("failed to read fixture file %s: %w", j.filePath, err)
	}
	var parsedInput any
	err = json.Unmarshal(originalJSON, &parsedInput)
	if err != nil {
		return diffable, fmt.Errorf("failed to parse fixture file %s: %w", j.filePath, err)
	}

	// early return for no modifications
	if slices.IndexFunc(j.jqModifications, func(mod string) bool { return strings.TrimSpace(mod) != "." }) == -1 {
		// convert raw input
		diffable, err = convertRecursively(parsedInput)
		if err != nil {
			return diffable, fmt.Errorf("failed to convert raw input to jsonmatch for test %s: %w", j.testIdentifier, err)
		}
		return diffable, nil
	}

	// do jq modifications
	jqModification := strings.Join(j.jqModifications, " | ")
	query, err := gojq.Parse(jqModification)
	if err != nil {
		return diffable, fmt.Errorf("failed to parse query for test %s: %w", j.testIdentifier, err)
	}
	code, err := gojq.Compile(query)
	if err != nil {
		return diffable, fmt.Errorf("failed to compile query for test %s: %w", j.testIdentifier, err)
	}
	iter := code.Run(parsedInput)
	var modifiedInput any
	for i := range 2 {
		v, ok := iter.Next()
		if !ok {
			break
		}
		if i > 0 {
			return diffable, errors.New("modifications which produce multiple results are not supported")
		}
		if err, ok = v.(error); ok {
			return diffable, fmt.Errorf("failed to apply modifications for test %s: %w", j.testIdentifier, err)
		}
		modifiedInput = v
	}

	// convert modified input
	diffable, err = convertRecursively(modifiedInput)
	if err != nil {
		return diffable, fmt.Errorf("failed to convert modifications to jsonmatch for test %s: %w", j.testIdentifier, err)
	}
	return diffable, nil
}

// DiffAgainst implements the [jsonmatch.Diffable] interface.
func (j JQModifiableJSONFixture) DiffAgainst(buf []byte) []jsonmatch.Diff {
	diffable, err := j.toDiffable()
	if err != nil {
		return []jsonmatch.Diff{{
			Kind:         fmt.Sprintf("fixture processing error (%s)", err.Error()),
			Pointer:      "",
			ExpectedJSON: "<unknown>",
			ActualJSON:   strings.ToValidUTF8(string(buf), "\uFFFD"),
		}}
	}
	return diffable.DiffAgainst(buf)
}

func convertRecursively(input any) (jsonmatch.Diffable, error) {
	switch v := input.(type) {
	// the first 5 cases only occur on top level, as convertRecursively is not called on lower scalars
	case nil:
		return jsonmatch.Null(), nil
	case bool:
		return jsonmatch.Scalar(v), nil
	case int:
		return jsonmatch.Scalar(v), nil
	case float64:
		return jsonmatch.Scalar(v), nil
	case string:
		return jsonmatch.Scalar(v), nil
	case map[string]any:
		result := make(jsonmatch.Object, len(v))
		for key, val := range v {
			if isScalarValue(val) {
				result[key] = val
			} else {
				converted, err := convertRecursively(val)
				if err != nil {
					return nil, err
				}
				result[key] = converted
			}
		}
		return result, nil
	case []any:
		result := make(jsonmatch.Array, 0, len(v))
		for _, val := range v {
			if isScalarValue(val) {
				result = append(result, val)
			} else {
				converted, err := convertRecursively(val)
				if err != nil {
					return nil, err
				}
				result = append(result, converted)
			}
		}
		return result, nil
	default:
		// this will happen with types like big.Int and json.Number and whatever other unnecessary fancy
		// types that gojq might return
		return nil, fmt.Errorf("received unsupported type from gojq %T", v)
	}
}

func isScalarValue(v any) bool {
	switch v.(type) {
	case nil, bool, int, float64, string:
		return true
	default:
		return false
	}
}
