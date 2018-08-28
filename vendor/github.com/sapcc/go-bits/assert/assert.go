/*******************************************************************************
*
* Copyright 2017 SAP SE
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

package assert

import (
	"reflect"
	"testing"
)

//DeepEqual checks if the actual and expected value are equal as
//determined by reflect.DeepEqual(), and t.Error()s otherwise.
func DeepEqual(t *testing.T, variable string, actual, expected interface{}) bool {
	t.Helper()
	if reflect.DeepEqual(actual, expected) {
		return true
	}

	t.Error("assert.DeepEqual failed for " + variable)
	t.Logf("\texpected = %#v\n", expected)
	t.Logf("\t  actual = %#v\n", actual)
	return false
}
