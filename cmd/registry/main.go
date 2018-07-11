//This file is kept in sync with github.com/docker/distribution/cmd/registry/main.go,
//but with most storage drivers removed and the swift-plus driver from this repo added instead.

package main

import (
	_ "net/http/pprof"

	"github.com/docker/distribution/registry"
	_ "github.com/docker/distribution/registry/auth/htpasswd"
	_ "github.com/docker/distribution/registry/auth/silly"
	_ "github.com/docker/distribution/registry/auth/token"
	_ "github.com/docker/distribution/registry/proxy"
	_ "github.com/docker/distribution/registry/storage/driver/filesystem"
	_ "github.com/docker/distribution/registry/storage/driver/inmemory"
	_ "github.com/docker/distribution/registry/storage/driver/middleware/redirect"
	_ "github.com/sapcc/keppel/cmd/registry/swift-plus"
)

func main() {
	registry.RootCmd.Execute()
}
