package cadf

import (
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/gofrs/uuid"
	"github.com/sapcc/go-bits/gopherpolicy"
)

// Event contains the CADF event according to CADF spec, section 6.6.1 Event (data)
// Extensions: requestPath (OpenStack, IBM), initiator.project_id/domain_id
// Omissions: everything that we do not use or not expose to API users
//  The JSON annotations are for parsing the result from ElasticSearch AND for generating the Hermes API response
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
	Action string `json:"action"`

	// Outcome of REST API call, eg. success/failure
	Outcome string `json:"outcome"`

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
	Name      string `json:"name"`
	Domain    string `json:"domain,omitempty"`
	ID        string `json:"id"`
	Addresses []struct {
		URL  string `json:"url"`
		Name string `json:"name,omitempty"`
	} `json:"addresses,omitempty"`
	Host        *Host        `json:"host,omitempty"`
	Attachments []Attachment `json:"attachments,omitempty"`
	// project_id and domain_id are OpenStack extensions (introduced by Keystone and keystone(audit)middleware)
	ProjectID string `json:"project_id,omitempty"`
	DomainID  string `json:"domain_id,omitempty"`
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
	Content interface{} `json:"content"`
}

// Timestamp for proper CADF format
type Timestamp struct {
	time.Time
}

// MarshalJSON for cadf format time
func (t Timestamp) MarshalJSON() ([]byte, error) {
	return []byte(t.Format(`"2006-01-02T15:04:05.999Z"`)), nil
}

// UnmarshalJSON for cadf format time
func (t *Timestamp) UnmarshalJSON(data []byte) (err error) {
	t.Time, err = time.Parse(`"2006-01-02T15:04:05.999Z"`, string(data))
	return
}

//EventParams contains parameters for creating an audit event.
type EventParams struct {
	Token           *gopherpolicy.Token
	Request         *http.Request
	ReasonCode      int
	Time            string
	ObserverUUID    string
	DomainID        string
	ProjectID       string
	ServiceType     string
	ServiceTypeURI  string
	ResourceName    string
	ResourceTypeURI string
	RejectReason    string
}

// NewEvent takes the necessary parameters and returns a new audit event.
func (p EventParams) NewEvent() Event {
	targetID := p.ProjectID
	if p.ProjectID == "" {
		targetID = p.DomainID
	}

	outcome := "failure"
	if p.ReasonCode == http.StatusOK {
		outcome = "success"
	}

	return Event{
		TypeURI:   "http://schemas.dmtf.org/cloud/audit/1.0/event",
		ID:        generateUUID(),
		EventTime: p.Time,
		EventType: "activity", // Activity is all we use for auditing. Activity/Monitor/Control
		Action:    "update",   // Create/Update/Delete
		Outcome:   outcome,    // Success/Failure/Pending
		Reason: Reason{
			ReasonType: "HTTP",
			ReasonCode: strconv.Itoa(p.ReasonCode),
		},
		Initiator: Resource{
			TypeURI:   "service/security/account/user",
			Name:      p.Token.Context.Auth["user_name"],
			ID:        p.Token.Context.Auth["user_id"],
			Domain:    p.Token.Context.Auth["domain_name"],
			DomainID:  p.Token.Context.Auth["domain_id"],
			ProjectID: p.Token.Context.Auth["project_id"],
			Host: &Host{
				Address: StripPort(p.Request.RemoteAddr),
				Agent:   p.Request.Header.Get("User-Agent"),
			},
		},
		Target: Resource{
			TypeURI:   p.ResourceTypeURI,
			ID:        targetID,
			DomainID:  p.DomainID,
			ProjectID: p.ProjectID,
		},
		Observer: Resource{
			TypeURI: p.ServiceTypeURI,
			Name:    p.ServiceType,
			ID:      p.ObserverUUID,
		},
		RequestPath: p.Request.URL.String(),
	}
}

//Generate an UUID based on random numbers (RFC 4122).
func generateUUID() string {
	u := uuid.Must(uuid.NewV4())
	return u.String()
}

//StripPort returns a host without the port number
func StripPort(hostPort string) string {
	host, _, err := net.SplitHostPort(hostPort)
	if err == nil {
		return host
	}
	return hostPort
}
