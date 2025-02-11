/*******************************************************************************
* Copyright 2025 Stefan Majewsky <majewsky@gmx.net>
* SPDX-License-Identifier: Apache-2.0
* Refer to the file "LICENSE" for details.
*******************************************************************************/

// Package optional provides an Option type for Go.
// A value of the Option type will be in one of two states: "Some" (containing a value) or "None" (containing no value).
//
// The purpose of the Option type is to more clearly distinguish the two situations that standard Go uses pointer types for:
//   - to provide a way to edit the inside of a value without changing the value itself
//   - to allow for a value to be either present or absent (with absence being represented as "nil")
//
// Using the Option type, pointers may be reserved for the first purpose.
// For instance, a type like *int32 would clearly represent an editable value, whereas Option[int32] clearly represents an optional value.
//
// # Clean import guarantee
//
// This package is supposed to be used as a dot-import:
//
//	import . "github.com/majewsky/go-option"
//
// To avoid backwards incompatibilities, we guarantee that, at least throughout the 1.x series,
// newer versions of this package will never export any more names than it currently does ("None", "Option" and "Some").
//
// # Marshalling considerations
//
// Marshaling into and from YAML using https://github.com/go-yaml/yaml is supported.
// The "omitempty" flag works as expected.
//
// Marshaling into and from JSON using encoding/json is supported, but the "omitempty" flag does not work.
// You must use the "omitzero" flag to get the same effect, but note that this flag is only supported by Go 1.24 and newer.
//
// # How to replace pointer types with Option types
//
// This abridged example shows the most common ways to interact with pointer types that represent optional values:
//
//	type ServerConfiguration struct {
//		CrashLogPath *string
//		ListenAddress *string
//		ThreadCount *uint64
//	}
//
//	func RunServer(cfg ServerConfiguration) {
//		// inform about a value being absent
//		if cfg.CrashLogPath == nil {
//			log.Print("crash logging disabled because no CrashLogPath is given")
//		}
//
//		// fill a default value if nil is given
//		listenAddress := "127.0.0.1:8080"
//		if cfg.ListenAddress != nil {
//			listenAddress = *listenAddress
//		}
//
//		// access contained value if present, or proceed without contained value if absent
//		if cfg.ThreadCount != nil {
//			startMultiThreadedServer(listenAddress, *cfg.ThreadCount)
//		} else {
//			startSingleThreadedServer(listenAddress)
//		}
//	}
//
// This is how the same code snippet looks when replacing the pointer types with Option types and taking advantage of the methods on type Option:
//
//	type ServerConfiguration struct {
//		CrashLogPath Option[string]
//		ListenAddress Option[string]
//		ThreadCount Option[uint64]
//	}
//
//	func RunServer(cfg ServerConfiguration) {
//		// "if x == nil" becomes "if x.IsNone()"; the opposite check is called IsSome()
//		if cfg.CrashLogPath.IsNone() {
//			log.Print("crash logging disabled because no CrashLogPath is given")
//		}
//
//		// default values can be filled with UnwrapOr()
//		listenAddress := cfg.ListenAddress.UnwrapOr("127.0.0.1:8080")
//
//		// Unpack() provides the contained value and a success flag, similar to the double-return-value form of map indexing
//		if threadCount, ok := cfg.ThreadCount.Unpack(); ok {
//			startMultiThreadedServer(listenAddress, *cfg.ThreadCount)
//		} else {
//			startSingleThreadedServer(listenAddress)
//		}
//	}
//
// # Differences to Rust
//
// This package's API is obviously modeled after the Option type in the Rust standard library, but with some exceptions.
//
//   - Go does not allow to introduce additional type parameters for individual methods.
//     Any methods that, in Rust, introduce the second type parameter U, cannot be represented in Go.
//     Some methods like and() or zip() could be allowed if the argument is restricted to Option[T] instead of Option[U],
//     but this restriction degrades their usefulness beyond reasonable limits.
//   - Go does not allow to introduce additional type restrictions in individual methods.
//     This makes methods like unzip() or cloned() unrepresentable in Go.
//     We might make these available as free-standing functions in the future, but if we do,
//     they will definitely not be in this package (see "Clean import guarantee" above).
//   - Mixing of struct receiver methods and pointer receiver methods on the same type is discouraged to avoid unintentional copies and data races.
//     Since most of the useful methods require only a struct receiver, we forego those that require a pointer receiver, like get_or_insert() or take().
//     The only exception to this is the methods implementing Unmarshaler interfaces, where concurrency bugs are very unlikely.
//
// Finally, the unwrap() function is not provided since its meaningless error message is not helpful in most contexts.
// We only provide expect(), but under the different name UnwrapOrPanic(), since expect() reads terribly in context. Consider the following example:
//
//	dbURL := GetDatabaseURLFromEnvironment().Expect("no DB connection found")
//	// ^ This reads like we *expect* to not find a DB connection, even though the opposite is true.
//
//	dbURL := GetDatabaseURLFromEnvironment().UnwrapOrPanic("no DB connection found")
//	// ^ This is a much clearer phrasing.
package option

import (
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"iter"
)

// Option is a type that contains either one or no instances of T.
type Option[T any] struct {
	// NOTE: `None()` must yield the zero value of this type.
	value  T
	isSome bool
}

////////////////////////////////////////////////////////////////////////////////
// constructors

// None constructs an Option instance that contains no value.
func None[T any]() Option[T] {
	var empty T
	return Option[T]{empty, false}
}

// Some constructs an Option instance that contains the provided value.
func Some[T any](value T) Option[T] {
	return Option[T]{value, true}
}

// NOTE: Cannot use options.FromPointer() here because of import cycle.
func fromPointer[T any](value *T) Option[T] {
	if value == nil {
		return None[T]()
	} else {
		return Some(*value)
	}
}

////////////////////////////////////////////////////////////////////////////////
// core API (methods sorted by name)

// AsPointer converts this Option into a pointer type.
//
// Usage of this function is discouraged because it breaks the clear distinction
// between interior mutability (pointer) vs. optionality (Option).
// It is provided for when an Option value needs to be passed to a library
// function that requires a pointer value.
func (o Option[T]) AsPointer() *T {
	if o.isSome {
		return &o.value
	} else {
		return nil
	}
}

// AsSlice returns a slice with either the contained value or nothing in it.
func (o Option[T]) AsSlice() []T {
	if o.isSome {
		return []T{o.value}
	} else {
		return nil
	}
}

// Filter removes the contained value (if any) from the Option if it does not match the predicate.
func (o Option[T]) Filter(predicate func(T) bool) Option[T] {
	if o.isSome && predicate(o.value) {
		return o
	} else {
		return None[T]()
	}
}

// IsNone returns whether the Option contains no value.
// Its inverse is IsSome().
func (o Option[T]) IsNone() bool {
	return !o.isSome
}

// IsSome returns whether the Option contains a value.
// Its inverse is IsNone().
func (o Option[T]) IsSome() bool {
	return o.isSome
}

// IsSomeAnd returns whether the Option contains a value that matches the given predicate.
func (o Option[T]) IsSomeAnd(predicate func(T) bool) bool {
	return o.isSome && predicate(o.value)
}

// IsNoneOr returns whether the Option is either empty, or contains a value that matches the given predicate.
//
// If the predicate compares against the zero value, use options.IsNoneOrZero() instead.
func (o Option[T]) IsNoneOr(predicate func(T) bool) bool {
	return !o.isSome || predicate(o.value)
}

// Iter returns an iterator that yields the contained value once (if any).
// If the Option is empty, the iterator yields nothing.
func (o Option[T]) Iter() iter.Seq[T] {
	if o.isSome {
		return func(yield func(T) bool) { yield(o.value) }
	} else {
		return func(yield func(T) bool) {}
	}
}

// Or returns the option itself if it contains a value, or otherwise returns "other".
//
// If you are passing the result of a function call, consider using OrElse() to avoid calling the function unless necessary.
func (o Option[T]) Or(other Option[T]) Option[T] {
	if o.isSome {
		return o
	} else {
		return other
	}
}

// Or returns the option itself if it contains a value, or otherwise runs the provided closure to produce the return value.
func (o Option[T]) OrElse(closure func() Option[T]) Option[T] {
	if o.isSome {
		return o
	} else {
		return closure()
	}
}

// Unpack returns the contained value (or the zero value if None), as well as if there was a contained value.
func (o Option[T]) Unpack() (T, bool) {
	return o.value, o.isSome
}

// UnwrapOr returns the contained value.
// If the Option is empty, the provided fallback value is returned instead.
func (o Option[T]) UnwrapOr(fallback T) T {
	if o.isSome {
		return o.value
	} else {
		return fallback
	}
}

// UnwrapOrElse returns the contained value.
// If the Option is empty, the provided closure is used to produce the return value.
func (o Option[T]) UnwrapOrElse(closure func() T) T {
	if o.isSome {
		return o.value
	} else {
		return closure()
	}
}

// UnwrapOrPanic returns the contained value, or panics with the given error message if it is empty.
func (o Option[T]) UnwrapOrPanic(msg any) T {
	if o.isSome {
		return o.value
	} else {
		panic(msg)
	}
}

// UnwrapOrPanicf is a shorthand for UnwrapOrPanic(fmt.Sprintf(msg, args...)).
func (o Option[T]) UnwrapOrPanicf(msg string, args ...any) T {
	if o.isSome {
		return o.value
	} else {
		panic(fmt.Sprintf(msg, args...))
	}
}

// Xor returns an option containing a value if exactly one of the two given options contains a value, or None otherwise.
func (o Option[T]) Xor(other Option[T]) Option[T] {
	if o.isSome == other.isSome {
		return None[T]()
	} else {
		return o.Or(other)
	}
}

////////////////////////////////////////////////////////////////////////////////
// formatting/marshalling support

// These are static assertions that Option implements the intended interfaces.
// (The YAML interfaces are not checked because we don't want to add 3rd-party lib deps here.)
var (
	_ fmt.Formatter    = Option[bool]{}
	_ sql.Scanner      = &Option[bool]{}
	_ driver.Valuer    = Option[bool]{}
	_ json.Marshaler   = Option[bool]{}
	_ json.Unmarshaler = &Option[bool]{}
)

// Format implements the fmt.Formatter interface.
//
// If there is a contained value, it will be formatted as if it was given directly.
// Otherwise, the string "<none>" will be formatted according to the specified width and flags.
func (o Option[T]) Format(f fmt.State, verb rune) {
	if o.isSome {
		fmt.Fprintf(f, fmt.FormatString(f, verb), o.value)
	} else {
		fmt.Fprintf(f, fmt.FormatString(f, 's'), "<none>")
	}
}

// IsZero implements the IsZeroer interface as understood by encoding/json and github.com/go-yaml/yaml.
// It is an alias of IsNone().
func (o Option[T]) IsZero() bool {
	return !o.isSome
}

type yamlMarshaler interface {
	MarshalYAML() (any, error)
}

// Scan implements the database/sql.Scanner interface.
func (o *Option[T]) Scan(src any) error {
	var data sql.Null[T]
	err := data.Scan(src)
	if err != nil {
		return err
	}

	if data.Valid {
		*o = Some(data.V)
	} else {
		*o = None[T]()
	}
	return nil
}

// Value implements the database/sql/driver.Valuer interface.
func (o Option[T]) Value() (driver.Value, error) {
	if o.isSome {
		return driver.DefaultParameterConverter.ConvertValue(o.value)
	} else {
		return nil, nil
	}
}

// MarshalJSON implements the encoding/json.Marshaler interface.
func (o Option[T]) MarshalJSON() ([]byte, error) {
	if o.isSome {
		return json.Marshal(o.value)
	} else {
		return []byte("null"), nil
	}
}

// UnmarshalJSON implements the encoding/json.Unmarshaler interface.
func (o *Option[T]) UnmarshalJSON(buf []byte) error {
	var data *T
	err := json.Unmarshal(buf, &data)
	if err != nil {
		return err
	}
	*o = fromPointer(data)
	return nil
}

// MarshalYAML implements the yaml.Marshaler interface from gopkg.in/yaml.v2 and v3.
func (o Option[T]) MarshalYAML() (any, error) {
	if o.isSome {
		// If we just return o.value directly here, MarshalYAML will not be called
		// on the value even if it exists. For this one specific case, we have to
		// take care ourselves.
		if m, ok := any(o.value).(yamlMarshaler); ok {
			return m.MarshalYAML()
		} else {
			return o.value, nil
		}
	} else {
		return nil, nil
	}
}

// UnmarshalYAML implements the yaml.Unmarshaler interface from gopkg.in/yaml.v2.
//
// gopkg.in/yaml.v3 supports this interface via backwards-compatibility,
// so we intentionally do not use the v3-only signature that refers to the yaml.Node type.
func (o *Option[T]) UnmarshalYAML(unmarshal func(any) error) error {
	var data *T
	err := unmarshal(&data)
	if err != nil {
		return err
	}
	*o = fromPointer(data)
	return nil
}
