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
//
//This is below internal/orchestration/ rather than internal/drivers/ because
//we want to measure code coverage for it.
package localprocesses

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"

	"github.com/sapcc/go-bits/logg"
	"github.com/sapcc/keppel/internal/keppel"
	"github.com/sapcc/keppel/internal/orchestration"
)

//Driver (driver ID "local-processes") is a orchestration.RegistryLauncher
//that uses a fleet of keppel-registry processes running on the same host as
//keppel-api.
type Driver struct {
	//protects non-thread-safe members (StorageDriver, Config, ListenPorts, NextListenPort)
	Mutex *sync.RWMutex
	//configuration from NewOrchestrationDriver()
	StorageDriver keppel.StorageDriver
	Config        keppel.Configuration
	//configuration from RegistryLauncher.Init()
	Context          context.Context
	WaitGroup        *sync.WaitGroup
	ConnectivityChan chan<- orchestration.RegistryConnectivityMessage
	//internal state
	ListenPorts    map[string]uint16 //key = accountName
	NextListenPort uint16
}

func init() {
	keppel.RegisterOrchestrationDriver("local-processes", func(storage keppel.StorageDriver, cfg keppel.Configuration, db keppel.DBAccessForOrchestrationDriver) (keppel.OrchestrationDriver, error) {

		prepareBaseConfig()
		prepareCertBundle(cfg)

		return &orchestration.Engine{
			Launcher: &Driver{
				Mutex:         &sync.RWMutex{},
				StorageDriver: storage,
				Config:        cfg,
				ListenPorts:   map[string]uint16{},
				//could be made configurable if it becomes a problem, but right now it isn't
				NextListenPort: 10000,
			},
			DB: db,
		}, nil
	})
}

//Init implements the orchestration.RegistryLauncher interface.
func (d *Driver) Init(ctx context.Context, wg *sync.WaitGroup, connectivityChan chan<- orchestration.RegistryConnectivityMessage, allAccounts []keppel.Account) {
	d.Context = ctx
	d.WaitGroup = wg
	d.ConnectivityChan = connectivityChan

	for _, account := range allAccounts {
		d.LaunchRegistry(account)
	}
}

//LaunchRegistry implements the orchestration.RegistryLauncher interface.
func (d *Driver) LaunchRegistry(account keppel.Account) {
	host, exists := d.getExistingRegistryHost(account)
	if exists {
		return
	}

	host, err := d.launchRegistry(account)
	if err == nil {
		waitUntilRegistryRunning(host)
	}
	d.ConnectivityChan <- orchestration.RegistryConnectivityMessage{
		AccountName: account.Name,
		Host:        host,
		Err:         err,
	}
}

func (d *Driver) getExistingRegistryHost(account keppel.Account) (string, bool) {
	d.Mutex.RLock()
	defer d.Mutex.RUnlock()
	port, exists := d.ListenPorts[account.Name]
	return fmt.Sprintf("localhost:%d", port), exists
}

func (d *Driver) launchRegistry(account keppel.Account) (string, error) {
	d.Mutex.Lock()
	defer d.Mutex.Unlock()

	d.NextListenPort++
	port := d.NextListenPort
	logg.Info("[account=%s] starting keppel-registry on port %d", account.Name, port)
	d.ListenPorts[account.Name] = port

	cmd := exec.Command("keppel-registry", "serve", baseConfigPath)
	cmd.Env = os.Environ()

	for k, v := range d.StorageDriver.GetEnvironment(account) {
		cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", k, v))
	}
	for k, v := range d.Config.ToRegistryEnvironment() {
		cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", k, v))
	}

	cmd.Env = append(cmd.Env,
		fmt.Sprintf("REGISTRY_HTTP_ADDR=:%d", port),
		"REGISTRY_HTTP_SECRET="+account.RegistryHTTPSecret,
		"REGISTRY_LOG_FIELDS_KEPPEL.ACCOUNT="+account.Name,
		"REGISTRY_AUTH_TOKEN_ROOTCERTBUNDLE="+issuerCertBundlePath,
	)

	//the REGISTRY_LOG_FIELDS_KEPPEL.ACCOUNT variable (see above) adds the account
	//name to all log messages produced by the keppel-registry (it is therefore
	//safe to send its log directly to our own stdout)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	err := cmd.Start()
	if err != nil {
		return "", err
	}

	//manage the process during its lifetime
	d.WaitGroup.Add(2)
	processResult := make(chan error)
	go func() {
		defer d.WaitGroup.Done()
		processResult <- cmd.Wait()

		d.ConnectivityChan <- orchestration.RegistryConnectivityMessage{
			AccountName: account.Name,
			Host:        "", //signal termination
		}

		d.Mutex.Lock()
		defer d.Mutex.Unlock()
		delete(d.ListenPorts, account.Name)
	}()
	go func() {
		defer d.WaitGroup.Done()
		waitOnProcess(d.Context, account.Name, cmd, processResult)
	}()

	return fmt.Sprintf("localhost:%d", port), nil
}

func waitUntilRegistryRunning(host string) {
	duration := 2 * time.Millisecond
	for try := 0; try < 10; try++ {
		_, err := http.Get(host)
		if err == nil {
			return
		}
		time.Sleep(duration)
		duration *= 2
	}
}

func waitOnProcess(ctx context.Context, accountName string, cmd *exec.Cmd, processResult <-chan error) {
	var err error
	receivedProcessResult := false

	//Two options:
	//1. Subprocess terminates abnormally. -> recv from processResult completes
	//   before pc.Interrupt is fired.
	//2. Subprocess does not terminate. -> At some point, processContext expires (to
	//   start the shutdown of keppel-api itself) or accountContext expires
	//   (because the account has been deleted). Send SIGINT to the subprocess,
	//   then recv its processResult.
	select {
	case <-ctx.Done():
		logg.Debug("[account=%s] sending SIGINT to keppel-registry because of process shutdown...", accountName)
		cmd.Process.Signal(os.Interrupt)
	case err = <-processResult:
		receivedProcessResult = true
	}
	if !receivedProcessResult {
		err = <-processResult
	}

	//skip error "signal: interrupt" that occurs during normal SIGINT-triggered shutdown
	if err != nil && !isShutdownBecauseOfSIGINT(err) {
		logg.Error("[account=%s] keppel-registry exited with error: %s", accountName, err.Error())
	} else {
		logg.Debug("[account=%s] keppel-registry exited normally", accountName)
	}
}

func isShutdownBecauseOfSIGINT(err error) bool {
	ee, ok := err.(*exec.ExitError)
	if !ok {
		return false
	}
	ws := ee.ProcessState.Sys().(syscall.WaitStatus)
	return ws.Signaled() && ws.Signal() == syscall.SIGINT
}
