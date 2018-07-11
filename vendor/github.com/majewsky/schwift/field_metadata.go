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

//FieldMetadata is a helper type that provides safe access to the metadata headers
//in a headers instance. It cannot be directly constructed, but each headers
//type has a method "Metadata" returning this type. For example:
//
//	hdr := NewObjectHeaders()
//	//the following two statements are equivalent
//	hdr["X-Object-Meta-Access"] = "strictly confidential"
//	hdr.Metadata().Set("Access", "strictly confidential")
type FieldMetadata struct {
	h Headers
	k string
}

//Clear works like Headers.Clear(), but prepends the metadata prefix to the key.
func (m FieldMetadata) Clear(key string) {
	m.h.Clear(m.k + key)
}

//Del works like Headers.Del(), but prepends the metadata prefix to the key.
func (m FieldMetadata) Del(key string) {
	m.h.Del(m.k + key)
}

//Get works like Headers.Get(), but prepends the metadata prefix to the key.
func (m FieldMetadata) Get(key string) string {
	return m.h.Get(m.k + key)
}

//Set works like Headers.Set(), but prepends the metadata prefix to the key.
func (m FieldMetadata) Set(key, value string) {
	m.h.Set(m.k+key, value)
}

func (m FieldMetadata) validate() error {
	return nil
}
