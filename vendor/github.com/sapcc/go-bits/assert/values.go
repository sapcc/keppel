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
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

//StringData implements HTTPRequestBody and HTTPResponseBody for plain strings.
type StringData string

//GetRequestBody implements the HTTPRequestBody interface.
func (s StringData) GetRequestBody() (io.Reader, error) {
	return strings.NewReader(string(s)), nil
}

//AssertResponseBody implements the HTTPResponseBody interface.
func (s StringData) AssertResponseBody(t *testing.T, requestInfo string, responseBody []byte) {
	t.Helper()

	responseStr := string(responseBody)
	if responseStr != string(s) {
		t.Error(requestInfo + ": got unexpected response body")
		t.Logf("\texpected = %q\n", string(s))
		t.Logf("\t  actual = %q\n", responseStr)
		t.FailNow()
	}
}

//JSONObject implements HTTPRequestBody and HTTPResponseBody for JSON objects.
type JSONObject map[string]interface{}

//GetRequestBody implements the HTTPRequestBody interface.
func (o JSONObject) GetRequestBody() (io.Reader, error) {
	buf, err := json.Marshal(o)
	return bytes.NewReader(buf), err
}

//AssertResponseBody implements the HTTPResponseBody interface.
func (o JSONObject) AssertResponseBody(t *testing.T, requestInfo string, responseBody []byte) {
	t.Helper()

	buf, err := json.Marshal(o)
	if err != nil {
		t.Fatal(err.Error())
	}

	//need to decode and re-encode the responseBody to ensure identical ordering of keys
	var data map[string]interface{}
	err = json.Unmarshal(responseBody, &data)
	if err == nil {
		responseBody, _ = json.Marshal(data)
	}

	if string(responseBody) != string(buf) {
		t.Error(requestInfo + ": got unexpected response body")
		t.Logf("\texpected = %q\n", string(buf))
		t.Logf("\t  actual = %q\n", string(responseBody))
		t.FailNow()
	}
}

//JSONFixtureFile implements HTTPResponseBody by locating the expected JSON
//response body in the given file.
type JSONFixtureFile string

//AssertResponseBody implements the HTTPResponseBody interface.
func (f JSONFixtureFile) AssertResponseBody(t *testing.T, requestInfo string, responseBody []byte) {
	t.Helper()

	var buf bytes.Buffer
	err := json.Indent(&buf, responseBody, "", "  ")
	if err != nil {
		t.Logf("Response body: %s", responseBody)
		t.Fatal(err)
	}
	buf.WriteByte('\n')
	FixtureFile(f).AssertResponseBody(t, requestInfo, buf.Bytes())
}

//FixtureFile implements HTTPResponseBody by locating the expected
//plain-text response body in the given file.
type FixtureFile string

//AssertResponseBody implements the HTTPResponseBody interface.
func (f FixtureFile) AssertResponseBody(t *testing.T, requestInfo string, responseBody []byte) {
	t.Helper()

	//write actual content to file to make it easy to copy the computed result over
	//to the fixture path when a new test is added or an existing one is modified
	fixturePathAbs, _ := filepath.Abs(string(f))
	actualPathAbs := fixturePathAbs + ".actual"
	err := ioutil.WriteFile(actualPathAbs, responseBody, 0644)
	if err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command("diff", "-u", fixturePathAbs, actualPathAbs)
	cmd.Stdin = nil
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err = cmd.Run()
	if err != nil {
		t.Fatalf("%s: body does not match: %s", requestInfo, err.Error())
	}
}
