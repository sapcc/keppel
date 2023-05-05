/*******************************************************************************
*
* Copyright 2023 SAP SE
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

// Option is a configuration option for a Job. Currently, only the number of
// goroutines can be configured, but more options could be added in the future.
//
// This type is an implementation of the Functional Options pattern, see e.g.
// <https://github.com/tmrts/go-patterns/blob/master/idiom/functional-options.md>
type Option func(*jobConfig)

type jobConfig struct {
	NumGoroutines uint
}

func newJobConfig(opts []Option) jobConfig {
	//default values
	cfg := jobConfig{
		NumGoroutines: 1,
	}

	//apply specific overrides
	for _, opt := range opts {
		opt(&cfg)
	}
	return cfg
}

// NumGoroutines is an option for a Job that allows the Job to use multiple
// goroutines, up to the specified number. The default value is 1, meaning that
// no concurrency will be employed.
func NumGoroutines(n uint) Option {
	return func(cfg *jobConfig) {
		cfg.NumGoroutines = n
	}
}
