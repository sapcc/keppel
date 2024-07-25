/*******************************************************************************
*
* Copyright 2017-2024 SAP SE
*
* Licensed under the Apache License, Version 2.0 (the "License");
* you may not use this file except in compliance with the License.
* You should have received a copy of the License along with this
* program. If not, you may obtain a copy of the License at
*
*     http://www.apache.org/licenses/LICENSE-2.0
*
* Unless required by applicable law or agreed to in writing, software
* distributed under the License is distributed on an "AS IS" BASIS,
* WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
* See the License for the specific language governing permissions and
* limitations under the License.
*
*******************************************************************************/

package liquid

import (
	"encoding/json"
	"fmt"
)

// Unit enumerates allowed values for the unit a resource's quota/usage is
// measured in.
type Unit string

const (
	// UnitNone is used for countable (rather than measurable) resources.
	UnitNone Unit = ""
	// UnitBytes is exactly that.
	UnitBytes Unit = "B"
	// UnitKibibytes is exactly that.
	UnitKibibytes Unit = "KiB"
	// UnitMebibytes is exactly that.
	UnitMebibytes Unit = "MiB"
	// UnitGibibytes is exactly that.
	UnitGibibytes Unit = "GiB"
	// UnitTebibytes is exactly that.
	UnitTebibytes Unit = "TiB"
	// UnitPebibytes is exactly that.
	UnitPebibytes Unit = "PiB"
	// UnitExbibytes is exactly that.
	UnitExbibytes Unit = "EiB"
	// UnitUnspecified is used as a placeholder when the unit is not known.
	UnitUnspecified Unit = "UNSPECIFIED"
)

var allValidUnits = []Unit{
	UnitNone,
	UnitBytes,
	UnitKibibytes,
	UnitMebibytes,
	UnitGibibytes,
	UnitTebibytes,
	UnitPebibytes,
	UnitExbibytes,
}

// UnmarshalYAML implements the yaml.Unmarshaler interface.
// This method validates that the named unit actually exists.
//
// Deprecated: This provides backwards-compatibility with existing YAML-based config file formats in Limes which will be replaced by JSON eventually.
func (u *Unit) UnmarshalYAML(unmarshal func(any) error) error {
	var s string
	err := unmarshal(&s)
	if err != nil {
		return err
	}
	for _, unit := range allValidUnits {
		if string(unit) == s {
			*u = unit
			return nil
		}
	}
	return fmt.Errorf("unknown unit: %q", s)
}

// UnmarshalJSON implements the json.Unmarshaler interface.
// This method validates that the named unit actually exists.
func (u *Unit) UnmarshalJSON(buf []byte) error {
	var s string
	err := json.Unmarshal(buf, &s)
	if err != nil {
		return err
	}
	for _, unit := range allValidUnits {
		if string(unit) == s {
			*u = unit
			return nil
		}
	}
	return fmt.Errorf("unknown unit: %q", s)
}

// Base returns the base unit of this unit. For units defined as a multiple of
// another unit, that unit is the base unit. Otherwise, the same unit and a
// multiple of 1 is returned.
func (u Unit) Base() (Unit, uint64) { //nolint:gocritic // not necessary to name the results
	switch u {
	case UnitKibibytes:
		return UnitBytes, 1 << 10
	case UnitMebibytes:
		return UnitBytes, 1 << 20
	case UnitGibibytes:
		return UnitBytes, 1 << 30
	case UnitTebibytes:
		return UnitBytes, 1 << 40
	case UnitPebibytes:
		return UnitBytes, 1 << 50
	case UnitExbibytes:
		return UnitBytes, 1 << 60
	default:
		return u, 1
	}
}
