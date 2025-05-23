// SPDX-FileCopyrightText: 2019 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package audittools

import (
	"context"
	"net/url"
	"time"

	"github.com/sapcc/go-api-declarations/cadf"

	"github.com/sapcc/go-bits/logg"
)

type auditTrail struct {
	EventSink           <-chan cadf.Event
	OnSuccessfulPublish func()
	OnFailedPublish     func()
}

// Commit takes a AuditTrail that receives audit events from an event sink and publishes them to
// a specific RabbitMQ Connection using the specified amqp URI and queue name.
// The OnSuccessfulPublish and OnFailedPublish closures are executed as per their respective case.
//
// This function blocks the current goroutine forever. It should be invoked with the "go" keyword.
func (t auditTrail) Commit(ctx context.Context, rabbitmqURI url.URL, rabbitmqQueueName string) {
	rc, err := newRabbitConnection(rabbitmqURI, rabbitmqQueueName)
	if err != nil {
		logg.Error(err.Error())
	}

	sendEvent := func(e *cadf.Event) bool {
		rc = refreshConnectionIfClosedOrOld(rc, rabbitmqURI, rabbitmqQueueName)
		err := rc.PublishEvent(ctx, e)
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

func refreshConnectionIfClosedOrOld(rc *rabbitConnection, uri url.URL, queueName string) *rabbitConnection {
	if !rc.IsNilOrClosed() {
		if time.Since(rc.LastConnectedAt) < 5*time.Minute {
			return rc
		}
		rc.Disconnect()
	}

	connection, err := newRabbitConnection(uri, queueName)
	if err != nil {
		logg.Error(err.Error())
		return nil
	}

	return connection
}
