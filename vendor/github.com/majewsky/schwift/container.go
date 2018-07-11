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
)

//Container represents a Swift container. Instances are usually obtained by
//traversing downwards from an account with Account.Container() or
//Account.Containers(), or upwards from an object with Object.Container().
type Container struct {
	a    *Account
	name string
	//cache
	headers *ContainerHeaders
}

//IsEqualTo returns true if both Container instances refer to the same container.
func (c *Container) IsEqualTo(other *Container) bool {
	return other.name == c.name && other.a.IsEqualTo(c.a)
}

//Container returns a handle to the container with the given name within this
//account. This function does not issue any HTTP requests, and therefore cannot
//ensure that the container exists. Use the Exists() function to check for the
//container's existence, or chain this function with the EnsureExists()
//function like so:
//
//	container, err := account.Container("documents").EnsureExists()
func (a *Account) Container(name string) *Container {
	return &Container{a: a, name: name}
}

//Account returns a handle to the account this container is stored in.
func (c *Container) Account() *Account {
	return c.a
}

//Name returns the container name.
func (c *Container) Name() string {
	return c.name
}

//Exists checks if this container exists, potentially by issuing a HEAD request
//if no Headers() have been cached yet.
func (c *Container) Exists() (bool, error) {
	_, err := c.Headers()
	if Is(err, http.StatusNotFound) {
		return false, nil
	} else if err != nil {
		return false, err
	}
	return true, nil
}

//Headers returns the ContainerHeaders for this container. If the ContainerHeaders
//has not been cached yet, a HEAD request is issued on the container.
//
//This operation fails with http.StatusNotFound if the container does not exist.
func (c *Container) Headers() (ContainerHeaders, error) {
	if c.headers != nil {
		return *c.headers, nil
	}

	resp, err := Request{
		Method:            "HEAD",
		ContainerName:     c.name,
		ExpectStatusCodes: []int{204},
	}.Do(c.a.backend)
	if err != nil {
		return ContainerHeaders{}, err
	}

	headers := ContainerHeaders{headersFromHTTP(resp.Header)}
	err = headers.Validate()
	if err != nil {
		return headers, err
	}
	c.headers = &headers
	return *c.headers, nil
}

//Update updates the container using a POST request. To add URL parameters, pass
//a non-nil *RequestOptions.
//
//If you are not sure whether the container exists, use Create() instead.
//
//A successful POST request implies Invalidate() since it may change metadata.
func (c *Container) Update(headers ContainerHeaders, opts *RequestOptions) error {
	_, err := Request{
		Method:            "POST",
		ContainerName:     c.name,
		Options:           cloneRequestOptions(opts, headers.Headers),
		ExpectStatusCodes: []int{204},
	}.Do(c.a.backend)
	if err == nil {
		c.Invalidate()
	}
	return err
}

//Create creates the container using a PUT request. To add URL parameters, pass
//a non-nil *RequestOptions.
//
//This function can be used regardless of whether the container exists or not.
//
//A successful PUT request implies Invalidate() since it may change metadata.
func (c *Container) Create(opts *RequestOptions) error {
	_, err := Request{
		Method:            "PUT",
		ContainerName:     c.name,
		Options:           opts,
		ExpectStatusCodes: []int{201, 202},
		DrainResponseBody: true,
	}.Do(c.a.backend)
	if err == nil {
		c.Invalidate()
	}
	return err
}

//Delete deletes the container using a DELETE request. To add URL parameters,
//pass a non-nil *RequestOptions.
//
//This operation fails with http.StatusConflict if the container is not empty.
//
//This operation fails with http.StatusNotFound if the container does not exist.
//
//A successful DELETE request implies Invalidate().
func (c *Container) Delete(opts *RequestOptions) error {
	_, err := Request{
		Method:            "DELETE",
		ContainerName:     c.name,
		Options:           opts,
		ExpectStatusCodes: []int{204},
	}.Do(c.a.backend)
	if err == nil {
		c.Invalidate()
	}
	return err
}

//Invalidate clears the internal cache of this Container instance. The next call
//to Headers() on this instance will issue a HEAD request on the container.
func (c *Container) Invalidate() {
	c.headers = nil
}

//EnsureExists issues a PUT request on this container.
//If the container does not exist yet, it will be created by this call.
//If the container exists already, this call does not change it.
//This function returns the same container again, because its intended use is
//with freshly constructed Container instances like so:
//
//	container, err := account.Container("documents").EnsureExists()
func (c *Container) EnsureExists() (*Container, error) {
	_, err := Request{
		Method:            "PUT",
		ContainerName:     c.name,
		ExpectStatusCodes: []int{201, 202},
		DrainResponseBody: true,
	}.Do(c.a.backend)
	return c, err
}

//Objects returns an ObjectIterator that lists the objects in this
//container. The most common use case is:
//
//	objects, err := container.Objects().Collect()
//
//You can extend this by configuring the iterator before collecting the results:
//
//	iter := container.Objects()
//	iter.Prefix = "test-"
//	objects, err := iter.Collect()
//
//Or you can use a different iteration method:
//
//	err := container.Objects().ForeachDetailed(func (info ObjectInfo) error {
//	    log.Printf("object %s is %d bytes large!\n",
//	        info.Object.Name(), info.SizeBytes)
//	})
//
func (c *Container) Objects() *ObjectIterator {
	return &ObjectIterator{Container: c}
}

//URL returns the canonical URL for this container on the server. This is
//particularly useful when the ReadACL on the account or container is set to
//allow anonymous read access.
func (c *Container) URL() (string, error) {
	return Request{
		ContainerName: c.name,
	}.URL(c.a.backend, nil)
}
