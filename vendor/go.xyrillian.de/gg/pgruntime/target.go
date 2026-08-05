// SPDX-FileCopyrightText: 2026 Stefan Majewsky <majewsky@gmx.net>
// SPDX-License-Identifier: Apache-2.0

package pgruntime

import (
	"fmt"
	"maps"
	"net"
	"net/url"
	"os"
	"strings"
)

// ConnectionTarget describes the location of and credentials and other connection options for the database server that [Connector.Connect] connects to.
//
//   - The HostName, UserName and DatabaseName fields are required. All other fields are optional.
//   - The ConnectionOptions field is intended for options coming in via config.
//     Its value must be formatted in the syntax accepted by [url.ParseQuery].
//   - The ExtraConnectionOptions field is intended for options coming in via code.
//   - Both ConnectionOptions fields accept all query parameters that are allowed in [libpq-style connection URIs].
//   - ApplicationName is an alternative way of setting ExtraConnectionOptions["fallback_application_name"].
//     If filled, pgruntime will also try to automatically append the hostname if available.
//     An explicit "application_name" setting in either ConnectionOptions or ExtraConnectionOptions takes precedence over this setting.
//
// This type is intended to be retrieved from environment variables, for example:
//
//	ct := pgruntime.ConnectionTarget{
//		HostName:          cmp.Or(os.Getenv("FOOBAR_DB_HOSTNAME"), "localhost"),
//		Port:              cmp.Or(os.Getenv("FOOBAR_DB_PORT"), "5432"),
//		UserName:          cmp.Or(os.Getenv("FOOBAR_DB_USERNAME"), "postgres"),
//		Password:          os.Getenv("FOOBAR_DB_PASSWORD"),
//		ConnectionOptions: os.Getenv("FOOBAR_DB_CONNECTION_OPTIONS"),
//		DatabaseName:      cmp.Or(os.Getenv("FOOBAR_DB_NAME"), "foobar"),
//	}))
//
// [libpq-style connection URIs]: https://www.postgresql.org/docs/current/libpq-connect.html#LIBPQ-CONNSTRING-URIS
type ConnectionTarget struct {
	HostName               string
	Port                   string
	UserName               string
	Password               string // default: <no password>
	DatabaseName           string
	ConnectionOptions      string
	ExtraConnectionOptions url.Values
	ApplicationName        string
}

const (
	connectionURISchemeShort = "postgres"
	connectionURISchemeLong  = "postgresql"
)

// ParseConnectionTargetFromURL parses a [libpq-style connection URI] into a [ConnectionTarget] instance.
// If there are connection options, they will be placed in the ConnectionOptions field, not in ExtraConnectionOptions or ApplicationName.
//
// The special libpq syntax with multiple host:port pairs is not supported by this function and will result in an error.
//
// [libpq-style connection URI]: https://www.postgresql.org/docs/current/libpq-connect.html#LIBPQ-CONNSTRING-URIS
func ParseConnectionTargetFromURL(u *url.URL) (ConnectionTarget, error) {
	var (
		result ConnectionTarget
		none   ConnectionTarget // shorthand for error return paths
	)

	if u.Scheme != connectionURISchemeShort && u.Scheme != connectionURISchemeLong {
		return none, fmt.Errorf(`in ParseConnectionTargetFromURL: expected scheme %q or %q, but got %q`, connectionURISchemeShort, connectionURISchemeLong, u.Scheme)
	}
	if u.Opaque != "" {
		return none, fmt.Errorf(`in ParseConnectionTargetFromURL: expected authority component, but got rootless path %q`, u.Opaque)
	}
	if u.Fragment != "" {
		return none, fmt.Errorf(`in ParseConnectionTargetFromURL: unexpected fragment %q`, u.Fragment)
	}

	var err error
	if strings.Contains(u.Host, ":") {
		result.HostName, result.Port, err = net.SplitHostPort(u.Host)
		if err != nil {
			return none, fmt.Errorf(`in ParseConnectionTargetFromURL: malformed host %q`, u.Host)
		}
	} else {
		result.HostName = u.Host
	}
	result.UserName = u.User.Username()
	result.Password, _ = u.User.Password()

	result.DatabaseName = strings.TrimPrefix(u.Path, "/")
	if strings.Contains(result.DatabaseName, "/") {
		return none, fmt.Errorf(`in ParseConnectionTargetFromURL: malformed database name %q`, result.DatabaseName)
	}

	if u.RawQuery != "" {
		_, err := url.ParseQuery(u.RawQuery)
		if err != nil {
			return none, fmt.Errorf(`in ParseConnectionTargetFromURL: malformed connection options: %w`, err)
		}
		result.ConnectionOptions = u.RawQuery
	}

	return result, nil
}

// dependency injection slot to insert a double during tests
var osHostname = os.Hostname

// IntoURL formats the ConnectionTarget as a libpq-style connection URI.
func (t ConnectionTarget) IntoURL() (*url.URL, error) {
	appName := t.ApplicationName
	if appName != "" {
		hostname, err := osHostname()
		if err == nil {
			appName += "@" + hostname
		}
	}

	rawQuery, err := mergeConnectionOptions(t.ConnectionOptions, t.ExtraConnectionOptions, appName)
	if err != nil {
		return nil, fmt.Errorf("in ConnectionTarget.IntoURL: malformed connection options: %w", err)
	}
	return &url.URL{
		Scheme:   connectionURISchemeShort, // could also use `connectionURISchemeLong`, but go-bits/easypg made this choice
		User:     t.buildUserInfo(),
		Host:     t.buildHostPort(),
		Path:     "/" + t.DatabaseName,
		RawQuery: rawQuery,
	}, nil
}

func (t ConnectionTarget) buildUserInfo() *url.Userinfo {
	if t.Password == "" {
		return url.User(t.UserName)
	} else {
		return url.UserPassword(t.UserName, t.Password)
	}
}

func (t ConnectionTarget) buildHostPort() string {
	if t.Port == "" {
		return t.HostName
	} else {
		return net.JoinHostPort(t.HostName, t.Port)
	}
}

func mergeConnectionOptions(opts string, extraOpts url.Values, appName string) (string, error) {
	if len(extraOpts) == 0 && appName == "" {
		return opts, nil
	}
	if opts == "" {
		if appName == "" {
			return extraOpts.Encode(), nil
		} else {
			cloned := maps.Clone(extraOpts)
			if cloned == nil {
				cloned = make(url.Values)
			}
			cloned.Set("fallback_application_name", appName)
			return cloned.Encode(), nil
		}
	}

	values, err := url.ParseQuery(opts)
	if err != nil {
		return "", err
	}
	maps.Copy(values, extraOpts)
	if appName != "" {
		values.Set("fallback_application_name", appName)
	}
	return values.Encode(), nil
}
