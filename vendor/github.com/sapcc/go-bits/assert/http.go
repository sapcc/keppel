/*******************************************************************************
*
* Copyright 2017-2018 SAP SE
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
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

//HTTPRequest is a HTTP request that gets executed by a unit test.
type HTTPRequest struct {
	//request properties
	Method        string
	Path          string
	RequestHeader map[string]string
	//request body
	RequestJSON interface{} //if non-nil, will be encoded as JSON
	//response properties
	ExpectStatusCode int
	//response body (only one of those may be set)
	ExpectBody *string //raw content (not a file path)
	ExpectJSON string  //path to JSON file
	ExpectFile string  //path to arbitrary file
}

//Check performs the HTTP request described by this HTTPRequest against the
//given http.Handler and compares the response with the expectations in the
//HTTPRequest.
func (r HTTPRequest) Check(t *testing.T, handler http.Handler) {
	t.Helper()

	var requestBody io.Reader
	if r.RequestJSON != nil {
		body, err := json.Marshal(r.RequestJSON)
		if err != nil {
			t.Fatal(err)
		}
		requestBody = bytes.NewReader([]byte(body))
	}
	request := httptest.NewRequest(r.Method, r.Path, requestBody)
	if r.RequestHeader != nil {
		for key, value := range r.RequestHeader {
			request.Header.Set(key, value)
		}
	}

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	response := recorder.Result()
	responseBytes, _ := ioutil.ReadAll(response.Body)

	if response.StatusCode != r.ExpectStatusCode {
		t.Errorf("%s %s: expected status code %d, got %d",
			r.Method, r.Path, r.ExpectStatusCode, response.StatusCode,
		)
	}

	switch {
	case r.ExpectBody != nil:
		responseStr := string(responseBytes)
		if responseStr != *r.ExpectBody {
			t.Fatalf("%s %s: expected body %#v, but got %#v",
				r.Method, r.Path, *r.ExpectBody, responseStr,
			)
		}
	case r.ExpectJSON != "":
		var buf bytes.Buffer
		err := json.Indent(&buf, responseBytes, "", "  ")
		if err != nil {
			t.Logf("Response body: %s", responseBytes)
			t.Fatal(err)
		}
		buf.WriteByte('\n')
		r.compareBodyToFixture(t, r.ExpectJSON, buf.Bytes())
	case r.ExpectFile != "":
		r.compareBodyToFixture(t, r.ExpectFile, responseBytes)
	}
}

func (r HTTPRequest) compareBodyToFixture(t *testing.T, fixturePath string, data []byte) {
	//write actual content to file to make it easy to copy the computed result over
	//to the fixture path when a new test is added or an existing one is modified
	fixturePathAbs, _ := filepath.Abs(fixturePath)
	actualPathAbs := fixturePathAbs + ".actual"
	err := ioutil.WriteFile(actualPathAbs, data, 0644)
	if err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command("diff", "-u", fixturePathAbs, actualPathAbs)
	cmd.Stdin = nil
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err = cmd.Run()
	if err != nil {
		t.Fatalf("%s %s: body does not match: %s", r.Method, r.Path, err.Error())
	}
}
