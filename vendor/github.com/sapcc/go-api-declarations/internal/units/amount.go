// SPDX-FileCopyrightText: 2017-2026 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package units

import (
	"fmt"
	"strconv"
	"strings"
)

// Amount describes an amount of a countable or measurable resource in terms of a base unit.
// This type provides basic serialization and deserialization for unit or amount strings,
// e.g. between "1 KiB" and Amount{"B", 1024}.
type Amount struct {
	Base   BaseUnit
	Factor uint64
}

// BaseUnit enumerates relevant base units for units and values that appear in
// the Limes and LIQUID APIs.
type BaseUnit string

const (
	// BaseUnitNone is used for countable (rather than measurable) resources.
	BaseUnitNone BaseUnit = ""
	// BaseUnitBytes is used for resources that are measured in bytes or any multiple thereof.
	BaseUnitBytes BaseUnit = "B"
)

// Format is a bitfield enumerating permissible formats for describing amounts.
// It is used by ParseAmount() and FormatAmount() to select which formats to accept/generate.
type Format int

const (
	// EmptyFormat allows the empty string (denoting BaseUnitNone).
	EmptyFormat Format = 1 << iota
	// NumberOnlyFormat allows bare numbers like "42". Only positive integers are accepted.
	NumberOnlyFormat
	// UnitOnlyFormat allows bare units like "B" or "KiB". This does not include BareUnitNone.
	UnitOnlyFormat
	// NumberWithUnitFormat allows numbers with units, e.g. "23 MiB" or "1 B". This does not include BareUnitNone.
	NumberWithUnitFormat
)

var bareUnitDefs = []struct {
	Symbol string
	Amount Amount
}{
	// the algorithm in String() relies on this list being sorted in descending order of amount
	{"EiB", Amount{BaseUnitBytes, 1 << 60}},
	{"PiB", Amount{BaseUnitBytes, 1 << 50}},
	{"TiB", Amount{BaseUnitBytes, 1 << 40}},
	{"GiB", Amount{BaseUnitBytes, 1 << 30}},
	{"MiB", Amount{BaseUnitBytes, 1 << 20}},
	{"KiB", Amount{BaseUnitBytes, 1 << 10}},
	{"B", Amount{BaseUnitBytes, 1}},
}

// ParseAmount parses a string representation of an amount, in one of the following forms:
//   - "<amount>", e.g. "42" (for BaseUnitNone)
//   - "<amount> <unit>", e.g. "23 MiB"
//   - "<unit>", e.g. "KiB" (only with allowBareUnit = true)
func ParseAmount(input string, formats Format) (Amount, error) {
	acceptEmpty := (formats & EmptyFormat) == EmptyFormat
	acceptNumberOnly := (formats & NumberOnlyFormat) == NumberOnlyFormat
	acceptUnitOnly := (formats & UnitOnlyFormat) == UnitOnlyFormat
	acceptNumberWithUnit := (formats & NumberWithUnitFormat) == NumberWithUnitFormat

	fields := strings.Fields(input)
	switch len(fields) {
	case 0:
		if acceptEmpty {
			return Amount{BaseUnitNone, 1}, nil
		}

	case 1:
		if acceptUnitOnly {
			for _, def := range bareUnitDefs {
				if def.Symbol == fields[0] {
					return def.Amount, nil
				}
			}
			if !acceptNumberOnly {
				return Amount{}, fmt.Errorf("invalid value %q: not a known unit name", input)
			}
		}

		if acceptNumberOnly {
			number, err := strconv.ParseUint(fields[0], 10, 64)
			if err != nil {
				if acceptUnitOnly {
					return Amount{}, fmt.Errorf("invalid value %q: not a known unit name, and parsing as number failed with: %w", input, err)
				} else {
					return Amount{}, fmt.Errorf("invalid value %q: %w", input, err)
				}
			}
			return Amount{BaseUnitNone, number}, nil
		}

	case 2:
		if acceptNumberWithUnit {
			number, err := strconv.ParseUint(fields[0], 10, 64)
			if err != nil {
				return Amount{}, fmt.Errorf("invalid value %q: %w", input, err)
			}
			for _, def := range bareUnitDefs {
				if def.Symbol == fields[1] {
					return def.Amount.MultiplyBy(number)
				}
			}
			return Amount{}, fmt.Errorf("invalid value %q: no such unit", input)
		}
	}

	desc, multipleFormats := formats.Description()
	if multipleFormats {
		return Amount{}, fmt.Errorf(`value %q does not match any expected format (%s)`, input, desc)
	} else {
		return Amount{}, fmt.Errorf(`value %q does not match expected format (%s)`, input, desc)
	}
}

// MultiplyBy multiplies this amount by the given factor.
//
// Returns an error on integer overflow, e.g. when a bytes-based unit is larger than 2^64 bytes (16 EiB).
func (a Amount) MultiplyBy(factor uint64) (Amount, error) {
	if factor == 0 {
		return Amount{Base: a.Base, Factor: 0}, nil
	}
	product := a.Factor * factor
	if product/factor != a.Factor {
		return Amount{}, fmt.Errorf("overflow while multiplying %s x %d", a.Format(NumberOnlyFormat|NumberWithUnitFormat), factor)
	}
	return Amount{Base: a.Base, Factor: product}, nil
}

// Format serializes this Amount into a string representation.
// Out of the given formats, the shortest possible format will be used.
// Panics if none of the allowed formats can represent this Amount.
func (a Amount) Format(formats Format) string {
	// for measured units, find the best unit to display them in without loss of precision
	// e.g. Amount{BaseUnitBytes, 524288} -> "512 KiB" (not "0.5 MiB" because amounts do not support fractional numbers)
	//
	// NOTE: This relies on `bareUnitDefs` being sorted such that the first match is always the best.
	unitSymbol, number := string(a.Base), a.Factor
	for _, def := range bareUnitDefs {
		if def.Amount.Base == a.Base && a.Factor%def.Amount.Factor == 0 {
			unitSymbol, number = def.Symbol, a.Factor/def.Amount.Factor
			break
		}
	}

	// generate the most compact format that the caller allows
	if (formats & EmptyFormat) == EmptyFormat {
		if number == 1 && unitSymbol == "" {
			return ""
		}
	}
	if (formats & NumberOnlyFormat) == NumberOnlyFormat {
		if unitSymbol == "" {
			return strconv.FormatUint(number, 10)
		}
	}
	if (formats & UnitOnlyFormat) == UnitOnlyFormat {
		if number == 1 {
			return unitSymbol
		}
	}
	if (formats & NumberWithUnitFormat) == NumberWithUnitFormat {
		if unitSymbol != "" {
			return strconv.FormatUint(number, 10) + " " + unitSymbol
		}
	}

	// caller has not allowed us to use any format that could display this amount
	desc, multipleFormats := formats.Description()
	if multipleFormats {
		panic(fmt.Sprintf("cannot display %#v using any of the selected formats (%s)", a, desc))
	} else {
		panic(fmt.Sprintf("cannot display %#v using the selected format (%s)", a, desc))
	}
}

// Description formats this set of formats as a description for use in error messages:
//
//	desc, _ := (units.UnitOnlyFormat | unit.NumberWithUnitFormat).String()
//	fmt.Println(desc) // prints: "<unit>" or "<number> <unit>"
func (f Format) Description() (output string, multipleFormats bool) {
	parts := make([]string, 0, 4)
	if (f & EmptyFormat) == EmptyFormat {
		parts = append(parts, `""`)
	}
	if (f & NumberOnlyFormat) == NumberOnlyFormat {
		parts = append(parts, `"<number>"`)
	}
	if (f & UnitOnlyFormat) == UnitOnlyFormat {
		parts = append(parts, `"<unit>"`)
	}
	if (f & NumberWithUnitFormat) == NumberWithUnitFormat {
		parts = append(parts, `"<number> <unit>"`)
	}
	return strings.Join(parts, " or "), len(parts) > 1
}
