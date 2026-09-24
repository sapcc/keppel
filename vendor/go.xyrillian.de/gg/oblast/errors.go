// SPDX-FileCopyrightText: 2026 Stefan Majewsky <majewsky@gmx.net>
// SPDX-License-Identifier: Apache-2.0

package oblast

import (
	"fmt"
	"reflect"
	"strings"
)

// MissingRecordError is returned by [Store.Update] if one of the rows to be updated does not exist in the DB.
type MissingRecordError[R any] struct {
	// The record that was provided to [Store.Update],
	// but for which no row with the same primary key values could be located.
	Record R
	plan   plan
}

// Error implements the builtin/error interface.
func (e MissingRecordError[R]) Error() string {
	keyDescs := make([]string, len(e.plan.PrimaryKeyColumnNames))
	v := reflect.ValueOf(e.Record)
	for idx, columnName := range e.plan.PrimaryKeyColumnNames {
		keyDescs[idx] = fmt.Sprintf("%s = %#v", columnName, v.FieldByIndex(e.plan.IndexByColumnName[columnName]))
	}
	return "could not UPDATE record that does not exist in the database: " + strings.Join(keyDescs, ", ")
}
