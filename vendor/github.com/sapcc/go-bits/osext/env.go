// SPDX-FileCopyrightText: 2022 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package osext

import (
	"fmt"
	"os"
	"strconv"

	"github.com/sapcc/go-bits/logg"
)

// NeedGetenv returns os.Getenv(key), or panics if the environment variable is
// not set.
func MustGetenv(key string) string {
	val, err := NeedGetenv(key)
	if err != nil {
		logg.Fatal(err.Error())
	}
	return val
}

// NeedGetenv returns os.Getenv(key), or an error if the environment variable is
// not set.
func NeedGetenv(key string) (string, error) {
	val := os.Getenv(key)
	if val == "" {
		return "", MissingEnvError{key}
	}
	return val, nil
}

// GetenvOrDefault returns os.Getenv(key), except that if the environment
// variable is not set, the given default value will be returned instead.
func GetenvOrDefault(key, defaultValue string) string {
	val := os.Getenv(key)
	if val == "" {
		return defaultValue
	}
	return val
}

// GetenvBool returns true if and only if the environment variable with the
// given key exists and contains a string that strconv.ParseBool() recognizes as
// true. Non-existent or malformed values will be coerced into "false".
//
// This method is commonly used for optional behavior flags, e.g. to activate
// debug logging.
func GetenvBool(key string) bool {
	val, err := strconv.ParseBool(os.Getenv(key))
	return val && err == nil
}

// MissingEnvError is an error that occurs when an required environment variable was not present.
type MissingEnvError struct {
	Key string
}

// Error implements the builtin/error interface.
func (e MissingEnvError) Error() string {
	return fmt.Sprintf("environment variable %q is not set", e.Key)
}
