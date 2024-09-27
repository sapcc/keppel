/*******************************************************************************
*
* Copyright 2024 SAP SE
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

package regexpext

// ConfigSet works similar to map[K]V in that it picks values of type V for
// keys of type K, but the keys in the data structure are actually regexes that
// can apply to an entire set of K instead of just one specific value of K.
//
// This type is intended to use in configuration, as a substitute for map[K]V
// that satisfies the DRY rule (Don't Repeat Yourself), or when the full set of
// relevant keys is not known at config editing time.
type ConfigSet[K ~string, V any] []struct {
	Key   BoundedRegexp `json:"key" yaml:"key"`
	Value V             `json:"value" yaml:"value"`
}

// The basis for both Pick and PickAndFill. This uses MatchString to leverage
// the specific optimizations in type BoundedRegexp for this function.
func (cs ConfigSet[K, V]) pick(key K) (BoundedRegexp, V, bool) {
	for _, entry := range cs {
		if entry.Key.MatchString(string(key)) {
			return entry.Key, entry.Value, true
		}
	}
	var zero V
	return "", zero, false
}

// Pick returns the first value entry whose key regex matches the supplied key, or
// the given default value if none of the entries in the ConfigSet matches the key.
func (cs ConfigSet[K, V]) Pick(key K, defaultValue V) V {
	_, value, ok := cs.pick(key)
	if ok {
		return value
	} else {
		return defaultValue
	}
}

// PickAndFill is like Pick, but if the regex in the matching entry contains
// parenthesized subexpressions (also known as capture groups), the fill
// callback is used to expand references to the captured texts in the value.
//
// Usage in code looks like this:
//
//	type ObjectType string
//	type MetricSource struct {
//		PrometheusQuery string
//	}
//	var cs ConfigSet[ObjectType, MetricSource]
//	// omitted: fill `cs` by parsing configuration
//
//	objectName := "foo_widget"
//	metricSource := cs.PickAndFill(objectName, MetricSource{}, func(ms *MetricSource, expand func(string) string) {
//		ms.PrometheusQuery = expand(ms.PrometheusQuery)
//	})
//
// With this, configuration can be condensed like in the example below:
//
//	originalConfig := ConfigSet[ObjectType, MetricSource]{
//		{ Key: "foo_widget", Value: MetricSource{PrometheusQuery: "count(foo_widgets)"} },
//		{ Key: "bar_widget", Value: MetricSource{PrometheusQuery: "count(bar_widgets)"} },
//		{ Key: "qux_widget", Value: MetricSource{PrometheusQuery: "count(qux_widgets)"} },
//	}
//	condensedConfig := ConfigSet[ObjectType, MetricSource]{
//		{ Key: "(foo|bar|qux)_widget", Value: MetricSource{PrometheusQuery: "count(${1}_widgets)"} },
//	}
//
// Expansion follows the same rules as for regexp.ExpandString() from the standard library.
func (cs ConfigSet[K, V]) PickAndFill(key K, defaultValue V, fill func(value *V, expand func(string) string)) V {
	keyRx, value, ok := cs.pick(key)
	if !ok {
		return defaultValue
	}

	rx, err := keyRx.Regexp()
	if err != nil {
		// defense in depth: this should not happen because the regex should have been validated at UnmarshalYAML time
		return defaultValue
	}
	match := rx.FindStringSubmatchIndex(string(key))
	if match == nil {
		// defense in depth: this should not happen because this is only called after the key has already matched
		return defaultValue
	}

	// match[0] always exists and refers to the full match; if there are capture groups, they are in match[1:]
	if len(match) > 1 {
		fill(&value, func(in string) string {
			return string(rx.ExpandString(nil, in, string(key), match))
		})
	}
	return value
}
