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
	"io/ioutil"
	"net"
	"net/url"
	"regexp"

	"github.com/docker/libtrust"
	"github.com/sapcc/go-bits/logg"
	"github.com/sapcc/keppel/pkg/database"
	"github.com/sapcc/keppel/pkg/drivers"
	yaml "gopkg.in/yaml.v2"
)

//State is the master singleton containing all globally shared handles and
//configuration values. It is filled by func ReadConfig().
var State *StateStruct

//StateStruct is the type of `var State`.
type StateStruct struct {
	Config           Configuration
	DB               *database.DB
	AuthDriver       drivers.AuthDriver
	StorageDriver    drivers.StorageDriver
	JWTIssuerKey     libtrust.PrivateKey
	JWTIssuerCertPEM string
}

//Configuration contains some configuration values that are not compiled during
//ReadConfig().
type Configuration struct {
	APIListenAddress string
	APIPublicURL     url.URL
	DatabaseURL      url.URL
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

type configuration struct {
	API struct {
		ListenAddress string `yaml:"listen_address"`
		PublicURL     string `yaml:"public_url"`
	} `yaml:"api"`
	DB struct {
		URL string `yaml:"url"`
	} `yaml:"db"`
	Auth    authDriverSection    `yaml:"auth"`
	Storage storageDriverSection `yaml:"storage"`
	Trust   struct {
		IssuerKeyIn  string `yaml:"issuer_key"`
		IssuerCertIn string `yaml:"issuer_cert"`
	} `yaml:"trust"`
}

//This is a separate type because of its UnmarshalYAML implementation.
type authDriverSection struct {
	Driver drivers.AuthDriver
}

//UnmarshalYAML implements the yaml.Unmarshaler interface.
func (a *authDriverSection) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var data struct {
		DriverName string `yaml:"driver"`
	}
	err := unmarshal(&data)
	if err != nil {
		return err
	}
	a.Driver, err = drivers.NewAuthDriver(data.DriverName)
	if err != nil {
		return err
	}
	return a.Driver.ReadConfig(unmarshal)
}

//This is a separate type because of its UnmarshalYAML implementation.
type storageDriverSection struct {
	Driver drivers.StorageDriver
}

//UnmarshalYAML implements the yaml.Unmarshaler interface.
func (s *storageDriverSection) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var data struct {
		DriverName string `yaml:"driver"`
	}
	err := unmarshal(&data)
	if err != nil {
		return err
	}
	s.Driver, err = drivers.NewStorageDriver(data.DriverName)
	if err != nil {
		return err
	}
	return s.Driver.ReadConfig(unmarshal)
}

//ReadConfig parses the given configuration file and fills the Config package
//variable.
func ReadConfig(path string) {
	//read config file
	configBytes, err := ioutil.ReadFile(path)
	if err != nil {
		logg.Fatal("read configuration file: %s", err.Error())
	}
	var cfg configuration
	err = yaml.Unmarshal(configBytes, &cfg)
	if err != nil {
		logg.Fatal("parse configuration: %s", err.Error())
	}

	//apply default values
	if cfg.API.ListenAddress == "" {
		cfg.API.ListenAddress = ":8080"
	}
	if cfg.API.PublicURL == "" {
		logg.Fatal("missing api.public_url")
	}
	if cfg.DB.URL == "" {
		logg.Fatal("missing db.url")
	}

	//compile into State
	publicURL, err := url.Parse(cfg.API.PublicURL)
	if err != nil {
		logg.Fatal("malformed api.public_url: %s", err.Error())
	}
	dbURL, err := url.Parse(cfg.DB.URL)
	if err != nil {
		logg.Fatal("malformed db.url: %s", err.Error())
	}
	db, err := database.Init(dbURL)
	if err != nil {
		logg.Fatal(err.Error())
	}

	err = cfg.Auth.Driver.Connect()
	if err != nil {
		logg.Fatal(err.Error())
	}

	State = &StateStruct{
		Config: Configuration{
			APIListenAddress: cfg.API.ListenAddress,
			APIPublicURL:     *publicURL,
			DatabaseURL:      *dbURL,
		},
		DB:               db,
		AuthDriver:       cfg.Auth.Driver,
		StorageDriver:    cfg.Storage.Driver,
		JWTIssuerKey:     getIssuerKey(cfg.Trust.IssuerKeyIn),
		JWTIssuerCertPEM: getIssuerCertPEM(cfg.Trust.IssuerCertIn),
	}
}

var (
	looksLikePEMRx    = regexp.MustCompile(`^\s*-----\s*BEGIN`)
	certificatePEMRx  = regexp.MustCompile(`^-----\s*BEGIN\s+CERTIFICATE\s*-----(?:\n|[a-zA-Z0-9+/=])*-----\s*END\s+CERTIFICATE\s*-----$`)
	stripWhitespaceRx = regexp.MustCompile(`(?m)^\s*|\s*$`)
)

func getIssuerKey(in string) libtrust.PrivateKey {
	if in == "" {
		logg.Fatal("missing trust.issuer_key")
	}

	//if it looks like PEM, it's probably PEM; otherwise it's a filename
	var buf []byte
	if looksLikePEMRx.MatchString(in) {
		buf = []byte(in)
	} else {
		var err error
		buf, err = ioutil.ReadFile(in)
		if err != nil {
			logg.Fatal(err.Error())
		}
	}
	buf = stripWhitespaceRx.ReplaceAll(buf, nil)

	key, err := libtrust.UnmarshalPrivateKeyPEM(buf)
	if err != nil {
		logg.Fatal("failed to read trust.issuer_key: " + err.Error())
	}
	return key
}

func getIssuerCertPEM(in string) string {
	if in == "" {
		logg.Fatal("missing trust.issuer_cert")
	}

	//if it looks like PEM, it's probably PEM; otherwise it's a filename
	if !looksLikePEMRx.MatchString(in) {
		buf, err := ioutil.ReadFile(in)
		if err != nil {
			logg.Fatal(err.Error())
		}
		in = string(buf)
	}
	in = stripWhitespaceRx.ReplaceAllString(in, "")

	if !certificatePEMRx.MatchString(in) {
		logg.Fatal("trust.issuer_cert does not look like a PEM-encoded X509 certificate: does not match regexp /%s/", certificatePEMRx.String())
	}
	return in
}
