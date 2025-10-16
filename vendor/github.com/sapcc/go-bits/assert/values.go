// SPDX-FileCopyrightText: 2018 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package assert

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/sergi/go-diff/diffmatchpatch"

	"github.com/sapcc/go-bits/internal/testdiff"
	"github.com/sapcc/go-bits/osext"
)

// ByteData implements the HTTPRequestBody and HTTPResponseBody for plain bytestrings.
type ByteData []byte

// GetRequestBody implements the HTTPRequestBody interface.
func (b ByteData) GetRequestBody() (io.Reader, error) {
	return bytes.NewReader([]byte(b)), nil
}

func logDiff(t *testing.T, expected, actual string) {
	t.Helper()

	if osext.GetenvBool("GOBITS_PRETTY_DIFF") {
		dmp := diffmatchpatch.New()
		diffs := dmp.DiffMain(fmt.Sprintf("%q\n", expected), fmt.Sprintf("%q\n", actual), false)
		t.Log(dmp.DiffPrettyText(diffs))
	} else {
		t.Logf("\texpected = %q\n", expected)
		t.Logf("\t  actual = %q\n", actual)
	}
}

// AssertResponseBody implements the HTTPResponseBody interface.
func (b ByteData) AssertResponseBody(t *testing.T, requestInfo string, responseBody []byte) bool {
	t.Helper()

	if !bytes.Equal([]byte(b), responseBody) {
		t.Error(requestInfo + ": got unexpected response body")
		logDiff(t, string(b), string(responseBody))
		return false
	}

	return true
}

// StringData implements HTTPRequestBody and HTTPResponseBody for plain strings.
type StringData string

// GetRequestBody implements the HTTPRequestBody interface.
func (s StringData) GetRequestBody() (io.Reader, error) {
	return strings.NewReader(string(s)), nil
}

// AssertResponseBody implements the HTTPResponseBody interface.
func (s StringData) AssertResponseBody(t *testing.T, requestInfo string, responseBody []byte) bool {
	t.Helper()

	responseStr := string(responseBody)
	if responseStr != string(s) {
		t.Errorf("%s: got unexpected response body", requestInfo)
		logDiff(t, string(s), responseStr)
		return false
	}

	return true
}

// JSONObject implements HTTPRequestBody and HTTPResponseBody for JSON objects.
type JSONObject map[string]any

// GetRequestBody implements the HTTPRequestBody interface.
func (o JSONObject) GetRequestBody() (io.Reader, error) {
	buf, err := json.Marshal(o)
	return bytes.NewReader(buf), err
}

// AssertResponseBody implements the HTTPResponseBody interface.
func (o JSONObject) AssertResponseBody(t *testing.T, requestInfo string, responseBody []byte) bool {
	t.Helper()

	buf, err := json.Marshal(o)
	if err != nil {
		t.Error(err.Error())
		return false
	}

	// need to decode and re-encode the responseBody to ensure identical ordering of keys
	var data map[string]any
	err = json.Unmarshal(responseBody, &data)
	if err == nil {
		responseBody, err = json.Marshal(data)
		if err != nil {
			t.Errorf("JSON marshalling failed: %s", err.Error())
			return false
		}
	}

	if string(responseBody) != string(buf) {
		t.Errorf("%s: got unexpected response body", requestInfo)
		logDiff(t, string(buf), string(responseBody))
		return false
	}

	return true
}

// JSONFixtureFile implements HTTPResponseBody by locating the expected JSON
// response body in the given file.
type JSONFixtureFile string

// AssertResponseBody implements the HTTPResponseBody interface.
func (f JSONFixtureFile) AssertResponseBody(t *testing.T, requestInfo string, responseBody []byte) bool {
	t.Helper()

	var buf bytes.Buffer
	err := json.Indent(&buf, responseBody, "", "  ")
	if err != nil {
		t.Logf("Response body: %s", responseBody)
		t.Fatal(err)
		return false
	}
	buf.WriteByte('\n')
	return FixtureFile(f).AssertResponseBody(t, requestInfo, buf.Bytes())
}

// FixtureFile implements HTTPResponseBody by locating the expected
// plain-text response body in the given file.
type FixtureFile string

// AssertResponseBody implements the HTTPResponseBody interface.
func (f FixtureFile) AssertResponseBody(t *testing.T, requestInfo string, responseBody []byte) bool {
	t.Helper()
	err := testdiff.DiffAgainstFixtureFile(string(f), responseBody)
	if err != nil {
		t.Errorf("%s: body does not match: %s", requestInfo, err.Error())
	}
	return err == nil
}
