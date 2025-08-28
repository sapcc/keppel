// SPDX-FileCopyrightText: 2025 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

// Package clone contains helper functions for implementing deep clones.
package clone

type Cloneable[Self any] interface {
	Clone() Self
}

// MapRecursively clones a map containing Cloneable values by recursing into Clone() of those values.
func MapRecursively[M ~map[K]V, K comparable, V Cloneable[V]](in M) M {
	out := make(M, len(in))
	for k, v := range in {
		out[k] = v.Clone()
	}
	return out
}

// MapOfPointersRecursively clones a map containing pointers to Cloneable values by recursing into Clone() of those values.
func MapOfPointersRecursively[M ~map[K]*V, K comparable, V Cloneable[V]](in M) M {
	out := make(M, len(in))
	for k, v := range in {
		cloned := (*v).Clone()
		out[k] = &cloned
	}
	return out
}

// SliceRecursively clones a slice containing Cloneable values by recursing into Clone() of those values.
func SliceRecursively[S ~[]V, V Cloneable[V]](in S) S {
	out := make(S, len(in))
	for idx, v := range in {
		out[idx] = v.Clone()
	}
	return out
}

// MapOfSlicesRecursively clones a map containing slices of Cloneable values.
func MapOfSlicesRecursively[M ~map[K]S, K comparable, S ~[]V, V Cloneable[V]](in M) M {
	out := make(M, len(in))
	for k, s := range in {
		out[k] = SliceRecursively(s)
	}
	return out
}
