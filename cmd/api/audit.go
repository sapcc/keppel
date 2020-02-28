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

package apicmd

import (
	"encoding/json"
	"os"
	"strconv"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/sapcc/go-bits/audittools"
	"github.com/sapcc/go-bits/logg"
	"github.com/sapcc/hermes/pkg/cadf"
	"github.com/sapcc/keppel/internal/keppel"
)

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

type auditor struct {
	OnStdout     bool
	EventSink    chan<- cadf.Event //nil if not wanted
	ObserverUUID string
}

func initAuditTrail() keppel.Auditor {
	prometheus.MustRegister(auditEventPublishSuccessCounter)
	prometheus.MustRegister(auditEventPublishFailedCounter)

	var eventSink chan cadf.Event
	if rabbitURI := os.Getenv("KEPPEL_AUDIT_RABBITMQ_URI"); rabbitURI != "" {
		eventSink = make(chan cadf.Event, 20)
		auditEventPublishSuccessCounter.Add(0)
		auditEventPublishFailedCounter.Add(0)

		go audittools.AuditTrail{
			EventSink:           eventSink,
			OnSuccessfulPublish: func() { auditEventPublishSuccessCounter.Inc() },
			OnFailedPublish:     func() { auditEventPublishFailedCounter.Inc() },
		}.Commit(rabbitURI, keppel.MustGetenv("KEPPEL_AUDIT_RABBITMQ_QUEUE_NAME"))
	}

	silent, _ := strconv.ParseBool(os.Getenv("KEPPEL_AUDIT_SILENT"))
	return auditor{
		OnStdout:     !silent,
		EventSink:    eventSink,
		ObserverUUID: audittools.GenerateUUID(),
	}
}

//Record implements the keppel.Auditor interface.
func (a auditor) Record(params audittools.EventParameters) {
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
