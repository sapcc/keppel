// SPDX-FileCopyrightText: 2017 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

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
