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

//FieldString is a helper type that provides type-safe access to a Swift header key
//whose value is a string. It cannot be directly constructed, but methods on
//the Headers types return this type. For example:
//
//	hdr := NewAccountHeaders()
//	//the following two statements are equivalent:
//	hdr["X-Container-Read"] = ".r:*,.rlistings"
//	hdr.ReadACL().Set(".r:*,.rlistings")
type FieldString struct {
	h Headers
	k string
}

//Exists checks whether there is a value for this header.
func (f FieldString) Exists() bool {
	return f.h.Get(f.k) != ""
}

//Get returns the value for this header, or the empty string if there is no value.
func (f FieldString) Get() string {
	return f.h.Get(f.k)
}

//Set writes a new value for this header into the corresponding headers
//instance.
func (f FieldString) Set(value string) {
	f.h.Set(f.k, value)
}

//Del removes this key from the original headers instance, so that the
//key will remain unchanged on the server during Update().
func (f FieldString) Del() {
	f.h.Del(f.k)
}

//Clear sets this key to an empty string in the original headers
//instance, so that the key will be removed on the server during Update().
func (f FieldString) Clear() {
	f.h.Clear(f.k)
}

func (f FieldString) validate() error {
	return nil
}
