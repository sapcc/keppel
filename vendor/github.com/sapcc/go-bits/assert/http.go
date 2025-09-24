// SPDX-FileCopyrightText: 2017-2018 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package assert

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// HTTPRequestBody is the type of field HTTPRequest.RequestBody.
// It is implemented by StringData and JSONObject.
type HTTPRequestBody interface {
	GetRequestBody() (io.Reader, error)
}

// HTTPResponseBody is the type of field HTTPRequest.ExpectBody.
// It is implemented by StringData and JSONObject.
type HTTPResponseBody interface {
	// Checks that the given actual response body is equal to this expected value.
	// `request` contains a user-readable representation of the original request,
	// for use in error messages.
	//
	// Returns whether the assertion was successful.
	AssertResponseBody(t *testing.T, requestInfo string, responseBody []byte) bool
}

// HTTPRequest is a HTTP request that gets executed by a unit test.
type HTTPRequest struct {
	// request properties
	Method string
	Path   string
	Header map[string]string
	Body   HTTPRequestBody
	// response properties
	ExpectStatus int
	ExpectBody   HTTPResponseBody
	ExpectHeader map[string]string
}

// Check performs the HTTP request described by this HTTPRequest against the
// given http.Handler and compares the response with the expectations in the
// HTTPRequest.
//
// The HTTP response is returned, along with the response body. (resp.Body is
// already exhausted when the function returns.) This is useful for tests that
// want to do further checks on `resp` or want to use data from the response.
//
// Warning: This function is considered deprecated.
// Please use httptest.Handler instead, which provides more flexible assertions.
// For example, instead of this:
//
//	assert.HTTPRequest {
//		Method:       "GET",
//		Path:         "/v1/info",
//		ExpectStatus: http.StatusOK,
//		ExpectBody:   assert.JSONObject{"error_count": 0},
//	}.Check(GinkgoT(), myHandler)
//
// Do this when using the regular std test runner:
//
//	h := httptest.NewHandler(myHandler)
//	resp := h.RespondTo(ctx, "GET /v1/info")
//	resp.ExpectJSON(t, http.StatusOK, jsonmatch.Object{"error_count": 0})
//
// Or do this when using Ginkgo/Gomega:
//
//	h := httptest.NewHandler(myHandler)
//	var info map[string]any
//	resp := h.RespondTo(ctx, "GET /v1/info", httptest.ReceiveJSONInto(&info))
//	Expect(resp).To(HaveHTTPStatus(http.StatusOK))
//	Expect(info).To(Equal(map[string]any{"error_count": 0}))
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

	hadErrors := false
	bodyShown := false

	response := recorder.Result()
	defer response.Body.Close()
	responseBytes, err := io.ReadAll(response.Body)

	if err != nil {
		hadErrors = true
		t.Errorf("Reading response body failed: %s", err.Error())
	}

	if response.StatusCode != r.ExpectStatus {
		hadErrors = true
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
		// json.Encoder.Encode() adds a stupid extra newline that we want to ignore
		if response.Header.Get("Content-Type") == "application/json" {
			responseBytes = bytes.TrimSuffix(responseBytes, []byte("\n"))
		}

		requestInfo := fmt.Sprintf("%s %s", r.Method, r.Path)
		if !r.ExpectBody.AssertResponseBody(t, requestInfo, responseBytes) {
			hadErrors = true
			bodyShown = true
		}
	}

	// in case of errors, it's usually very helpful to see the response body
	// (particularly for 4xx and 5xx responses), so make sure that it gets shown)
	if hadErrors && !bodyShown {
		t.Logf("%s %s: response body was %q", r.Method, r.Path, string(responseBytes))
	}

	return response, responseBytes
}
