/*******************************************************************************
*
* Copyright 2019 SAP SE
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

package audittools

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gofrs/uuid/v5"
	"github.com/sapcc/go-api-declarations/cadf"

	"github.com/sapcc/go-bits/httpext"
	"github.com/sapcc/go-bits/logg"
)

// TargetRenderer is the interface that different event types "must" implement
// in order to render the respective cadf.Event.Target section.
type TargetRenderer interface {
	Render() cadf.Resource
}

// UserInfo is implemented by types that describe a user who is taking an
// action on an OpenStack API. The most important implementor of this interface
// is *gopherpolicy.Token.
type UserInfo interface {
	UserUUID() string
	UserName() string
	UserDomainName() string
	// ProjectScopeUUID returns the empty string if the user's token is not for a project scope.
	ProjectScopeUUID() string
	// ProjectScopeName returns the empty string if the user's token is not for a project scope.
	ProjectScopeName() string
	// ProjectScopeDomainName returns the empty string if the user's token is not for a project scope.
	ProjectScopeDomainName() string
	// DomainScopeUUID returns the empty string if the user's token is not for a domain scope.
	DomainScopeUUID() string
	// DomainScopeName returns the empty string if the user's token is not for a domain scope.
	DomainScopeName() string
	// ApplicationCredentialID returns the empty string if the user's token was created through a different authentication method.
	ApplicationCredentialID() string
}

// NonStandardUserInfo is an extension interface for type UserInfo that allows a
// UserInfo instance to render its own cadf.Resource. This is useful for
// UserInfo implementors representing special roles that are not backed by a
// Keystone user.
type NonStandardUserInfo interface {
	UserInfo
	AsInitiator() cadf.Resource
}

// EventParameters contains the necessary parameters for generating a cadf.Event.
type EventParameters struct {
	Time    time.Time
	Request *http.Request
	// User is usually a *gopherpolicy.Token instance.
	User UserInfo
	// ReasonCode is used to determine whether the Event.Outcome was a 'success' or 'failure'.
	// It is recommended to use a constant from: https://golang.org/pkg/net/http/#pkg-constants
	ReasonCode int
	Action     cadf.Action
	Observer   struct {
		TypeURI string
		Name    string
		ID      string
	}
	Target TargetRenderer
}

// NewEvent uses EventParameters to generate an audit event.
// Warning: this function uses GenerateUUID() to generate the Event.ID, if that fails
// then the concerning error will be logged and it will result in program termination.
func NewEvent(p EventParameters) cadf.Event {
	outcome := cadf.FailureOutcome
	if p.ReasonCode >= 200 && p.ReasonCode < 300 {
		outcome = cadf.SuccessOutcome
	}

	var initiator cadf.Resource
	if u, ok := p.User.(NonStandardUserInfo); ok {
		initiator = u.AsInitiator()
	} else {
		initiator = cadf.Resource{
			TypeURI: "service/security/account/user",
			// information about user
			Name:   p.User.UserName(),
			Domain: p.User.UserDomainName(),
			ID:     p.User.UserUUID(),
			Host: &cadf.Host{
				Address: httpext.GetRequesterIPFor(p.Request),
				Agent:   p.Request.Header.Get("User-Agent"),
			},
			// information about user's scope (only one of both will be filled)
			DomainID:          p.User.DomainScopeUUID(),
			DomainName:        p.User.DomainScopeName(),
			ProjectID:         p.User.ProjectScopeUUID(),
			ProjectName:       p.User.ProjectScopeName(),
			ProjectDomainName: p.User.ProjectScopeDomainName(),
			AppCredentialID:   p.User.ApplicationCredentialID(),
		}
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
		Initiator: initiator,
		Target:    p.Target.Render(),
		Observer: cadf.Resource{
			TypeURI: p.Observer.TypeURI,
			Name:    p.Observer.Name,
			ID:      p.Observer.ID,
		},
		RequestPath: p.Request.URL.String(),
	}
}

// GenerateUUID generates an UUID based on random numbers (RFC 4122).
// Failure will result in program termination.
func GenerateUUID() string {
	u, err := uuid.NewV4()
	if err != nil {
		logg.Fatal(err.Error())
	}
	return u.String()
}
