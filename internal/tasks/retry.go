/******************************************************************************
*
*  Copyright 2020 SAP SE
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

package tasks

import (
	"context"
	"errors"
	"time"
)

type retryOpts struct {
	Period      time.Duration
	MaxAttempts int
}

//defaultRetryOpts is the default setting for retryOpts. This is in a variable
//so the unit tests can dial it back to avoid useless waits.
var defaultRetryOpts = retryOpts{
	Period:      5 * time.Second,
	MaxAttempts: 10,
}

//retry will run action every retryOpts.period until:
//  1. the action is successful (err == nil)
//  2. the retryOpts.maxAttempts elapses
//  3. the context expires
func retry(ctx context.Context, o retryOpts, action func() error) error {
	var err error
	i := 0

LOOP:
	for {
		select {
		case <-ctx.Done():
			break LOOP
		default:
			if i == o.MaxAttempts {
				break LOOP
			}
			if err = action(); err == nil {
				return nil
			}
			i++
			time.Sleep(o.Period)
		}
	}

	if err != nil {
		return err
	}
	return errors.New("action could not be completed successfully within the given period and iterations")
}
