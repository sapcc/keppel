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

package keppel

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
)

//RegistryV2ErrorCode is the closed set of error codes that can appear in type
//RegistryV2Error.
type RegistryV2ErrorCode string

//Possible values for RegistryV2ErrorCode.
const (
	ErrBlobUnknown         RegistryV2ErrorCode = "BLOB_UNKNOWN"
	ErrBlobUploadInvalid   RegistryV2ErrorCode = "BLOB_UPLOAD_INVALID"
	ErrBlobUploadUnknown   RegistryV2ErrorCode = "BLOB_UPLOAD_UNKNOWN"
	ErrDigestInvalid       RegistryV2ErrorCode = "DIGEST_INVALID"
	ErrManifestBlobUnknown RegistryV2ErrorCode = "MANIFEST_BLOB_UNKNOWN"
	ErrManifestInvalid     RegistryV2ErrorCode = "MANIFEST_INVALID"
	ErrManifestUnknown     RegistryV2ErrorCode = "MANIFEST_UNKNOWN"
	ErrManifestUnverified  RegistryV2ErrorCode = "MANIFEST_UNVERIFIED"
	ErrNameInvalid         RegistryV2ErrorCode = "NAME_INVALID"
	ErrNameUnknown         RegistryV2ErrorCode = "NAME_UNKNOWN"
	ErrSizeInvalid         RegistryV2ErrorCode = "SIZE_INVALID"
	ErrTagInvalid          RegistryV2ErrorCode = "TAG_INVALID"
	ErrUnauthorized        RegistryV2ErrorCode = "UNAUTHORIZED"
	ErrDenied              RegistryV2ErrorCode = "DENIED"
	ErrUnsupported         RegistryV2ErrorCode = "UNSUPPORTED"
)

//With is a convenience function for constructing type RegistryV2Error.
func (c RegistryV2ErrorCode) With(msg string, args ...interface{}) *RegistryV2Error {
	var err error
	if msg != "" {
		if len(args) > 0 {
			err = fmt.Errorf(msg, args...)
		} else {
			err = errors.New(msg)
		}
	}
	return &RegistryV2Error{Code: c, Inner: err}
}

var apiErrorMessages = map[RegistryV2ErrorCode]string{
	ErrBlobUnknown:         "blob unknown to registry",
	ErrBlobUploadInvalid:   "blob upload invalid",
	ErrBlobUploadUnknown:   "blob upload unknown to registry",
	ErrDigestInvalid:       "provided digest did not match uploaded content",
	ErrManifestBlobUnknown: "manifest blob unknown to registry",
	ErrManifestInvalid:     "manifest invalid",
	ErrManifestUnknown:     "manifest unknown",
	ErrManifestUnverified:  "manifest failed signature verification",
	ErrNameInvalid:         "invalid repository name",
	ErrNameUnknown:         "repository name not known to registry",
	ErrSizeInvalid:         "provided length did not match content length",
	ErrTagInvalid:          "manifest tag did not match URI",
	ErrUnauthorized:        "authentication required",
	ErrDenied:              "requested access to the resource is denied",
	ErrUnsupported:         "operation is unsupported",
}

var apiErrorStatusCodes = map[RegistryV2ErrorCode]int{
	ErrBlobUnknown:         http.StatusNotFound,
	ErrBlobUploadInvalid:   http.StatusUnprocessableEntity,
	ErrBlobUploadUnknown:   http.StatusNotFound,
	ErrDigestInvalid:       http.StatusUnprocessableEntity,
	ErrManifestBlobUnknown: http.StatusNotFound,
	ErrManifestInvalid:     http.StatusUnprocessableEntity,
	ErrManifestUnknown:     http.StatusNotFound,
	ErrManifestUnverified:  http.StatusUnprocessableEntity,
	ErrNameInvalid:         http.StatusUnprocessableEntity,
	ErrNameUnknown:         http.StatusNotFound,
	ErrSizeInvalid:         http.StatusUnprocessableEntity,
	ErrTagInvalid:          http.StatusUnprocessableEntity,
	ErrUnauthorized:        http.StatusUnauthorized,
	ErrDenied:              http.StatusForbidden,
	ErrUnsupported:         http.StatusNotImplemented,
}

//RegistryV2Error is the error type expected by clients of the docker-registry
//v2 API.
type RegistryV2Error struct {
	Code  RegistryV2ErrorCode
	Inner error //optional
}

//MarshalJSON implements the json.Marshaler interface.
func (e *RegistryV2Error) MarshalJSON() ([]byte, error) {
	data := struct {
		Code    string  `json:"code"`
		Message string  `json:"message"`
		Detail  *string `json:"detail,keepempty"`
	}{
		Code:    string(e.Code),
		Message: apiErrorMessages[e.Code],
	}
	if e.Inner != nil {
		detail := e.Inner.Error()
		data.Detail = &detail
	}
	return json.Marshal(data)
}

//WriteAsRegistryV2ResponseTo reports this error in the format used by the
//Registry V2 API.
func (e *RegistryV2Error) WriteAsRegistryV2ResponseTo(w http.ResponseWriter) {
	w.WriteHeader(apiErrorStatusCodes[e.Code])
	buf, _ := json.Marshal(struct {
		Errors []*RegistryV2Error `json:"errors"`
	}{
		Errors: []*RegistryV2Error{e},
	})
	w.Write(append(buf, '\n'))
}

//WriteAsTextTo reports this error in a plain text format.
func (e *RegistryV2Error) WriteAsTextTo(w http.ResponseWriter) {
	w.WriteHeader(apiErrorStatusCodes[e.Code])
	w.Write([]byte(e.Error()))
}

//Error implements the builtin/error interface.
func (e *RegistryV2Error) Error() string {
	text := apiErrorMessages[e.Code]
	if e.Inner != nil {
		text += ": " + e.Inner.Error()
	}
	return text
}
