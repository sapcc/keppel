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
	"strconv"
)

//FieldUint64 is a helper type that provides type-safe access to a Swift header
//whose value is an unsigned integer. It cannot be directly constructed, but
//methods on the Headers types return this type. For example:
//
//	hdr := NewAccountHeaders()
//	//the following two statements are equivalent:
//	hdr["X-Account-Meta-Quota-Bytes"] = "1048576"
//	hdr.BytesUsedQuota().Set(1 << 20)
type FieldUint64 struct {
	h Headers
	k string
}

//Exists checks whether there is a value for this header.
func (f FieldUint64) Exists() bool {
	return f.h.Get(f.k) != ""
}

//Get returns the value for this header, or 0 if there is no value (or if it is
//not a valid uint64).
func (f FieldUint64) Get() uint64 {
	v, err := strconv.ParseUint(f.h.Get(f.k), 10, 64)
	if err != nil {
		return 0
	}
	return v
}

//Set writes a new value for this header into the corresponding headers
//instance.
func (f FieldUint64) Set(value uint64) {
	f.h.Set(f.k, strconv.FormatUint(value, 10))
}

//Del removes this key from the original headers instance, so that the key will
//remain unchanged on the server during Update().
func (f FieldUint64) Del() {
	f.h.Del(f.k)
}

//Clear sets this key to an empty string in the original headers instance, so
//that the key will be removed on the server during Update().
func (f FieldUint64) Clear() {
	f.h.Clear(f.k)
}

func (f FieldUint64) validate() error {
	val := f.h.Get(f.k)
	if val == "" {
		return nil
	}
	_, err := strconv.ParseUint(val, 10, 64)
	if err == nil {
		return nil
	}
	return MalformedHeaderError{f.k, err}
}

////////////////////////////////////////////////////////////////////////////////

//FieldUint64Readonly is a readonly variant of FieldUint64. It is used for
//fields that cannot be set by the client.
type FieldUint64Readonly struct {
	h Headers
	k string
}

//Exists checks whether there is a value for this header.
func (f FieldUint64Readonly) Exists() bool {
	return f.h.Get(f.k) != ""
}

//Get returns the value for this header, or 0 if there is no value (or if it is
//not a valid uint64).
func (f FieldUint64Readonly) Get() uint64 {
	return FieldUint64{f.h, f.k}.Get()
}

func (f FieldUint64Readonly) validate() error {
	return FieldUint64{f.h, f.k}.validate()
}
