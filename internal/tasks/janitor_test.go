/*******************************************************************************
*
* Copyright 2022 SAP SE
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

package tasks

import (
	"testing"
	"time"
)

func TestAddJitter(t *testing.T) {
	baseDuration := 60 * time.Minute
	lowerBound := baseDuration * 9 / 10
	upperBound := baseDuration * 11 / 10

	smallerCount := 0
	biggerCount := 0

	//take 1000 samples of addJitter()
	for idx := 0; idx < 1000; idx++ {
		d := addJitter(baseDuration)
		//no sample should be outside the +/-10% range of the base duration
		if d < lowerBound {
			t.Errorf("expected jittered duration to be above %s, but got %s", lowerBound, d)
		}
		if d > upperBound {
			t.Errorf("expected jittered duration to be below %s, but got %s", upperBound, d)
		}
		//count samples into two simple buckets
		if d < baseDuration {
			smallerCount++
		}
		if d > baseDuration {
			biggerCount++
		}
	}

	//very simple sanity-check: both buckets should have ~500 samples
	if smallerCount < 450 {
		t.Errorf("expected half of the samples to be smaller than %s, but got only %.2f%% smaller samples",
			baseDuration, float64(smallerCount)/1000.)
	}
	if biggerCount < 450 {
		t.Errorf("expected half of the samples to be bigger than %s, but got only %.2f%% bigger samples",
			baseDuration, float64(biggerCount)/1000.)
	}
}
