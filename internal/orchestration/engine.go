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

package orchestration

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/sapcc/go-bits/logg"
	"github.com/sapcc/keppel/internal/keppel"
)

//RegistryLauncher is an interface for starting keppel-registry instances
//for accounts. It is implemented by orchestration drivers that use type
//Engine (see documentation over there for details).
type RegistryLauncher interface {
	//Ensures that a keppel-registry process is running for the given account.
	//On success, returns the host:port where the registry's HTTP API can be
	//reached. Arguments:
	//
	//- All goroutines spawned by this action shall be tracked in `wg`.
	//- `processCtx` expires when this keppel-api instance is shutting down.
	//  All goroutines tracked by `wg` shall shutdown when this happens.
	//- `accountCtx` expires when the account is deleted.
	//- `notifyTerminated` shall be called when the keppel-registry instance is
	//  no longer available (either due to controlled shutdown on context expiry
	//  or because of an abnormal error).
	//
	//LaunchRegistry() is called by Engine.Run() and therefore always executes in
	//the same goroutine.
	LaunchRegistry(processCtx, accountCtx context.Context, account keppel.Account, wg *sync.WaitGroup, notifyTerminated func()) (string, error)
}

//Engine is a common baseline for orchestration drivers that manage real
//keppel-registry fleets (as opposed to mock drivers for use in testing). It
//implements the OrchestrationDriver interface, but defers the actual work of
//starting a keppel-registry instance to an OrchestrationStrategy instance.
type Engine struct {
	Launcher RegistryLauncher
	DB       keppel.DBAccessForOrchestrationDriver
	//filled by e.Run()
	hostRequestChan   chan<- hostRequest
	reportRequestChan chan<- reportRequest
}

type hostRequest struct {
	Account keppel.Account
	Result  chan<- string
}

//DoHTTPRequest implements the keppel.OrchestrationDriver interface.
func (e *Engine) DoHTTPRequest(account keppel.Account, r *http.Request, opts keppel.RequestOptions) (*http.Response, error) {
	//We don't mess with mutexes. The goroutine that executes `e.Run()` is
	//holding all the strings, and we only talk to it via `e.hostRequestChan`.

	resultChan := make(chan string, 1)
	e.hostRequestChan <- hostRequest{
		Account: account,
		Result:  resultChan,
	}

	r.URL.Scheme = "http"
	r.URL.Host = <-resultChan
	r.Host = ""

	client := http.DefaultClient
	if (opts & keppel.DoNotFollowRedirects) != 0 {
		client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		}
	}

	return client.Do(r)
}

type reportRequest struct {
	Result chan<- map[string]string
}

//ReportState is used by unit tests to inquire about the internal state of the
//Engine. The return value is a map indicating the running keppel-registry
//instances: Each entry maps an account name to its hostname.
func (e *Engine) ReportState() map[string]string {
	resultChan := make(chan map[string]string, 1)
	e.reportRequestChan <- reportRequest{resultChan}
	return <-resultChan
}

type registryTerminatedMessage struct {
	AccountName string
}

type registryState struct {
	Host             string
	CancelAccountCtx func()
}

//Run implements the keppel.OrchestrationDriver interface.
func (e *Engine) Run(ctx context.Context) (ok bool) {
	hostRequestChan := make(chan hostRequest)
	e.hostRequestChan = hostRequestChan
	reportRequestChan := make(chan reportRequest)
	e.reportRequestChan = reportRequestChan
	go e.ensureAllRegistriesAreRunning()

	processCtx, cancel := context.WithCancel(ctx)
	registryTerminatedChan := make(chan registryTerminatedMessage)
	var wg sync.WaitGroup
	runningRegistries := make(map[string]registryState) //key = account name

	//Overview of how this main loop works:
	//
	//1. We don't mess with mutexes. This goroutine is holding all the strings,
	//   and everyone else talks to it via `e.hostRequestChan`.
	//
	//2. Each call to LaunchRegistry() spawns a keppel-registry.
	//   LaunchRegistry() may launch some goroutines that manage the child
	//   process during its lifetime. Those goroutines are tracked by `wg`.
	//
	//3. When the original `ctx` expires, the aforementioned goroutines will
	//   cleanly shutdown because their `processCtx` expires. When they're done
	//   shutting down, the main loop (which is waiting on `wg`) unblocks and
	//   returns true.
	//
	//4. Abnormal termination of a running keppel-registry is not a fatal
	//   error. Its observing goroutine will call notifyTerminated() so that the
	//   main loop can update its bookkeeping accordingly. The next request
	//   for that Keppel account will launch a new registry.
	//
	ok = true
	for {
		select {
		case <-ctx.Done():
			logg.Debug("received interrupt - shutting down all goroutines...")
			//silence govet (cancel() is a no-op since ctx and therefore processCtx
			//has already expired, but govet cannot understand that and suspects a
			//context leak)
			cancel()

			//if we called wg.Wait() right now, we could block because children might be
			//trying to send termination notifications, but we won't read them -> set
			//up a bogus receiver that discards those notifications to unblock us
			go func() {
				for msg := range registryTerminatedChan {
					logg.Debug("[account=%s] discarded termination notice for keppel-registry", msg.AccountName)
				}
				//also send bogus responses to all pending requests to unblock any
				//HTTP handlers that called DoHTTPRequest() in the meantime
				//TODO: test coverage for this
				for req := range hostRequestChan {
					if req.Result != nil {
						req.Result <- ""
					}
				}
				for req := range reportRequestChan {
					req.Result <- nil
				}
			}()

			//wait on children
			logg.Debug("waiting for goroutines to shut down...")
			wg.Wait()
			logg.Debug("all goroutines shut down!")
			return ok

		case msg := <-registryTerminatedChan:
			logg.Debug("[account=%s] received termination notice for keppel-registry", msg.AccountName)
			//when we get this message, the goroutines for this registry should
			//already be shutting down, but better be safe than sorry and instruct
			//them to shut down explicitly; I don't want to get stuck in wg.Wait()
			//because some rogue goroutine didn't get the memo
			if state, exists := runningRegistries[msg.AccountName]; exists {
				state.CancelAccountCtx()
			}
			delete(runningRegistries, msg.AccountName)

		case req := <-reportRequestChan:
			logg.Debug("received engine report request")
			result := make(map[string]string, len(runningRegistries))
			for accountName, state := range runningRegistries {
				result[accountName] = state.Host
			}
			req.Result <- result

		case req := <-hostRequestChan:
			accountName := req.Account.Name
			logg.Debug("[account=%s] received host request", accountName)
			state, exists := runningRegistries[accountName]
			if !exists {
				logg.Debug("[account=%s] launching keppel-registry...", accountName)
				//start registry if not yet available
				accountCtx, cancelAccountCtx := context.WithCancel(context.Background())
				notifyTerminated := func() {
					logg.Debug("[account=%s] sending terminating notice for keppel-registry...", accountName)
					//NOTE: this callback runs in an arbitrary goroutine; use only
					//thread-safe operations!
					registryTerminatedChan <- registryTerminatedMessage{accountName}
				}
				host, err := e.Launcher.LaunchRegistry(
					processCtx, accountCtx, req.Account, &wg, notifyTerminated,
				)
				if err == nil {
					logg.Debug("[account=%s] keppel-registry running at %s", accountName, host)
					state = registryState{host, cancelAccountCtx}
					runningRegistries[accountName] = state
				} else {
					//TODO: test coverage for this branch
					logg.Error("[account=%s] failed to start keppel-registry: %s", accountName, err.Error())
					//failure to start new keppel-registries is considered a fatal error
					ok = false
					cancel()
					//silence govet (since we're not retaining cancelAccountCtx in
					//`runningRegistries`, it suspects a context leak)
					cancelAccountCtx()
				}
			}
			if req.Result != nil { //is nil when called from ensureAllRegistriesAreRunning()
				req.Result <- state.Host
			}
		}
	}
}

func (e *Engine) ensureAllRegistriesAreRunning() {
	for {
		accounts, err := e.DB.AllAccounts()
		if err != nil {
			logg.Error("failed to enumerate accounts: " + err.Error())
		} else {
			for _, account := range accounts {
				//this starts the keppel-registry process for the account if not yet running
				e.hostRequestChan <- hostRequest{Account: account}
			}
		}

		//polling interval
		time.Sleep(1 * time.Minute)
	}
}
