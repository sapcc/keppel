// SPDX-FileCopyrightText: 2021 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package processor

import "github.com/prometheus/client_golang/prometheus"

var (
	// InboundManifestCacheHitCounter is a prometheus.CounterVec.
	InboundManifestCacheHitCounter = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "keppel_inbound_manifest_cache_hits",
			Help: "Counter for manifests pulled by Keppel from external registries where the inbound cache had a hit and no external request was made.",
		},
		[]string{"external_hostname"},
	)
	// InboundManifestCacheMissCounter is a prometheus.CounterVec.
	InboundManifestCacheMissCounter = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "keppel_inbound_manifest_cache_misses",
			Help: "Counter for manifests pulled by Keppel from external registries where the inbound cache had a cache miss and therefore an external request had to be made.",
		},
		[]string{"external_hostname"},
	)
)

func init() {
	prometheus.MustRegister(InboundManifestCacheHitCounter)
	prometheus.MustRegister(InboundManifestCacheMissCounter)
}
