// SPDX-FileCopyrightText: 2020 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package api

import (
	"github.com/prometheus/client_golang/prometheus"
)

var (
	// BlobBytesPulledCounter is a prometheus.CounterVec.
	BlobBytesPulledCounter = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "keppel_pulled_blob_bytes",
			Help: "Counts blob content bytes that are pulled from Keppel.",
		},
		[]string{"account", "auth_tenant_id", "method"},
	)
	// BlobBytesPushedCounter is a prometheus.CounterVec.
	BlobBytesPushedCounter = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "keppel_pushed_blob_bytes",
			Help: "Counts blob content bytes that are pushed into Keppel.",
		},
		[]string{"account", "auth_tenant_id", "method"},
	)
	// BlobsPulledCounter is a prometheus.CounterVec.
	BlobsPulledCounter = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "keppel_pulled_blobs",
			Help: "Counts blobs that are pulled from Keppel.",
		},
		[]string{"account", "auth_tenant_id", "method"},
	)
	// BlobsPushedCounter is a prometheus.CounterVec.
	BlobsPushedCounter = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "keppel_pushed_blobs",
			Help: "Counts blobs that are pushed into Keppel.",
		},
		[]string{"account", "auth_tenant_id", "method"},
	)
	// ManifestsPulledCounter is a prometheus.CounterVec.
	ManifestsPulledCounter = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "keppel_pulled_manifests",
			Help: "Counts manifests that are pulled from Keppel.",
		},
		[]string{"account", "auth_tenant_id", "method"},
	)
	// ManifestsPushedCounter is a prometheus.CounterVec.
	ManifestsPushedCounter = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "keppel_pushed_manifests",
			Help: "Counts manifests that are pushed into Keppel.",
		},
		[]string{"account", "auth_tenant_id", "method"},
	)
	// UploadsAbortedCounter is a prometheus.CounterVec.
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
