// SPDX-FileCopyrightText: 2019 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package test

import (
	"go.xyrillian.de/gg/jsonmatch"

	"github.com/sapcc/keppel/internal/keppel"
)

// ErrorCode wraps keppel.RegistryV2ErrorCode with an implementation of the [jsonmatch.Diffable] interface.
type ErrorCode keppel.RegistryV2ErrorCode

// DiffAgainst implements the jsonmatch.Diffable interface.
func (e ErrorCode) DiffAgainst(buf []byte) []jsonmatch.Diff {
	return jsonmatch.Object{
		"errors": []jsonmatch.Object{{
			"code":    string(e),
			"message": jsonmatch.Irrelevant(),
			"detail":  jsonmatch.Irrelevant(),
		}},
	}.DiffAgainst(buf)
}

// ErrorCodeWithMessage extends ErrorCode with an expected detail message.
type ErrorCodeWithMessage struct {
	Code    keppel.RegistryV2ErrorCode
	Message string
}

// DiffAgainst implements the jsonmatch.Diffable interface.
func (e ErrorCodeWithMessage) DiffAgainst(buf []byte) []jsonmatch.Diff {
	return jsonmatch.Object{
		"errors": []jsonmatch.Object{{
			"code":    string(e.Code),
			"message": e.Message,
			"detail":  jsonmatch.Irrelevant(),
		}},
	}.DiffAgainst(buf)
}
