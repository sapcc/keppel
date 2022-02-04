/*******************************************************************************
*
* Copyright 2018 SAP SE
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
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/sergi/go-diff/diffmatchpatch"
)

//ByteData implements the HTTPRequestBody and HTTPResponseBody for plain bytestrings.
type ByteData []byte

//GetRequestBody implements the HTTPRequestBody interface.
func (b ByteData) GetRequestBody() (io.Reader, error) {
	return bytes.NewReader([]byte(b)), nil
}

func logDiff(t *testing.T, expected, actual string) {
	t.Helper()

	prettyDiff, _ := strconv.ParseBool(os.Getenv("GOBITS_PRETTY_DIFF"))
	if prettyDiff {
		dmp := diffmatchpatch.New()
		diffs := dmp.DiffMain(fmt.Sprintf("%q\n", expected), fmt.Sprintf("%q\n", actual), false)
		t.Logf(dmp.DiffPrettyText(diffs))
	} else {
		t.Logf("\texpected = %q\n", string(expected))
		t.Logf("\t  actual = %q\n", string(actual))
	}
}

//AssertResponseBody implements the HTTPResponseBody interface.
func (b ByteData) AssertResponseBody(t *testing.T, requestInfo string, responseBody []byte) bool {
	t.Helper()

	if !bytes.Equal([]byte(b), responseBody) {
		t.Error(requestInfo + ": got unexpected response body")
		logDiff(t, string(b), string(responseBody))
		return false
	}

	return true
}

//StringData implements HTTPRequestBody and HTTPResponseBody for plain strings.
type StringData string

//GetRequestBody implements the HTTPRequestBody interface.
func (s StringData) GetRequestBody() (io.Reader, error) {
	return strings.NewReader(string(s)), nil
}

//AssertResponseBody implements the HTTPResponseBody interface.
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

//JSONObject implements HTTPRequestBody and HTTPResponseBody for JSON objects.
type JSONObject map[string]interface{}

//GetRequestBody implements the HTTPRequestBody interface.
func (o JSONObject) GetRequestBody() (io.Reader, error) {
	buf, err := json.Marshal(o)
	return bytes.NewReader(buf), err
}

//AssertResponseBody implements the HTTPResponseBody interface.
func (o JSONObject) AssertResponseBody(t *testing.T, requestInfo string, responseBody []byte) bool {
	t.Helper()

	buf, err := json.Marshal(o)
	if err != nil {
		t.Error(err.Error())
		return false
	}

	//need to decode and re-encode the responseBody to ensure identical ordering of keys
	var data map[string]interface{}
	err = json.Unmarshal(responseBody, &data)
	if err == nil {
		responseBody, _ = json.Marshal(data)
	}

	if string(responseBody) != string(buf) {
		t.Errorf("%s: got unexpected response body", requestInfo)
		logDiff(t, string(buf), string(responseBody))
		return false
	}

	return true
}

//JSONFixtureFile implements HTTPResponseBody by locating the expected JSON
//response body in the given file.
type JSONFixtureFile string

//AssertResponseBody implements the HTTPResponseBody interface.
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

//FixtureFile implements HTTPResponseBody by locating the expected
//plain-text response body in the given file.
type FixtureFile string

//AssertResponseBody implements the HTTPResponseBody interface.
func (f FixtureFile) AssertResponseBody(t *testing.T, requestInfo string, responseBody []byte) bool {
	t.Helper()

	//write actual content to file to make it easy to copy the computed result over
	//to the fixture path when a new test is added or an existing one is modified
	fixturePathAbs, _ := filepath.Abs(string(f))
	actualPathAbs := fixturePathAbs + ".actual"
	err := os.WriteFile(actualPathAbs, responseBody, 0644)
	if err != nil {
		t.Fatal(err)
		return false
	}

	cmd := exec.Command("diff", "-u", fixturePathAbs, actualPathAbs)
	cmd.Stdin = nil
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err = cmd.Run()
	if err != nil {
		t.Errorf("%s: body does not match: %s", requestInfo, err.Error())
	}

	return err == nil
}
