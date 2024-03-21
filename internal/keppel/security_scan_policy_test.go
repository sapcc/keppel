/*******************************************************************************
*
* Copyright 2023 SAP SE
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
	"reflect"
	"strings"
	"testing"

	"github.com/aquasecurity/trivy/pkg/types"
)

func TestReportTypeAlignment(t *testing.T) {
	// This test is designed to trip when newer Trivy versions add/change/remove
	// fields in `types.Report`. In this case, the respective changes need to be
	// carried over into type enrichedReport on our side.

	theirType := reflect.ValueOf(types.Report{}).Type()
	theirFields := make(map[string]reflect.StructField)
	for idx := range theirType.NumField() {
		field := theirType.Field(idx)
		theirFields[field.Name] = field
	}

	ourType := reflect.ValueOf(enrichedReport{}).Type()
	for idx := range ourType.NumField() {
		ourField := ourType.Field(idx)

		// fields that only exist on our side are allowed, but they must be serialized as "X-Keppel-..."
		theirField, exists := theirFields[ourField.Name]
		if !exists {
			if !strings.HasPrefix(string(ourField.Tag), `json:"X-Keppel-`) {
				t.Errorf("unexpected field in type enrichedReport: %#v", ourField)
			}
			continue
		}

		// fields that exist on both sides are allowed if they agree in type and serialization strategy
		if theirField.Type != ourField.Type {
			t.Errorf("type mismatch in type enrichedReport: expected type %s for field %s, but got %s",
				theirField.Type.String(), ourField.Name, ourField.Type.String(),
			)
		}
		if theirField.Tag != ourField.Tag {
			t.Errorf("type mismatch in type enrichedReport: expected tag %q for field %s, but got %q",
				theirField.Tag, ourField.Name, ourField.Tag,
			)
		}

		delete(theirFields, ourField.Name)
	}

	// fields that only exist on their side are allowed if they are not serialized into JSON
	for _, theirField := range theirFields {
		if theirField.Tag != `json:"-"` {
			t.Errorf("missing field in type enrichedReport: %#v", theirField)
		}
	}
}
