# letsencrypt-go-shim

Package `github.com/docker/distribution` depends on the library
`rsc.io/letsencrypt` which is incredibly outdated and causes trouble when
trying to vendor it. This shim substitutes for `rsc.io/letsencrypt` and just
throws errors when someone actually tries to use it.
