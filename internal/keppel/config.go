// SPDX-FileCopyrightText: 2018 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package keppel

import (
	"bytes"
	"crypto"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/golang-jwt/jwt/v5"
	"github.com/redis/go-redis/v9"
	"github.com/sapcc/go-api-declarations/bininfo"
	"github.com/sapcc/go-bits/easypg"
	"github.com/sapcc/go-bits/logg"
	"github.com/sapcc/go-bits/must"
	"github.com/sapcc/go-bits/osext"
	"github.com/sapcc/go-bits/pluggable"

	"github.com/sapcc/keppel/internal/trivy"
)

// Configuration contains all configuration values that are not specific to a
// certain driver.
type Configuration struct {
	APIPublicHostname        string
	AnycastAPIPublicHostname string
	JWTIssuerKeys            []crypto.PrivateKey
	AnycastJWTIssuerKeys     []crypto.PrivateKey
	Trivy                    *trivy.Config
}

var (
	looksLikePEMRx    = regexp.MustCompile(`^\s*-----\s*BEGIN`)
	stripWhitespaceRx = regexp.MustCompile(`(?m)^\s*|\s*$`)
)

// ParseIssuerKey parses the contents of the KEPPEL_ISSUER_KEY variable.
func ParseIssuerKey(in string) (crypto.PrivateKey, error) {
	// if it looks like PEM, it's probably PEM; otherwise it's a filename
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

	// we support either ed25519 keys (preferred) or RSA keys (legacy), and we
	// decide which one we have based on which parsing attempt does not fail
	//
	// TODO remove RSA support after all production instances have been migrated
	// to ed25519
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

// GetDatabaseURLFromEnvironment reads the KEPPEL_DB_* environment variables.
func GetDatabaseURLFromEnvironment() (dbURL url.URL, dbName string) {
	dbName = osext.GetenvOrDefault("KEPPEL_DB_NAME", "keppel")
	return must.Return(easypg.URLFrom(easypg.URLParts{
		HostName:          osext.GetenvOrDefault("KEPPEL_DB_HOSTNAME", "localhost"),
		Port:              osext.GetenvOrDefault("KEPPEL_DB_PORT", "5432"),
		UserName:          osext.GetenvOrDefault("KEPPEL_DB_USERNAME", "postgres"),
		Password:          os.Getenv("KEPPEL_DB_PASSWORD"),
		ConnectionOptions: os.Getenv("KEPPEL_DB_CONNECTION_OPTIONS"),
		DatabaseName:      dbName,
	})), dbName
}

// ParseConfiguration obtains a keppel.Configuration instance from the
// corresponding environment variables. Aborts on error.
func ParseConfiguration() Configuration {
	logg.Debug("parsing configuration...")

	cfg := Configuration{
		APIPublicHostname:        osext.MustGetenv("KEPPEL_API_PUBLIC_FQDN"),
		AnycastAPIPublicHostname: os.Getenv("KEPPEL_API_ANYCAST_FQDN"),
	}

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

	trivyURL := mayGetenvURL("KEPPEL_TRIVY_URL")
	if trivyURL != nil {
		additionalPullableRepos := strings.Split(os.Getenv("KEPPEL_TRIVY_ADDITIONAL_PULLABLE_REPOS"), ",")
		cfg.Trivy = &trivy.Config{
			AdditionalPullableRepos: additionalPullableRepos,
			Token:                   osext.MustGetenv("KEPPEL_TRIVY_TOKEN"),
			URL:                     *trivyURL,
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
	pass := os.Getenv(prefix + "_PASSWORD")
	host := osext.GetenvOrDefault(prefix+"_HOSTNAME", "localhost")
	port := osext.GetenvOrDefault(prefix+"_PORT", "6379")
	dbNum := osext.GetenvOrDefault(prefix+"_DB_NUM", "0")
	db, err := strconv.Atoi(dbNum)
	if err != nil {
		return nil, fmt.Errorf("invalid value for %s: %q", prefix+"_DB_NUM", dbNum)
	}

	return &redis.Options{
		Network:    "tcp",
		Password:   pass,
		Addr:       net.JoinHostPort(host, port),
		ClientName: bininfo.Component(),
		DB:         db,
	}, nil
}

// newDriver parses a config JSON as found in a KEPPEL_DRIVER_* variable,
// initializes the respective driver, and unmarshals config parameters into it.
//
// This is the reusable part of the implementations for NewAuthDriver, NewStorageDriver etc.
func newDriver[P pluggable.Plugin](driverType string, registry pluggable.Registry[P], configJSON string, init func(P) error) (P, error) {
	var zero P // for error returns

	var cfg struct {
		PluginTypeID string          `json:"type"`
		Params       json.RawMessage `json:"params"`
	}
	err := UnmarshalJSONStrict([]byte(configJSON), &cfg)
	if err != nil {
		return zero, fmt.Errorf("cannot unmarshal %s config %q: %w", driverType, configJSON, err)
	}
	if len(cfg.Params) == 0 {
		// configJSON was just a type, e.g. `{"type":"unittest"}`
		cfg.Params = json.RawMessage("{}")
	}
	logg.Debug("initializing %s %q", driverType, configJSON)

	driver, ok := registry.TryInstantiate(cfg.PluginTypeID).Unpack()
	if !ok {
		return zero, fmt.Errorf("no such %s: %q", driverType, cfg.PluginTypeID)
	}
	err = json.Unmarshal([]byte(cfg.Params), driver)
	if err != nil {
		return zero, fmt.Errorf("cannot unmarshal params for %s %q: %w", driverType, cfg.PluginTypeID, err)
	}
	err = init(driver)
	if err != nil {
		return zero, fmt.Errorf("could not initialize %s %q: %w", driverType, cfg.PluginTypeID, err)
	}
	return driver, nil
}

// Like yaml.UnmarshalStrict(), but for JSON.
func UnmarshalJSONStrict(buf []byte, target any) error {
	dec := json.NewDecoder(bytes.NewReader(buf))
	dec.DisallowUnknownFields()
	return dec.Decode(target)
}
