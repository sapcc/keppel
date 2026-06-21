// SPDX-FileCopyrightText: 2026 Stefan Majewsky <majewsky@gmx.net>
// SPDX-License-Identifier: Apache-2.0

package path

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	. "go.xyrillian.de/gg/option"
)

// Path is used to identify the current location within a nested data structure
// while recursing through it. For example, when comparing
//
//	actual = { "foo": { "bar": [ 5, 23 ] } }
//	expected = { "foo": { "bar": [ 5, 42 ] } }
//
// we would generate a diff at the path {"foo", "bar", 1}.
// Since diffs are usually rare, we only build Pointer strings
// out of these paths when we really need them.
// During recursion, Path holds a sequence of path elements,
// most of which are constants to keep allocations to a minimum.
//
// # Warning
//
// Because the same Path slice is heavily reused across nested function calls,
// it is not safe to store references to the Path slice during such a recursion.
type Path []Element

// Path preallocates a decently sized path buffer for use in a recursion.
func NewPath() Path {
	return make([]Element, 0, 32)
}

// Element occurs in type [Path]. Only one of both fields is set per instance.
type Element struct {
	// used by both package jsonmatch and package assert
	Key   Option[string]
	Index int

	// only used by package assert (panics when used with AsJSONPointer())
	Slice       Option[SliceBounds]
	MapKey      Option[any] // holds a value of type K for traversing into types ~map[K]V
	TypeCast    string      // holds a %T formatting of a type
	Dereference bool        // marks when a pointer is dereferenced
}

// SliceBounds appears in type [Element].
type SliceBounds struct {
	Start int
	End   int
}

// KeyElement is a shorthand for constructing an Element with the Key field set.
func KeyElement(key string) Element { return Element{Key: Some(key)} }

// IndexElement is a shorthand for constructing an Element with the Index field set.
func IndexElement(idx int) Element { return Element{Index: idx} }

// SliceElement is a shorthand for constructing an Element with the Slice field set.
func SliceElement(start, end int) Element { return Element{Slice: Some(SliceBounds{start, end})} }

// MapKeyElement is a shorthand for constructing an Element with the MapKey field set.
func MapKeyElement(key any) Element { return Element{MapKey: Some(key)} }

// TypeCastElement is a shorthand for constructing an Element with the TypeCast field set.
func TypeCastElement(typeStr string) Element { return Element{TypeCast: typeStr} }

// DereferenceElement is a shorthand for constructing an Element with the Dereference field set.
func DereferenceElement() Element { return Element{Dereference: true} }

// AsJSONPointer serializes p as a JSON pointer (RFC 6901).
func (p Path) AsJSONPointer() string {
	if len(p) == 0 {
		return ""
	}
	fragments := make([]string, len(p)+1)
	fragments[0] = ""
	for idx, elem := range p {
		if elem.Dereference {
			panic("Dereference elements cannot be used with AsJSONPointer()")
		}
		if elem.TypeCast != "" {
			panic("TypeCast elements cannot be used with AsJSONPointer()")
		}
		if elem.MapKey.IsSome() {
			panic("MapKey elements cannot be used with AsJSONPointer()")
		}
		if elem.Slice.IsSome() {
			panic("Slice elements cannot be used with AsJSONPointer()")
		}
		if key, ok := elem.Key.Unpack(); ok {
			fragments[idx+1] = keyIntoPointerFragment(key)
		} else {
			fragments[idx+1] = strconv.Itoa(elem.Index)
		}
	}
	return strings.Join(fragments, "/")
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

// AsGoExpression serializes p as a partial Go expression like `value.Objects["foo.txt"].Lines[42]`.
func (p Path) AsGoExpression(baseVariable string) string {
	b := &strings.Builder{}
	fmt.Fprint(b, baseVariable)
	for idx, elem := range p {
		if elem.Dereference {
			if idx != len(p)-1 && p[idx+1].Key.IsSome() {
				// simplify `(*foo).Bar` to `foo.Bar`
				continue
			}
			str := b.String()
			b = &strings.Builder{}
			fmt.Fprintf(b, "(*%s)", str)
		} else if elem.TypeCast != "" {
			fmt.Fprintf(b, `.(%s)`, elem.TypeCast)
		} else if mapKey, ok := elem.MapKey.Unpack(); ok {
			fmt.Fprintf(b, `[%#v]`, mapKey)
		} else if bounds, ok := elem.Slice.Unpack(); ok {
			fmt.Fprintf(b, `[%d:%d]`, bounds.Start, bounds.End)
		} else if key, ok := elem.Key.Unpack(); ok {
			fmt.Fprintf(b, `.%s`, key)
		} else {
			fmt.Fprintf(b, `[%d]`, elem.Index)
		}
	}
	return b.String()
}
