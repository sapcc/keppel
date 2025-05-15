// SPDX-FileCopyrightText: 2022 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

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

	// take 1000 samples of addJitter()
	for range 1000 {
		d := addJitter(baseDuration)
		// no sample should be outside the +/-10% range of the base duration
		if d < lowerBound {
			t.Errorf("expected jittered duration to be above %s, but got %s", lowerBound, d)
		}
		if d > upperBound {
			t.Errorf("expected jittered duration to be below %s, but got %s", upperBound, d)
		}
		// count samples into two simple buckets
		if d < baseDuration {
			smallerCount++
		}
		if d > baseDuration {
			biggerCount++
		}
	}

	// very simple sanity-check: both buckets should have ~500 samples
	if smallerCount < 450 {
		t.Errorf("expected half of the samples to be smaller than %s, but got only %.2f%% smaller samples",
			baseDuration, 100*float64(smallerCount)/1000.)
	}
	if biggerCount < 450 {
		t.Errorf("expected half of the samples to be bigger than %s, but got only %.2f%% bigger samples",
			baseDuration, 100*float64(biggerCount)/1000.)
	}
}
