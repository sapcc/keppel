/******************************************************************************
*
*  Copyright 2019 SAP SE
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

package keppel

import (
	"encoding/json"
	"net/http"
	"os"
	"strconv"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/sapcc/go-bits/audittools"
	"github.com/sapcc/go-bits/logg"
	"github.com/sapcc/hermes/pkg/cadf"
	"github.com/streadway/amqp"
)

//Auditor is a component that forwards audit events to the appropriate logs.
//It is used by some of the API modules.
type Auditor interface {
	//Record forwards the given audit event to the audit log.
	//EventParameters.Observer will be filled by the auditor.
	Record(params audittools.EventParameters)
}

//AuditContext collects arguments that business logic methods need only for
//generating audit events.
type AuditContext struct {
	Authorization Authorization
	Request       *http.Request
}

////////////////////////////////////////////////////////////////////////////////
// janitorUserInfo

//janitorUserInfo is an audittools.NonStandardUserInfo representing the
//keppel-janitor (who does not have a corresponding OpenStack user). It can be
//used via `var JanitorAuthorization`.
type janitorUserInfo struct {
	TaskName string
}

//UserUUID implements the audittools.UserInfo interface.
func (janitorUserInfo) UserUUID() string {
	return "" //unused
}

//UserName implements the audittools.UserInfo interface.
func (janitorUserInfo) UserName() string {
	return "" //unused
}

//UserDomainName implements the audittools.UserInfo interface.
func (janitorUserInfo) UserDomainName() string {
	return "" //unused
}

//ProjectScopeUUID implements the audittools.UserInfo interface.
func (janitorUserInfo) ProjectScopeUUID() string {
	return "" //unused
}

//DomainScopeUUID implements the audittools.UserInfo interface.
func (janitorUserInfo) DomainScopeUUID() string {
	return "" //unused
}

//AsInitiator implements the audittools.NonStandardUserInfo interface.
func (u janitorUserInfo) AsInitiator() cadf.Resource {
	return cadf.Resource{
		TypeURI: "service/docker-registry/janitor-task",
		Name:    u.TaskName,
		Domain:  "keppel",
		ID:      u.TaskName,
	}
}

////////////////////////////////////////////////////////////////////////////////
// auditorImpl

var (
	auditEventPublishSuccessCounter = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "keppel_successful_auditevent_publish",
			Help: "Counter for successful audit event publish to RabbitMQ server.",
		})
	auditEventPublishFailedCounter = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "keppel_failed_auditevent_publish",
			Help: "Counter for failed audit event publish to RabbitMQ server.",
		})
)

//auditorImpl is the productive implementation of the Auditor interface.
//(We only expose the interface publicly because we want to be able to
//substitute a double in unit tests.)
type auditorImpl struct {
	OnStdout     bool
	EventSink    chan<- cadf.Event //nil if not wanted
	ObserverUUID string
}

//InitAuditTrail initializes a Auditor from the configuration variables
//found in the environment.
func InitAuditTrail() Auditor {
	prometheus.MustRegister(auditEventPublishSuccessCounter)
	prometheus.MustRegister(auditEventPublishFailedCounter)

	var eventSink chan cadf.Event
	if rabbitQueueName := os.Getenv("KEPPEL_AUDIT_RABBITMQ_QUEUE_NAME"); rabbitQueueName != "" {
		portStr := GetenvOrDefault("KEPPEL_AUDIT_RABBITMQ_PORT", "5672")
		port, err := strconv.Atoi(portStr)
		if err != nil {
			logg.Fatal("invalid value for KEPPEL_AUDIT_RABBITMQ_PORT: %s", err.Error())
		}
		rabbitURI := amqp.URI{
			Scheme:   "amqp",
			Host:     GetenvOrDefault("KEPPEL_AUDIT_RABBITMQ_HOSTNAME", "localhost"),
			Port:     port,
			Username: GetenvOrDefault("KEPPEL_AUDIT_RABBITMQ_USERNAME", "guest"),
			Password: GetenvOrDefault("KEPPEL_AUDIT_RABBITMQ_PASSWORD", "guest"),
			Vhost:    "/",
		}

		eventSink = make(chan cadf.Event, 20)
		auditEventPublishSuccessCounter.Add(0)
		auditEventPublishFailedCounter.Add(0)

		go audittools.AuditTrail{
			EventSink:           eventSink,
			OnSuccessfulPublish: func() { auditEventPublishSuccessCounter.Inc() },
			OnFailedPublish:     func() { auditEventPublishFailedCounter.Inc() },
		}.Commit(rabbitQueueName, rabbitURI)
	}

	silent := ParseBool(os.Getenv("KEPPEL_AUDIT_SILENT"))
	return auditorImpl{
		OnStdout:     !silent,
		EventSink:    eventSink,
		ObserverUUID: audittools.GenerateUUID(),
	}
}

//Record implements the Auditor interface.
func (a auditorImpl) Record(params audittools.EventParameters) {
	params.Observer.TypeURI = "service/docker-registry"
	params.Observer.Name = "keppel"
	params.Observer.ID = a.ObserverUUID

	event := audittools.NewEvent(params)

	if a.OnStdout {
		msg, _ := json.Marshal(event)
		logg.Other("AUDIT", string(msg))
	}

	if a.EventSink != nil {
		a.EventSink <- event
	}
}
