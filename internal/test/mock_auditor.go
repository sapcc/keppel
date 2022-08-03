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

package test

import (
	"encoding/json"
	"testing"

	"github.com/sapcc/go-api-declarations/cadf"
	"github.com/sapcc/go-bits/assert"
	"github.com/sapcc/go-bits/audittools"
)

var (
	//CADFReasonOK is a helper to make cadf.Event literals shorter.
	CADFReasonOK = cadf.Reason{
		ReasonType: "HTTP",
		ReasonCode: "200",
	}
)

// ToJSON is a more compact equivalent of json.Marshal() that panics on error
// instead of returning it, and which returns string instead of []byte.
func ToJSON(x interface{}) string {
	result, err := json.Marshal(x)
	if err != nil {
		panic(err.Error())
	}
	return string(result)
}

// Auditor is a test recorder that satisfies the keppel.Auditor interface.
type Auditor struct {
	events []cadf.Event
}

// Record implements the keppel.Auditor interface.
func (a *Auditor) Record(params audittools.EventParameters) {
	a.events = append(a.events, a.normalize(audittools.NewEvent(params)))
}

// ExpectEvents checks that the recorded events are equivalent to the supplied expectation.
func (a *Auditor) ExpectEvents(t *testing.T, expectedEvents ...cadf.Event) {
	t.Helper()
	if len(expectedEvents) == 0 {
		expectedEvents = nil
	} else {
		for idx, event := range expectedEvents {
			expectedEvents[idx] = a.normalize(event)
		}
	}
	assert.DeepEqual(t, "CADF events", a.events, expectedEvents)

	//reset state for next test
	a.events = nil
}

// IgnoreEventsUntilNow clears the list of recorded events, so that the next
// ExpectEvents() will only cover events generated after this point.
func (a *Auditor) IgnoreEventsUntilNow() {
	a.events = nil
}

func (a *Auditor) normalize(event cadf.Event) cadf.Event {
	//overwrite some attributes where we don't care about variance
	event.TypeURI = "http://schemas.dmtf.org/cloud/audit/1.0/event"
	event.ID = "00000000-0000-0000-0000-000000000000"
	event.EventTime = "2006-01-02T15:04:05.999999+00:00"
	event.EventType = "activity"
	if event.Initiator.TypeURI != "service/docker-registry/janitor-task" {
		//for janitor tasks, we *are* interested in the initiator because special
		//attributes like relevant GC policies get encoded there
		event.Initiator = cadf.Resource{}
	}
	event.Observer = cadf.Resource{}
	return event
}
