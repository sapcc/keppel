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
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"testing"
)

//HTTPRequestBody is the type of field HTTPRequest.RequestBody.
//It is implemented by StringData and JSONObject.
type HTTPRequestBody interface {
	GetRequestBody() (io.Reader, error)
}

//HTTPResponseBody is the type of field HTTPRequest.ExpectBody.
//It is implemented by StringData and JSONObject.
type HTTPResponseBody interface {
	//Checks that the given actual response body is equal to this expected value.
	//`request` contains a user-readable representation of the original request,
	//for use in error messages.
	AssertResponseBody(t *testing.T, requestInfo string, responseBody []byte)
}

//HTTPRequest is a HTTP request that gets executed by a unit test.
type HTTPRequest struct {
	//request properties
	Method string
	Path   string
	Header map[string]string
	Body   HTTPRequestBody
	//response properties
	ExpectStatus int
	ExpectBody   HTTPResponseBody
	ExpectHeader map[string]string
}

//Check performs the HTTP request described by this HTTPRequest against the
//given http.Handler and compares the response with the expectations in the
//HTTPRequest.
//
//The HTTP response is returned, along with the response body. (resp.Body is
//already exhausted when the function returns.) This is useful for tests that
//want to do further checks on `resp` or want to use data from the response.
func (r HTTPRequest) Check(t *testing.T, handler http.Handler) (resp *http.Response, responseBody []byte) {
	t.Helper()

	var requestBody io.Reader
	if r.Body != nil {
		var err error
		requestBody, err = r.Body.GetRequestBody()
		if err != nil {
			t.Fatal(err)
		}
	}
	request := httptest.NewRequest(r.Method, r.Path, requestBody)
	if r.Header != nil {
		for key, value := range r.Header {
			request.Header.Set(key, value)
		}
	}

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	response := recorder.Result()
	responseBytes, _ := ioutil.ReadAll(response.Body)

	if response.StatusCode != r.ExpectStatus {
		t.Errorf("%s %s: expected status code %d, got %d",
			r.Method, r.Path, r.ExpectStatus, response.StatusCode,
		)
	}

	for key, value := range r.ExpectHeader {
		actual := response.Header.Get(key)
		if actual != value {
			t.Errorf("%s %s: expected %s: %q, got %s: %q",
				r.Method, r.Path, key, value, key, actual,
			)
		}
	}

	if r.ExpectBody != nil {
		requestInfo := fmt.Sprintf("%s %s", r.Method, r.Path)
		r.ExpectBody.AssertResponseBody(t, requestInfo, responseBytes)
	}

	return response, responseBytes
}
