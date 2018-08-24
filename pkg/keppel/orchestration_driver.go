/*******************************************************************************
*
* Copyright 2018 SAP SE
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

package keppel

import (
	"context"
	"errors"
	"net/http"
)

//OrchestrationDriver is the abstract interface for the orchestrator that
//manages the keppel-registry fleet.
type OrchestrationDriver interface {
	//ReadConfig unmarshals the configuration for this driver type into this
	//driver instance. The `unmarshal` function works exactly like in
	//UnmarshalYAML. This method shall only fail if the input data is malformed.
	//It shall not make any network requests; use Run for that.
	ReadConfig(unmarshal func(interface{}) error) error
	//DoHTTPRequest forwards the given request to the keppel-registry for the
	//given account. If this keppel-registry is not running, it may be launched
	//as a result of this call.
	DoHTTPRequest(account Account, r *http.Request) (*http.Response, error)
	//Run is called exactly once by main() to launch all persistent goroutines
	//used by the orchestrator. All resources shall be scoped on the given context.
	//Run() shall block until the context expires or a fatal error is encountered.
	//Returns whether a fatal error was encountered.
	Run(ctx context.Context) (ok bool)
}

var orchestrationDriverFactories = make(map[string]func() OrchestrationDriver)

//NewOrchestrationDriver creates a new OrchestrationDriver using one of the
//factory functions registered with RegisterOrchestrationDriver().
func NewOrchestrationDriver(name string) (OrchestrationDriver, error) {
	factory := orchestrationDriverFactories[name]
	if factory != nil {
		return factory(), nil
	}
	return nil, errors.New("no such orchestration driver: " + name)
}

//RegisterOrchestrationDriver registers an OrchestrationDriver. Call this from
//func init() of the package defining the OrchestrationDriver.
func RegisterOrchestrationDriver(name string, factory func() OrchestrationDriver) {
	if _, exists := orchestrationDriverFactories[name]; exists {
		panic("attempted to register multiple orchestration drivers with name = " + name)
	}
	orchestrationDriverFactories[name] = factory
}
