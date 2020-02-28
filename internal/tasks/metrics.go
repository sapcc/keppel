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
	cleanupAbandonedUploadSuccessCounter = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "keppel_successful_abandoned_upload_cleanups",
		Help: "Counter for successful cleanup of abandoned uploads.",
	})
	cleanupAbandonedUploadFailedCounter = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "keppel_failed_abandoned_upload_cleanups",
		Help: "Counter for failed cleanup of abandoned uploads.",
	})
)

func init() {
	prometheus.MustRegister(cleanupAbandonedUploadSuccessCounter)
	prometheus.MustRegister(cleanupAbandonedUploadFailedCounter)
}

func (j *Janitor) initializeCounters() {
	//add 0 to all counters to ensure that the relevant timeseries exist
	cleanupAbandonedUploadSuccessCounter.Add(0)
	cleanupAbandonedUploadFailedCounter.Add(0)
}
