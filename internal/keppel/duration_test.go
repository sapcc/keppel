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
	"testing"
	"time"
)

const oneDay = 24 * time.Hour

var durationTestCases = []struct {
	Value        time.Duration
	ExpectedJSON string
}{
	{90 * time.Second, `{"value":90,"unit":"s"}`},
	{120 * time.Second, `{"value":2,"unit":"m"}`},
	{1 * time.Hour, `{"value":1,"unit":"h"}`},
	{14 * oneDay, `{"value":2,"unit":"w"}`},
	{730 * oneDay, `{"value":2,"unit":"y"}`},
}

func TestDurationMarshalling(t *testing.T) {
	for _, c := range durationTestCases {
		//test marshalling
		actualJSON, err := json.Marshal(Duration(c.Value))
		if err != nil {
			t.Errorf("cannot marshal %q: %s", c.Value.String(), err.Error())
		}
		if string(actualJSON) != c.ExpectedJSON {
			t.Errorf("while marshalling %q: expected %q, but got %q", c.Value.String(), c.ExpectedJSON, string(actualJSON))
		}

		//test unmarshalling
		var actual Duration
		err = json.Unmarshal([]byte(c.ExpectedJSON), &actual)
		if err != nil {
			t.Errorf("cannot unmarshal %q: %s", c.ExpectedJSON, err.Error())
		}
		if actual != Duration(c.Value) {
			t.Errorf("while unmarshalling %q: expected %q, but got %q", c.ExpectedJSON, c.Value.String(), time.Duration(actual).String())
		}
	}

	//test marshalling error: fractional second value
	inputDuration := 2500 * time.Millisecond
	_, err := json.Marshal(Duration(inputDuration))
	expectedError := `json: error calling MarshalJSON for type keppel.Duration: duration is not a multiple of 1 second: "2.5s"`
	if err == nil {
		t.Errorf("while marshalling %q: expected error %q, but got no error", inputDuration.String(), expectedError)
	} else if err.Error() != expectedError {
		t.Errorf("while marshalling %q: expected error %q, but got %q", inputDuration.String(), expectedError, err.Error())
	}

	//test unmarshaling error: invalid unit
	inputJSON := `{"value":10,"unit":"x"}`
	var actual Duration
	err = json.Unmarshal([]byte(inputJSON), &actual)
	expectedError = `unknown duration unit: "x"`
	if err == nil {
		t.Errorf("while unmarshalling %q: expected error %q, but got no error", inputJSON, expectedError)
	} else if err.Error() != expectedError {
		t.Errorf("while unmarshalling %q: expected error %q, but got %q", inputJSON, expectedError, err.Error())
	}
}
