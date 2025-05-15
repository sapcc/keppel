// SPDX-FileCopyrightText: 2019 SAP SE
// SPDX-License-Identifier: Apache-2.0

package test

import (
	"encoding/json"

	"github.com/sapcc/go-api-declarations/cadf"
)

var (
	// CADFReasonOK is a helper to make cadf.Event literals shorter.
	CADFReasonOK = cadf.Reason{
		ReasonType: "HTTP",
		ReasonCode: "200",
	}
)

// ToJSON is a more compact equivalent of json.Marshal() that panics on error
// instead of returning it, and which returns string instead of []byte.
func ToJSON(x any) string {
	result, err := json.Marshal(x)
	if err != nil {
		panic(err.Error())
	}
	return string(result)
}
