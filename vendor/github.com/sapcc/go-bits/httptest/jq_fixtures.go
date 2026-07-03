// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package httptest

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/itchyny/gojq"
	"go.xyrillian.de/gg/jsonmatch"
)

// JQUnmodifiedContent is an interface to represent types that can be used as
// variables in jq expressions, but cannot be modified themselves.
type JQUnmodifiedContent interface {
	parse() (any, error)
}

// JQUnmodifiedJSONString makes a string containing a raw JSON payload
// available as [JQUnmodifiedContent] for a ModifyWithVariable() call on a [JQModifiableContent].
func JQUnmodifiedJSONString(value string) JQUnmodifiedContent {
	return jqUnmodifiedJSONString(value)
}

type jqUnmodifiedJSONString string

// parse implements the [JQUnmodifiedContent] interface.
func (j jqUnmodifiedJSONString) parse() (any, error) {
	var parsed any
	err := json.Unmarshal([]byte(j), &parsed)
	if err != nil {
		return "", fmt.Errorf("failed to parse input: %w", err)
	}
	return parsed, nil
}

// JQUnmodifiedJSONFixture makes a JSON fixture file available as
// [JQUnmodifiedContent] for a ModifyWithVariable() call on a [JQModifiableContent].
func JQUnmodifiedJSONFixture(value string) JQUnmodifiedContent {
	return jqUnmodifiedJSONFixture(value)
}

type jqUnmodifiedJSONFixture string

// parse implements the [JQUnmodifiedContent] interface.
func (j jqUnmodifiedJSONFixture) parse() (any, error) {
	pathString := string(j)
	originalJSON, err := os.ReadFile(pathString)
	if err != nil {
		return "", fmt.Errorf("failed to read fixture file %s: %w", pathString, err)
	}
	jAsString := jqUnmodifiedJSONString(originalJSON)
	parsed, err := jAsString.parse()
	if err != nil {
		return nil, fmt.Errorf("failed to process fixture file %s: %w", pathString, err)
	}
	return parsed, nil
}

// JQModifiableContent is an interface to represent types that can be modified
// with jq expressions.
type JQModifiableContent interface {
	jsonmatch.Diffable
	json.Marshaler
	// Modify appends one or more jq expressions that will be applied to the
	// modifiable content when functions from the [jsonmatch.Diffable]
	// interface are called. Multiple modifications are piped together in the
	// order they were added. The receiver is returned to allow method chaining.
	Modify(modifications ...string) JQModifiableContent
	// ModifyWithVariable appends one jq expression that will be applied to the
	// modifiable content when functions from the [jsonmatch.Diffable]
	// interface are called. Contrary to Modify, it can reference a variable via
	// the statically defined "$ref" variable. Multiple modifications are piped
	// together in the order they were added. The receiver is returned to allow
	// method chaining.
	ModifyWithVariable(modification string, ref JQUnmodifiedContent) JQModifiableContent
}

// jqModifiableJSONString represents a JSON string, that can be modified
// with jq syntax and implements the [jsonmatch.Diffable] interface.
type jqModifiableJSONString struct {
	jsonString      string
	refs            []JQUnmodifiedContent
	testIdentifier  string
	jqModifications []string
}

// NewJQModifiableJSONString creates a new [JQModifiableContent] for the
// given JSON string. The testIdentifier is used in error messages to
// help identify which test caused a failure.
func NewJQModifiableJSONString(jsonString, testIdentifier string) JQModifiableContent {
	return jqModifiableJSONString{
		jsonString:     jsonString,
		testIdentifier: testIdentifier,
	}
}

// Modify implements the [JQModifiableContent] interface.
func (j jqModifiableJSONString) Modify(modifications ...string) JQModifiableContent {
	result := j
	result.jqModifications = append(slices.Clone(j.jqModifications), modifications...)
	return result
}

// ModifyWithVariable implements the [JQModifiableContent] interface.
func (j jqModifiableJSONString) ModifyWithVariable(modification string, ref JQUnmodifiedContent) JQModifiableContent {
	result := j
	result.jqModifications = append(slices.Clone(j.jqModifications), modification)
	result.refs = append(slices.Clone(j.refs), ref)
	return result
}

// MarshalJSON implements the json.Marshaler interface.
func (j jqModifiableJSONString) MarshalJSON() ([]byte, error) {
	diffable, err := j.toDiffable()
	if err != nil {
		return nil, err
	}
	return json.Marshal(diffable)
}

// toDiffable parses the JSON string, applies all accumulated jq
// modifications, and converts the result into a jsonmatch.Diffable. It returns
// an error if the JSON is malformed, the jq expression is invalid,
// or the expression produces multiple results.
func (j jqModifiableJSONString) toDiffable() (diffable jsonmatch.Diffable, err error) {
	var parsedInput any
	err = json.Unmarshal([]byte(j.jsonString), &parsedInput)
	if err != nil {
		return diffable, fmt.Errorf("failed to parse input for test %q: %w", j.testIdentifier, err)
	}

	// early return for no modifications
	if slices.IndexFunc(j.jqModifications, func(mod string) bool { return strings.TrimSpace(mod) != "." }) == -1 {
		// convert raw input
		diffable, err = convertRecursively(parsedInput)
		if err != nil {
			return diffable, fmt.Errorf("failed to convert raw input to jsonmatch for test %q: %w", j.testIdentifier, err)
		}
		return diffable, nil
	}

	// prepare refs
	jqModification := strings.Join(j.jqModifications, " | ")
	refRegex := regexp.MustCompile(`\$ref\b`)
	i := 0
	jqModification = string(refRegex.ReplaceAllFunc([]byte(jqModification), func(match []byte) []byte {
		i++
		return []byte("$ref" + strconv.Itoa(i))
	}))
	if i != len(j.refs) {
		return diffable, fmt.Errorf("different number of $ref used than provided for test %q: %d used, %d provided", j.testIdentifier, i, len(j.refs))
	}
	refVars := make([]string, len(j.refs))
	refStrings := make([]any, len(j.refs))
	for i, ref := range j.refs {
		refVars[i] = "$ref" + strconv.Itoa(i+1)
		refStrings[i], err = ref.parse()
		if err != nil {
			return diffable, fmt.Errorf("failed to read data for $ref #%d for test %q: %w", i+1, j.testIdentifier, err)
		}
	}

	// do jq modifications
	query, err := gojq.Parse(jqModification)
	if err != nil {
		return diffable, fmt.Errorf("failed to parse query for test %q: %w", j.testIdentifier, err)
	}
	code, err := gojq.Compile(query, gojq.WithVariables(refVars))
	if err != nil {
		return diffable, fmt.Errorf("failed to compile query for test %q: %w", j.testIdentifier, err)
	}
	iter := code.Run(parsedInput, refStrings...)
	var modifiedInput any
	for i := range 2 {
		v, ok := iter.Next()
		if !ok {
			break
		}
		if i > 0 {
			return diffable, fmt.Errorf("failed to apply modifications for test %q: modifications which produce multiple results are not supported", j.testIdentifier)
		}
		if err, ok = v.(error); ok {
			return diffable, fmt.Errorf("failed to apply modifications for test %q: %w", j.testIdentifier, err)
		}
		modifiedInput = v
	}

	// convert modified input
	diffable, err = convertRecursively(modifiedInput)
	if err != nil {
		return diffable, fmt.Errorf("failed to convert modifications to jsonmatch for test %q: %w", j.testIdentifier, err)
	}
	return diffable, nil
}

// DiffAgainst implements the [JQModifiableContent] interface.
func (j jqModifiableJSONString) DiffAgainst(buf []byte) []jsonmatch.Diff {
	diffable, err := j.toDiffable()
	if err != nil {
		return []jsonmatch.Diff{{
			Kind:         fmt.Sprintf("JSON string processing error (%s)", err.Error()),
			Pointer:      "",
			ExpectedJSON: "<unknown>",
			ActualJSON:   strings.ToValidUTF8(string(buf), "\uFFFD"),
		}}
	}
	return diffable.DiffAgainst(buf)
}

// jqModifiableJSONFixture represents a JSON fixture file, that can be
// modified with jq syntax and implements the jsonmatch.Diffable interface.
// We do not save it back to a modified fixture file, so that we circumvent
// all the possible problems of JSON-fixture files.
type jqModifiableJSONFixture struct {
	filePath        string
	refs            []JQUnmodifiedContent
	testIdentifier  string
	jqModifications []string
}

// NewJQModifiableJSONFixture creates a new [JQModifiableContent] for the
// given JSON fixture file path. The testIdentifier is used in error messages to
// help identify which test caused a failure.
func NewJQModifiableJSONFixture(filePath, testIdentifier string) JQModifiableContent {
	return jqModifiableJSONFixture{
		filePath:       filePath,
		testIdentifier: testIdentifier,
	}
}

// Modify implements the [JQModifiableContent] interface.
func (j jqModifiableJSONFixture) Modify(modifications ...string) JQModifiableContent {
	result := j
	result.jqModifications = append(slices.Clone(j.jqModifications), modifications...)
	return result
}

// ModifyWithVariable implements the [JQModifiableContent] interface.
func (j jqModifiableJSONFixture) ModifyWithVariable(modification string, ref JQUnmodifiedContent) JQModifiableContent {
	result := j
	result.jqModifications = append(slices.Clone(j.jqModifications), modification)
	result.refs = append(slices.Clone(j.refs), ref)
	return result
}

// MarshalJSON implements the json.Marshaler interface.
func (j jqModifiableJSONFixture) MarshalJSON() ([]byte, error) {
	diffable, err := j.toDiffable()
	if err != nil {
		return nil, err
	}
	return json.Marshal(diffable)
}

// toDiffable reads the JSON fixture file, applies all accumulated jq
// modifications, and converts the result into a jsonmatch.Diffable. It returns
// an error if the file cannot be read, the JSON is malformed, the jq expression
// is invalid, or the expression produces multiple results.
func (j jqModifiableJSONFixture) toDiffable() (diffable jsonmatch.Diffable, err error) {
	originalJSON, err := os.ReadFile(j.filePath)
	if err != nil {
		return diffable, fmt.Errorf("failed to read fixture file %s: %w", j.filePath, err)
	}

	// handle as string to share code
	stringRepresentation := jqModifiableJSONString{
		jsonString:      string(originalJSON),
		refs:            j.refs,
		testIdentifier:  j.testIdentifier,
		jqModifications: j.jqModifications,
	}
	diffable, err = stringRepresentation.toDiffable()
	if err != nil {
		return diffable, fmt.Errorf("failed to process fixture file %s: %w", j.filePath, err)
	}
	return diffable, nil
}

// DiffAgainst implements the [JQModifiableContent] interface.
func (j jqModifiableJSONFixture) DiffAgainst(buf []byte) []jsonmatch.Diff {
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
