// SPDX-FileCopyrightText: 2025 Stefan Majewsky <majewsky@gmx.net>
// SPDX-License-Identifier: Apache-2.0

// Package options provides additional functions for type option.Option
// that cannot be expressed as methods on the Option type itself.
package options // import "go.xyrillian.de/gg/options"

import (
	"cmp"

	. "go.xyrillian.de/gg/option"
)

// NOTE: Keep functions sorted by name.

// FromPointer converts a *T into an Option[T].
func FromPointer[T any](value *T) Option[T] {
	if value == nil {
		return None[T]()
	} else {
		return Some(*value)
	}
}

// IsNoneOrZero returns whether o is either empty, or contains a zero value.
func IsNoneOrZero[T comparable](o Option[T]) bool {
	return o.IsNoneOr(func(value T) bool {
		var zero T
		return zero == value
	})
}

// Map applies the given function to the value contained in o, if there is one.
func Map[T, U any](o Option[T], mapping func(T) U) Option[U] {
	if t, ok := o.Unpack(); ok {
		return Some(mapping(t))
	} else {
		return None[U]()
	}
}

// Max returns the largest of its input values, while disregarding None values.
// If there are no Some values, None is returned.
func Max[T cmp.Ordered](inputs ...Option[T]) Option[T] {
	var (
		result T
		isSome = false
	)
	for _, i := range inputs {
		value, ok := i.Unpack()
		if ok && (!isSome || result < value) {
			result = value
			isSome = true
		}
	}
	if isSome {
		return Some(result)
	} else {
		return None[T]()
	}
}

// Min returns the smallest of its input values, while disregarding None values.
// If there are no Some values, None is returned.
func Min[T cmp.Ordered](inputs ...Option[T]) Option[T] {
	var (
		result T
		isSome = false
	)
	for _, i := range inputs {
		value, ok := i.Unpack()
		if ok && (!isSome || result > value) {
			result = value
			isSome = true
		}
	}
	if isSome {
		return Some(result)
	} else {
		return None[T]()
	}
}
