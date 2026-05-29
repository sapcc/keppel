// SPDX-FileCopyrightText: 2026 Stefan Majewsky <majewsky@gmx.net>
// SPDX-License-Identifier: Apache-2.0

package oblast

// RuntimeIndex provides methods for sorting records (R) by some type of key (K) at runtime.
// It is most commonly used with the result of [Store.Select] or [Store.SelectWhere], to build a lookup table for or partition of the retrieved records.
type RuntimeIndex[R any, K comparable] func(R) K

// NewRuntimeIndex casts a function into type [RuntimeIndex].
//
// In practice, this is more compact than writing the cast directly
// because type arguments can be inferred for function calls, but not type casts.
func NewRuntimeIndex[R any, K comparable](f func(R) K) RuntimeIndex[R, K] {
	return RuntimeIndex[R, K](f)
}

// Index builds a lookup table of the provided records.
//
// This should only be used when the index yields unique values for each record.
// If there can be duplicates, use [RuntimeIndex.Partition] instead.
func (i RuntimeIndex[R, K]) Index(records []R) map[K]R {
	result := make(map[K]R, len(records))
	for _, r := range records {
		result[i(r)] = r
	}
	return result
}

// IndexFrom is like Index, but can directly wrap a [Store.Select] or [Store.SelectWhere] call.
// If there is an error, it is passed through unchanged.
func (i RuntimeIndex[R, K]) IndexFrom(records []R, err error) (map[K]R, error) {
	if err != nil {
		return nil, err
	}
	return i.Index(records), nil
}

// Partition builds a partition of the resulting records by their index value.
// Within each partition, the original order of records is retained.
func (i RuntimeIndex[R, K]) Partition(records []R) map[K][]R {
	result := make(map[K][]R, len(records))
	for _, r := range records {
		key := i(r)
		result[key] = append(result[key], r)
	}
	return result
}

// PartitionFrom is like Partition, but can directly wrap a [Store.Select] or [Store.SelectWhere] call.
// If there is an error, it is passed through unchanged.
func (i RuntimeIndex[R, K]) PartitionFrom(records []R, err error) (map[K][]R, error) {
	if err != nil {
		return nil, err
	}
	return i.Partition(records), nil
}
