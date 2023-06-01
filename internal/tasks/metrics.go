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
	sweepStorageSuccessCounter = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "keppel_successful_storage_sweeps",
		Help: "Counter for successful garbage collections of an account's backing storage.",
	})
	sweepStorageFailedCounter = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "keppel_failed_storage_sweeps",
		Help: "Counter for failed garbage collections of an account's backing storage.",
	})
	syncManifestsSuccessCounter = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "keppel_successful_manifest_syncs",
		Help: "Counter for successful manifest syncs in replica repos.",
	})
	syncManifestsFailedCounter = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "keppel_failed_manifest_syncs",
		Help: "Counter for failed manifest syncs in replica repos.",
	})
	validateBlobSuccessCounter = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "keppel_successful_blob_validations",
		Help: "Counter for successful blob validations.",
	})
	validateBlobFailedCounter = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "keppel_failed_blob_validations",
		Help: "Counter for failed blob validations.",
	})
	validateManifestSuccessCounter = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "keppel_successful_manifest_validations",
		Help: "Counter for successful manifest validations.",
	})
	validateManifestFailedCounter = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "keppel_failed_manifest_validations",
		Help: "Counter for failed manifest validations.",
	})

	metricsRegistered = false
)

func (j *Janitor) initializeCounters() {
	if !metricsRegistered {
		metricsRegistered = true
		prometheus.MustRegister(checkVulnerabilitySuccessCounter)
		prometheus.MustRegister(checkVulnerabilityFailedCounter)
		prometheus.MustRegister(checkVulnerabilityRetriedCounter)
		prometheus.MustRegister(sweepStorageSuccessCounter)
		prometheus.MustRegister(sweepStorageFailedCounter)
		prometheus.MustRegister(syncManifestsSuccessCounter)
		prometheus.MustRegister(syncManifestsFailedCounter)
		prometheus.MustRegister(validateBlobSuccessCounter)
		prometheus.MustRegister(validateBlobFailedCounter)
		prometheus.MustRegister(validateManifestSuccessCounter)
		prometheus.MustRegister(validateManifestFailedCounter)
	}

	//add 0 to all counters to ensure that the relevant timeseries exist
	checkVulnerabilitySuccessCounter.Add(0)
	checkVulnerabilityFailedCounter.Add(0)
	checkVulnerabilityRetriedCounter.Add(0)
	sweepStorageSuccessCounter.Add(0)
	sweepStorageFailedCounter.Add(0)
	syncManifestsSuccessCounter.Add(0)
	syncManifestsFailedCounter.Add(0)
	validateBlobSuccessCounter.Add(0)
	validateBlobFailedCounter.Add(0)
	validateManifestSuccessCounter.Add(0)
	validateManifestFailedCounter.Add(0)
}
