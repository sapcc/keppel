/*******************************************************************************
*
* Copyright 2022 SAP SE
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

// Package cadf provides data structures for working with CADF events as per the CADF spec.
//
// SAP CCloud developers wishing to publish audit events to Hermes are advised
// to use the github.com/sapcc/go-bits/audittools package.
package cadf

import "encoding/json"

// Event contains the CADF event according to CADF spec, section 6.6.1 Event (data)
// Extensions: requestPath (OpenStack, IBM), initiator.project_id/domain_id
// Omissions: everything that we do not use or not expose to API users
//
// The JSON annotations are for parsing the result from ElasticSearch AND for generating the Hermes API response
type Event struct {
	// CADF Event Schema
	TypeURI string `json:"typeURI"`

	// CADF generated event id
	ID string `json:"id"`

	// CADF generated timestamp
	EventTime string `json:"eventTime"`

	// Characterizes events: eg. activity
	EventType string `json:"eventType"`

	// CADF action mapping for GET call on an OpenStack REST API
	Action Action `json:"action"`

	// Outcome of REST API call, eg. success/failure
	Outcome Outcome `json:"outcome"`

	// Standard response for successful HTTP requests
	Reason Reason `json:"reason,omitempty"`

	// CADF component that contains the RESOURCE
	// that initiated, originated, or instigated the event's
	// ACTION, according to the OBSERVER
	Initiator Resource `json:"initiator"`

	// CADF component that contains the RESOURCE
	// against which the ACTION of a CADF Event
	// Record was performed, was attempted, or is
	// pending, according to the OBSERVER.
	Target Resource `json:"target"`

	// CADF component that contains the RESOURCE
	// that generates the CADF Event Record based on
	// its observation (directly or indirectly) of the Actual Event
	Observer Resource `json:"observer"`

	// Attachment contains self-describing extensions to the event
	Attachments []Attachment `json:"attachments,omitempty"`

	// Request path on the OpenStack service REST API call
	RequestPath string `json:"requestPath,omitempty"`
}

// Resource contains attributes describing a (OpenStack-) Resource
type Resource struct {
	TypeURI   string `json:"typeURI"`
	Name      string `json:"name,omitempty"`
	Domain    string `json:"domain,omitempty"`
	ID        string `json:"id,omitempty"`
	Addresses []struct {
		URL  string `json:"url"`
		Name string `json:"name,omitempty"`
	} `json:"addresses,omitempty"`
	Host        *Host        `json:"host,omitempty"`
	Attachments []Attachment `json:"attachments,omitempty"`
	// project_id and domain_id are OpenStack extensions (introduced by Keystone and keystone(audit)middleware)
	ProjectID string `json:"project_id,omitempty"`
	DomainID  string `json:"domain_id,omitempty"`
	// project_name, project_domain_name, domain_name, application_credential_id, request_id and global_request_id
	// are Hermes extensions for initiator resources only (not for target or observer)
	ProjectName       string `json:"project_name,omitempty"`
	ProjectDomainName string `json:"project_domain_name,omitempty"`
	DomainName        string `json:"domain_name,omitempty"`
	AppCredentialID   string `json:"application_credential_id,omitempty"`
	RequestID         string `json:"request_id,omitempty"`
	GlobalRequestID   string `json:"global_request_id,omitempty"`
}

// Reason contains HTTP Code and Type, and is optional in the CADF spec
type Reason struct {
	ReasonType string `json:"reasonType,omitempty"`
	ReasonCode string `json:"reasonCode,omitempty"`
}

// Host contains optional Information about the Host
type Host struct {
	ID       string `json:"id,omitempty"`
	Address  string `json:"address,omitempty"`
	Agent    string `json:"agent,omitempty"`
	Platform string `json:"platform,omitempty"`
}

// Attachment contains self-describing extensions to the event
type Attachment struct {
	// Note: name is optional in CADF spec. to permit unnamed attachments
	Name string `json:"name,omitempty"`
	// this is messed-up in the spec.: the schema and examples says contentType. But the text often refers to typeURI.
	// Using typeURI would surely be more consistent. OpenStack uses typeURI, IBM supports both
	// (but forgot the name property)
	TypeURI string `json:"typeURI"`
	// Content contains the payload of the attachment. In theory this means any type.
	// In practise we have to decide because otherwise ES does based one first value
	// An interface allows arrays of json content. This should be json in the content.
	//
	// Use func NewJSONAttachment() to create well-formed attachments that Hermes can consume.
	Content any `json:"content"`
}

// NewJSONAttachment creates an Attachment of type "mime:application/json" by
// serializing the given content as JSON.
//
// If an error is returned, it will be from json.Marshal().
func NewJSONAttachment(name string, content any) (Attachment, error) {
	switch content := content.(type) {
	case json.RawMessage:
		return Attachment{
			Name:    name,
			TypeURI: "mime:application/json",
			Content: string(content),
		}, nil
	default:
		buf, err := json.Marshal(content)
		if err != nil {
			return Attachment{}, err
		}
		return Attachment{
			Name:    name,
			TypeURI: "mime:application/json",
			Content: string(buf),
		}, nil
	}
}
