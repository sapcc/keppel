/******************************************************************************
*
*  Copyright 2019 SAP SE
*
*  Licensed under the Apache License, Version 2.0 (the "License");
*  you may not use this file except in compliance with the License.
*  You may obtain a copy of the License at
*
*      http://www.apache.org/licenses/LICENSE-2.0
*
*  Unless required by applicable law or agreed to in writing, software
*  distributed under the License is distributed on an "AS IS" BASIS,
*  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
*  See the License for the specific language governing permissions and
*  limitations under the License.
*
******************************************************************************/

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

	matches := len(data.Errors) == 1 && data.Errors[0].Code == e.Code
	if matches {
		matches = e.Message == "" || data.Errors[0].Message == e.Message
	}
	if !matches {
		t.Errorf(requestInfo + ": got unexpected error")
		t.Logf("\texpected = %q\n", expectedStr)
		t.Logf("\tactual = %q\n", string(responseBody))
	}

	return matches
}
