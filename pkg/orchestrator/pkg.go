/******************************************************************************
*
*  Copyright 2018 Stefan Majewsky <majewsky@gmx.net>
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

package orchestrator

import (
	"context"

	"github.com/sapcc/go-bits/logg"
	"github.com/sapcc/keppel/pkg/database"
)

//GetPortRequest can be sent into `type API` to ask the orchestrator for the
//TCP port where the keppel-registry process for the given account is
//listening. The orchestrator will take care of starting the keppel-registry
//process if it is not running yet.
type GetPortRequest struct {
	Account database.Account
	Result  chan<- uint16
}

type processExitMessage struct {
	AccountName string
}

//API is used by HTTP handlers to communicate with the Orchestrator
//which manages the keppel-registry processes.
type API struct {
	GetPortRequestChan chan<- GetPortRequest
}

//Orchestrator is managing keppel-registry processes on the main loop.
type Orchestrator struct {
	getPortRequestChan <-chan GetPortRequest
	listenPorts        map[string]uint16
	nextListenPort     uint16
}

//NewOrchestrator prepares a new Orchestrator instance.
func NewOrchestrator() (*Orchestrator, *API) {
	gprChan := make(chan GetPortRequest)
	return &Orchestrator{
			getPortRequestChan: gprChan,
			listenPorts:        make(map[string]uint16),
			nextListenPort:     10000, //TODO make configurable?
		}, &API{
			GetPortRequestChan: gprChan,
		}
}

//Run runs this orchestrator until the given context expires or until a fatal
//error is encountered. Returns whether a fatal error was encountered.
func (o *Orchestrator) Run(ctx context.Context) (ok bool) {
	interruptChan := make(chan struct{})
	processExitChan := make(chan processExitMessage)
	pc := processContext{
		Interrupt:       interruptChan,
		ProcessExitChan: processExitChan,
	}

	//Overview of how this main loop works:
	//
	//1. Each call to pc.startRegistry() spawns a keppel-registry process.
	//   pc.startRegistry() will launch some goroutines that manage the child
	//   process during its lifetime. Those goroutines are tracked by
	//   pc.WaitGroup.
	//
	//2. When the original ctx expires, the main loop closes interruptChan to
	//   instruct the aforementioned goroutines to cleanly shutdown all
	//   keppel-registry processes. When they're done shutting down,
	//   the main loop (which is waiting on pc.WaitGroup) unblocks and returns true.
	//
	//3. Abnormal termination of a single keppel-registry process is not a fatal
	//   error. Its observing goroutine will send a processExitMessage that the
	//   main loop uses to update its bookkeeping accordingly. The next request
	//   for that Keppel account will launch a new keppel-registry process.
	//
	ok = true
	for {
		select {
		case <-ctx.Done():
			//signal to child processes to exit
			close(interruptChan)
			//wait on child processes
			pc.WaitGroup.Wait()
			return ok

		case msg := <-processExitChan:
			delete(o.listenPorts, msg.AccountName)

		case req := <-o.getPortRequestChan:
			port, exists := o.listenPorts[req.Account.Name]
			if !exists {
				o.nextListenPort++
				port = o.nextListenPort
				err := pc.startRegistry(req.Account, port)
				if err != nil {
					logg.Error("[account=%s] failed to start keppel-registry: %s", req.Account.Name, err.Error())
					//failure to start new keppel-registries is considered a fatal error
					ok = false
					close(interruptChan)
				}
			}
			o.listenPorts[req.Account.Name] = port
			req.Result <- port
		}
	}
}
