/*******************************************************************************
*
* Copyright 2018 SAP SE
*
* Licensed under the Apache License, Version 2.0 (the "License");
* you may not use this file except in compliance with the License.
* You should have received a copy of the License along with this
* program. If not, you may obtain a copy of the License at
*
*     http://www.apache.org/licenses/LICENSE-2.0
*
* Unless required by applicable law or agreed to in writing, software
* distributed under the License is distributed on an "AS IS" BASIS,
* WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
* See the License for the specific language governing permissions and
* limitations under the License.
*
*******************************************************************************/

package keppel

import (
	"fmt"
	"io/ioutil"
	"net"
	"net/url"
	"regexp"

	"github.com/docker/libtrust"
)

//Configuration contains some configuration values that are not compiled during
//ReadConfig().
type Configuration struct {
	APIPublicURL     url.URL
	DatabaseURL      url.URL
	JWTIssuerKey     libtrust.PrivateKey
	JWTIssuerCertPEM string
}

//APIPublicHostname returns the hostname from the APIPublicURL.
func (cfg Configuration) APIPublicHostname() string {
	hostAndMaybePort := cfg.APIPublicURL.Host
	host, _, err := net.SplitHostPort(hostAndMaybePort)
	if err == nil {
		return host
	}
	return hostAndMaybePort //looks like there is no port in here after all
}

//ToRegistryEnvironment returns a set of environment variables that pass this
//Configuration down into a keppel-registry.
func (cfg Configuration) ToRegistryEnvironment() map[string]string {
	publicHost := cfg.APIPublicHostname()
	return map[string]string{
		"REGISTRY_AUTH_TOKEN_REALM":   cfg.APIPublicURL.String() + "/keppel/v1/auth",
		"REGISTRY_AUTH_TOKEN_SERVICE": publicHost,
		"REGISTRY_AUTH_TOKEN_ISSUER":  "keppel-api@" + publicHost,
	}
}

var (
	looksLikePEMRx    = regexp.MustCompile(`^\s*-----\s*BEGIN`)
	certificatePEMRx  = regexp.MustCompile(`^-----\s*BEGIN\s+CERTIFICATE\s*-----(?:\n|[a-zA-Z0-9+/=])*-----\s*END\s+CERTIFICATE\s*-----$`)
	stripWhitespaceRx = regexp.MustCompile(`(?m)^\s*|\s*$`)
)

//ParseIssuerKey parses the contents of the KEPPEL_ISSUER_KEY variable.
func ParseIssuerKey(in string) (libtrust.PrivateKey, error) {
	//if it looks like PEM, it's probably PEM; otherwise it's a filename
	var buf []byte
	if looksLikePEMRx.MatchString(in) {
		buf = []byte(in)
	} else {
		var err error
		buf, err = ioutil.ReadFile(in)
		if err != nil {
			return nil, err
		}
	}
	buf = stripWhitespaceRx.ReplaceAll(buf, nil)

	key, err := libtrust.UnmarshalPrivateKeyPEM(buf)
	if err != nil {
		return nil, fmt.Errorf("failed to read KEPPEL_ISSUER_KEY: " + err.Error())
	}
	return key, nil
}

//ParseIssuerCertPEM parses the contents of the KEPPEL_ISSUER_CERT variable.
func ParseIssuerCertPEM(in string) (string, error) {
	//if it looks like PEM, it's probably PEM; otherwise it's a filename
	if !looksLikePEMRx.MatchString(in) {
		buf, err := ioutil.ReadFile(in)
		if err != nil {
			return "", err
		}
		in = string(buf)
	}
	in = stripWhitespaceRx.ReplaceAllString(in, "")

	if !certificatePEMRx.MatchString(in) {
		return "", fmt.Errorf("KEPPEL_ISSUER_CERT does not look like a PEM-encoded X509 certificate: does not match regexp /%s/", certificatePEMRx.String())
	}
	return in, nil
}
