// SPDX-FileCopyrightText: 2026 Stefan Majewsky <majewsky@gmx.net>
// SPDX-License-Identifier: Apache-2.0

package assert

import (
	"fmt"
	"reflect"
	"slices"
	"strings"
	"unicode/utf8"

	"go.xyrillian.de/gg/internal/path"
)

// Equal checks whether both supplied values are equal according to the rules of [reflect.DeepEqual].
//
// If there is a difference within a structured data type,
// this function will try to be smart about reporting only the most specific pieces that differ,
// but this is done on a best-effort basis.
// The error messages produced by this assertion should be expected to change between releases
// as additional effort is expended to establish a new level of best effort.
func Equal[V any](t TestingTB, actual, expected V) bool {
	if reflect.DeepEqual(actual, expected) {
		return true
	}
	t.Helper()

	// NOTE: consider the warning in the docstring of [path.Path]
	p := path.NewPath()
	result := findInequalities(p, reflect.ValueOf(actual), reflect.ValueOf(expected))

	// serialize results into strings first in order to print in sorted order (for deterministic behavior in this package's own tests)
	errors := make([]string, len(result))
	for idx, ineq := range result {
		if ineq.Pointer == "actual" {
			errors[idx] = fmt.Sprintf("expected %s, but got %s", ineq.Expected, ineq.Actual)
		} else {
			errors[idx] = fmt.Sprintf("at %s: expected %s, but got %s", ineq.Pointer, ineq.Expected, ineq.Actual)
		}
	}
	slices.Sort(errors)
	for _, err := range errors {
		t.Error(err)
	}

	return false
}

// NOTE: Several notes on the implementation of Equal().
//
// - All findInequalities...() functions assume that `actual` and `expected` are definitely unequal,
//   and so may only be called if reflect.Equal() on these same arguments has returned false.
//
// - All findInequalities...() functions further assume that `actual.Type() == expected.Type()`.
//   This is ensured at the API boundary through the type signature of assert.Equal(),
//   and then only needs to be re-established when recursing into values of kind Interface.
//
// - When nonempty diffs are generated, running a full DeepEqual() at each level is indeed extremely inefficient.
//   However, reflect.DeepEqual() is more likely to handle bizarre corner cases
//   and new type system features better than our implementation,
//   so we rely on it as a source of ground truth.
//
//   Furthermore, in the vastly more important case of an empty diff (i.e. a passing test),
//   reflect.DeepEqual() is likely to be more efficient than what we do because of
//   having both been around and scrutinized for much longer than our implementation,
//   so doing it first will usually be faster.

type inequality struct {
	Pointer  string
	Actual   string
	Expected string
}

func formatValue(v reflect.Value) string {
	return fmt.Sprintf("%#v", v)
}

func findInequalities(p path.Path, actual, expected reflect.Value) (result []inequality) {
	// try to recurse into structured type to find the specific location of the inequality
	// (thus producing a more succinct error message esp. with large and deeply nested structures)
	switch actual.Kind() { //nolint:exhaustive
	case reflect.Array, reflect.Slice:
		result = findInequalitiesInArrayOrSlice(p, actual, expected)
	case reflect.Map:
		result = findInequalitiesInMap(p, actual, expected)
	case reflect.Struct:
		result = findInequalitiesInStruct(p, actual, expected)
	case reflect.Pointer:
		result = findInequalitiesInPointer(p, actual, expected)
	case reflect.Interface:
		if !actual.IsNil() && !expected.IsNil() {
			// can only recurse if the invariant of this function is upheld: both sides must be of equal types
			actualElem := actual.Elem()
			expectedElem := expected.Elem()
			if actualElem.Type() == expectedElem.Type() {
				subpath := append(p, path.TypeCastElement(fmt.Sprintf("%T", actualElem.Interface())))
				result = findInequalities(subpath, actualElem, expectedElem)
			}
		}
	}

	// if we do not have a recursion method for the type in question,
	// or if our own implementation somehow fails to find the inequality,
	// the safe fallback is to report the entire value as unequal
	if len(result) == 0 {
		return []inequality{{p.AsGoExpression("actual"), formatValue(actual), formatValue(expected)}}
	}
	return result
}

func findInequalitiesInArrayOrSlice(p path.Path, actual, expected reflect.Value) (result []inequality) {
	// special case: for ~[]byte types (e.g. json.RawMessage) containing a valid Unicode string on both sides,
	// a diff using string literals is likely to be vastly more readable
	if actual.Type().Elem() == reflect.TypeFor[byte]() {
		actualPayload := actual.Convert(reflect.TypeFor[[]byte]()).Interface().([]byte)
		expectedPayload := expected.Convert(reflect.TypeFor[[]byte]()).Interface().([]byte)
		if utf8.Valid(actualPayload) && utf8.Valid(expectedPayload) {
			return []inequality{{
				Pointer:  p.AsGoExpression("actual"),
				Actual:   formatByteSliceViaString(actualPayload),
				Expected: formatByteSliceViaString(expectedPayload),
			}}
		}
	}

	// recurse into all elements
	for idx := range max(actual.Len(), expected.Len()) {
		subpath := append(p, path.IndexElement(idx))
		switch {
		case idx >= actual.Len():
			result = append(result, inequality{
				Pointer:  subpath.AsGoExpression("actual"),
				Actual:   "<missing>",
				Expected: formatValue(expected.Index(idx)),
			})
		case idx >= expected.Len():
			result = append(result, inequality{
				Pointer:  subpath.AsGoExpression("actual"),
				Actual:   formatValue(actual.Index(idx)),
				Expected: "<missing>",
			})
		default:
			actualElem := actual.Index(idx)
			expectedElem := expected.Index(idx)
			if !reflect.DeepEqual(actualElem.Interface(), expectedElem.Interface()) {
				result = append(result, findInequalities(subpath, actualElem, expectedElem)...)
			}
		}
	}

	// if multiple elements differ, check if reporting the whole slice as different is more compact
	// (this helps with slices of simple types, e.g. []int,
	// but will not be used for large records where only a single field differs in all of them)
	if len(result) > 4 {
		overallTextLength := 0
		for _, ineq := range result {
			overallTextLength += len(ineq.Pointer) + len(ineq.Actual) + len(ineq.Expected)
		}
		ineq := buildSingleInequalityForArrayOrSlice(p, actual, expected)
		if len(ineq.Pointer)+len(ineq.Actual)+len(ineq.Expected) < overallTextLength {
			return []inequality{ineq}
		}
	}

	return result
}

func formatByteSliceViaString(buf []byte) string {
	str := string(buf)
	if strings.Contains(str, `"`) && !strings.Contains(str, "`") {
		return fmt.Sprintf("[]byte(`%s`)", str)
	} else {
		return fmt.Sprintf("[]byte(%q)", str)
	}
}

func buildSingleInequalityForArrayOrSlice(p path.Path, actual, expected reflect.Value) inequality {
	// This is a helper for findInequalitiesInArrayOrSlice() that reports only a single inequality for the entire thing.
	// But it still tries to be clever, and will omit the longest common prefix and suffix to shorten the output.
	maxTruncateableLength := min(actual.Len(), expected.Len())

	commonPrefixLength := 0
	for idx := range maxTruncateableLength {
		actualElem := actual.Index(idx)
		expectedElem := expected.Index(idx)
		if reflect.DeepEqual(actualElem.Interface(), expectedElem.Interface()) {
			commonPrefixLength = idx + 1
		} else {
			break
		}
	}

	commonSuffixLength := 0
	for idx := range max(0, maxTruncateableLength-commonPrefixLength) {
		actualElem := actual.Index(actual.Len() - 1 - idx)
		expectedElem := expected.Index(expected.Len() - 1 - idx)
		if reflect.DeepEqual(actualElem.Interface(), expectedElem.Interface()) {
			commonSuffixLength = idx + 1
		} else {
			break
		}
	}

	if commonPrefixLength > 0 || commonSuffixLength > 0 {
		p = append(p, path.SliceElement(commonPrefixLength, expected.Len()-commonSuffixLength))
		actual = actual.Slice(commonPrefixLength, actual.Len()-commonSuffixLength)
		expected = expected.Slice(commonPrefixLength, expected.Len()-commonSuffixLength)
	}

	return inequality{
		Pointer:  p.AsGoExpression("actual"),
		Actual:   formatValue(actual),
		Expected: formatValue(expected),
	}
}

func findInequalitiesInMap(p path.Path, actual, expected reflect.Value) (result []inequality) {
	// recurse into all keys of `actual`
	iter := actual.MapRange()
	for iter.Next() {
		key, actualElem := iter.Key(), iter.Value()
		subpath := append(p, path.MapKeyElement(key.Interface()))
		expectedElem := expected.MapIndex(key)
		if expectedElem.IsValid() {
			if !reflect.DeepEqual(actualElem.Interface(), expectedElem.Interface()) {
				result = append(result, findInequalities(subpath, actualElem, expectedElem)...)
			}
		} else {
			result = append(result, inequality{
				Pointer:  subpath.AsGoExpression("actual"),
				Actual:   formatValue(actualElem),
				Expected: "<missing>",
			})
		}
	}

	// recurse into all keys of `expected` (but consider only those missing in `actual` to avoid duplicate reports)
	iter = expected.MapRange()
	for iter.Next() {
		key, expectedElem := iter.Key(), iter.Value()
		subpath := append(p, path.MapKeyElement(key.Interface()))
		if !actual.MapIndex(key).IsValid() {
			result = append(result, inequality{
				Pointer:  subpath.AsGoExpression("actual"),
				Actual:   "<missing>",
				Expected: formatValue(expectedElem),
			})
		}
	}

	return result
}

func findInequalitiesInStruct(p path.Path, actual, expected reflect.Value) (result []inequality) {
	// recurse into all addressable fields
	//
	// If only values in unexported fields differ, this function will return nothing,
	// but that's fine because of the fallback behavior in findInequalities().
	for field := range actual.Type().Fields() {
		if !field.IsExported() {
			continue
		}
		subpath := append(p, path.KeyElement(field.Name))
		actualElem := actual.FieldByIndex(field.Index)
		expectedElem := expected.FieldByIndex(field.Index)
		if !reflect.DeepEqual(actualElem.Interface(), expectedElem.Interface()) {
			result = append(result, findInequalities(subpath, actualElem, expectedElem)...)
		}
	}
	return result
}

func findInequalitiesInPointer(p path.Path, actual, expected reflect.Value) []inequality {
	if actual.IsNil() {
		if expected.IsNil() {
			// defense in depth: should not be reachable -> use the fallback behavior in findInequalities()
			return nil
		} else {
			return []inequality{{
				Pointer:  p.AsGoExpression("actual"),
				Actual:   "nil",
				Expected: "pointer to " + formatValue(expected.Elem()),
			}}
		}
	} else {
		if expected.IsNil() {
			return []inequality{{
				Pointer:  p.AsGoExpression("actual"),
				Actual:   "pointer to " + formatValue(actual.Elem()),
				Expected: "nil",
			}}
		} else {
			subpath := append(p, path.DereferenceElement())
			return findInequalities(subpath, actual.Elem(), expected.Elem())
		}
	}
}
