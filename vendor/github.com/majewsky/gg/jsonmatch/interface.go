// SPDX-FileCopyrightText: 2025 Stefan Majewsky <majewsky@gmx.net>
// SPDX-License-Identifier: Apache-2.0

// Package jsonmatch implements matching of encoded JSON payloads against fixed assertions.
// The interface is most suited for unit tests, and intended for functions that return encoded JSON payloads (such as HTTP API handlers).
// Below is an example how package jsonmatch can be used together with only the standard library.
//
// In all likelihood, you will already have your own test assertion library to use on top of std.
// Package jsonmatch is intended to be low-level enough to be easy to integrate with whatever assertion library you like to use.
//
//	import (
//		"net/http"
//		"net/http/httptest"
//
//		"github.com/majewsky/gg/jsonmatch"
//	)
//
//	func TestJSONMatchOfResponseBody(t*testing.T) {
//		// this example assumes that the implementation being tested
//		// has an HTTP handler implementing GET /v1/things
//		var h http.Handler = buildAPIHandler()
//
//		// use net/http/httptest to run a request
//		req := httptest.NewRequest(http.MethodGet, "/v1/things", nil)
//		resp := httptest.NewRecorder()
//		h.ServeHTTP(resp, req)
//		if resp.Code != http.StatusOK {
//			t.Error("unexpected error")
//		}
//
//		// check that the response payload contains the data that we expect
//		expected := jsonmatch.Object{
//			"things": []jsonmatch.Object{
//				{ "id": 1, "name": "First thing" },
//				{ "id": 2, "name": "Second thing" },
//			},
//		}
//		for _, diff := range expected.DiffAgainst(resp.Body.Bytes()) {
//			t.Error(diff.String())
//		}
//	}
//
// # Assertion format
//
// As shown in the example above, this package revolves around writing out assertions for how a JSON payload looks in your test's source code.
//
//	expected := jsonmatch.Object{
//		"things": []jsonmatch.Object{
//			{ "id": 1, "name": "First thing" },
//			{ "id": 2, "name": "Second thing" },
//		},
//		"keywords": jsonmatch.Array{"example", "test"},
//	}
//	diffs := expected.DiffAgainst(actual)
//
// The example above demonstrates the recommended style:
//   - All scalar values in the assertion (bools, numbers, strings and nulls) use the respective predeclared Go value types.
//   - Objects use the jsonmatch.Object type instead of map[string]any.
//   - Arrays of only objects use the []jsonmatch.Object type.
//   - Other arrays use the jsonmatch.Array type instead of []any or more specific array/slice types.
//
// It is possible to write jsonmatch.Object as map[string]any and jsonmatch.Array as []any, like this:
//
//	expected := map[string]any{
//		"things": []map[string]any{
//			{ "id": 1, "name": "First thing" },
//			{ "id": 2, "name": "Second thing" },
//		},
//		"keywords": []any{"example", "test"},
//	}
//	diffs := jsonmatch.Object(expected).DiffAgainst(actual)
//
// We do not recommend this style, as using the jsonmatch.Object and jsonmatch.Array identifiers better communicates the intent of the literal.
//
// # Recommendation: Do not use complex types in assertions
//
// We recommend avoiding more specific types than basic maps, slices and predeclared value types in the assertion.
// It is tempting to reuse types from the implementation, but this risks repeating errors from the implementation in the test.
// Consider the following example:
//
//	// from the implementation
//	type Thing struct {
//		ID   int    `json:"id"`
//		Name string `json:"naem"`
//	}
//
//	expected := jsonmatch.Object{
//		"things": []Thing{
//			{ ID: 1, Name: "First thing" },
//			{ ID: 2, Name: "Second thing" },
//		},
//		"keywords": jsonmatch.Array{"example", "test"},
//	}
//	diffs := expected.DiffAgainst(actual)
//
// In this example, we have made a mistake in the implementation.
// The field "name" has been misspelled, so it will be marshalled as "naem" instead.
// Because the test unmarshals into the same type as the implementation, it will not be able to uncover this error.
// This example might be a bit contrived, but keeping test logic separate from implementation logic is especially important for types using advanced marshalling logic through custom implementations of the json.Marshaler and json.Unmarshaler interfaces.
//
// # Capturing nondeterministic data
//
// Sometimes, JSON payloads may contain randomly-generated fields like UUIDs or non-deterministic data like timestamps that cannot be predicted when writing the test code.
// For these situations, package jsonmatch provides the CaptureField function.
// The example below shows a test exercising a PUT endpoint to create an object, capturing the object's ID while asserting on the rest of the response, and then using that ID to exercise a GET endpoint that displays the created object.
//
//	req1 := httptest.NewRequest(http.MethodPut, "/v1/things/new", strings.NewReader(`{"name":"hello"}`)
//	// ...
//
//	var uuid string
//	diffs := jsonmatch.Object {
//		"thing": jsonmatch.Object {
//			"id": jsonmatch.CaptureField(&uuid),
//			"name": "hello",
//		},
//	}.DiffAgainst(resp1.Body.Bytes())
//	// ...
//
//	req2 := httptest.NewRequest(http.MethodGet, "/v1/things")
//	// ...
//
//	diffs = jsonmatch.Object {
//		"things": []jsonmatch.Object {
//			{
//				"id": uuid,
//				"name": "hello",
//			},
//		},
//	}.DiffAgainst(resp2.Body.Bytes())
//	// ...
package jsonmatch

import (
	"encoding/json"
	"errors"
	"fmt"
)

// Diffable is the common interface of types Object, Array, Scalar and Null from this package.
// The DiffAgainst function compares the value contained in the Diffable against an encoded JSON payload.
//
// The implementation will try to generate diffs as granularly as possible.
// For example:
//
//	expected := jsonmatch.Object{
//		"things": []jsonmatch.Object{
//			{ "id": 1, "name": "First thing" },
//			{ "id": 2, "name": "Second thing" },
//		},
//	}
//	actual := `{"things": [{"id": 1, "name": "First widget"}, {"id": 3, "name": "Second thing"}]}`
//	// this call...
//	diffs := expected.DiffAgainst(actual)
//	// ...will return something like this
//	diffs := []jsonmatch.Diff{
//		{ Kind: "value mismatch", Pointer: "/things/0/name", ExpectedJSON: "First thing", ActualJSON: "First widget" },
//		{ Kind: "value mismatch", Pointer: "/things/1/id", ExpectedJSON: "2", ActualJSON: "3" },
//	}
//
// However, the implementation will only recurse into substructures of the following well-known types: jsonmatch.Object, map[string]any, jsonmatch.Array, []any, []jsonmatch.Object, []map[string]any.
// Any other map, array, slice, struct or pointer type will be treated as a black box:
// If its JSON serialization differs from that of the respective section of the actual payload, a diff will be generated for its entirety only, not for any specific subfields.
type Diffable interface {
	DiffAgainst([]byte) []Diff
}

var (
	_ Diffable = Array{}
	_ Diffable = Object{}
	_ Diffable = scalar{}
)

// Array implements diffing against an encoded JSON payload that is expected to contain an array.
// Please refer to the package documentation for how to use this type.
type Array []any

// DiffAgainst implements the Diffable interface.
func (a Array) DiffAgainst(buf []byte) []Diff {
	return diffAgainst([]any(a), buf)
}

// Object implements diffing against an encoded JSON payload that is expected to contain an object.
// Please refer to the package documentation for how to use this type.
type Object map[string]any

// DiffAgainst implements the Diffable interface.
func (o Object) DiffAgainst(buf []byte) []Diff {
	return diffAgainst(map[string]any(o), buf)
}

// Null implements diffing against an encoded JSON payload that is expected just the value `null`.
// This type is only used on the top level of the JSON payload.
// Within type Object or type Array, put a `nil` directly.
func Null() Diffable {
	return scalar{nil}
}

// Scalar implements diffing against an encoded JSON payload that is expected to contain just a scalar value (a number, string or boolean).
// This type is only used on the top level of the JSON payload.
// Within type Object or type Array, put the value directly.
func Scalar[S ScalarValue](value S) Diffable {
	return scalar{value}
}

type scalar struct {
	Value any
}

// DiffAgainst implements the Diffable interface.
func (s scalar) DiffAgainst(buf []byte) []Diff {
	return diffAgainst(s.Value, buf)
}

// ScalarValue is an interface containing every type that can be given to func Scalar.
type ScalarValue interface {
	~bool |
		~int | ~int8 | ~int16 | ~int32 | ~int64 |
		~uint | ~uint8 | ~uint16 | ~uint32 | ~uint64 | ~uintptr |
		~float32 | ~float64 |
		~string
}

// Diff is a difference between the actual encoded JSON payload given to a DiffAgainst() call, and the expectation encoded in the object that DiffAgainst() was called on.
// See type Diffable for details on how diffing works.
type Diff struct {
	// Kind explains the type of difference.
	// No stability guarantee is given for the values that can occur in this field.
	// Values in this field are expected to read well when formatted using fmt.Sprintf("%s at %s", diff.Kind, diff.Pointer).
	Kind string
	// Pointer explains where the difference occurred within the Diffable.
	// If ExpectedJSON and ActualJSON refer to the whole Diffable and the whole encoded JSON payload, then Pointer is the empty string.
	Pointer Pointer
	// A serialization of the respective part of the Diffable, or an error message or type description wrapped in <angle brackets>.
	ExpectedJSON string
	// A serialization of the respective part of the Diffable.
	ActualJSON string
}

// String returns a simple and complete string representation of the contents of this Diff.
func (d Diff) String() string {
	if d.Pointer == "" {
		return fmt.Sprintf("%s: expected %s, but got %s", d.Kind, d.ExpectedJSON, d.ActualJSON)
	} else {
		return fmt.Sprintf("%s at %s: expected %s, but got %s", d.Kind, d.Pointer, d.ExpectedJSON, d.ActualJSON)
	}
}

// Pointer is a JSON pointer (RFC 6901) that references a particular JSON value relative to the root of the encoded JSON payload that was given to DiffAgainst().
// It appears in type Diff.
//
// This type is intended to become synonymous with encoding/json/jsontext.Pointer once that type is stabilized.
type Pointer string

// CaptureField returns a capture slot that can be placed in a jsonmatch.Object or jsonmatch.Array instance to capture individual non-deterministic values during an assertion.
// Please refer to the package documentation for details and usage examples.
//
// Capture slots only work inside data structures that DiffAgainst() knows how to recurse into.
// Please refer to the documentation on type Diffable for details.
func CaptureField[T any](target *T) any {
	// NOTE: The public interface is using generics because that allows enforcing
	// that `target` is passed as pointer. But the internal representation holds
	// `target` as `any` because not having type arguments on the capturedField
	// type makes it easier to reflect on that type.
	return capturedField{target}
}

type capturedField struct {
	PointerToTarget any
}

// MarshalJSON implements the json.Marshaler interface by transparently marshaling the contained value.
//
// This implementation ensures that `capturedField` looks like its payload
// when serialized for a "type mismatch" or "value mismatch" error message.
func (f capturedField) MarshalJSON() ([]byte, error) {
	return json.Marshal(f.PointerToTarget)
}

// UnmarshalJSON implements the json.Unmarshaler interface by always throwing an error.
//
// This implementation ensures that `capturedField` is not placed into a
// container that DiffAgainst() does not know how to recurse into.
func (f capturedField) UnmarshalJSON(buf []byte) error {
	return errors.New("cannot unmarshal into jsonmatch.CaptureField()")
}
