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

package keppel

import (
	"encoding/json"
	"fmt"
	"time"
)

//Duration is a time.Duration with custom JSON marshalling/unmarshalling logic.
type Duration time.Duration

//JSON format for type Duration.
type durationObj struct {
	Value int64  `json:"value"`
	Unit  string `json:"unit"`
}

var units = []struct {
	Name   string
	Length Duration
}{
	//ordered from big to small
	{"y", Duration(365 * 24 * time.Hour)},
	{"w", Duration(7 * 24 * time.Hour)},
	{"d", Duration(24 * time.Hour)},
	{"h", Duration(time.Hour)},
	{"m", Duration(time.Minute)},
	{"s", Duration(time.Second)},
}

//MarshalJSON implements the json.Marshaler interface.
func (d Duration) MarshalJSON() ([]byte, error) {
	//special case (without this, the following loop would render 0 as "0 years"
	//which is a bit odd)
	if d == 0 {
		return json.Marshal(durationObj{0, "s"})
	}

	//use largest unit that does not lose accuracy
	for _, unit := range units {
		if d%unit.Length == 0 {
			return json.Marshal(durationObj{int64(d / unit.Length), unit.Name})
		}
	}

	return nil, fmt.Errorf("duration is not a multiple of 1 second: %q", time.Duration(d).String())
}

//UnmarshalJSON implements the json.Unmarshaler interface.
func (d *Duration) UnmarshalJSON(src []byte) error {
	var obj durationObj
	err := json.Unmarshal(src, &obj)
	if err != nil {
		return err
	}

	for _, unit := range units {
		if unit.Name == obj.Unit {
			*d = Duration(obj.Value) * unit.Length
			return nil
		}
	}

	return fmt.Errorf("unknown duration unit: %q", obj.Unit)
}
