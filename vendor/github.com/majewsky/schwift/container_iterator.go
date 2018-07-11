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
	"fmt"
	"time"
)

//ContainerInfo is a result type returned by ContainerIterator for detailed
//container listings. The metadata in this type is a subset of Container.Headers(),
//but since it is returned as part of the detailed container listing, it can be
//obtained without making additional HEAD requests on the container(s).
type ContainerInfo struct {
	Container    *Container
	ObjectCount  uint64
	BytesUsed    uint64
	LastModified time.Time
}

//ContainerIterator iterates over the accounts in a container. It is typically
//constructed with the Account.Containers() method. For example:
//
//	//either this...
//	iter := account.Containers()
//	iter.Prefix = "test-"
//	containers, err := iter.Collect()
//
//	//...or this
//	containers, err := schwift.ContainerIterator{
//		Account: account,
//		Prefix: "test-",
//	}.Collect()
//
//When listing containers via a GET request on the account, you can choose to
//receive container names only (via the methods without the "Detailed" suffix),
//or container names plus some basic metadata fields (via the methods with the
//"Detailed" suffix). See struct ContainerInfo for which metadata is returned.
//
//To obtain any other metadata, you can call Container.Headers() on the result
//container, but this will issue a separate HEAD request for each container.
//
//Use the "Detailed" methods only when you use the extra metadata in struct
//ContainerInfo; detailed GET requests are more expensive than simple ones that
//return only container names.
type ContainerIterator struct {
	Account *Account
	//When Prefix is set, only containers whose name starts with this string are
	//returned.
	Prefix string
	//Options may contain additional headers and query parameters for the GET request.
	Options *RequestOptions

	base *iteratorBase
}

func (i *ContainerIterator) getBase() *iteratorBase {
	if i.base == nil {
		i.base = &iteratorBase{i: i}
	}
	return i.base
}

//NextPage queries Swift for the next page of container names. If limit is
//>= 0, not more than that many container names will be returned at once. Note
//that the server also has a limit for how many containers to list in one
//request; the lower limit wins.
//
//The end of the container listing is reached when an empty list is returned.
//
//This method offers maximal flexibility, but most users will prefer the
//simpler interfaces offered by Collect() and Foreach().
func (i *ContainerIterator) NextPage(limit int) ([]*Container, error) {
	names, err := i.getBase().nextPage(limit)
	if err != nil {
		return nil, err
	}

	result := make([]*Container, len(names))
	for idx, name := range names {
		result[idx] = i.Account.Container(name)
	}
	return result, nil
}

//NextPageDetailed is like NextPage, but includes basic metadata.
func (i *ContainerIterator) NextPageDetailed(limit int) ([]ContainerInfo, error) {
	b := i.getBase()

	var document []struct {
		BytesUsed       uint64 `json:"bytes"`
		ObjectCount     uint64 `json:"count"`
		LastModifiedStr string `json:"last_modified"`
		Name            string `json:"name"`
	}
	err := b.nextPageDetailed(limit, &document)
	if err != nil {
		return nil, err
	}
	if len(document) == 0 {
		b.setMarker("") //indicate EOF to iteratorBase
		return nil, nil
	}

	result := make([]ContainerInfo, len(document))
	for idx, data := range document {
		result[idx].Container = i.Account.Container(data.Name)
		result[idx].BytesUsed = data.BytesUsed
		result[idx].ObjectCount = data.ObjectCount
		result[idx].LastModified, err = time.Parse(time.RFC3339Nano, data.LastModifiedStr+"Z")
		if err != nil {
			//this error is sufficiently obscure that we don't need to expose a type for it
			return nil, fmt.Errorf("Bad field containers[%d].last_modified: %s", idx, err.Error())
		}
	}

	b.setMarker(result[len(result)-1].Container.Name())
	return result, nil
}

//Foreach lists the container names matching this iterator and calls the
//callback once for every container. Iteration is aborted when a GET request fails,
//or when the callback returns a non-nil error.
func (i *ContainerIterator) Foreach(callback func(*Container) error) error {
	for {
		containers, err := i.NextPage(-1)
		if err != nil {
			return err
		}
		if len(containers) == 0 {
			return nil //EOF
		}
		for _, c := range containers {
			err := callback(c)
			if err != nil {
				return err
			}
		}
	}
}

//ForeachDetailed is like Foreach, but includes basic metadata.
func (i *ContainerIterator) ForeachDetailed(callback func(ContainerInfo) error) error {
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

//Collect lists all container names matching this iterator. For large sets of
//containers that cannot be retrieved at once, Collect handles paging behind
//the scenes. The return value is always the complete set of containers.
func (i *ContainerIterator) Collect() ([]*Container, error) {
	var result []*Container
	for {
		containers, err := i.NextPage(-1)
		if err != nil {
			return nil, err
		}
		if len(containers) == 0 {
			return result, nil //EOF
		}
		result = append(result, containers...)
	}
}

//CollectDetailed is like Collect, but includes basic metadata.
func (i *ContainerIterator) CollectDetailed() ([]ContainerInfo, error) {
	var result []ContainerInfo
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
