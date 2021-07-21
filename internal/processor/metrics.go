/*******************************************************************************
*
* Copyright 2021 SAP SE
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

package processor

import "github.com/prometheus/client_golang/prometheus"

var (
	//InboundManifestCacheHitCounter is a prometheus.CounterVec.
	InboundManifestCacheHitCounter = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "keppel_inbound_manifest_cache_hits",
			Help: "Counter for manifests pulled by Keppel from external registries where the inbound cache had a hit and no external request was made.",
		},
		[]string{"external_hostname"},
	)
	//InboundManifestCacheMissCounter is a prometheus.CounterVec.
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
