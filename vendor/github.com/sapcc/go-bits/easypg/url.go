/*******************************************************************************
*
* Copyright 2022 SAP SE
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

package easypg

import (
	"fmt"
	"net"
	"net/url"
	"os"

	"github.com/sapcc/go-api-declarations/bininfo"
)

// URLParts contains the arguments for func URLFrom(), see documentation over
// there.
type URLParts struct {
	HostName          string //required
	Port              string //optional (default value = 5432 for postgres:// scheme)
	UserName          string //required
	Password          string //optional
	ConnectionOptions string //optional
	DatabaseName      string //required
}

// This will be modified during unit tests to replace os.Hostname() with a test double.
var osHostname = os.Hostname

// URLFrom constructs a libpq connection URL from the provided parts. The parts
// are typically retrieved from environment variables, for example:
//
//	cfg.PostgresURL = easypg.URLFrom(easypg.URLParts {
//		HostName:          osext.GetenvOrDefault("FOOBAR_DB_HOSTNAME", "localhost"),
//		Port:              osext.GetenvOrDefault("FOOBAR_DB_PORT", "5432"),
//		UserName:          osext.GetenvOrDefault("FOOBAR_DB_USERNAME", "postgres"),
//		Password:          os.Getenv("FOOBAR_DB_PASSWORD"),
//		ConnectionOptions: os.Getenv("FOOBAR_DB_CONNECTION_OPTIONS"),
//		DatabaseName:      osext.GetenvOrDefault("FOOBAR_DB_NAME", "foobar"),
//	})
//
// We provide URLFrom() as a separate function, instead of just putting the
// fields of URLParts into the Configuration struct, to accommodate applications
// that may want to accept a fully-formed postgres:// URL from outside instead
// of building it up from individual parts.
func URLFrom(parts URLParts) (*url.URL, error) {
	connOpts, err := url.ParseQuery(parts.ConnectionOptions)
	if err != nil {
		return nil, fmt.Errorf("cannot parse DB connection options (%q): %w", parts.ConnectionOptions, err)
	}
	hostname, err := osHostname()
	if err == nil {
		connOpts.Set("application_name", fmt.Sprintf("%s@%s", bininfo.Component(), hostname))
	} else {
		connOpts.Set("application_name", bininfo.Component())
	}

	result := url.URL{
		Scheme:   "postgres",
		Host:     parts.HostName,
		Path:     parts.DatabaseName,
		RawQuery: connOpts.Encode(),
	}

	if parts.Password == "" {
		result.User = url.User(parts.UserName)
	} else {
		result.User = url.UserPassword(parts.UserName, parts.Password)
	}
	if parts.Port != "" {
		result.Host = net.JoinHostPort(parts.HostName, parts.Port)
	}

	return &result, nil
}
