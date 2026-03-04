// SPDX-FileCopyrightText: 2025 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package liquid

import (
	"math/big"
	"time"

	. "github.com/majewsky/gg/option"
)

// ForeachOptionType calls action with every Option[] type that appears in the LIQUID API and returns a slice with the results.
// This is intended for use with the cmpopts.EquateComparable() function when using github.com/google/go-cmp/cmp.
func ForeachOptionType[T any](action func(...any) T) []T {
	return []T{
		action(Option[*big.Int]{}),
		action(Option[ProjectMetadata]{}),
		action(Option[time.Time]{}),
		action(Option[uint64]{}),
		action(Option[int64]{}),
		action(Option[CommitmentStatus]{}),
		action(Option[CategoryName]{}),
	}
}
