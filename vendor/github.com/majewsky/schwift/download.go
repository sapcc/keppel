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
	"io"
	"io/ioutil"
)

//DownloadedObject is returned by Object.Download(). It wraps the io.ReadCloser
//from http.Response.Body with convenience methods for collecting the contents
//into a byte slice or string.
//
//	var obj *swift.Object
//
//	//Do NOT do this!
//	reader, err := obj.Download(nil).AsReadCloser()
//	bytes, err := ioutil.ReadAll(reader)
//	err := reader.Close()
//	str := string(bytes)
//
//	//Do this instead:
//	str, err := obj.Download(nil).AsString()
//
//Since all methods on DownloadedObject are irreversible, the idiomatic way of
//using DownloadedObject is to call one of its members immediately, without
//storing the DownloadedObject instance in a variable first.
//
//	var obj *swift.Object
//
//	//Do NOT do this!
//	downloaded := obj.Download(nil)
//	reader, err := downloaded.AsReadCloser()
//
//	//Do this instead:
//	reader, err := obj.Download(nil).AsReadCloser()
type DownloadedObject struct {
	r   io.ReadCloser
	err error
}

//AsReadCloser returns an io.ReadCloser containing the contents of the
//downloaded object.
func (o DownloadedObject) AsReadCloser() (io.ReadCloser, error) {
	return o.r, o.err
}

//AsByteSlice collects the contents of this downloaded object into a byte slice.
func (o DownloadedObject) AsByteSlice() ([]byte, error) {
	if o.err != nil {
		return nil, o.err
	}
	slice, err := ioutil.ReadAll(o.r)
	closeErr := o.r.Close()
	if err == nil {
		err = closeErr
	}
	return slice, closeErr
}

//AsString collects the contents of this downloaded object into a string.
func (o DownloadedObject) AsString() (string, error) {
	slice, err := o.AsByteSlice()
	return string(slice), err
}
