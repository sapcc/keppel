//This file is kept in sync with github.com/docker/distribution/cmd/registry/main.go,
//but with most storage drivers removed and the swift-plus driver from this repo added instead.

package main

import (
	"crypto/tls"
	"net/http"
	_ "net/http/pprof"
	"os"

	"github.com/docker/distribution/registry"
	_ "github.com/docker/distribution/registry/auth/token"
	_ "github.com/docker/distribution/registry/proxy"
	_ "github.com/docker/distribution/registry/storage/driver/filesystem"
	_ "github.com/docker/distribution/registry/storage/driver/inmemory"
	_ "github.com/docker/distribution/registry/storage/driver/middleware/redirect"
	_ "github.com/sapcc/keppel/pkg/registry/swift-plus"
)

func main() {
	//I have some trouble getting Keppel to connect to our staging OpenStack
	//through mitmproxy (which is very useful for development and debugging) when
	//TLS certificate verification is enabled. Therefore, allow to turn it off
	//with an env variable. (It's very important that this is not the standard
	//"KEPPEL_DEBUG" variable. That one is meant to be useful for production
	//systems, where you definitely don't want to turn off certificate
	//verification.)
	if os.Getenv("KEPPEL_INSECURE") == "1" {
		http.DefaultTransport.(*http.Transport).TLSClientConfig = &tls.Config{
			InsecureSkipVerify: true,
		}
		http.DefaultClient.Transport = http.DefaultTransport
	}

	registry.RootCmd.Execute()
}
