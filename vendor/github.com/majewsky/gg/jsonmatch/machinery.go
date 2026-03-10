// SPDX-FileCopyrightText: 2025 Stefan Majewsky <majewsky@gmx.net>
// SPDX-License-Identifier: Apache-2.0

package jsonmatch

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"

	. "github.com/majewsky/gg/option"
)

func marshalExpectedForDiff(value any) string {
	// `expected` values can technically contain any sort of nonsense,
	// so we want to print something useful even if marshaling fails
	buf, err := json.Marshal(value)
	if err != nil {
		return fmt.Sprintf("<not marshalable to JSON, %%#v is %#v>", value)
	}
	return string(buf)
}

func marshalActualForDiff(value any) string {
	// `actual` values are always safe to marshal because they were
	// unmarshaled from JSON into any and thus can only contain safe
	buf, err := json.Marshal(value)
	if err != nil {
		// this line is therefore unreachable in tests and only exists as defense in depth
		return fmt.Sprintf("<marshal error: %s>", err.Error())
	}
	return string(buf)
}

// Given a string that is probably a JSON message, look at the first non-blank
// character to determine what kind of value the JSON message has on its top level.
// The exact character returned does not matter; this is only used to check if two messages are vaguely of the same type.
func kindForJSONMessage(s string) byte {
	s = strings.TrimSpace(s)
	if s == "" {
		// defense in depth: this function should never be called on functionally empty inputs
		return '?'
	}
	b := s[0]
	switch b {
	case '{', '[', '"', 'n':
		return b // object, array, string or null respectively
	case '0', '1', '2', '3', '4', '5', '6', '7', '8', '9', '-':
		return '0' // number (NOTE: leading + is not allowed, leading decimal dot is not allowed without a 0 before it)
	case 't', 'f':
		return 'b' // boolean
	default:
		return '?' // syntax error
	}
}

// Entrypoint into this file coming from all DiffAgainst() implementations.
func diffAgainst(expected any, buf []byte) []Diff {
	var actual any
	err := json.Unmarshal(buf, &actual)
	if err != nil {
		return []Diff{{
			Kind:         fmt.Sprintf("unmarshal error (%s)", err.Error()),
			Pointer:      "",
			ExpectedJSON: marshalExpectedForDiff(expected),
			ActualJSON:   strings.ToValidUTF8(string(buf), "\uFFFD"),
		}}
	}

	// While recursing through the object, we maintain a `path` that identifies
	// where we are in the callstack, e.g. when comparing
	//
	//	actual = { "foo": { "bar": [ 5, 23 ] } }
	//	expected = { "foo": { "bar": [ 5, 42 ] } }
	//
	// we would generate a diff at Path = {"foo", "bar", 1}. Since diffs are
	// usually rare, we only build Pointer strings out of these paths when we
	// really need them. During recursion, `path` is maintained as a sequence of
	// path fragments, most of which are constants to keep allocations to a
	// minimum. WARNING: Because the `path` slice is heavily reused across nested
	// function calls, it is not safe to store references to the `path` slice.
	path := make([]pathElement, 0, 32)
	return getDiffsForValue(path, expected, actual)
}

type pathElement struct {
	Key   Option[string]
	Index int
}

func keyElement(key string) pathElement { return pathElement{Some(key), 0} }
func indexElement(idx int) pathElement  { return pathElement{None[string](), idx} }

func pathIntoPointer(path []pathElement) Pointer {
	if len(path) == 0 {
		return ""
	}
	fragments := make([]string, len(path)+1)
	fragments[0] = ""
	for idx, elem := range path {
		if key, ok := elem.Key.Unpack(); ok {
			fragments[idx+1] = keyIntoPointerFragment(key)
		} else {
			fragments[idx+1] = strconv.Itoa(elem.Index)
		}
	}
	return Pointer(strings.Join(fragments, "/"))
}

func keyIntoPointerFragment(key string) string {
	buf, _ := json.Marshal(key)
	s := string(buf)
	s = strings.TrimPrefix(s, "\"")
	s = strings.TrimSuffix(s, "\"")
	s = strings.ReplaceAll(s, "~", "~0")
	s = strings.ReplaceAll(s, "/", "~1")
	return s
}

// NOTE: getDiffsForValue is the main part of the recursion to generate the diff.
func getDiffsForValue(path []pathElement, expected, actual any) []Diff {
	// specialized handling for relevant recursible or capturable types
	switch expected := expected.(type) {
	case map[string]any:
		return getDiffsForObject(path, expected, actual)
	case Object:
		return getDiffsForObject(path, expected, actual)
	case []any:
		return getDiffsForArray(path, expected, actual)
	case []map[string]any:
		downcasted := make([]any, len(expected))
		for idx, val := range expected {
			downcasted[idx] = val
		}
		return getDiffsForArray(path, downcasted, actual)
	case []Object:
		downcasted := make([]any, len(expected))
		for idx, val := range expected {
			downcasted[idx] = val
		}
		return getDiffsForArray(path, downcasted, actual)
	case capturedField:
		return getDiffsForCapturedField(path, expected, actual)
	case nil:
		// this case needs to be handled separately because the code below
		// cannot deal with reflect.TypeOf(expected) returning nil
		if actual == nil {
			return nil
		} else {
			return []Diff{{
				Kind:         "type mismatch",
				Pointer:      pathIntoPointer(path),
				ExpectedJSON: "null",
				ActualJSON:   marshalActualForDiff(actual),
			}}
		}
	}

	// generic handling for values or structures that we do not recurse into further:
	// check that `expected` encodes to JSON in an equivalent way to `actual`
	actualJSON := marshalActualForDiff(actual)
	expectedJSON := marshalExpectedForDiff(expected)
	if expectedJSON == actualJSON {
		return nil
	}

	// if `expected` is using a custom type, we might have to do some heavy lifting:
	// `actual` has all its objects as map[string]any, so keys serialize in sorted order,
	// but `expected` might have struct type instead, where keys serialize in declaration order;
	// this can be normalized by roundtripping `expectedJSON` through map[string]any once
	// (if any of these steps fail, this is intentionally not an error because it's only a last resort)
	var roundtrip any
	err := json.Unmarshal([]byte(expectedJSON), &roundtrip)
	if err == nil {
		buf, err := json.Marshal(roundtrip)
		if err == nil {
			expectedJSON = string(buf)
			if expectedJSON == actualJSON {
				return nil
			}
		}
	}

	kind := "value mismatch"
	if kindForJSONMessage(actualJSON) != kindForJSONMessage(expectedJSON) {
		kind = "type mismatch"
	}
	return []Diff{{
		Kind:         kind,
		Pointer:      pathIntoPointer(path),
		ExpectedJSON: expectedJSON,
		ActualJSON:   actualJSON,
	}}
}

func getDiffsForObject(path []pathElement, expected map[string]any, actual any) []Diff {
	if actual, ok := actual.(map[string]any); ok {
		return getDiffsForConfirmedObject(path, expected, actual)
	}
	return []Diff{{
		Kind:         "type mismatch",
		Pointer:      pathIntoPointer(path),
		ExpectedJSON: marshalExpectedForDiff(expected),
		ActualJSON:   marshalActualForDiff(actual),
	}}
}

func getDiffsForConfirmedObject(path []pathElement, expected, actual map[string]any) (diffs []Diff) {
	// recurse into all fields
	for _, key := range slices.Sorted(maps.Keys(actual)) {
		subpath := append(path, keyElement(key))
		expectedValue, exists := expected[key]
		if exists {
			diffs = append(diffs, getDiffsForValue(subpath, expectedValue, actual[key])...)
		} else {
			diffs = append(diffs, Diff{
				Kind:         "value mismatch",
				Pointer:      pathIntoPointer(subpath),
				ExpectedJSON: "<missing>",
				ActualJSON:   marshalActualForDiff(actual[key]),
			})
		}
	}
	for _, key := range slices.Sorted(maps.Keys(expected)) {
		_, exists := actual[key]
		if !exists {
			subpath := append(path, keyElement(key))
			diffs = append(diffs, Diff{
				Kind:         "value mismatch",
				Pointer:      pathIntoPointer(subpath),
				ExpectedJSON: marshalExpectedForDiff(expected[key]),
				ActualJSON:   "<missing>",
			})
		}
	}

	return diffs
}

func getDiffsForArray(path []pathElement, expected []any, actual any) []Diff {
	if actual, ok := actual.([]any); ok {
		return getDiffsForConfirmedArray(path, expected, actual)
	}
	return []Diff{{
		Kind:         "type mismatch",
		Pointer:      pathIntoPointer(path),
		ExpectedJSON: marshalExpectedForDiff(expected),
		ActualJSON:   marshalActualForDiff(actual),
	}}
}

func getDiffsForConfirmedArray(path []pathElement, expected, actual []any) (diffs []Diff) {
	// recurse into all elements
	for idx := range max(len(actual), len(expected)) {
		subpath := append(path, indexElement(idx))
		switch {
		case idx >= len(actual):
			diffs = append(diffs, Diff{
				Kind:         "value mismatch",
				Pointer:      pathIntoPointer(subpath),
				ActualJSON:   "<missing>",
				ExpectedJSON: marshalExpectedForDiff(expected[idx]),
			})
		case idx >= len(expected):
			diffs = append(diffs, Diff{
				Kind:         "value mismatch",
				Pointer:      pathIntoPointer(subpath),
				ActualJSON:   marshalActualForDiff(actual[idx]),
				ExpectedJSON: "<missing>",
			})
		default:
			diffs = append(diffs, getDiffsForValue(subpath, expected[idx], actual[idx])...)
		}
	}

	return diffs
}

func getDiffsForCapturedField(path []pathElement, expected capturedField, actual any) []Diff {
	actualJSON := marshalActualForDiff(actual)
	err := json.Unmarshal([]byte(actualJSON), expected.PointerToTarget)
	if err != nil {
		return []Diff{{
			Kind:         fmt.Sprintf("cannot unmarshal into capture slot (%s)", err.Error()),
			Pointer:      pathIntoPointer(path),
			ActualJSON:   actualJSON,
			ExpectedJSON: fmt.Sprintf("<capture slot of type %T>", expected.PointerToTarget),
		}}
	}
	return nil
}
