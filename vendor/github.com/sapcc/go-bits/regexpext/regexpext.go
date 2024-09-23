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

// Package regexpext provides Regexp wrapper types that automatically compile
// regex strings while they are being unmarshaled from YAML or JSON files. If
// the compilation fails, the error will be reported as part of the Unmarshal
// operation.
package regexpext

// NOTE on the implementation approach.
//
// We cannot make PlainRegexp and BoundedRegexp structs, because that would
// break omitempty serialization [1]. We also cannot do
//
//     type PlainRegexp *regexp.Regexp
//
// because newtypes based on pointer types do not allow method implementations
// (but we need those to implement MarshalJSON etc.). We also cannot do
//
//     type PlainRegexp regexp.Regexp
//
// because the standard Regexp type has all its methods declared on its
// respective pointer type, so these methods would not be passed onto
// our newtypes. The last remaining option is to have our newtypes only store
// the original regex strings:
//
//     type PlainRegexp string
//
// The validity of the regex string is validated during UnmarshalYAML or
// UnmarshalJSON. To avoid the need to compile the same regex string multiple
// times, we are using a small cache inside this package. Realistically, we are
// only going to observe a small number of regex strings (mostly from config
// files), so a small cache should be enough to avoid duplicate regex
// compilations in most cases.
//
// The API is slightly complicated by the fact that we cannot rule out the
// possibility that application code messes with the regex strings inside the
// PlainRegexp and BoundedRegexp instances. Therefore, it is always
// possible that we encounter invalid regex strings, even if we did an
// unmarshal before and found a valid regex string at that point.
//
// Therefore, the Regexp() methods need to return an error. The most common
// regex operations (MatchString and FindStringSubmatch) are provided as
// additional methods with a simplified interface to counter this complication.
//
// [1]: https://github.com/golang/go/issues/11939

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	lru "github.com/hashicorp/golang-lru/v2"
)

// isLiteral returns true if none of the chars in `x` are regexp syntax.
// For example, isLiteral("foo_bar12") is true, but isLiteral("foo*bar") is false.
func isLiteral(x string) bool {
	// the character list comes from `var specialBytes` in src/regexp/regexp.go of std
	return !strings.ContainsAny(x, "\\.+*?()|[]{}^$")
}

// PlainRegexp is a regex string that implements the Marshaler and Unmarshaler
// interfaces for encoding/json and gopkg.in/yaml.v2, respectively. This type
// also works with gopkg.in/yaml.v3: Even though the Unmarshaler interface has
// changed in yaml.v3, v3 also still supports the v2 interface. The
// "PlainRegexp" name is in contrast to type BoundedRegexp, see documentation
// over there.
//
// During unmarshaling, absent string values will behave the same as empty
// string values. In both cases, the Regexp will match every input.
type PlainRegexp string

func (r PlainRegexp) MarshalJSON() ([]byte, error)           { return json.Marshal(string(r)) }
func (r PlainRegexp) MarshalYAML() (any, error)              { return string(r), nil }
func (r *PlainRegexp) UnmarshalJSON(buf []byte) error        { return parseJSON(buf, r.set, false) }
func (r *PlainRegexp) UnmarshalYAML(u func(any) error) error { return parseYAML(u, r.set, false) }
func (r *PlainRegexp) set(s string)                          { *r = PlainRegexp(s) }

// Regexp returns the parsed regexp.Regexp instance for this PlainRegexp.
// An error is returned if the regular expression string inside this
// PlainRegexp is invalid. If the PlainRegexp was unmarshaled from JSON
// or YAML, no error will ever be returned because invalid regex strings were
// already rejected during unmarshalling.
func (r PlainRegexp) Regexp() (*regexp.Regexp, error) {
	return compile(string(r), false)
}

// Shorthand for `r.Regexp()` followed by `rx.MatchString()`. If regex parsing
// returns an error, this function returns false.
func (r PlainRegexp) MatchString(in string) bool {
	// optimization: match literals without regex compilation to reduce pressure on `cache`
	if isLiteral(string(r)) {
		return strings.Contains(in, string(r))
	}

	rx, err := r.Regexp()
	if err != nil {
		return false
	}
	return rx.MatchString(in)
}

// Shorthand for `r.Regexp()` followed by `rx.FindStringSubmatch()`. If regex parsing
// returns an error, this function returns nil.
func (r PlainRegexp) FindStringSubmatch(in string) []string {
	// optimization: match literals without regex compilation to reduce pressure on `cache`
	if isLiteral(string(r)) {
		if strings.Contains(in, string(r)) {
			return []string{string(r)}
		} else {
			return nil
		}
	}

	rx, err := r.Regexp()
	if err != nil {
		return nil
	}
	return rx.FindStringSubmatch(in)
}

// BoundedRegexp is like PlainRegexp, but ^ and $ anchors will automatically be
// added to the start and end of the regexp, respectively. For example, when
// unmarshaling the value "foo|bar" into a BoundedRegexp, the unmarshaled
// regexp will be "^(?:foo|bar)$".
//
// During unmarshaling, absent string values will behave the same as empty
// string values. In both cases, the Regexp will be identical to "^$" and only
// match empty inputs.
type BoundedRegexp string

func (r BoundedRegexp) MarshalJSON() ([]byte, error)           { return json.Marshal(string(r)) }
func (r BoundedRegexp) MarshalYAML() (any, error)              { return string(r), nil }
func (r *BoundedRegexp) UnmarshalJSON(buf []byte) error        { return parseJSON(buf, r.set, true) }
func (r *BoundedRegexp) UnmarshalYAML(u func(any) error) error { return parseYAML(u, r.set, true) }
func (r *BoundedRegexp) set(s string)                          { *r = BoundedRegexp(s) }

// Regexp returns the parsed regexp.Regexp instance for this PlainRegexp. An
// error is returned if the regular expression string inside this PlainRegexp
// is invalid. If the PlainRegexp was unmarshaled from JSON or YAML, no error
// will ever be returned because invalid regex strings were already rejected
// during unmarshalling.
func (r BoundedRegexp) Regexp() (*regexp.Regexp, error) {
	return compile(string(r), true)
}

// Shorthand for `r.Regexp()` followed by `rx.MatchString()`. If regex parsing
// returns an error, this function returns false.
func (r BoundedRegexp) MatchString(in string) bool {
	// optimization: match literals without regex compilation to reduce pressure on `cache`
	if isLiteral(string(r)) {
		return in == string(r)
	}

	rx, err := r.Regexp()
	if err != nil {
		return false
	}
	return rx.MatchString(in)
}

// Shorthand for `r.Regexp()` followed by `rx.FindStringSubmatch()`. If regex parsing
// returns an error, this function returns nil.
func (r BoundedRegexp) FindStringSubmatch(in string) []string {
	// optimization: match literals without regex compilation to reduce pressure on `cache`
	if isLiteral(string(r)) {
		if in == string(r) {
			return []string{string(r)}
		} else {
			return nil
		}
	}

	rx, err := r.Regexp()
	if err != nil {
		return nil
	}
	return rx.FindStringSubmatch(in)
}

type cacheKey struct {
	Regex     string
	IsBounded bool
}

var (
	cache *lru.Cache[cacheKey, *regexp.Regexp]
)

func init() {
	// lru.New() only fails if a non-negative size is given, so it's safe to ignore the error here
	//nolint:errcheck
	cache, _ = lru.New[cacheKey, *regexp.Regexp](64)
}

func parseJSON(buf []byte, set func(string), isBounded bool) error {
	var in string
	err := json.Unmarshal(buf, &in)
	if err != nil {
		return err
	}
	_, err = compile(in, isBounded)
	if err != nil {
		return err
	}
	set(in)
	return nil
}

func parseYAML(unmarshal func(any) error, set func(string), isBounded bool) error {
	var in string
	err := unmarshal(&in)
	if err != nil {
		return err
	}
	_, err = compile(in, isBounded)
	if err != nil {
		return err
	}
	set(in)
	return nil
}

func compile(in string, isBounded bool) (*regexp.Regexp, error) {
	key := cacheKey{in, isBounded}
	rx, ok := cache.Get(key)
	if ok {
		return rx, nil
	}
	pattern := in
	if isBounded {
		pattern = fmt.Sprintf("^(?:%s)$", in)
	}
	rx, err := regexp.Compile(pattern)
	if err != nil {
		return nil, fmt.Errorf("%q is not a valid regexp: %w", in, err)
	}
	cache.Add(key, rx)
	return rx, nil
}
