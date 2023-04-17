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
	"crypto"
	"encoding/base64"
	"fmt"
	"net"
	"net/url"
	"os"
	"regexp"
	"strconv"

	"github.com/golang-jwt/jwt/v4"
	"github.com/redis/go-redis/v9"
	"github.com/sapcc/go-bits/easypg"
	"github.com/sapcc/go-bits/logg"
	"github.com/sapcc/go-bits/must"
	"github.com/sapcc/go-bits/osext"

	"github.com/sapcc/keppel/internal/clair"
)

// Configuration contains all configuration values that are not specific to a
// certain driver.
type Configuration struct {
	APIPublicHostname        string
	AnycastAPIPublicHostname string
	DatabaseURL              *url.URL
	JWTIssuerKeys            []crypto.PrivateKey
	AnycastJWTIssuerKeys     []crypto.PrivateKey
	ClairClient              *clair.Client
}

var (
	looksLikePEMRx    = regexp.MustCompile(`^\s*-----\s*BEGIN`)
	stripWhitespaceRx = regexp.MustCompile(`(?m)^\s*|\s*$`)
)

// ParseIssuerKey parses the contents of the KEPPEL_ISSUER_KEY variable.
func ParseIssuerKey(in string) (crypto.PrivateKey, error) {
	//if it looks like PEM, it's probably PEM; otherwise it's a filename
	var buf []byte
	if looksLikePEMRx.MatchString(in) {
		buf = []byte(in)
	} else {
		var err error
		buf, err = os.ReadFile(in)
		if err != nil {
			return nil, err
		}
	}
	buf = stripWhitespaceRx.ReplaceAll(buf, nil)

	//we support either ed25519 keys (preferred) or RSA keys (legacy), and we
	//decide which one we have based on which parsing attempt does not fail
	//
	//TODO remove RSA support after all production instances have been migrated
	//to ed25519
	ed25519Key, err1 := jwt.ParseEdPrivateKeyFromPEM(buf)
	if err1 == nil {
		return ed25519Key, nil
	}
	rsaKey, err2 := jwt.ParseRSAPrivateKeyFromPEM(buf)
	if err2 == nil {
		return rsaKey, nil
	}
	return nil, fmt.Errorf("neither an ed25519 private key (%q) nor an RSA private key (%q)", err1.Error(), err2.Error())
}

// ParseConfiguration obtains a keppel.Configuration instance from the
// corresponding environment variables. Aborts on error.
func ParseConfiguration() Configuration {
	cfg := Configuration{
		APIPublicHostname:        osext.MustGetenv("KEPPEL_API_PUBLIC_FQDN"),
		AnycastAPIPublicHostname: os.Getenv("KEPPEL_API_ANYCAST_FQDN"),
	}
	cfg.DatabaseURL = must.Return(easypg.URLFrom(easypg.URLParts{
		HostName:          osext.GetenvOrDefault("KEPPEL_DB_HOSTNAME", "localhost"),
		Port:              osext.GetenvOrDefault("KEPPEL_DB_PORT", "5432"),
		UserName:          osext.GetenvOrDefault("KEPPEL_DB_USERNAME", "postgres"),
		Password:          os.Getenv("KEPPEL_DB_PASSWORD"),
		ConnectionOptions: os.Getenv("KEPPEL_DB_CONNECTION_OPTIONS"),
		DatabaseName:      osext.GetenvOrDefault("KEPPEL_DB_NAME", "keppel"),
	}))

	parseIssuerKeys := func(prefix string) []crypto.PrivateKey {
		key, err := ParseIssuerKey(osext.MustGetenv(prefix + "_ISSUER_KEY"))
		if err != nil {
			logg.Fatal("failed to read %s_ISSUER_KEY: %s", prefix, err.Error())
		}
		prevKeyStr := os.Getenv(prefix + "_PREVIOUS_ISSUER_KEY")
		if prevKeyStr == "" {
			return []crypto.PrivateKey{key}
		}
		prevKey, err := ParseIssuerKey(prevKeyStr)
		if err != nil {
			logg.Fatal("failed to read %s_PREVIOUS_ISSUER_KEY: %s", prefix, err.Error())
		}
		return []crypto.PrivateKey{key, prevKey}
	}

	cfg.JWTIssuerKeys = parseIssuerKeys("KEPPEL")
	if cfg.AnycastAPIPublicHostname != "" {
		cfg.AnycastJWTIssuerKeys = parseIssuerKeys("KEPPEL_ANYCAST")
	}

	clairURL := mayGetenvURL("KEPPEL_CLAIR_URL")
	if clairURL != nil {
		//Clair does a base64 decode of the key given in its configuration; I find
		//this quite unnecessary and surprising, but in order to not cause any
		//additional confusion, we do the same thing
		key, err := base64.StdEncoding.DecodeString(osext.MustGetenv("KEPPEL_CLAIR_PRESHARED_KEY"))
		if err != nil {
			logg.Fatal("failed to read KEPPEL_CLAIR_PRESHARED_KEY: " + err.Error())
		}
		cfg.ClairClient = &clair.Client{
			BaseURL:            *clairURL,
			PresharedKey:       key,
			NotificationSecret: osext.MustGetenv("KEPPEL_CLAIR_NOTIFICATION_SECRET"),
		}
	}

	return cfg
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

// GetRedisOptions returns a redis.Options by getting the required parameters
// from environment variables:
//
//	REDIS_PASSWORD, REDIS_HOSTNAME, REDIS_PORT, and REDIS_DB_NUM.
//
// The environment variable keys are prefixed with the provided prefix.
func GetRedisOptions(prefix string) (*redis.Options, error) {
	prefix += "_REDIS"
	pass := os.Getenv(prefix + "_PASSWORD")
	host := osext.GetenvOrDefault(prefix+"_HOSTNAME", "localhost")
	port := osext.GetenvOrDefault(prefix+"_PORT", "6379")
	dbNum := osext.GetenvOrDefault(prefix+"_DB_NUM", "0")
	db, err := strconv.Atoi(dbNum)
	if err != nil {
		return nil, fmt.Errorf("invalid value for %s: %q", prefix+"_DB_NUM", dbNum)
	}

	return &redis.Options{
		Network:  "tcp",
		Password: pass,
		Addr:     net.JoinHostPort(host, port),
		DB:       db,
	}, nil
}
