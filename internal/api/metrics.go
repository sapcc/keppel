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

package api

import (
	"github.com/prometheus/client_golang/prometheus"
)

var (
	//BlobBytesPulledCounter is a prometheus.CounterVec.
	BlobBytesPulledCounter = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "keppel_pulled_blob_bytes",
			Help: "Counts blob content bytes that are pulled from Keppel.",
		},
		[]string{"account", "auth_tenant_id", "method"},
	)
	//BlobBytesPushedCounter is a prometheus.CounterVec.
	BlobBytesPushedCounter = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "keppel_pushed_blob_bytes",
			Help: "Counts blob content bytes that are pushed into Keppel.",
		},
		[]string{"account", "auth_tenant_id", "method"},
	)
	//BlobsPulledCounter is a prometheus.CounterVec.
	BlobsPulledCounter = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "keppel_pulled_blobs",
			Help: "Counts blobs that are pulled from Keppel.",
		},
		[]string{"account", "auth_tenant_id", "method"},
	)
	//BlobsPushedCounter is a prometheus.CounterVec.
	BlobsPushedCounter = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "keppel_pushed_blobs",
			Help: "Counts blobs that are pushed into Keppel.",
		},
		[]string{"account", "auth_tenant_id", "method"},
	)
	//ManifestsPulledCounter is a prometheus.CounterVec.
	ManifestsPulledCounter = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "keppel_pulled_manifests",
			Help: "Counts manifests that are pulled from Keppel.",
		},
		[]string{"account", "auth_tenant_id", "method"},
	)
	//ManifestsPushedCounter is a prometheus.CounterVec.
	ManifestsPushedCounter = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "keppel_pushed_manifests",
			Help: "Counts manifests that are pushed into Keppel.",
		},
		[]string{"account", "auth_tenant_id", "method"},
	)
	//UploadsAbortedCounter is a prometheus.CounterVec.
	UploadsAbortedCounter = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "keppel_aborted_uploads",
			Help: "Counts blob uploads into Keppel that fail and get aborted by Keppel.",
		},
		[]string{"account", "auth_tenant_id", "method"},
	)
)

func init() {
	prometheus.MustRegister(BlobBytesPulledCounter)
	prometheus.MustRegister(BlobBytesPushedCounter)
	prometheus.MustRegister(BlobsPulledCounter)
	prometheus.MustRegister(BlobsPushedCounter)
	prometheus.MustRegister(ManifestsPulledCounter)
	prometheus.MustRegister(ManifestsPushedCounter)
	prometheus.MustRegister(UploadsAbortedCounter)
}
