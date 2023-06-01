/******************************************************************************
*
*  Copyright 2020 SAP SE
*
*  Licensed under the Apache License, Version 2.0 (the "License");
*  you may not use this file except in compliance with the License.
*  You may obtain a copy of the License at
*
*      http://www.apache.org/licenses/LICENSE-2.0
*
*  Unless required by applicable law or agreed to in writing, software
*  distributed under the License is distributed on an "AS IS" BASIS,
*  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
*  See the License for the specific language governing permissions and
*  limitations under the License.
*
******************************************************************************/

package tasks

import "github.com/prometheus/client_golang/prometheus"

var (
	checkVulnerabilitySuccessCounter = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "keppel_successful_vulnerability_checks",
		Help: "Counter for successful updates of the vulnerability status of a manifest.",
	})
	checkVulnerabilityFailedCounter = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "keppel_failed_vulnerability_checks",
		Help: "Counter for failed updates of the vulnerability status of a manifest.",
	})
	checkVulnerabilityRetriedCounter = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "keppel_retried_vulnerability_checks",
		Help: "Counter for vulnerability checks that were retried due to transient errors in Clair.",
	})

	metricsRegistered = false
)

func (j *Janitor) initializeCounters() {
	if !metricsRegistered {
		metricsRegistered = true
		prometheus.MustRegister(checkVulnerabilitySuccessCounter)
		prometheus.MustRegister(checkVulnerabilityFailedCounter)
		prometheus.MustRegister(checkVulnerabilityRetriedCounter)
		prometheus.MustRegister(validateManifestSuccessCounter)
		prometheus.MustRegister(validateManifestFailedCounter)
	}

	//add 0 to all counters to ensure that the relevant timeseries exist
	checkVulnerabilitySuccessCounter.Add(0)
	checkVulnerabilityFailedCounter.Add(0)
	checkVulnerabilityRetriedCounter.Add(0)
}
