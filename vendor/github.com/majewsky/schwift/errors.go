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
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

var (
	//ErrChecksumMismatch is returned by Object.Upload() when the Etag in the
	//server response does not match the uploaded data.
	ErrChecksumMismatch = errors.New("Etag on uploaded object does not match MD5 checksum of uploaded data")
	//ErrNoContainerName is returned by Request.Do() if ObjectName is given, but
	//ContainerName is empty.
	ErrNoContainerName = errors.New("missing container name")
	//ErrMalformedContainerName is returned by Request.Do() if ContainerName
	//contains slashes.
	ErrMalformedContainerName = errors.New("container name may not contain slashes")
	//ErrNotSupported is returned by bulk operations, large object operations,
	//etc. if the server does not support the requested operation.
	ErrNotSupported = errors.New("operation not supported by this Swift server")
	//ErrAccountMismatch is returned by operations on an account that accept
	//containers/objects as arguments, if some or all of the provided
	//containers/objects are located in a different account.
	ErrAccountMismatch = errors.New("some of the given objects are not in this account")
	//ErrContainerMismatch is returned by operations on a container that accept
	//objects as arguments, if some or all of the provided objects are located in
	//a different container.
	ErrContainerMismatch = errors.New("some of the given objects are not in this container")
	//ErrNotLarge is returned by Object.AsLargeObject() if the object does not
	//exist, or if it is not a large object composed out of segments.
	ErrNotLarge = errors.New("not a large object")
	//ErrSegmentInvalid is returned by LargeObject.AddSegment() if the segment
	//provided is malformed or uses features not supported by the LargeObject's
	//strategy. See documentation for LargeObject.AddSegment() for details.
	ErrSegmentInvalid = errors.New("segment invalid or incompatible with large object strategy")
)

//UnexpectedStatusCodeError is generated when a request to Swift does not yield
//a response with the expected successful status code. The actual status code
//can be checked with the Is() function; see documentation over there.
type UnexpectedStatusCodeError struct {
	ExpectedStatusCodes []int
	ActualResponse      *http.Response
	ResponseBody        []byte
}

//Error implements the builtin/error interface.
func (e UnexpectedStatusCodeError) Error() string {
	codeStrs := make([]string, len(e.ExpectedStatusCodes))
	for idx, code := range e.ExpectedStatusCodes {
		codeStrs[idx] = strconv.Itoa(code)
	}
	msg := fmt.Sprintf("expected %s response, got %d instead",
		strings.Join(codeStrs, "/"),
		e.ActualResponse.StatusCode,
	)
	if len(e.ResponseBody) > 0 {
		msg += ": " + string(e.ResponseBody)
	}
	return msg
}

//BulkObjectError is the error message for a single object in a bulk operation.
//It is not generated individually, only as part of BulkError.
type BulkObjectError struct {
	ContainerName string
	ObjectName    string
	StatusCode    int
}

//Error implements the builtin/error interface.
func (e BulkObjectError) Error() string {
	return fmt.Sprintf("%s/%s: %d %s",
		e.ContainerName, e.ObjectName,
		e.StatusCode, http.StatusText(e.StatusCode),
	)
}

//BulkError is returned by Account.BulkUpload() when the archive was
//uploaded and unpacked successfully, but some (or all) objects could not be
//saved in Swift; and by Account.BulkDelete() when not all requested objects
//could be deleted.
type BulkError struct {
	//StatusCode contains the overall HTTP status code of the operation.
	StatusCode int
	//OverallError contains the fatal error that aborted the bulk operation, or a
	//summary of which recoverable errors were encountered. It may be empty.
	OverallError string
	//ObjectErrors contains errors that occurred while working on individual
	//objects or containers. It may be empty if no such errors occurred.
	ObjectErrors []BulkObjectError
}

//Error implements the builtin/error interface. To fit into one line, it
//condenses the ObjectErrors into a count.
func (e BulkError) Error() string {
	result := fmt.Sprintf("%d %s", e.StatusCode, http.StatusText(e.StatusCode))
	if e.OverallError != "" {
		result += ": " + e.OverallError
	}
	if len(e.ObjectErrors) > 0 {
		result += fmt.Sprintf(" (+%d object errors)", len(e.ObjectErrors))
	}
	return result
}

//Is checks if the given error is an UnexpectedStatusCodeError for that status
//code. For example:
//
//	err := container.Delete(nil)
//	if err != nil {
//	    if schwift.Is(err, http.StatusNotFound) {
//	        //container does not exist -> just what we wanted
//	        return nil
//	    } else {
//	        //report unexpected error
//	        return err
//	    }
//	}
//
//It is safe to pass a nil error, in which case Is() always returns false.
func Is(err error, code int) bool {
	if e, ok := err.(UnexpectedStatusCodeError); ok {
		return e.ActualResponse.StatusCode == code
	}
	return false
}

//MalformedHeaderError is generated when a response from Swift contains a
//malformed header.
type MalformedHeaderError struct {
	Key        string
	ParseError error
}

//Error implements the builtin/error interface.
func (e MalformedHeaderError) Error() string {
	return "Bad header " + e.Key + ": " + e.ParseError.Error()
}
