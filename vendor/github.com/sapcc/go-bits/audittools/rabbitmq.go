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
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/sapcc/hermes/pkg/cadf"
	"github.com/streadway/amqp"
)

// RabbitConnection represents a unique connection to some RabbitMQ server with
// an open Channel and a declared Queue.
type RabbitConnection struct {
	Inner   *amqp.Connection
	Channel *amqp.Channel
	Queue   amqp.Queue

	IsConnected     bool
	LastConnectedAt time.Time
}

// NewRabbitConnection returns a new RabbitConnection using the specified amqp URI
// and queue name.
func NewRabbitConnection(uri, queueName string) (*RabbitConnection, error) {
	//establish a connection with the RabbitMQ server
	conn, err := amqp.Dial(uri)
	if err != nil {
		return nil, fmt.Errorf("audittools: rabbitmq: failed to establish a connection with the server: %s", err.Error())
	}

	//open a unique, concurrent server channel to process the bulk of AMQP messages
	ch, err := conn.Channel()
	if err != nil {
		return nil, fmt.Errorf("audittools: rabbitmq: failed to open a channel: %s", err.Error())
	}

	//declare a queue to hold and deliver messages to consumers
	q, err := ch.QueueDeclare(
		queueName, // name of the queue
		false,     // durable: queue should survive cluster reset (or broker restart)
		false,     // autodelete when unused
		false,     // exclusive: queue only accessible by connection that declares and deleted when the connection closes
		false,     // noWait: the queue will assume to be declared on the server
		nil,       // arguments for advanced config
	)
	if err != nil {
		return nil, fmt.Errorf("audittools: rabbitmq: failed to declare a queue: %s", err.Error())
	}

	return &RabbitConnection{
		Inner:           conn,
		Channel:         ch,
		Queue:           q,
		IsConnected:     true,
		LastConnectedAt: time.Now(),
	}, nil
}

// Disconnect is a helper function for closing a RabbitConnection.
func (c *RabbitConnection) Disconnect() {
	c.Channel.Close()
	c.Inner.Close()
	c.IsConnected = false
}

// PublishEvent publishes a cadf.Event to a specific RabbitMQ Connection.
// A nil pointer for event parameter will return an error.
func (c *RabbitConnection) PublishEvent(event *cadf.Event) error {
	if !c.IsConnected {
		return errors.New("audittools: could not publish event: not connected to a RabbitMQ server")
	}
	if event == nil {
		return errors.New("audittools: could not publish event: got a nil pointer for 'event' parameter")
	}
	b, err := json.Marshal(event)
	if err != nil {
		return err
	}
	return c.Channel.Publish(
		"",           // exchange: publish to default
		c.Queue.Name, // routing key: same as queue name
		false,        // mandatory: don't publish if no queue is bound that matches the routing key
		false,        // immediate: don't publish if no consumer on the matched queue is ready to accept the delivery
		amqp.Publishing{
			ContentType: "text/plain",
			Body:        b,
		},
	)
}
