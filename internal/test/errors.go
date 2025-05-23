// SPDX-FileCopyrightText: 2019 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package test

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/sapcc/keppel/internal/keppel"
)

// ErrorCode wraps keppel.RegistryV2ErrorCode with an implementation of the
// assert.HTTPResponseBody interface.
type ErrorCode keppel.RegistryV2ErrorCode

// AssertResponseBody implements the assert.HTTPResponseBody interface.
func (e ErrorCode) AssertResponseBody(t *testing.T, requestInfo string, responseBody []byte) bool {
	t.Helper()
	wrapped := ErrorCodeWithMessage{keppel.RegistryV2ErrorCode(e), ""}
	return wrapped.AssertResponseBody(t, requestInfo, responseBody)
}

// ErrorCodeWithMessage extends ErrorCode with an expected detail message.
type ErrorCodeWithMessage struct {
	Code    keppel.RegistryV2ErrorCode
	Message string
}

// AssertResponseBody implements the assert.HTTPResponseBody interface.
func (e ErrorCodeWithMessage) AssertResponseBody(t *testing.T, requestInfo string, responseBody []byte) bool {
	t.Helper()
	var data struct {
		Errors []struct {
			Code    keppel.RegistryV2ErrorCode `json:"code"`
			Message string                     `json:"message"`
		} `json:"errors"`
	}
	err := json.Unmarshal(responseBody, &data)
	if err != nil {
		t.Errorf("%s: cannot decode JSON: %s", requestInfo, err.Error())
		t.Logf("\tresponse body = %q", string(responseBody))
		return false
	}

	expectedStr := string(e.Code)
	if e.Message != "" {
		expectedStr = fmt.Sprintf("%s with message: %s", e.Code, e.Message)
	}

	var matches bool
	responseStr := string(responseBody)
	if len(data.Errors) == 1 {
		responseStr = fmt.Sprintf("%s with message: %s", data.Errors[0].Code, data.Errors[0].Message)

		if data.Errors[0].Code == e.Code {
			matches = e.Message == "" || data.Errors[0].Message == e.Message
		}
	}

	if !matches {
		t.Error(requestInfo + ": got unexpected error")
		t.Logf("\texpected = %q\n", expectedStr)
		t.Logf("\tactual = %q\n", responseStr)
	}

	return matches
}
