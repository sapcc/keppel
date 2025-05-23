// SPDX-FileCopyrightText: 2019-2022 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package httpapi

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/sapcc/go-api-declarations/bininfo"
)

// MetricsConfig contains configuration options for ConfigureMetrics().
type MetricsConfig struct {
	AppName                  string // leave empty to use bininfo.Component()
	FirstByteDurationBuckets []float64
	ResponseDurationBuckets  []float64
	RequestBodySizeBuckets   []float64
	ResponseBodySizeBuckets  []float64
}

var (
	metricsConfigured       bool
	metricsAppName          string
	metricFirstByteDuration *prometheus.HistogramVec
	metricResponseDuration  *prometheus.HistogramVec
	metricRequestBodySize   *prometheus.HistogramVec
	metricResponseBodySize  *prometheus.HistogramVec

	// interface for tests only
	metricsRegisterer = prometheus.DefaultRegisterer
)

func testSetRegisterer(r prometheus.Registerer) {
	metricsRegisterer = r

	// We need to reset this flag at the start of each test, in case multiple
	// tests want to register metrics to their own registries respectively.
	metricsConfigured = false
}

// ConfigureMetrics sets up the metrics emitted by this package. This function
// must be called exactly once before the first call to Compose(), but only if
// the default configuration needs to be overridden.
func ConfigureMetrics(cfg MetricsConfig) {
	if metricsConfigured {
		panic("ConfigureMetrics called multiple times or after Compose")
	}

	metricsConfigured = true
	if cfg.AppName == "" {
		metricsAppName = bininfo.Component()
	} else {
		metricsAppName = cfg.AppName
	}

	labelNames := []string{"app", "method", "status", "endpoint"}

	metricFirstByteDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "httpmux_first_byte_seconds",
		Help:    "Duration in seconds until the first byte was sent in response to HTTP requests received by the application.",
		Buckets: cfg.FirstByteDurationBuckets,
	}, labelNames)
	metricResponseDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "httpmux_response_duration_seconds",
		Help:    "Duration in seconds until the full response was sent in response to HTTP requests received by the application.",
		Buckets: cfg.ResponseDurationBuckets,
	}, labelNames)
	metricRequestBodySize = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "httpmux_request_size_bytes",
		Help:    "Size in bytes of HTTP request bodies received by the application.",
		Buckets: cfg.RequestBodySizeBuckets,
	}, labelNames)
	metricResponseBodySize = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "httpmux_response_size_bytes",
		Help:    "Size in bytes of response bodies sent in response to HTTP requests received by the application.",
		Buckets: cfg.ResponseBodySizeBuckets,
	}, labelNames)

	metricsRegisterer.MustRegister(metricFirstByteDuration)
	metricsRegisterer.MustRegister(metricResponseDuration)
	metricsRegisterer.MustRegister(metricRequestBodySize)
	metricsRegisterer.MustRegister(metricResponseBodySize)
}

var (
	// taken from <https://github.com/sapcc/helm-charts/blob/20f70f7071fcc03c3cee3f053ddc7e3989a05ae8/openstack/swift/etc/statsd-exporter.yaml#L23>
	defaultDurationBuckets = []float64{0.025, 0.1, 0.25, 1, 2.5}

	// 1024 and 8192 indicate that the request/response probably fits inside a single
	// ethernet frame or jumboframe, respectively
	defaultBodySizeBuckets = []float64{1024, 8192, 1000000, 10000000}
)

func autoConfigureMetricsIfNecessary() {
	if metricsConfigured {
		return
	}
	ConfigureMetrics(MetricsConfig{
		AppName:                  "", // autofill from bininfo.Component()
		FirstByteDurationBuckets: defaultDurationBuckets,
		ResponseDurationBuckets:  defaultDurationBuckets,
		RequestBodySizeBuckets:   defaultBodySizeBuckets,
		ResponseBodySizeBuckets:  defaultBodySizeBuckets,
	})
}
