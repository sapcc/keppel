// SPDX-FileCopyrightText: 2021 SAP SE
// SPDX-License-Identifier: Apache-2.0

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
		// test marshalling
		actualJSON, err := json.Marshal(Duration(c.Value))
		if err != nil {
			t.Errorf("cannot marshal %q: %s", c.Value.String(), err.Error())
		}
		if string(actualJSON) != c.ExpectedJSON {
			t.Errorf("while marshalling %q: expected %q, but got %q", c.Value.String(), c.ExpectedJSON, string(actualJSON))
		}

		// test unmarshalling
		var actual Duration
		err = json.Unmarshal([]byte(c.ExpectedJSON), &actual)
		if err != nil {
			t.Errorf("cannot unmarshal %q: %s", c.ExpectedJSON, err.Error())
		}
		if actual != Duration(c.Value) {
			t.Errorf("while unmarshalling %q: expected %q, but got %q", c.ExpectedJSON, c.Value.String(), time.Duration(actual).String())
		}
	}

	// test marshalling error: fractional second value
	inputDuration := 2500 * time.Millisecond
	_, err := json.Marshal(Duration(inputDuration))
	expectedError := `json: error calling MarshalJSON for type keppel.Duration: duration is not a multiple of 1 second: "2.5s"`
	if err == nil {
		t.Errorf("while marshalling %q: expected error %q, but got no error", inputDuration.String(), expectedError)
	} else if err.Error() != expectedError {
		t.Errorf("while marshalling %q: expected error %q, but got %q", inputDuration.String(), expectedError, err.Error())
	}

	// test unmarshalling error: invalid unit
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
