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
	"net/http"
	"net/url"
	"regexp"

	"github.com/docker/libtrust"
	"github.com/gophercloud/gophercloud"
	"github.com/gophercloud/gophercloud/openstack"
	"github.com/sapcc/go-bits/logg"
	"github.com/sapcc/keppel/pkg/database"
	os "github.com/sapcc/keppel/pkg/openstack"
	yaml "gopkg.in/yaml.v2"
)

//State is the master singleton containing all globally shared handles and
//configuration values. It is filled by func ReadConfig().
var State *StateStruct

//StateStruct is the type of `var State`.
type StateStruct struct {
	Config           Configuration
	DB               *database.DB
	ServiceUser      *os.ServiceUser
	JWTIssuerKey     libtrust.PrivateKey
	JWTIssuerCertPEM string
}

//Configuration contains some configuration values that are not compiled during
//ReadConfig().
type Configuration struct {
	APIListenAddress string
	APIPublicURL     url.URL
	DatabaseURL      url.URL
	OpenStack        OpenStackConfiguration //TODO ugly; refactor to get rid of this
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

//OpenStackConfiguration is a part of type Configuration.
type OpenStackConfiguration struct {
	Auth struct {
		AuthURL           string `yaml:"auth_url"`
		UserName          string `yaml:"user_name"`
		UserDomainName    string `yaml:"user_domain_name"`
		ProjectName       string `yaml:"project_name"`
		ProjectDomainName string `yaml:"project_domain_name"`
		Password          string `yaml:"password"`
	} `yaml:"auth"`
	LocalRoleName  string `yaml:"local_role"`
	PolicyFilePath string `yaml:"policy_path"`
	//TODO remove when https://github.com/gophercloud/gophercloud/issues/1141 is accepted
	UserID string `yaml:"user_id"`
}

type configuration struct {
	API struct {
		ListenAddress string `yaml:"listen_address"`
		PublicURL     string `yaml:"public_url"`
	} `yaml:"api"`
	DB struct {
		URL string `yaml:"url"`
	} `yaml:"db"`
	OpenStack OpenStackConfiguration `yaml:"openstack"`
	Trust     struct {
		IssuerKeyIn  string `yaml:"issuer_key"`
		IssuerCertIn string `yaml:"issuer_cert"`
	} `yaml:"trust"`
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

	State = &StateStruct{
		Config: Configuration{
			APIListenAddress: cfg.API.ListenAddress,
			APIPublicURL:     *publicURL,
			DatabaseURL:      *dbURL,
			OpenStack:        cfg.OpenStack,
		},
		DB:               db,
		ServiceUser:      initServiceUser(&cfg),
		JWTIssuerKey:     getIssuerKey(cfg.Trust.IssuerKeyIn),
		JWTIssuerCertPEM: getIssuerCertPEM(cfg.Trust.IssuerCertIn),
	}
}

func initServiceUser(cfg *configuration) *os.ServiceUser {
	c := cfg.OpenStack
	if c.Auth.AuthURL == "" {
		logg.Fatal("missing openstack.auth.auth_url")
	}
	if c.Auth.UserName == "" {
		logg.Fatal("missing openstack.auth.user_name")
	}
	if c.Auth.UserDomainName == "" {
		logg.Fatal("missing openstack.auth.user_domain_name")
	}
	if c.Auth.Password == "" {
		logg.Fatal("missing openstack.auth.password")
	}
	if c.Auth.ProjectName == "" {
		logg.Fatal("missing openstack.auth.project_name")
	}
	if c.Auth.ProjectDomainName == "" {
		logg.Fatal("missing openstack.auth.project_domain_name")
	}
	if c.LocalRoleName == "" {
		logg.Fatal("missing openstack.local_role")
	}
	if c.PolicyFilePath == "" {
		logg.Fatal("missing openstack.policy_path")
	}
	//TODO remove when https://github.com/gophercloud/gophercloud/issues/1141 is accepted
	if c.UserID == "" {
		logg.Fatal("missing openstack.user_id")
	}

	var err error
	provider, err := openstack.NewClient(c.Auth.AuthURL)
	if err != nil {
		logg.Fatal("cannot initialize OpenStack client: %v", err)
	}

	//use http.DefaultClient, esp. to pick up the KEPPEL_INSECURE flag
	provider.HTTPClient = *http.DefaultClient

	err = openstack.Authenticate(provider, gophercloud.AuthOptions{
		IdentityEndpoint: c.Auth.AuthURL,
		AllowReauth:      true,
		Username:         c.Auth.UserName,
		DomainName:       c.Auth.UserDomainName,
		Password:         c.Auth.Password,
		Scope: &gophercloud.AuthScope{
			ProjectName: c.Auth.ProjectName,
			DomainName:  c.Auth.ProjectDomainName,
		},
	})
	if err != nil {
		logg.Fatal("cannot fetch initial Keystone token: %v", err)
	}

	serviceUser, err := os.NewServiceUser(
		provider, c.UserID, c.LocalRoleName, c.PolicyFilePath)
	if err != nil {
		logg.Fatal(err.Error())
	}
	return serviceUser
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
