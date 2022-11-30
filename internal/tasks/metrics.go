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
	announceAccountToFederationSuccessCounter = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "keppel_successful_account_federation_announcements",
		Help: "Counter for successful announcements of existing accounts to the federation driver.",
	})
	announceAccountToFederationFailedCounter = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "keppel_failed_account_federation_announcements",
		Help: "Counter for failed announcements of existing accounts to the federation driver.",
	})
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
	checkForNewVulnerabilitySuccessCounter = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "keppel_successful_new_vulnerability_checks",
		Help: "Counter for successful updates of the pending vulnerability status of a manifest.",
	})
	checkForNewVulnerabilityFailedCounter = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "keppel_failed_new_vulnerability_checks",
		Help: "Counter for failed updates of the pending vulnerability status of a manifest.",
	})
	cleanupAbandonedUploadSuccessCounter = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "keppel_successful_abandoned_upload_cleanups",
		Help: "Counter for successful cleanup of abandoned uploads.",
	})
	cleanupAbandonedUploadFailedCounter = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "keppel_failed_abandoned_upload_cleanups",
		Help: "Counter for failed cleanup of abandoned uploads.",
	})
	imageGCSuccessCounter = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "keppel_successful_image_garbage_collections",
		Help: "Counter for successful garbage collection runs in repos.",
	})
	imageGCFailedCounter = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "keppel_failed_image_garbage_collections",
		Help: "Counter for failed garbage collection runs in repos.",
	})
	sweepBlobMountsSuccessCounter = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "keppel_successful_blob_mount_sweeps",
		Help: "Counter for successful garbage collections on blob mounts in a repo.",
	})
	sweepBlobMountsFailedCounter = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "keppel_failed_blob_mount_sweeps",
		Help: "Counter for failed garbage collections on blob mounts in a repo.",
	})
	sweepBlobsSuccessCounter = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "keppel_successful_blob_sweeps",
		Help: "Counter for successful garbage collections on blobs in an account.",
	})
	sweepBlobsFailedCounter = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "keppel_failed_blob_sweeps",
		Help: "Counter for failed garbage collections on blobs in an account.",
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
		prometheus.MustRegister(announceAccountToFederationSuccessCounter)
		prometheus.MustRegister(announceAccountToFederationFailedCounter)
		prometheus.MustRegister(checkVulnerabilitySuccessCounter)
		prometheus.MustRegister(checkVulnerabilityFailedCounter)
		prometheus.MustRegister(checkVulnerabilityRetriedCounter)
		prometheus.MustRegister(checkForNewVulnerabilitySuccessCounter)
		prometheus.MustRegister(checkForNewVulnerabilityFailedCounter)
		prometheus.MustRegister(cleanupAbandonedUploadSuccessCounter)
		prometheus.MustRegister(cleanupAbandonedUploadFailedCounter)
		prometheus.MustRegister(imageGCSuccessCounter)
		prometheus.MustRegister(imageGCFailedCounter)
		prometheus.MustRegister(sweepBlobMountsSuccessCounter)
		prometheus.MustRegister(sweepBlobMountsFailedCounter)
		prometheus.MustRegister(sweepBlobsSuccessCounter)
		prometheus.MustRegister(sweepBlobsFailedCounter)
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
	announceAccountToFederationSuccessCounter.Add(0)
	announceAccountToFederationFailedCounter.Add(0)
	checkVulnerabilitySuccessCounter.Add(0)
	checkVulnerabilityFailedCounter.Add(0)
	checkVulnerabilityRetriedCounter.Add(0)
	checkForNewVulnerabilitySuccessCounter.Add(0)
	checkForNewVulnerabilityFailedCounter.Add(0)
	cleanupAbandonedUploadSuccessCounter.Add(0)
	cleanupAbandonedUploadFailedCounter.Add(0)
	imageGCSuccessCounter.Add(0)
	imageGCFailedCounter.Add(0)
	sweepBlobMountsSuccessCounter.Add(0)
	sweepBlobMountsFailedCounter.Add(0)
	sweepBlobsSuccessCounter.Add(0)
	sweepBlobsFailedCounter.Add(0)
	sweepStorageSuccessCounter.Add(0)
	sweepStorageFailedCounter.Add(0)
	syncManifestsSuccessCounter.Add(0)
	syncManifestsFailedCounter.Add(0)
	validateBlobSuccessCounter.Add(0)
	validateBlobFailedCounter.Add(0)
	validateManifestSuccessCounter.Add(0)
	validateManifestFailedCounter.Add(0)
}
