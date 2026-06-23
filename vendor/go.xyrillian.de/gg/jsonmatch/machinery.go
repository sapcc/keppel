// SPDX-FileCopyrightText: 2025 Stefan Majewsky <majewsky@gmx.net>
// SPDX-License-Identifier: Apache-2.0

package jsonmatch

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"

	"go.xyrillian.de/gg/internal/path"
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
	// unmarshaled from JSON into any and thus can only contain safe types
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

	// NOTE: consider the warning in the docstring of [path.Path]
	p := path.NewPath()
	return getDiffsForValue(p, expected, actual)
}

const (
	kindValueMismatch  = "value mismatch"
	kindTypeMismatch   = "type mismatch"
	kindDispatchFailed = "dispatch failed"
)

// NOTE: getDiffsForValue is the main part of the recursion to generate the diff.
func getDiffsForValue(p path.Path, expected, actual any) []Diff {
	// specialized handling for relevant recursible or capturable types
	switch expected := expected.(type) {
	case map[string]any:
		return getDiffsForObject(p, expected, actual)
	case Object:
		return getDiffsForObject(p, expected, actual)
	case []any:
		return getDiffsForArray(p, expected, actual)
	case Array:
		return getDiffsForArray(p, expected, actual)
	case []map[string]any:
		downcasted := make([]any, len(expected))
		for idx, val := range expected {
			downcasted[idx] = val
		}
		return getDiffsForArray(p, downcasted, actual)
	case []Object:
		downcasted := make([]any, len(expected))
		for idx, val := range expected {
			downcasted[idx] = val
		}
		return getDiffsForArray(p, downcasted, actual)
	case capturedField:
		return getDiffsForCapturedField(p, expected, actual)
	case irrelevant:
		return nil
	case nil:
		// this case needs to be handled separately because the code below
		// cannot deal with reflect.TypeOf(expected) returning nil
		if actual == nil {
			return nil
		} else {
			return []Diff{{
				Kind:         kindTypeMismatch,
				Pointer:      Pointer(p.AsJSONPointer()),
				ExpectedJSON: "null",
				ActualJSON:   marshalActualForDiff(actual),
			}}
		}
	}

	// generic handling for custom Diffables
	// (if any unexpected error occurs here, we fall back to the default handling)
	if diffable, ok := expected.(Diffable); ok {
		// `actual` values are always safe to marshal because they were
		// unmarshaled from JSON into any and thus can only contain safe types
		buf, err := json.Marshal(actual)
		if err != nil {
			// this branch is therefore unreachable in tests and only exists as defense in depth
			return []Diff{{
				Kind:         kindDispatchFailed,
				Pointer:      Pointer(p.AsJSONPointer()),
				ExpectedJSON: fmt.Sprintf("<custom diffable: %#v>", diffable),
				ActualJSON:   fmt.Sprintf("<marshal error: %s>", err.Error()),
			}}
		}
		diffs := diffable.DiffAgainst(buf)
		for idx, diff := range diffs {
			diff.Pointer = Pointer(p.AsJSONPointer()) + diff.Pointer
			diffs[idx] = diff
		}
		return diffs
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
		Pointer:      Pointer(p.AsJSONPointer()),
		ExpectedJSON: expectedJSON,
		ActualJSON:   actualJSON,
	}}
}

func getDiffsForObject(p path.Path, expected map[string]any, actual any) []Diff {
	if actual, ok := actual.(map[string]any); ok {
		return getDiffsForConfirmedObject(p, expected, actual)
	}
	return []Diff{{
		Kind:         kindTypeMismatch,
		Pointer:      Pointer(p.AsJSONPointer()),
		ExpectedJSON: marshalExpectedForDiff(expected),
		ActualJSON:   marshalActualForDiff(actual),
	}}
}

func getDiffsForConfirmedObject(p path.Path, expected, actual map[string]any) (diffs []Diff) {
	// recurse into all fields
	for _, key := range slices.Sorted(maps.Keys(actual)) {
		subpath := append(p, path.KeyElement(key))
		expectedValue, exists := expected[key]
		if exists {
			diffs = append(diffs, getDiffsForValue(subpath, expectedValue, actual[key])...)
		} else {
			diffs = append(diffs, Diff{
				Kind:         kindValueMismatch,
				Pointer:      Pointer(subpath.AsJSONPointer()),
				ExpectedJSON: "<missing>",
				ActualJSON:   marshalActualForDiff(actual[key]),
			})
		}
	}
	for _, key := range slices.Sorted(maps.Keys(expected)) {
		_, exists := actual[key]
		if !exists {
			subpath := append(p, path.KeyElement(key))
			diffs = append(diffs, Diff{
				Kind:         kindValueMismatch,
				Pointer:      Pointer(subpath.AsJSONPointer()),
				ExpectedJSON: marshalExpectedForDiff(expected[key]),
				ActualJSON:   "<missing>",
			})
		}
	}

	return diffs
}

func getDiffsForArray(p path.Path, expected []any, actual any) []Diff {
	if actual, ok := actual.([]any); ok {
		return getDiffsForConfirmedArray(p, expected, actual)
	}
	return []Diff{{
		Kind:         kindTypeMismatch,
		Pointer:      Pointer(p.AsJSONPointer()),
		ExpectedJSON: marshalExpectedForDiff(expected),
		ActualJSON:   marshalActualForDiff(actual),
	}}
}

func getDiffsForConfirmedArray(p path.Path, expected, actual []any) (diffs []Diff) {
	// recurse into all elements
	for idx := range max(len(actual), len(expected)) {
		subpath := append(p, path.IndexElement(idx))
		switch {
		case idx >= len(actual):
			diffs = append(diffs, Diff{
				Kind:         kindValueMismatch,
				Pointer:      Pointer(subpath.AsJSONPointer()),
				ActualJSON:   "<missing>",
				ExpectedJSON: marshalExpectedForDiff(expected[idx]),
			})
		case idx >= len(expected):
			diffs = append(diffs, Diff{
				Kind:         kindValueMismatch,
				Pointer:      Pointer(subpath.AsJSONPointer()),
				ActualJSON:   marshalActualForDiff(actual[idx]),
				ExpectedJSON: "<missing>",
			})
		default:
			diffs = append(diffs, getDiffsForValue(subpath, expected[idx], actual[idx])...)
		}
	}

	return diffs
}

func getDiffsForCapturedField(p path.Path, expected capturedField, actual any) []Diff {
	actualJSON := marshalActualForDiff(actual)
	err := json.Unmarshal([]byte(actualJSON), expected.PointerToTarget)
	if err != nil {
		return []Diff{{
			Kind:         fmt.Sprintf("cannot unmarshal into capture slot (%s)", err.Error()),
			Pointer:      Pointer(p.AsJSONPointer()),
			ActualJSON:   actualJSON,
			ExpectedJSON: fmt.Sprintf("<capture slot of type %T>", expected.PointerToTarget),
		}}
	}
	return nil
}
