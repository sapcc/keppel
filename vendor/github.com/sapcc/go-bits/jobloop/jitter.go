/*******************************************************************************
*
* Copyright 2025 SAP SE
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

package jobloop

import (
	"math/rand"
	"time"
)

// Jitter is a strategy for randomizing task recurrence intervals.
//
// When a background job performs a certain task for each object on a specific
// interval, it is usually desirable to not schedule the next task to take
// place after exactly that interval.
//
// For example, consider a blob storage service with a background job to check
// the validity of each individual blob every 24 hours. If a lot of blobs are
// uploaded at once, adhering to the exact 24-hour interval will cause high
// load in the system every day at the same time.
//
// To counteract this, we recommend that the calculation of a followup task
// deadline use jitter like this:
//
//	// instead of this...
//	blob.NextValidationAt = now.Add(24 * time.Hour)
//	// ...do this
//	blob.NextValidationAt = now.Add(jobloop.DefaultJitter(24 * time.Hour))
type Jitter func(time.Duration) time.Duration

// DefaultJitter returns a random duration within +/- 10% of the requested value.
// See explanation on type Jitter for when this is useful.
func DefaultJitter(d time.Duration) time.Duration {
	//nolint:gosec // This is not crypto-relevant, so math/rand is okay.
	r := rand.Float64() //NOTE: 0 <= r < 1
	return time.Duration(float64(d) * (0.9 + 0.2*r))
}

// NoJitter returns the input value unchanged.
//
// This can be used in place of DefaultJitter to ensure deterministic behavior in tests.
func NoJitter(d time.Duration) time.Duration {
	return d
}
