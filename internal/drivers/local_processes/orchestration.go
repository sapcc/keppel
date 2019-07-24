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

//Package localprocesses provides the orchestration driver "local-processes"
//which starts keppel-registry processes on the same process where keppel-api
//is running.
package localprocesses

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/sapcc/go-bits/logg"
	"github.com/sapcc/keppel/internal/keppel"
)

type driver struct {
	storage            keppel.StorageDriver
	getPortRequestChan chan getPortRequest
	//the following fields are only accessed by Run(), so no locking is necessary^
	listenPorts    map[string]uint16
	nextListenPort uint16
}

func init() {
	keppel.RegisterOrchestrationDriver("local-processes", func(storage keppel.StorageDriver) (keppel.OrchestrationDriver, error) {
		return &driver{
			storage:            storage,
			getPortRequestChan: make(chan getPortRequest),
			listenPorts:        make(map[string]uint16),
			nextListenPort:     10000, //TODO make configurable?
		}, nil
	})
}

type getPortRequest struct {
	Account keppel.Account
	Result  chan<- uint16
}

//DoHTTPRequest implements the keppel.OrchestrationDriver interface.
func (d *driver) DoHTTPRequest(account keppel.Account, r *http.Request) (*http.Response, error) {
	resultChan := make(chan uint16, 1)
	d.getPortRequestChan <- getPortRequest{
		Account: account,
		Result:  resultChan,
	}

	r.URL.Scheme = "http"
	r.URL.Host = fmt.Sprintf("localhost:%d", <-resultChan)
	return http.DefaultClient.Do(r)
}

type processExitMessage struct {
	AccountName string
}

//Run implements the keppel.OrchestrationDriver interface.
func (d *driver) Run(ctx context.Context) (ok bool) {
	prepareBaseConfig()
	prepareCertBundle()
	go d.ensureAllRegistriesAreRunning()

	innerCtx, cancel := context.WithCancel(ctx)
	processExitChan := make(chan processExitMessage)
	pc := processContext{
		StorageDriver:   d.storage,
		Context:         innerCtx,
		ProcessExitChan: processExitChan,
	}

	//Overview of how this main loop works:
	//
	//1. Each call to pc.startRegistry() spawns a keppel-registry process.
	//   pc.startRegistry() will launch some goroutines that manage the child
	//   process during its lifetime. Those goroutines are tracked by
	//   pc.WaitGroup.
	//
	//2. When the original ctx expires, the aforementioned goroutines will
	//   cleanly shutdown all keppel-registry processes. When they're done
	//   shutting down, the main loop (which is waiting on pc.WaitGroup) unblocks
	//   and returns true.
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
			//silence govet (cancel() is a no-op since ctx and therefore innerCtx has
			//already expired, but govet cannot understand that and suspects a context leak)
			cancel()
			//wait on child processes
			pc.WaitGroup.Wait()
			return ok

		case msg := <-processExitChan:
			delete(d.listenPorts, msg.AccountName)

		case req := <-d.getPortRequestChan:
			port, exists := d.listenPorts[req.Account.Name]
			if !exists {
				d.nextListenPort++
				port = d.nextListenPort
				err := pc.startRegistry(req.Account, port)
				if err != nil {
					logg.Error("[account=%s] failed to start keppel-registry: %s", req.Account.Name, err.Error())
					//failure to start new keppel-registries is considered a fatal error
					ok = false
					cancel()
				}
			}
			d.listenPorts[req.Account.Name] = port
			if req.Result != nil { //is nil when called from ensureAllRegistriesAreRunning()
				req.Result <- port
			}
		}
	}
}

func (d *driver) ensureAllRegistriesAreRunning() {
	for {
		var accounts []keppel.Account
		_, err := keppel.State.DB.Select(&accounts, `SELECT * FROM accounts`)
		if err != nil {
			logg.Error("failed to enumerate accounts: " + err.Error())
			accounts = nil
		}
		for _, account := range accounts {
			//this starts the keppel-registry process for the account if not yet running
			d.getPortRequestChan <- getPortRequest{Account: account}
		}

		//polling interval
		time.Sleep(1 * time.Minute)
	}
}
