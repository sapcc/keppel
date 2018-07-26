/******************************************************************************
*
*  Copyright 2018 Stefan Majewsky <majewsky@gmx.net>
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

package schwift

import (
	"context"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
	"strings"
)

//RequestOptions is used to pass additional headers and values to a request.
//
//When preparing a RequestOptions instance with additional headers, the
//preferred way is to create an AccountHeaders, ContainerHeaders and
//ObjectHeaders instance and use the type-safe API on these types. Then use the
//ToOpts() method on that instance. For example:
//
//	hdr := NewObjectHeaders()
//	hdr.ContentType().Set("image/png")
//	hdr.Metadata().Set("color", "blue")
//	opts := hdr.ToOpts() //type *schwift.RequestOptions
//
type RequestOptions struct {
	Headers Headers
	Values  url.Values
	Context context.Context
}

func cloneRequestOptions(orig *RequestOptions, additional Headers) *RequestOptions {
	result := RequestOptions{
		Headers: make(Headers),
		Values:  make(url.Values),
	}
	if orig != nil {
		for k, v := range orig.Headers {
			result.Headers[k] = v
		}
		for k, v := range orig.Values {
			result.Values[k] = v
		}
		result.Context = orig.Context
	}
	for k, v := range additional {
		result.Headers[k] = v
	}
	return &result
}

//Request contains the parameters that can be set in a request to the Swift API.
type Request struct {
	Method        string //"GET", "HEAD", "PUT", "POST" or "DELETE"
	ContainerName string //empty for requests on accounts
	ObjectName    string //empty for requests on accounts/containers
	Options       *RequestOptions
	Body          io.Reader
	//ExpectStatusCodes can be left empty to disable this check, otherwise
	//schwift.UnexpectedStatusCodeError may be returned.
	ExpectStatusCodes []int
	//DrainResponseBody can be set if the caller is not interested in the
	//response body. This is implied for Response.StatusCode == 204.
	DrainResponseBody bool
}

//URL returns the full URL for this request.
func (r Request) URL(backend Backend, values url.Values) (string, error) {
	uri, err := url.Parse(backend.EndpointURL())
	if err != nil {
		return "", err
	}
	if !strings.HasSuffix(uri.Path, "/") {
		uri.Path += "/"
	}

	if r.ContainerName == "" {
		if r.ObjectName != "" {
			return "", ErrNoContainerName
		}
	} else {
		if strings.Contains(r.ContainerName, "/") {
			return "", ErrMalformedContainerName
		}
		uri.Path += r.ContainerName + "/" + r.ObjectName
	}

	uri.RawQuery = values.Encode()
	return uri.String(), nil
}

//Do executes this request on the given Backend.
func (r Request) Do(backend Backend) (*http.Response, error) {
	//build URL
	var values url.Values
	if r.Options != nil {
		values = r.Options.Values
	}
	uri, err := r.URL(backend, values)
	if err != nil {
		return nil, err
	}

	//build request
	req, err := http.NewRequest(r.Method, uri, r.Body)
	if err != nil {
		return nil, err
	}

	if r.Options != nil {
		for k, v := range r.Options.Headers {
			req.Header[k] = []string{v}
		}
		if r.Options.Context != nil {
			req = req.WithContext(r.Options.Context)
		}
	}
	if r.Body != nil {
		req.Header.Set("Expect", "100-continue")
	}

	resp, err := backend.Do(req)
	if err != nil {
		return nil, err
	}

	//return success if error code matches expectation
	if len(r.ExpectStatusCodes) == 0 {
		//check disabled -> return response unaltered
		return resp, nil
	}
	for _, code := range r.ExpectStatusCodes {
		if code == resp.StatusCode {
			var err error
			if r.DrainResponseBody || resp.StatusCode == 204 {
				err = drainResponseBody(resp)
			}
			return resp, err
		}
	}

	//unexpected status code -> generate error
	buf, err := collectResponseBody(resp)
	if err != nil {
		return nil, err
	}
	return nil, UnexpectedStatusCodeError{
		ExpectedStatusCodes: r.ExpectStatusCodes,
		ActualResponse:      resp,
		ResponseBody:        buf,
	}
}

func drainResponseBody(r *http.Response) error {
	_, err := io.Copy(ioutil.Discard, r.Body)
	if err != nil {
		return err
	}
	return r.Body.Close()
}

func collectResponseBody(r *http.Response) ([]byte, error) {
	buf, err := ioutil.ReadAll(r.Body)
	if err != nil {
		return nil, err
	}
	return buf, r.Body.Close()
}
