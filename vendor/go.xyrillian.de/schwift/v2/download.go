// SPDX-FileCopyrightText: 2018 Stefan Majewsky <majewsky@gmx.net>
// SPDX-License-Identifier: Apache-2.0

package schwift

import (
	"io"
)

// DownloadedObject is returned by Object.Download(). It wraps the io.ReadCloser
// from http.Response.Body with convenience methods for collecting the contents
// into a byte slice or string.
//
//	var obj *swift.Object
//
//	//Do NOT do this!
//	reader, err := obj.Download(nil).AsReadCloser()
//	bytes, err := io.ReadAll(reader)
//	err := reader.Close()
//	str := string(bytes)
//
//	//Do this instead:
//	str, err := obj.Download(nil).AsString()
//
// Since all methods on DownloadedObject are irreversible, the idiomatic way of
// using DownloadedObject is to call one of its members immediately, without
// storing the DownloadedObject instance in a variable first.
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

// AsReadCloser returns an io.ReadCloser containing the contents of the
// downloaded object.
func (o DownloadedObject) AsReadCloser() (io.ReadCloser, error) {
	return o.r, o.err
}

// AsByteSlice collects the contents of this downloaded object into a byte slice.
func (o DownloadedObject) AsByteSlice() ([]byte, error) {
	if o.err != nil {
		return nil, o.err
	}
	slice, err := io.ReadAll(o.r)
	closeErr := o.r.Close()
	if err == nil {
		err = closeErr
	}
	return slice, err
}

// AsString collects the contents of this downloaded object into a string.
func (o DownloadedObject) AsString() (string, error) {
	slice, err := o.AsByteSlice()
	return string(slice), err
}
