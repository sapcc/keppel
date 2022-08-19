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
	"net/url"
	"time"

	"github.com/sapcc/go-api-declarations/cadf"

	"github.com/sapcc/go-bits/logg"
)

// AuditTrail holds an event sink for receiving audit events and closure functions
// that are executed in case of successful and failed publishing.
type AuditTrail struct {
	EventSink           <-chan cadf.Event
	OnSuccessfulPublish func()
	OnFailedPublish     func()
}

// Commit takes a AuditTrail that receives audit events from an event sink and publishes them to
// a specific RabbitMQ Connection using the specified amqp URI and queue name.
// The OnSuccessfulPublish and OnFailedPublish closures are executed as per
// their respective case.
func (t AuditTrail) Commit(rabbitmqURI url.URL, rabbitmqQueueName string) {
	rc, err := NewRabbitConnection(rabbitmqURI, rabbitmqQueueName)
	if err != nil {
		logg.Error(err.Error())
	}

	sendEvent := func(e *cadf.Event) bool {
		rc = refreshConnectionIfClosedOrOld(rc, rabbitmqURI, rabbitmqQueueName)
		err := rc.PublishEvent(e)
		if err != nil {
			t.OnFailedPublish()
			logg.Error("audittools: failed to publish audit event with ID %q: %s", e.ID, err.Error())
			return false
		}
		t.OnSuccessfulPublish()
		return true
	}

	var pendingEvents []cadf.Event
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case e := <-t.EventSink:
			if successful := sendEvent(&e); !successful {
				pendingEvents = append(pendingEvents, e)
			}
		case <-ticker.C:
			for len(pendingEvents) > 0 {
				successful := false // until proven otherwise

				nextEvent := pendingEvents[0]
				if successful = sendEvent(&nextEvent); !successful {
					// One more try before giving up. We simply set rc to nil
					// and sendEvent() will take care of refreshing the
					// connection.
					time.Sleep(5 * time.Second)
					rc = nil
					successful = sendEvent(&nextEvent)
				}

				if successful {
					pendingEvents = pendingEvents[1:]
				} else {
					break
				}
			}
		}
	}
}

func refreshConnectionIfClosedOrOld(rc *RabbitConnection, uri url.URL, queueName string) *RabbitConnection {
	if !rc.IsNilOrClosed() {
		if time.Since(rc.LastConnectedAt) < 5*time.Minute {
			return rc
		}
		rc.Disconnect()
	}

	connection, err := NewRabbitConnection(uri, queueName)
	if err != nil {
		logg.Error(err.Error())
		return nil
	}

	return connection
}
