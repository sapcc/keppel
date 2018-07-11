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

/*

Package schwift is a client library for OpenStack Swift
(https://github.com/openstack/swift, https://openstack.org).

Authentication with Gophercloud

Schwift does not implement authentication (neither Keystone nor Swift v1), but
can be plugged into any library that does. The most common choice is
Gophercloud (https://github.com/gophercloud/gophercloud).

When using Gophercloud, you usually start by obtaining a
gophercloud.ServiceClient for Swift like so:

	import (
		"github.com/gophercloud/gophercloud/openstack"
		"github.com/gophercloud/utils/openstack/clientconfig"
	)

	//option 1: build a gophercloud.AuthOptions instance yourself
	provider, err := openstack.AuthenticatedClient(authOptions)
	client, err := openstack.NewObjectStorageV1(provider, gophercloud.EndpointOpts{})

	//option 2: have Gophercloud read the standard OS_* environment variables
	provider, err := clientConfig.AuthenticatedClient(nil)
	client, err := openstack.NewObjectStorageV1(provider, gophercloud.EndpointOpts{})

	//option 3: if you're using Swift's builtin authentication instead of Keystone
	provider, err := openstack.NewClient("http://swift.example.com:8080")
	client, err := swauth.NewObjectStorageV1(provider, swauth.AuthOpts {
		User: "project:user",
		Key:  "password",
	})

Then, in all these cases, you use gopherschwift to convert the
gophercloud.ServiceClient into a schwift.Account instance, from which point you
have access to all of schwift's API:

	import "github.com/majewsky/schwift/gopherschwift"

	account, err := gopherschwift.Wrap(client)

Authentication with a different OpenStack library

If you use a different Go library to handle Keystone/Swift authentication, take
the client object that it provides and wrap it into something that implements
the schwift.Backend interface. Then use schwift.InitializeAccount() to obtain a
schwift.Account.

Caching

When a GET or HEAD request is sent by an Account, Container or Object instance,
the headers associated with that thing will be stored in that instance and not
retrieved again.

	obj := account.Container("foo").Object("bar")

	hdr, err := obj.Headers() //sends HTTP request "HEAD <storage-url>/foo/bar"
	...
	hdr, err = obj.Headers()  //returns cached values immediately

If this behavior is not desired, the Invalidate() method can be used to clear
caches on any Account, Container or Object instance. Some methods that modify
the instance on the server call Invalidate() automatically, e.g. Object.Upload(),
Update() or Delete(). This will be indicated in the method's documentation.

Error handling

When a method on an Account, Container or Object instance makes a HTTP request
to Swift and Swift returns an unexpected status code, a
schwift.UnexpectedStatusCodeError will be returned. Schwift provides the
convenience function Is() to check the status code of these errors to detect
common failure situations:

	obj := account.Container("foo").Object("bar")
	err := obj.Upload(bytes.NewReader(data), nil)

	if schwift.Is(err, http.StatusRequestEntityTooLarge) {
		log.Print("quota exceeded for container foo!")
	} else if err != nil {
		log.Fatal("unexpected error: " + err.Error())
	}

The documentation for a method may indicate certain common error conditions
that can be detected this way by stating that "This method fails with
http.StatusXXX if ...". Because of the wide variety of failure modes in Swift,
this information is not guaranteed to be exhaustive.

*/
package schwift
