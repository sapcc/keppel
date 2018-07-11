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
	"net/http"
	"strconv"
	"time"
)

//FieldHTTPTimeReadonly is a helper type that provides type-safe access to a
//readonly Swift header whose value is a HTTP timestamp like this:
//
//	Mon, 02 Jan 2006 15:04:05 GMT
//
//It cannot be directly constructed, but methods on the Headers types return
//this type. For example:
//
//	//suppose you have:
//	hdr, err := obj.Headers()
//
//	//you could do this:
//	time, err := time.Parse(time.RFC1123, hdr.Get("Last-Modified"))
//
//	//or you can just:
//	time := hdr.UpdatedAt().Get()
//
//Don't worry about the missing `err` in the last line. When the header fails
//to parse, Object.Headers() already returns the corresponding
//MalformedHeaderError.
type FieldHTTPTimeReadonly struct {
	h Headers
	k string
}

//Exists checks whether there is a value for this header.
func (f FieldHTTPTimeReadonly) Exists() bool {
	return f.h.Get(f.k) != ""
}

//Get returns the value for this header, or the zero value if there is no value
//(or if it is not a valid timestamp).
func (f FieldHTTPTimeReadonly) Get() time.Time {
	t, err := http.ParseTime(f.h.Get(f.k))
	if err != nil {
		return time.Time{}
	}
	return t
}

func (f FieldHTTPTimeReadonly) validate() error {
	val := f.h.Get(f.k)
	if val == "" {
		return nil
	}
	_, err := http.ParseTime(val)
	if err == nil {
		return nil
	}
	return MalformedHeaderError{f.k, err}
}

////////////////////////////////////////////////////////////////////////////////

//FieldUnixTime is a helper type that provides type-safe access to a Swift
//header whose value is a UNIX timestamp. It cannot be directly constructed,
//but methods on the Headers types return this type. For example:
//
//	//suppose you have:
//	hdr, err := obj.Headers()
//
//	//you could do all this:
//	sec, err := strconv.ParseFloat(hdr.Get("X-Delete-At"), 64)
//	time := time.Unix(0, int64(1e9 * sec))
//
//	//or you can just:
//	time := hdr.ExpiresAt().Get()
//
//Don't worry about the missing `err` in the last line. When the header fails
//to parse, Object.Headers() already returns the corresponding
//MalformedHeaderError.
type FieldUnixTime struct {
	h Headers
	k string
}

//Exists checks whether there is a value for this header.
func (f FieldUnixTime) Exists() bool {
	return f.h.Get(f.k) != ""
}

//Get returns the value for this header, or the zero value if there is no value
//(or if it is not a valid timestamp).
func (f FieldUnixTime) Get() time.Time {
	v, err := strconv.ParseFloat(f.h.Get(f.k), 64)
	if err != nil {
		return time.Time{}
	}
	return time.Unix(0, int64(1e9*v))
}

//Set writes a new value for this header into the corresponding headers
//instance.
func (f FieldUnixTime) Set(value time.Time) {
	f.h.Set(f.k, strconv.FormatUint(uint64(value.UnixNano())/1e9, 10))
}

//Del removes this key from the original headers instance, so that the key will
//remain unchanged on the server during Update().
func (f FieldUnixTime) Del() {
	f.h.Del(f.k)
}

//Clear sets this key to an empty string in the original headers instance, so
//that the key will be removed on the server during Update().
func (f FieldUnixTime) Clear() {
	f.h.Clear(f.k)
}

func (f FieldUnixTime) validate() error {
	val := f.h.Get(f.k)
	if val == "" {
		return nil
	}
	_, err := strconv.ParseFloat(val, 64)
	if err == nil {
		return nil
	}
	return MalformedHeaderError{f.k, err}
}

////////////////////////////////////////////////////////////////////////////////

//FieldUnixTimeReadonly is a readonly variant of FieldUnixTime. It is used for
//fields that cannot be set by the client.
type FieldUnixTimeReadonly struct {
	h Headers
	k string
}

//Exists checks whether there is a value for this header.
func (f FieldUnixTimeReadonly) Exists() bool {
	return f.h.Get(f.k) != ""
}

//Get returns the value for this header, or the zero value if there is no value
//(or if it is not a valid timestamp).
func (f FieldUnixTimeReadonly) Get() time.Time {
	return FieldUnixTime{f.h, f.k}.Get()
}

func (f FieldUnixTimeReadonly) validate() error {
	return FieldUnixTime{f.h, f.k}.validate()
}
