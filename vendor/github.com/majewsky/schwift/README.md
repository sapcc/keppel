# Schwift

[![GoDoc](https://godoc.org/github.com/majewsky/schwift?status.svg)](https://godoc.org/github.com/majewsky/schwift)

This is a Go client library for [OpenStack Swift](https://github.com/openstack/swift). I made this after growing
frustrated with the inflexible API design of [`ncw/swift`](https://github.com/ncw/swift); see [near the
bottom](#why-another-swift-client-library) for details.

This library is currently in **beta**: It's already used by some projects, and I'm working towards a
stable 1.0 version with API compatibility promises, but [a few things are still
missing](https://github.com/majewsky/schwift/issues/1).

## Installation

You can get this with `go get github.com/majewsky/schwift`. When using this in an application, vendoring is recommended.

## Usage

This library uses [Gophercloud](https://github.com/gophercloud/gophercloud) to handle authentication, so to use Schwift, you have to first build a `gophercloud.ServiceClient` and then pass that to `gopherschwift.Wrap()` to get a handle on the Swift account.

For example, to connect to Swift using OpenStack Keystone authentication:

```go
import (
  "github.com/gophercloud/gophercloud"
  "github.com/gophercloud/gophercloud/openstack"
  "github.com/majewsky/schwift/gopherschwift"
)

authOptions, err := openstack.AuthOptionsFromEnv()
provider, err := openstack.AuthenticatedClient(authOptions)
client, err := openstack.NewObjectStorageV1(provider, gophercloud.EndpointOpts{})

account, err := gopherschwift.Wrap(client, nil)
```

To connect to Swift using Swift's built-in authentication:

```go
import (
  "github.com/gophercloud/gophercloud/openstack"
  "github.com/gophercloud/gophercloud/openstack/objectstore/v1/swauth"
  "github.com/majewsky/schwift/gopherschwift"
)

provider, err := openstack.NewClient("http://swift.example.com:8080")
client, err := swauth.NewObjectStorageV1(provider, swauth.AuthOpts {
    User: "project:user",
    Key:  "password",
})

account, err := gopherschwift.Wrap(client, nil)
```

From this point, follow the [API documentation](https://godoc.org/github.com/majewsky/schwift) for what you can do with
the `schwift.Account` object.

## Why another Swift client library?

The most popular Swift client library is [`ncw/swift`](https://github.com/ncw/swift). I have [used
it](https://github.com/docker/distribution/pull/2441) [extensively](https://github.com/sapcc/swift-http-import) and my
main gripe with it is that its API is mostly based on single functions. When your API is a function, you cannot easily
add further arguments to it without breaking backwards compatiblity. Whenever someone wants to do something slightly
different, an entirely new function needs to be added. To witness, ncw/swift has five functions for listing objects,
four functions for downloading objects, and three functions for uploading objects. (And that's without considering the
separate API for large objects.) And still, when you try to do something that's not one of the 10 most common things,
you're going to run into dead ends where the API does not allow you do specify that one URL parameter that you need.
Like that one day [when I filed five issues in a row because every function in the API that I tried turned out to be
missing something](https://github.com/ncw/swift/issues?utf8=%E2%9C%93&q=is%3Aissue+author%3Amajewsky+created%3A2017-11).

Schwift improves on ncw/swift by:

- allowing the user to set arbitary headers and URL parameters in every request method,
- including a pointer to `RequestOpts` in every request method, which can later be extended with new members without
  breaking backwards compatiblity, and
- providing a generic `Request.Do()` method as a last resort for users who need to do a request that absolutely cannot
  be made with the existing request methods.

### What about Gophercloud?

Schwift uses Gophercloud for authentication. That solves one problem that ncw/swift has, namely that you cannot
use the Keystone token that ncw/swift fetches for talking to other OpenStack services.

But besides the auth code, Schwift avoids all other parts of Gophercloud. Gophercloud, like many other OpenStack client
libraries, is modeled frankly around the "JSON-in, JSON-out" request-response-based design that all OpenStack APIs
share. All of them, except for Swift. A lot of the infrastructure that Gophercloud provides is not suited for Swift,
mostly on account of it not using JSON bodies anywhere.

Furthermore, the API of Gophercloud is modeled around individual requests and responses, which means that there will
probably never be support for advanced features like large objects unless you're willing to do all the footwork
yourself.

Schwift improves on Gophercloud by providing a object-oriented API that respects and embraces Swift's domain model and
API design.
