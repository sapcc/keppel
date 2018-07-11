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
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/majewsky/schwift/capabilities"
)

//BulkUploadFormat enumerates possible archive formats for Container.BulkUpload().
type BulkUploadFormat string

const (
	//BulkUploadTar is a plain tar archive.
	BulkUploadTar BulkUploadFormat = "tar"
	//BulkUploadTarGzip is a GZip-compressed tar archive.
	BulkUploadTarGzip BulkUploadFormat = "tar.gz"
	//BulkUploadTarBzip2 is a BZip2-compressed tar archive.
	BulkUploadTarBzip2 BulkUploadFormat = "tar.bz2"
)

//BulkUpload extracts an archive (which may contain multiple files) into a
//Swift account. The path of each file in the archive is appended to the
//uploadPath to form the FullName() of the resulting Object.
//
//For example, when uploading an archive that contains the file "a/b/c":
//
//	//This uploads the file into the container "a" as object "b/c".
//	account.BulkUpload("", format, contents, nil)
//	//This uploads the file into the container "foo" as object "a/b/c".
//	account.BulkUpload("foo", format, contents, nil)
//	//This uploads the file into the container "foo" as object "bar/baz/a/b/c".
//	account.BulkUpload("foo/bar/baz", format, contents, nil)
//
//The first return value indicates the number of files that have been created
//on the server side. This may be lower than the number of files in the archive
//if some files could not be saved individually (e.g. because a quota was
//exceeded in the middle of the archive extraction).
//
//If not nil, the error return value is *usually* an instance of BulkError.
//
//This operation returns (0, ErrNotSupported) if the server does not support
//bulk-uploading.
func (a *Account) BulkUpload(uploadPath string, format BulkUploadFormat, contents io.Reader, opts *RequestOptions) (int, error) {
	caps, err := a.Capabilities()
	if err != nil {
		return 0, err
	}
	if caps.BulkUpload == nil {
		return 0, ErrNotSupported
	}

	req := Request{
		Method:            "PUT",
		Body:              contents,
		Options:           cloneRequestOptions(opts, nil),
		ExpectStatusCodes: []int{200},
	}
	req.Options.Headers.Set("Accept", "application/json")
	req.Options.Values.Set("extract-archive", string(format))

	fields := strings.SplitN(strings.Trim(uploadPath, "/"), "/", 2)
	req.ContainerName = fields[0]
	if len(fields) == 2 {
		req.ObjectName = fields[1]
	}

	resp, err := req.Do(a.backend)
	if err != nil {
		return 0, err
	}

	result, err := parseBulkResponse(resp.Body)
	return result.NumberFilesCreated, err
}

func parseResponseStatus(status string) (int, error) {
	//`status` looks like "201 Created"
	fields := strings.SplitN(status, " ", 2)
	return strconv.Atoi(fields[0])
}

func makeBulkObjectError(fullName string, statusCode int) BulkObjectError {
	nameFields := strings.SplitN(fullName, "/", 2)
	for len(nameFields) < 2 {
		nameFields = append(nameFields, "")
	}
	return BulkObjectError{
		ContainerName: nameFields[0],
		ObjectName:    nameFields[1],
		StatusCode:    statusCode,
	}
}

//BulkDelete deletes a large number of objects (and containers) at once.
//Containers are queued at the end of the deletion, so a container can be
//deleted in the same call in which all objects in it are deleted.
//
//For example, to delete all objects in a container:
//
//	var container *schwift.Container
//
//	objects, err := container.Objects().Collect()
//	numDeleted, numNotFound, err := container.Account().BulkDelete(objects, nil, nil)
//
//To also delete the container:
//
//	var container *schwift.Container
//
//	objects, err := container.Objects().Collect()
//	numDeleted, numNotFound, err := container.Account().BulkDelete(
//	    objects, []*schwift.Container{container}, nil)
//
//If the server does not support bulk-deletion, this function falls back to
//deleting each object and container individually, and aggregates the result.
//
//If not nil, the error return value is *usually* an instance of BulkError.
//
//The objects may be located in multiple containers, but they and the
//containers must all be located in the given account. (Otherwise,
//ErrAccountMismatch is returned.)
func (a *Account) BulkDelete(objects []*Object, containers []*Container, opts *RequestOptions) (numDeleted int, numNotFound int, deleteError error) {
	//validate that all given objects are in this account
	for _, obj := range objects {
		if !a.IsEqualTo(obj.Container().Account()) {
			return 0, 0, ErrAccountMismatch
		}
	}
	for _, container := range containers {
		if !a.IsEqualTo(container.Account()) {
			return 0, 0, ErrAccountMismatch
		}
	}

	//check capabilities to choose deletion method
	caps, err := a.Capabilities()
	if err != nil {
		return 0, 0, err
	}
	if caps.BulkDelete == nil || !capabilities.AllowBulkDelete {
		return a.bulkDeleteSingle(objects, containers, opts)
	}
	chunkSize := int(caps.BulkDelete.MaximumDeletesPerRequest)

	//collect names of things to delete into one big list
	var names []string
	for _, object := range objects {
		object.Invalidate() //deletion must invalidate objects!
		names = append(names, fmt.Sprintf("/%s/%s",
			url.PathEscape(object.Container().Name()),
			url.PathEscape(object.Name()),
		))
	}
	for _, container := range containers {
		container.Invalidate() //deletion must invalidate objects!
		names = append(names, "/"+url.PathEscape(container.Name()))
	}

	//split list into chunks according to maximum allowed
	//chunk size; aggregate results
	for len(names) > 0 {
		//this condition holds only in the final iteration
		if chunkSize > len(names) {
			chunkSize = len(names)
		}
		chunk := names[0:chunkSize]
		names = names[chunkSize:]

		numDeletedNow, numNotFoundNow, err := a.bulkDelete(chunk, opts)
		numDeleted += numDeletedNow
		numNotFound += numNotFoundNow
		if err != nil {
			return numDeleted, numNotFound, err
		}
	}

	return numDeleted, numNotFound, nil
}

//Implementation of BulkDelete() for servers that *do not* support bulk
//deletion.
func (a *Account) bulkDeleteSingle(objects []*Object, containers []*Container, opts *RequestOptions) (int, int, error) {
	var (
		numDeleted  = 0
		numNotFound = 0
		errs        []BulkObjectError
	)

	handleSingleError := func(containerName, objectName string, err error) error {
		if err == nil {
			numDeleted++
			return nil
		}
		if Is(err, http.StatusNotFound) {
			numNotFound++
			return nil
		}
		if statusErr, ok := err.(UnexpectedStatusCodeError); ok {
			errs = append(errs, BulkObjectError{
				ContainerName: containerName,
				ObjectName:    objectName,
				StatusCode:    statusErr.ActualResponse.StatusCode,
			})
			return nil
		}
		//unexpected error type -> stop early
		return err
	}

	for _, obj := range objects {
		err := obj.Delete(nil, opts) //this implies Invalidate()
		err = handleSingleError(obj.Container().Name(), obj.Name(), err)
		if err != nil {
			return numDeleted, numNotFound, err
		}
	}

	for _, container := range containers {
		err := container.Delete(opts) //this implies Invalidate()
		err = handleSingleError(container.Name(), "", err)
		if err != nil {
			return numDeleted, numNotFound, err
		}
	}

	if len(errs) == 0 {
		return numDeleted, numNotFound, nil
	}
	return numDeleted, numNotFound, BulkError{
		StatusCode:   errs[0].StatusCode,
		OverallError: http.StatusText(errs[0].StatusCode),
		ObjectErrors: errs,
	}
}

//Implementation of BulkDelete() for servers that *do* support bulk deletion.
//This function is called *after* chunking, so `len(names) <=
//account.Capabilities.BulkDelete.MaximumDeletesPerRequest`.
func (a *Account) bulkDelete(names []string, opts *RequestOptions) (int, int, error) {
	req := Request{
		Method:            "DELETE",
		Body:              strings.NewReader(strings.Join(names, "\n") + "\n"),
		Options:           cloneRequestOptions(opts, nil),
		ExpectStatusCodes: []int{200},
	}
	req.Options.Headers.Set("Accept", "application/json")
	req.Options.Headers.Set("Content-Type", "text/plain")
	req.Options.Values.Set("bulk-delete", "true")
	resp, err := req.Do(a.backend)
	if err != nil {
		return 0, 0, err
	}

	result, err := parseBulkResponse(resp.Body)
	return result.NumberDeleted, result.NumberNotFound, err
}

type bulkResponse struct {
	//ResponseStatus indicates the overall result as a HTTP status string, e.g.
	//"201 Created" or "500 Internal Error".
	ResponseStatus string `json:"Response Status"`
	//ResponseBody contains an overall error message for errors that are not
	//related to a single file in the archive (e.g. "invalid tar file" or "Max
	//delete failures exceeded").
	ResponseBody string `json:"Response Body"`
	//Errors contains error messages for individual files. Each entry is a
	//[]string with 2 elements, the object's fullName and the HTTP status for
	//this file's upload (e.g. "412 Precondition Failed").
	Errors [][]string `json:"Errors"`
	//NumberFilesCreated is included in the BulkUpload result only.
	NumberFilesCreated int `json:"Number Files Created"`
	//NumberDeleted is included in the BulkDelete result only.
	NumberDeleted int `json:"Number Deleted"`
	//NumberNotFound is included in the BulkDelete result only.
	NumberNotFound int `json:"Number Not Found"`
}

func parseBulkResponse(body io.ReadCloser) (bulkResponse, error) {
	var resp bulkResponse
	err := json.NewDecoder(body).Decode(&resp)
	closeErr := body.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return resp, err
	}

	//parse `resp` into type BulkError
	bulkErr := BulkError{
		OverallError: resp.ResponseBody,
	}
	bulkErr.StatusCode, err = parseResponseStatus(resp.ResponseStatus)
	if err != nil {
		return resp, err
	}
	for _, suberr := range resp.Errors {
		if len(suberr) != 2 {
			continue //wtf
		}
		statusCode, err := parseResponseStatus(suberr[1])
		if err != nil {
			return resp, err
		}
		bulkErr.ObjectErrors = append(bulkErr.ObjectErrors,
			makeBulkObjectError(suberr[0], statusCode),
		)
	}

	//is BulkError really an error?
	if len(bulkErr.ObjectErrors) == 0 && bulkErr.OverallError == "" && bulkErr.StatusCode >= 200 && bulkErr.StatusCode < 300 {
		return resp, nil
	}
	return resp, bulkErr
	//NOTE: `resp` is passed back to the caller to read the counters
	//(resp.NumberFilesCreated etc.)
}
