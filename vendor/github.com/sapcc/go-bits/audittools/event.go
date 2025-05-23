// SPDX-FileCopyrightText: 2019 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package audittools

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gofrs/uuid/v5"
	"github.com/sapcc/go-api-declarations/cadf"

	"github.com/sapcc/go-bits/httpext"
	"github.com/sapcc/go-bits/must"
)

// Target is implemented by types that describe the target object of an audit event.
// It appears in the high-level Event type from this package.
type Target interface {
	// Serializes this object into its wire format as it appears in the Target field of type cadf.Event.
	Render() cadf.Resource
}

// Observer is like cadf.Resource, but contains only the fields that need to be
// set for an event observer.
type Observer struct {
	TypeURI string
	Name    string
	ID      string
}

// ToCADF is a low-level function that converts this observer into the CADF format.
// This function is intended for implementors of Auditor.Record() only.
func (o Observer) ToCADF() cadf.Resource {
	return cadf.Resource{
		TypeURI: o.TypeURI,
		Name:    o.Name,
		ID:      o.ID,
	}
}

// UserInfo is implemented by types that describe a user who is taking an action on an OpenStack service.
// The most important implementor of this interface is *gopherpolicy.Token, for actions taken by authenticated users.
// Application-specific custom implementors can be used for actions taken by internal processes like cronjobs.
type UserInfo interface {
	// Serializes this object into its wire format as it appears in the Initiator field of type cadf.Event.
	// For events originating in HTTP request handlers, the provided Host object should be placed in the Host field of the result.
	AsInitiator(cadf.Host) cadf.Resource
}

// Event is a high-level representation of an audit event.
// The Auditor will serialize it into its wire format (type cadf.Event) before sending it to Hermes.
type Event struct {
	Time    time.Time
	Request *http.Request
	// User is usually a *gopherpolicy.Token instance.
	User UserInfo
	// ReasonCode is used to determine whether the Event.Outcome was a 'success' or 'failure'.
	// It is recommended to use a constant from: https://golang.org/pkg/net/http/#pkg-constants
	ReasonCode int
	Action     cadf.Action
	Target     Target
}

// EventParameters is a deprecated alias for Event.
type EventParameters = Event

// ToCADF is a low-level function that converts this event into the CADF format.
// Most applications will use the high-level interface of Auditor.Record() instead.
//
// Warning: This function uses GenerateUUID() to generate the Event.ID.
// Unexpected errors during UUID generation will be logged and result in program termination.
func (p Event) ToCADF(observer cadf.Resource) cadf.Event {
	outcome := cadf.FailureOutcome
	if p.ReasonCode >= 200 && p.ReasonCode < 300 {
		outcome = cadf.SuccessOutcome
	}

	return cadf.Event{
		TypeURI:   "http://schemas.dmtf.org/cloud/audit/1.0/event",
		ID:        GenerateUUID(),
		EventTime: p.Time.Format("2006-01-02T15:04:05.999999+00:00"),
		EventType: "activity",
		Action:    p.Action,
		Outcome:   outcome,
		Reason: cadf.Reason{
			ReasonType: "HTTP",
			ReasonCode: strconv.Itoa(p.ReasonCode),
		},
		Initiator: p.User.AsInitiator(cadf.Host{
			Address: httpext.GetRequesterIPFor(p.Request),
			Agent:   p.Request.Header.Get("User-Agent"),
		}),
		Target:      p.Target.Render(),
		Observer:    observer,
		RequestPath: p.Request.URL.String(),
	}
}

// GenerateUUID generates an UUID based on random numbers (RFC 4122).
// Failure will result in program termination.
func GenerateUUID() string {
	return must.Return(uuid.NewV4()).String()
}
