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

package test

import (
	"time"

	"github.com/alicebob/miniredis/v2"
)

// Clock is a deterministic clock for unit tests. It starts at the Unix epoch
// and only advances when Clock.Step() is called.
type Clock struct {
	currentTime int64
	MiniRedis   *miniredis.Miniredis
}

// Now reads the clock.
func (c *Clock) Now() time.Time {
	return time.Unix(c.currentTime, 0).UTC()
}

// Step advances the clock by one second.
func (c *Clock) Step() {
	c.currentTime++
	if c.MiniRedis != nil {
		c.MiniRedis.SetTime(c.Now())
	}
}

// StepBy advances the clock by the given duration.
func (c *Clock) StepBy(d time.Duration) {
	c.currentTime += int64(d / time.Second)
	if c.MiniRedis != nil {
		c.MiniRedis.SetTime(c.Now())
	}
}
