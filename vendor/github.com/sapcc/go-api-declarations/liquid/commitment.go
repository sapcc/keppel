// SPDX-FileCopyrightText: 2025 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package liquid

import (
	"time"

	. "github.com/majewsky/gg/option"
)

// CommitmentChangeRequest is the request payload format for POST /v1/change-commitments.
type CommitmentChangeRequest struct {
	AZ AvailabilityZone `json:"az"`

	// DryRun indicates that this request is not an actual change by the user, but a request to determine the
	// current possibilities within the services' capacity. When set to true, the liquid and any following consulted
	// services must not save the changeRequest to the database.
	DryRun bool `json:"dryRun"`

	// The same version number that was reported in the Version field of a GET /v1/info response.
	// The liquid shall reject this request if the version here differs from the value in the ServiceInfo currently held by the liquid.
	// This is used to ensure that Limes does not request commitment changes based on outdated resource metadata.
	InfoVersion int64 `json:"infoVersion"`

	// On the first level, the commitment changeset is grouped by project.
	//
	// Changesets may span over multiple projects e.g. when moving commitments from one project to another.
	// In this case, the changeset will show the commitment as being deleted in the source project, and as being created in the target project.
	ByProject map[ProjectUUID]ProjectCommitmentChangeset `json:"byProject"`
}

// CommitmentChangeResponse is the response payload format for POST /v1/change-commitments.
type CommitmentChangeResponse struct {
	// If req.RequiresConfirmation() was true, this field shall be empty if the changeset is confirmed, or contain a human-readable error message if the changeset was rejected.
	// If req.RequiresConfirmation() was false, Limes will ignore this field (or, at most, log it silently).
	//
	// This field should only be used to report when a well-formed CommitmentChangeRequest required confirmation, but could not be confirmed because of a lack of capacity or similar.
	// For malformed CommitmentChangeRequest objects, the liquid must return a non-200 status code as per the usual convention of this API.
	RejectionReason string `json:"rejectionReason,omitempty"`

	// If RejectionReason is not empty, this field may optionally indicate how long the caller should wait before reattempting this change.
	//
	// For changes originating in Limes, Limes itself may honor this information.
	// For changes requested by a user through the Limes API, Limes may forward this information to the user.
	RetryAt Option[time.Time] `json:"retryAt,omitzero"`
}

// ProjectCommitmentChangeset appears in type CommitmentChangeRequest.
// It contains all commitments that are part of a single atomic changeset that belong to a specific project in a specific AZ.
type ProjectCommitmentChangeset struct {
	// Metadata about the project from Keystone.
	// Only included if the ServiceInfo declared a need for it.
	ProjectMetadata Option[ProjectMetadata] `json:"projectMetadata,omitzero"`

	// On the second level, the commitment changeset is grouped by resource.
	//
	// Changesets may span over multiple resources when converting commitments for one resource into commitments for another resource.
	// In this case, the changeset will show the original commitment being deleted in one resource, and a new commitment being created in another.
	ByResource map[ResourceName]ResourceCommitmentChangeset `json:"byResource"`
}

// ResourceCommitmentChangeset appears in type CommitmentChangeRequest.
// It contains all commitments that are part of a single atomic changeset that belong to a given resource within a specific project and AZ.
type ResourceCommitmentChangeset struct {
	// The sum of all commitments in CommitmentStatusConfirmed for the given resource, project and AZ before and after applying the proposed commitment changeset.
	//
	// For example, if this changeset shows a confirmed commitment with Amount = 6 as being created,
	// and one with Amount = 9 as being deleted,
	// and also there are several other commitments with a total Amount = 100 that the changeset does not touch,
	// then we will have TotalConfirmedBefore = 109 and TotalConfirmedAfter = 106.
	TotalConfirmedBefore uint64 `json:"totalConfirmedBefore"`
	TotalConfirmedAfter  uint64 `json:"totalConfirmedAfter"`

	// Same as above, but for commitments in CommitmentStatusGuaranteed.
	TotalGuaranteedBefore uint64 `json:"totalGuaranteedBefore"`
	TotalGuaranteedAfter  uint64 `json:"totalGuaranteedAfter"`

	// A commitment changeset may contain multiple commitments for a single resource within the same project.
	// For example, when a commitment is split into two parts, the changeset will show the original commitment being deleted and two new commitments being created.
	Commitments []Commitment `json:"commitments"`
}

// Commitment appears in type CommitmentChangeRequest.
//
// The commitment is located in a certain project and applies to a certain resource within a certain AZ.
// These metadata are implied by where the commitment is found within type CommitmentChangeRequest.
type Commitment struct {
	// The same UUID may appear multiple times within the same changeset for one specific circumstance:
	// If a commitment moves between projects, it will appear as being deleted in the source project and again as being created in the target project.
	UUID CommitmentUUID `json:"uuid"`

	// These two status fields communicate one of three possibilities:
	//   - If OldStatus.IsNone() and NewStatus.IsSome(), the commitment is being created (or moved to this location).
	//   - If OldStatus.IsSome() and NewStatus.IsNone(), the commitment is being deleted (or moved away from this location).
	//   - If OldStatus.IsSome() and NewStatus.IsSome(), the commitment is only changing its status (e.g. from "confirmed" to "expired" when ExpiresAt has passed).
	OldStatus Option[CommitmentStatus] `json:"oldStatus"`
	NewStatus Option[CommitmentStatus] `json:"newStatus"`

	Amount uint64 `json:"amount"`

	// For commitments in status "planned", this field contains the point in time in the future when the user wants for it to move into status "confirmed".
	// If confirmation is not possible by that point in time, the commitment will move into status "pending" until it can be confirmed.
	//
	// For all other status values, this field contains the point in time when the status transitioned into status "confirmed",
	// or None() if the commitment was created for immediate confirmation and therefore started in status "confirmed".
	ConfirmBy Option[time.Time] `json:"confirmBy,omitzero"`

	// This field contains the point in time when the commitment moves into status "expired", unless it is deleted or moves into status "superseded" first.
	ExpiresAt time.Time `json:"expiresAt"`

	// OldExpiresAt is set when the expiration date of an existing commitment is changed. Depending on its status
	// RequiresConfirmation() will evaulate to different results.
	OldExpiresAt Option[time.Time] `json:"oldExpiresAt,omitzero"`
}

// CommitmentStatus is an enum containing the various lifecycle states of type Commitment.
// The following state transitions are allowed:
//
//	start = "planned" -> "pending" -> "confirmed"   // normal commitment that takes effect after the ConfirmBy date
//	start = "guaranteed" -> "confirmed"             // pre-confirmed commitment that takes effect at the ConfirmBy date
//	start = "confirmed"                             // commitment that takes effect right away (ConfirmBy = nil)
//	anyNonFinal -> "expired" = final                // commitment stops taking effect after ExpiresAt
//	anyNonFinal -> "superseded" = final             // commitment stops taking effect if replaced by other commitments
type CommitmentStatus string

const (
	// CommitmentStatusPlanned means that the commitment has a ConfirmBy date in the future.
	// Planned commitments are used to notify the cloud about future resource demand.
	// The cloud has not committed to fulfilling this resource demand in the future.
	CommitmentStatusPlanned CommitmentStatus = "planned"
	// CommitmentStatusPending means that the commitment has a ConfirmBy date in the past, but the cloud has not confirmed it yet.
	// Pending commitments usually only stick around when there is not enough capacity to cover all current resource demands.
	CommitmentStatusPending CommitmentStatus = "pending"
	// CommitmentStatusGuaranteed means that the commitment has a ConfirmBy date in the future.
	// Similar to CommitmentStatusPlanned, this type of commitment notifies the cloud about future resource demand.
	// But unlike CommitmentStatusPlanned, the cloud has already committed to honoring this demand in the future.
	// Upon the passing of the ConfirmBy date, the commitment will certainly and immediately move into CommitmentStatusConfirmed.
	CommitmentStatusGuaranteed CommitmentStatus = "guaranteed"
	// CommitmentStatusConfirmed means that the commitment has been confirmed and is being honored by the cloud.
	// Confirmed commitments represent current resource demand that the cloud is able to guarantee.
	CommitmentStatusConfirmed CommitmentStatus = "confirmed"
	// CommitmentStatusSuperseded means that the commitment is no longer being honored by the cloud because it has been replaced by other commitments.
	// For example, when splitting a commitment into two halves, the new commitments will have the same status as the old commitment, and the old commitment will move into status "superseded".
	CommitmentStatusSuperseded CommitmentStatus = "superseded"
	// CommitmentStatusExpired means that the commitment is no longer being honored by the cloud because its lifetime has expired.
	// Expired commitments can be renewed by the user manually, but that involves creating a new commitment separately, such that ConfirmBy of the new commitment is equal to ExpiresAt of the old commitment.
	CommitmentStatusExpired CommitmentStatus = "expired"
)

// IsValid returns whether the given status is one of the predefined enum variants.
func (s CommitmentStatus) IsValid() bool {
	switch s {
	case CommitmentStatusPlanned, CommitmentStatusPending, CommitmentStatusGuaranteed, CommitmentStatusConfirmed, CommitmentStatusSuperseded, CommitmentStatusExpired:
		return true
	default:
		return false
	}
}

// RequiresConfirmation describes if this request requires confirmation from the liquid.
// The RejectionReason in type CommitmentChangeResponse may only be used if this returns true.
//
// Examples for RequiresConfirmation = true include commitments moving into or spawning in the "guaranteed" or "confirmed" statuses, or conversion of commitments between resources.
// Examples for RequiresConfirmation = false include commitments being split, moving into the "expired" status or being hard deleted.
func (req CommitmentChangeRequest) RequiresConfirmation() bool {
	// the request requires confirmation if any one ResourceCommitmentChangeset does
	for _, pc := range req.ByProject {
		for _, rc := range pc.ByResource {
			// the only case which requires confirmation when the totals do not change is when a commitments expiresAt
			// changes and it was confirmed or guaranteed before.
			for _, c := range rc.Commitments {
				if (c.NewStatus == Some(CommitmentStatusConfirmed) || c.NewStatus == Some(CommitmentStatusGuaranteed)) && c.OldExpiresAt.IsSome() {
					return true
				}
			}

			// if the relevant totals do not change, no confirmation is required
			if rc.TotalConfirmedBefore == rc.TotalConfirmedAfter && rc.TotalGuaranteedBefore == rc.TotalGuaranteedAfter {
				continue
			}

			// otherwise, confirmation is required except if the changes can be explained by the following types of status changes:
			//   - guaranteed/confirmed -> expired (based on a pre-determined schedule that was known at confirmation time)
			//   - guaranteed -> confirmed (the former includes an implicit approval for moving into the latter at ConfirmBy)
			var (
				// NOTE: This algorithm is purposefully written to never use subtractions.
				// If the totals values provided in the request are incorrect, subtractions could overflow below zero.
				// Additions, on the other hand, should always be fine unless any commitment amount or totals value is extremely large
				// (which realistically would only occur as the result of a previous uint64 overflow).
				expectedGuaranteedReduction uint64 = 0
				expectedConfirmedReduction  uint64 = 0
				expectedConfirmedIncrease   uint64 = 0
			)
			for _, c := range rc.Commitments {
				switch {
				case c.OldStatus == Some(CommitmentStatusConfirmed) && (c.NewStatus == Some(CommitmentStatusExpired) || c.NewStatus == None[CommitmentStatus]()):
					expectedConfirmedReduction += c.Amount
				case c.OldStatus == Some(CommitmentStatusGuaranteed) && (c.NewStatus == Some(CommitmentStatusExpired) || c.NewStatus == None[CommitmentStatus]()):
					expectedGuaranteedReduction += c.Amount
				case c.OldStatus == Some(CommitmentStatusGuaranteed) && c.NewStatus == Some(CommitmentStatusConfirmed):
					expectedGuaranteedReduction += c.Amount
					expectedConfirmedIncrease += c.Amount
				}
			}
			if rc.TotalConfirmedBefore+expectedConfirmedIncrease != rc.TotalConfirmedAfter+expectedConfirmedReduction {
				return true
			}
			if rc.TotalGuaranteedBefore != rc.TotalGuaranteedAfter+expectedGuaranteedReduction {
				return true
			}
		}
	}

	return false
}
