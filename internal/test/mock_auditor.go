/*******************************************************************************
*
* Copyright 2019 SAP SE
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
