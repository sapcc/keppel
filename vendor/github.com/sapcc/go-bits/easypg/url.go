// SPDX-FileCopyrightText: 2022 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package easypg

import (
	"fmt"
	"net"
	"net/url"
	"os"

	"github.com/sapcc/go-api-declarations/bininfo"
)

// URLParts contains the arguments for func URLFrom(), see documentation over
// there. JSON Tags are to prevent linting errors - this is never serialized.
type URLParts struct {
	HostName               string            `json:"-"` // required
	Port                   string            `json:"-"` // optional (default value = 5432 for postgres:// scheme)
	UserName               string            `json:"-"` // required
	Password               string            `json:"-"` // optional
	ConnectionOptions      string            `json:"-"` // optional (usually used for options coming in via config)
	ExtraConnectionOptions map[string]string `json:"-"` // optional (usually used for options coming in via code)
	DatabaseName           string            `json:"-"` // required
}

// This will be modified during unit tests to replace os.Hostname() with a test double.
var osHostname = os.Hostname

// URLFrom constructs a libpq connection URL from the provided parts. The parts
// are typically retrieved from environment variables, for example:
//
//	dbURL := must.Return(easypg.URLFrom(easypg.URLParts {
//		HostName:          osext.GetenvOrDefault("FOOBAR_DB_HOSTNAME", "localhost"),
//		Port:              osext.GetenvOrDefault("FOOBAR_DB_PORT", "5432"),
//		UserName:          osext.GetenvOrDefault("FOOBAR_DB_USERNAME", "postgres"),
//		Password:          os.Getenv("FOOBAR_DB_PASSWORD"),
//		ConnectionOptions: os.Getenv("FOOBAR_DB_CONNECTION_OPTIONS"),
//		DatabaseName:      osext.GetenvOrDefault("FOOBAR_DB_NAME", "foobar"),
//	}))
//	db := must.Return(easypg.Connect(dbURL, easypg.Configuration{ ... }))
//
// We provide URLFrom() as a separate function, instead of just putting the
// fields of URLParts into the Configuration struct, to accommodate applications
// that may want to accept a fully-formed postgres:// URL from outside instead
// of building it up from individual parts.
func URLFrom(parts URLParts) (url.URL, error) {
	connOpts, err := url.ParseQuery(parts.ConnectionOptions)
	if err != nil {
		return url.URL{}, fmt.Errorf("cannot parse DB connection options (%q): %w", parts.ConnectionOptions, err)
	}

	hostname, err := osHostname()
	if err == nil {
		connOpts.Set("application_name", fmt.Sprintf("%s@%s", bininfo.Component(), hostname))
	} else {
		connOpts.Set("application_name", bininfo.Component())
	}

	for k, v := range parts.ExtraConnectionOptions {
		connOpts.Set(k, v)
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

	return result, nil
}
