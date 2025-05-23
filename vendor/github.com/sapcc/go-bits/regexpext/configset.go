// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package regexpext

import . "github.com/majewsky/gg/option"

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
func (cs ConfigSet[K, V]) pick(key K) (BoundedRegexp, Option[V]) {
	for _, entry := range cs {
		if entry.Key.MatchString(string(key)) {
			return entry.Key, Some(entry.Value)
		}
	}
	return "", None[V]()
}

// Pick returns the first value entry whose key regex matches the supplied key, or
// None if no entry in the ConfigSet matches the key.
func (cs ConfigSet[K, V]) Pick(key K) Option[V] {
	_, value := cs.pick(key)
	return value
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
//	metricSource := cs.PickAndFill(objectName, func(ms *MetricSource, expand func(string) string) {
//		ms.PrometheusQuery = expand(ms.PrometheusQuery)
//	}).UnwrapOr(MetricSource{})
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
func (cs ConfigSet[K, V]) PickAndFill(key K, fill func(value *V, expand func(string) string)) Option[V] {
	keyRx, maybeValue := cs.pick(key)
	value, ok := maybeValue.Unpack()
	if !ok {
		return None[V]()
	}

	rx, err := keyRx.Regexp()
	if err != nil {
		// defense in depth: this should not happen because the regex should have been validated at UnmarshalYAML time
		return None[V]()
	}
	match := rx.FindStringSubmatchIndex(string(key))
	if match == nil {
		// defense in depth: this should not happen because this is only called after the key has already matched
		return None[V]()
	}

	// match[0] always exists and refers to the full match; if there are capture groups, they are in match[1:]
	if len(match) > 1 {
		fill(&value, func(in string) string {
			return string(rx.ExpandString(nil, in, string(key), match))
		})
	}
	return Some(value)
}
