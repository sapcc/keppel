// SPDX-FileCopyrightText: 2017-2024 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package units

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
)

// Unit represents the unit a resource or rate is measured in.
type Unit struct {
	amount Amount
}

var (
	// UnitNone is used for countable (rather than measurable) resources.
	//
	UnitNone = Unit{
		amount: Amount{Base: BaseUnitNone, Factor: 0},
		// ^ NOTE: Factor = 0 does not make sense, it should be 1 (as in `realUnitNone` below).
		// We need this because LIQUID allows representing UnitNone as an omitted Unit field.
		// Therefore, UnitNone must be equal to the zero value of type Unit.
		// Every method on this type accounts for this weird special case.
	}
	realUnitNone = Unit{
		amount: Amount{Base: BaseUnitNone, Factor: 1},
	}

	// UnitBytes is exactly that.
	UnitBytes = makeBytesUnit(1)
	// UnitKibibytes is exactly that.
	UnitKibibytes = makeBytesUnit(1 << 10)
	// UnitMebibytes is exactly that.
	UnitMebibytes = makeBytesUnit(1 << 20)
	// UnitGibibytes is exactly that.
	UnitGibibytes = makeBytesUnit(1 << 30)
	// UnitTebibytes is exactly that.
	UnitTebibytes = makeBytesUnit(1 << 40)
	// UnitPebibytes is exactly that.
	UnitPebibytes = makeBytesUnit(1 << 50)
	// UnitExbibytes is exactly that.
	UnitExbibytes = makeBytesUnit(1 << 60)
)

func makeBytesUnit(factor uint64) Unit {
	return Unit{
		amount: Amount{Base: BaseUnitBytes, Factor: factor},
	}
}

// MultiplyBy multiplies this unit by the given factor.
// This should only be used to construct non-standard units:
//
//	// okay
//	customUnit, err := UnitGibibytes.MultiplyBy(4)
//	// do not do this, use UnitTebibytes directly
//	tebibytesUnit, err := UnitGibibytes.MultiplyBy(1024)
//
// Returns an error on integer overflow, e.g. when a bytes-based unit is larger than 2^64 bytes (16 EiB).
//
// Panics if factor is 0, since that would produce a nonsensical unit.
// Panics when trying to scale UnitNone, since that produces units that cannot be serialized under the current API specification.
func (u Unit) MultiplyBy(factor uint64) (Unit, error) {
	if factor == 0 {
		panic("cannot multiply units by a zero factor")
	}
	if u == UnitNone {
		// the result could only be serialized with NumberOnlyFormat, which is not part of `validFormatsForUnit`
		// (we could technically allow `factor == 1`, but that case has no value to it)
		panic("cannot scale UnitNone because results would not be representable with Unit's serialization rules")
	}
	amount, err := u.amount.MultiplyBy(factor)
	return Unit{amount}, err
}

const validFormatsForUnit = EmptyFormat | UnitOnlyFormat | NumberWithUnitFormat

func parseUnit(input string) (Unit, error) {
	if input == "" {
		return UnitNone, nil
	}

	amount, err := ParseAmount(input, validFormatsForUnit)
	if err != nil {
		return Unit{}, err
	}
	return Unit{amount}, nil
}

// UnmarshalJSON implements the json.Unmarshaler interface.
// This method validates that the named unit actually exists.
func (u *Unit) UnmarshalJSON(buf []byte) error {
	var s string
	err := json.Unmarshal(buf, &s)
	if err != nil {
		return err
	}

	*u, err = parseUnit(s)
	return err
}

// MarshalJSON implements the json.Marshaler interface.
func (u Unit) MarshalJSON() ([]byte, error) {
	return json.Marshal(u.String())
}

// IsZero implements the Zeroer interface used by the omitzero option in encoding/json.
func (u Unit) IsZero() bool {
	return u == UnitNone
}

// Scan implements the database/sql.Scanner interface.
func (u *Unit) Scan(src any) (err error) {
	switch src := src.(type) {
	case string:
		*u, err = parseUnit(src)
		return err
	case []byte:
		*u, err = parseUnit(string(src))
		return err
	default:
		return fmt.Errorf("cannot scan into Unit from type %T", src)
	}
}

// Value implements the database/sql/driver.Valuer interface.
func (u Unit) Value() (driver.Value, error) {
	return u.String(), nil
}

// String implements the fmt.Stringer interface.
func (u Unit) String() string {
	if u == UnitNone {
		return realUnitNone.String()
	}
	return u.amount.Format(validFormatsForUnit)
}

// Base returns the base unit of this unit. For units defined as a multiple of
// another unit, that unit is the base unit. Otherwise, the same unit and a
// multiple of 1 is returned.
func (u Unit) Base() (Unit, uint64) { //nolint:gocritic // not necessary to name the results
	if u == UnitNone {
		return realUnitNone.Base()
	}
	return Unit{Amount{Base: u.amount.Base, Factor: 1}}, u.amount.Factor
}
