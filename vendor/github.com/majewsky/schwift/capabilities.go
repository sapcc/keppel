/******************************************************************************
*
*  Copyright 2018 Stefan Majewsky <majewsky@gmx.net>
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

package schwift

//Capabilities describes a subset of the capabilities that Swift can report
//under its /info endpoint. This struct is obtained through the
//Account.Capabilities() method. To query capabilities not represented in this
//struct, see Account.QueryCapabilities().
//
//All direct members of struct Capabilities, except for "Swift", are pointers.
//If any of these is nil, it indicates that the middleware corresponding to
//that field is not available on this server.
type Capabilities struct {
	BulkDelete *struct {
		MaximumDeletesPerRequest uint `json:"max_deletes_per_request"`
		MaximumFailedDeletes     uint `json:"max_failed_deletes"`
	} `json:"bulk_delete"`
	BulkUpload *struct {
		MaximumContainersPerExtraction uint `json:"max_containers_per_extraction"`
		MaximumFailedExtractions       uint `json:"max_failed_extractions"`
	} `json:"bulk_upload"`
	StaticLargeObject *struct {
		MaximumManifestSegments uint `json:"max_manifest_segments"`
		MaximumManifestSize     uint `json:"max_manifest_size"`
		MinimumSegmentSize      uint `json:"min_segment_size"`
	} `json:"slo"`
	Swift struct {
		AccountAutocreate          bool                `json:"account_autocreate"`
		AccountListingLimit        uint                `json:"account_listing_limit"`
		AllowAccountManagement     bool                `json:"allow_account_management"`
		ContainerListingLimit      uint                `json:"container_listing_limit"`
		ExtraHeaderCount           uint                `json:"extra_header_count"`
		MaximumAccountNameLength   uint                `json:"max_account_name_length"`
		MaximumContainerNameLength uint                `json:"max_container_name_length"`
		MaximumFileSize            uint                `json:"max_file_size"`
		MaximumHeaderSize          uint                `json:"max_header_size"`
		MaximumMetaCount           uint                `json:"max_meta_count"`
		MaximumMetaNameLength      uint                `json:"max_meta_name_length"`
		MaximumMetaOverallSize     uint                `json:"max_meta_overall_size"`
		MaximumMetaValueLength     uint                `json:"max_meta_value_length"`
		MaximumObjectNameLength    uint                `json:"max_object_name_length"`
		Policies                   []StoragePolicySpec `json:"policies"`
		StrictCORSMode             bool                `json:"strict_cors_mode"`
		Version                    string              `json:"version"`
	} `json:"swift"`
	Swift3 *struct {
		AllowMultipartUploads     bool   `json:"allow_multipart_uploads"`
		MaximumBucketListing      uint   `json:"max_bucket_listing"`
		MaximumMultiDeleteObjects uint   `json:"max_multi_delete_objects"`
		MaximumPartsListing       uint   `json:"max_parts_listing"`
		MaximumUploadPartNumber   uint   `json:"max_upload_part_num"`
		Version                   string `json:"version"`
	} `json:"swift3"`
	Symlink *struct {
		MaximumLoopCount uint `json:"symloop_max"`
	} `json:"symlink"`
	TempAuth *struct {
		AccountACLs bool `json:"account_acls"`
	} `json:"tempauth"`
	TempURL *struct {
		IncomingAllowHeaders  []string `json:"incoming_allow_headers"`
		IncomingRemoveHeaders []string `json:"incoming_remove_headers"`
		Methods               []string `json:"methods"`
		OutgoingAllowHeaders  []string `json:"outgoing_allow_headers"`
		OutgoingRemoveHeaders []string `json:"outgoing_remove_headers"`
	} `json:"tempurl"`
}

//StoragePolicySpec is a subtype that appears in struct Capabilities.
type StoragePolicySpec struct {
	Name    string `json:"name"`
	Aliases string `json:"aliases"`
	Default bool   `json:"default"`
}
