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
	"fmt"
	"reflect"
	"testing"

	"github.com/sergi/go-diff/diffmatchpatch"

	"github.com/sapcc/go-bits/osext"
)

// DeepEqual checks if the actual and expected value are equal as
// determined by reflect.DeepEqual(), and t.Error()s otherwise.
func DeepEqual[V any](t *testing.T, variable string, actual, expected V) bool {
	t.Helper()
	if reflect.DeepEqual(actual, expected) {
		return true
	}

	//NOTE: We HAVE TO use %#v here, even if it's verbose. Every other generic
	// formatting directive will not correctly distinguish all values, and thus
	// possibly render empty diffs on failure. For example,
	//
	//	fmt.Sprintf("%+v\n", []string{})    == "[]\n"
	//	fmt.Sprintf("%+v\n", []string(nil)) == "[]\n"
	//
	t.Error("assert.DeepEqual failed for " + variable)
	if osext.GetenvBool("GOBITS_PRETTY_DIFF") {
		dmp := diffmatchpatch.New()
		diffs := dmp.DiffMain(fmt.Sprintf("%#v\n", actual), fmt.Sprintf("%#v\n", expected), false)
		t.Log(dmp.DiffPrettyText(diffs))
	} else {
		t.Logf("\texpected = %#v\n", expected)
		t.Logf("\t  actual = %#v\n", actual)
	}

	return false
}
