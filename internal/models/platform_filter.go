/*******************************************************************************
*
* Copyright 2021 SAP SE
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

package models

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"reflect"

	imagespecs "github.com/opencontainers/image-spec/specs-go/v1"
)

// PlatformFilter appears in type Account. For replica accounts, it restricts
// which submanifests get replicated when a list manifest is replicated.
type PlatformFilter []imagespecs.Platform

// Scan implements the sql.Scanner interface.
func (f *PlatformFilter) Scan(src any) error {
	in, ok := src.(string)
	if !ok {
		return fmt.Errorf("cannot deserialize %T into %T", src, f)
	}

	// default value: empty string = no filter
	if in == "" {
		*f = nil
		return nil
	}

	// otherwise deserialize from JSON
	var list []imagespecs.Platform
	err := json.Unmarshal([]byte(in), &list)
	if err != nil {
		return fmt.Errorf("cannot deserialize into PlatformFilter: %w", err)
	}

	*f = list
	return nil
}

// Value implements the driver.Valuer interface.
func (f PlatformFilter) Value() (driver.Value, error) {
	// default value: no filter == empty string
	if len(f) == 0 {
		return "", nil
	}

	// otherwise serialize to JSON
	return json.Marshal(f)
}

// Includes checks whether the given platform is included in this filter.
func (f PlatformFilter) Includes(platform imagespecs.Platform) bool {
	// default value: empty filter accepts everything
	if len(f) == 0 {
		return true
	}

	for _, p := range f {
		//NOTE: This check could be much more elaborate, e.g. consider only fields
		// that are not empty in `p`.
		if reflect.DeepEqual(p, platform) {
			return true
		}
	}
	return false
}

// IsEqualTo checks whether both filters are equal.
func (f PlatformFilter) IsEqualTo(other PlatformFilter) bool {
	if len(f) != len(other) {
		return false
	}

	for idx, p := range f {
		if !reflect.DeepEqual(p, other[idx]) {
			return false
		}
	}
	return true
}
