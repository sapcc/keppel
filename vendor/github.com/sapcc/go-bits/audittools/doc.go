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

/*
Package audittools provides helper functions for establishing a connection to
a RabbitMQ server (with sane defaults) and publishing messages to it.

It comes with a ready-to-use implementation that can be used to publish the audit trail
of an application to a RabbitMQ server, or it can be used as a reference to build your
own.

One usage of the aforementioned implementation can be:
	package yourPackageName

	import (
		"net/url"
		...

		"github.com/sapcc/go-bits/audittools"
		...
	)

	var eventPublishSuccessCounter = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "yourApplication_successful_auditevent_publish",
			Help: "Counter for successful audit event publish to RabbitMQ server.",
		},
	)
	var	eventPublishFailedCounter = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "yourApplication_failed_auditevent_publish",
			Help: "Counter for failed audit event publish to RabbitMQ server.",
		},
	)

	var EventSink chan<- cadf.Event

	func init() {
		s := make(chan cadf.Event, 20)
		EventSink = s

		onSuccessFunc := func() {
			eventPublishSuccessCounter.Inc()
		}
		onFailFunc() := func() {
			eventPublishFailedCounter.Inc()
		}

		rabbitmqQueueName := "down-the-rabbit-hole"
		rabbitmqURI := url.URL{
			Scheme: "amqp",
			Host:   net.JoinHostPort("localhost", "5672"),
			User:   url.UserPassword("guest", "guest"),
			Path:   "/",
		}

		go audittools.AuditTrail{
			EventSink:           s,
			OnSuccessfulPublish: onSuccessFunc,
			OnFailedPublish:     onFailFunc,
		}.Commit(rabbitmqURI.String(), rabbitmqQueueName)
	}

	func someFunction() {
		event := generateCADFEvent()
		if EventSink != nil {
			EventSink <- event
		}
	}
*/
package audittools
