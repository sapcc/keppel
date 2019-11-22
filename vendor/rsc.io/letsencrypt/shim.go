//Package letsencrypt is a empty shim for rsc.io/letsencrypt to make
//github.com/docker/distribution compile without rsc.io/letsencrypt.
package letsencrypt

import (
	"crypto/tls"
	"errors"
	"log"
)

var errUnimplemented = errors.New("letsencrypt is not available in this binary")

type Manager struct{}

func (Manager) CacheFile(val string) error {
	return errUnimplemented
}

func (Manager) Registered() bool {
	return false
}

func (Manager) Register(val1 string, val2 func(string) bool) error {
	return errUnimplemented
}

func (Manager) SetHosts(val []string) {
	log.Fatal(errUnimplemented.Error())
}

func (Manager) GetCertificate(*tls.ClientHelloInfo) (*tls.Certificate, error) {
	return nil, errUnimplemented
}
