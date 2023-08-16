/******************************************************************************
*
*  Copyright 2023 SAP SE
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

package mock

import (
	"time"
)

// Clock is a deterministic clock for unit tests. It starts at the Unix epoch
// and only advances when Clock.StepBy() is called.
type Clock struct {
	currentTime int64
	listeners   []func(time.Time)
}

// NewClock starts a new Clock at the Unix epoch.
func NewClock() *Clock {
	return &Clock{currentTime: 0}
}

// AddListener registers a callback that will be called whenever the clock is
// advanced. It will also be called once immediately.
func (c *Clock) AddListener(callback func(time.Time)) {
	c.listeners = append(c.listeners, callback)
	callback(c.Now())
}

// Now reads the clock. This function can be used as a test double for time.Now().
func (c *Clock) Now() time.Time {
	return time.Unix(c.currentTime, 0).UTC()
}

// StepBy advances the clock by the given duration.
func (c *Clock) StepBy(d time.Duration) {
	c.currentTime += int64(d / time.Second)
	for _, callback := range c.listeners {
		callback(c.Now())
	}
}
