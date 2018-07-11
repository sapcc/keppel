/*******************************************************************************
*
* Copyright 2018 Stefan Majewsky <majewsky@gmx.net>
*
* This program is free software: you can redistribute it and/or modify it under
* the terms of the GNU General Public License as published by the Free Software
* Foundation, either version 3 of the License, or (at your option) any later
* version.
*
* This program is distributed in the hope that it will be useful, but WITHOUT ANY
* WARRANTY; without even the implied warranty of MERCHANTABILITY or FITNESS FOR
* A PARTICULAR PURPOSE. See the GNU General Public License for more details.
*
* You should have received a copy of the GNU General Public License along with
* this program. If not, see <http://www.gnu.org/licenses/>.
*
*******************************************************************************/

package schwift

import (
	"fmt"
	"regexp"
	"time"
)

//ObjectInfo is a result type returned by ObjectIterator for detailed
//object listings. The metadata in this type is a subset of Object.Headers(),
//but since it is returned as part of the detailed object listing, it can be
//obtained without making additional HEAD requests on the object(s).
type ObjectInfo struct {
	Object       *Object
	SizeBytes    uint64
	ContentType  string
	Etag         string
	LastModified time.Time
	//SymlinkTarget is only set for symlinks.
	SymlinkTarget *Object
	//If the ObjectInfo refers to an actual object, then SubDirectory is empty.
	//If the ObjectInfo refers to a pseudo-directory, then SubDirectory contains
	//the path of the pseudo-directory and all other fields are nil/zero/empty.
	//Pseudo-directories will only be reported for ObjectIterator.Delimiter != "".
	SubDirectory string
}

//ObjectIterator iterates over the objects in a container. It is typically
//constructed with the Container.Objects() method. For example:
//
//	//either this...
//	iter := container.Objects()
//	iter.Prefix = "test-"
//	objects, err := iter.Collect()
//
//	//...or this
//	objects, err := schwift.ObjectIterator{
//		Container: container,
//		Prefix: "test-",
//	}.Collect()
//
//When listing objects via a GET request on the container, you can choose to
//receive object names only (via the methods without the "Detailed" suffix),
//or object names plus some basic metadata fields (via the methods with the
//"Detailed" suffix). See struct ObjectInfo for which metadata is returned.
//
//To obtain any other metadata, you can call Object.Headers() on the result
//object, but this will issue a separate HEAD request for each object.
//
//Use the "Detailed" methods only when you use the extra metadata in struct
//ObjectInfo; detailed GET requests are more expensive than simple ones that
//return only object names.
//
//Note that, when Delimiter is set, instances of *Object that you receive from
//the iterator may refer to a pseudo-directory instead of an actual object, in
//which case Exists() will return false.
type ObjectIterator struct {
	Container *Container
	//When Prefix is set, only objects whose name starts with this string are
	//returned.
	Prefix string
	//When Delimiter is set, objects whose name contains this string (after the
	//prefix, if any) will be condensed into pseudo-directories in the result.
	//See documentation for Swift for details.
	Delimiter string
	//Options may contain additional headers and query parameters for the GET request.
	Options *RequestOptions

	base *iteratorBase
}

func (i *ObjectIterator) getBase() *iteratorBase {
	if i.base == nil {
		i.base = &iteratorBase{i: i}
	}
	return i.base
}

//NextPage queries Swift for the next page of object names. If limit is
//>= 0, not more than that many object names will be returned at once. Note
//that the server also has a limit for how many objects to list in one
//request; the lower limit wins.
//
//The end of the object listing is reached when an empty list is returned.
//
//This method offers maximal flexibility, but most users will prefer the
//simpler interfaces offered by Collect() and Foreach().
func (i *ObjectIterator) NextPage(limit int) ([]*Object, error) {
	names, err := i.getBase().nextPage(limit)
	if err != nil {
		return nil, err
	}

	result := make([]*Object, len(names))
	for idx, name := range names {
		result[idx] = i.Container.Object(name)
	}
	return result, nil
}

//The symlink_path attribute looks like "/v1/AUTH_foo/containername/obje/ctna/me".
var symlinkPathRx = regexp.MustCompile(`^/v1/([^/]+)/([^/]+)/(.+)$`)

//NextPageDetailed is like NextPage, but includes basic metadata.
func (i *ObjectIterator) NextPageDetailed(limit int) ([]ObjectInfo, error) {
	b := i.getBase()

	var document []struct {
		//either all of this:
		SizeBytes       uint64 `json:"bytes"`
		ContentType     string `json:"content_type"`
		Etag            string `json:"hash"`
		LastModifiedStr string `json:"last_modified"`
		Name            string `json:"name"`
		SymlinkPath     string `json:"symlink_path"`
		//or just this:
		Subdir string `json:"subdir"`
	}
	err := b.nextPageDetailed(limit, &document)
	if err != nil {
		return nil, err
	}
	if len(document) == 0 {
		b.setMarker("") //indicate EOF to iteratorBase
		return nil, nil
	}

	result := make([]ObjectInfo, len(document))
	marker := ""
	for idx, data := range document {
		if data.Subdir == "" {
			marker = data.Name
			result[idx].Object = i.Container.Object(data.Name)
			result[idx].ContentType = data.ContentType
			result[idx].Etag = data.Etag
			result[idx].SizeBytes = data.SizeBytes
			result[idx].LastModified, err = time.Parse(time.RFC3339Nano, data.LastModifiedStr+"Z")
			if err != nil {
				//this error is sufficiently obscure that we don't need to expose a type for it
				return nil, fmt.Errorf("Bad field objects[%d].last_modified: %s", idx, err.Error())
			}
			if data.SymlinkPath != "" {
				match := symlinkPathRx.FindStringSubmatch(data.SymlinkPath)
				if match == nil {
					//like above
					return nil, fmt.Errorf("Bad field objects[%d].symlink_path: %q", idx, data.SymlinkPath)
				}
				a := i.Container.a
				if a.Name() != match[1] {
					a = a.SwitchAccount(match[1])
				}
				result[idx].SymlinkTarget = a.Container(match[2]).Object(match[3])
			}
		} else {
			marker = data.Subdir
			result[idx].SubDirectory = data.Subdir
		}
	}

	b.setMarker(marker)
	return result, nil
}

//Foreach lists the object names matching this iterator and calls the
//callback once for every object. Iteration is aborted when a GET request fails,
//or when the callback returns a non-nil error.
func (i *ObjectIterator) Foreach(callback func(*Object) error) error {
	for {
		objects, err := i.NextPage(-1)
		if err != nil {
			return err
		}
		if len(objects) == 0 {
			return nil //EOF
		}
		for _, o := range objects {
			err := callback(o)
			if err != nil {
				return err
			}
		}
	}
}

//ForeachDetailed is like Foreach, but includes basic metadata.
func (i *ObjectIterator) ForeachDetailed(callback func(ObjectInfo) error) error {
	for {
		infos, err := i.NextPageDetailed(-1)
		if err != nil {
			return err
		}
		if len(infos) == 0 {
			return nil //EOF
		}
		for _, ci := range infos {
			err := callback(ci)
			if err != nil {
				return err
			}
		}
	}
}

//Collect lists all object names matching this iterator. For large sets of
//objects that cannot be retrieved at once, Collect handles paging behind
//the scenes. The return value is always the complete set of objects.
func (i *ObjectIterator) Collect() ([]*Object, error) {
	var result []*Object
	for {
		objects, err := i.NextPage(-1)
		if err != nil {
			return nil, err
		}
		if len(objects) == 0 {
			return result, nil //EOF
		}
		result = append(result, objects...)
	}
}

//CollectDetailed is like Collect, but includes basic metadata.
func (i *ObjectIterator) CollectDetailed() ([]ObjectInfo, error) {
	var result []ObjectInfo
	for {
		infos, err := i.NextPageDetailed(-1)
		if err != nil {
			return nil, err
		}
		if len(infos) == 0 {
			return result, nil //EOF
		}
		result = append(result, infos...)
	}
}
