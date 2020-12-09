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
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/docker/libtrust"
	"github.com/sapcc/go-bits/logg"
)

//Configuration contains all configuration values that are not specific to a
//certain driver.
type Configuration struct {
	APIPublicURL        url.URL
	AnycastAPIPublicURL *url.URL
	DatabaseURL         url.URL
	JWTIssuerKey        libtrust.PrivateKey
	AnycastJWTIssuerKey *libtrust.PrivateKey
}

//IsAnycastRequest returns true if this configuration has anycast enabled and
//the given request is for the anycast API.
func (c Configuration) IsAnycastRequest(r *http.Request) bool {
	if c.AnycastAPIPublicURL == nil {
		return false
	}

	//case 1: anycast request explicitly reverse-proxied to us from the
	//keppel-api that originally received it
	forwardedBy := r.Header.Get("X-Keppel-Forwarded-By")
	if forwardedBy != "" {
		return true
	}

	//case 2: anycast request originating from the user
	u1 := OriginalRequestURL(r)
	u2 := *c.AnycastAPIPublicURL
	return u1.Scheme == u2.Scheme && u1.Host == u2.Host
}

var (
	looksLikePEMRx    = regexp.MustCompile(`^\s*-----\s*BEGIN`)
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

	return libtrust.UnmarshalPrivateKeyPEM(buf)
}

//ParseConfiguration obtains a keppel.Configuration instance from the
//corresponding environment variables. Aborts on error.
func ParseConfiguration() Configuration {
	cfg := Configuration{
		APIPublicURL:        mustGetenvURL("KEPPEL_API_PUBLIC_URL"),
		AnycastAPIPublicURL: mayGetenvURL("KEPPEL_API_ANYCAST_URL"),
		DatabaseURL:         getDbURL(),
	}

	var err error
	cfg.JWTIssuerKey, err = ParseIssuerKey(MustGetenv("KEPPEL_ISSUER_KEY"))
	if err != nil {
		logg.Fatal("failed to read KEPPEL_ISSUER_KEY: " + err.Error())
	}

	if cfg.AnycastAPIPublicURL != nil {
		key, err := ParseIssuerKey(MustGetenv("KEPPEL_ANYCAST_ISSUER_KEY"))
		if err != nil {
			logg.Fatal("failed to read KEPPEL_ANYCAST_ISSUER_KEY: " + err.Error())
		}
		cfg.AnycastJWTIssuerKey = &key
	}

	return cfg
}

// ParseBool is like strconv.ParseBool() but doesn't return any error.
func ParseBool(str string) bool {
	v, _ := strconv.ParseBool(str)
	return v
}

//MustGetenv is like os.Getenv, but aborts with an error message if the given
//environment variable is missing or empty.
func MustGetenv(key string) string {
	val := os.Getenv(key)
	if val == "" {
		logg.Fatal("missing environment variable: %s", key)
	}
	return val
}

func mustGetenvURL(key string) url.URL {
	val := MustGetenv(key)
	parsed, err := url.Parse(val)
	if err != nil {
		logg.Fatal("malformed %s: %s", key, err.Error())
	}
	return *parsed
}

func mayGetenvURL(key string) *url.URL {
	val := os.Getenv(key)
	if val == "" {
		return nil
	}
	parsed, err := url.Parse(val)
	if err != nil {
		logg.Fatal("malformed %s: %s", key, err.Error())
	}
	return parsed
}

//GetenvOrDefault is like os.Getenv but it also takes a default value which is
//returned if the given environment variable is missing or empty.
func GetenvOrDefault(key, defaultVal string) string {
	val := os.Getenv(key)
	if val == "" {
		val = defaultVal
	}
	return val
}

func getDbURL() url.URL {
	dbName := GetenvOrDefault("KEPPEL_DB_NAME", "keppel")
	dbUsername := GetenvOrDefault("KEPPEL_DB_USERNAME", "postgres")
	dbPass := os.Getenv("KEPPEL_DB_PASSWORD")
	dbHost := GetenvOrDefault("KEPPEL_DB_HOSTNAME", "localhost")
	dbPort := GetenvOrDefault("KEPPEL_DB_PORT", "5432")
	dbConnOpts := os.Getenv("KEPPEL_DB_CONNECTION_OPTIONS")

	return url.URL{
		Scheme:   "postgres",
		User:     url.UserPassword(dbUsername, dbPass),
		Host:     net.JoinHostPort(dbHost, dbPort),
		Path:     dbName,
		RawQuery: dbConnOpts,
	}
}

// GetRedisURL returns a Redis connection URL by getting the required
// parameters from environment variables:
//   REDIS_PASSWORD, REDIS_HOSTNAME, REDIS_PORT, and REDIS_DB_NUM.
//
// The environment variable keys are prefixed with the provided prefix.
func GetRedisURL(prefix string) string {
	prefix = strings.Join([]string{prefix, "REDIS"}, "_")
	pass := os.Getenv(prefix + "_PASSWORD")
	host := GetenvOrDefault(prefix+"_HOSTNAME", "localhost")
	port := GetenvOrDefault(prefix+"_PORT", "6379")
	dbNum := GetenvOrDefault(prefix+"_DB_NUM", "0")

	url := url.URL{
		Scheme: "redis",
		User:   url.UserPassword("", pass),
		Host:   net.JoinHostPort(host, port),
		Path:   dbNum,
	}
	return url.String()
}
