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
	//no mutexes necessary since LaunchRegistry() is guaranteed to
	//always run in the same goroutine
	StorageDriver  keppel.StorageDriver
	Config         keppel.Configuration
	NextListenPort uint16
}

func init() {
	keppel.RegisterOrchestrationDriver("local-processes", func(storage keppel.StorageDriver, cfg keppel.Configuration, db keppel.DBAccessForOrchestrationDriver) (keppel.OrchestrationDriver, error) {

		prepareBaseConfig()
		prepareCertBundle(cfg)

		//could be made configurable if it becomes a problem, but right now it isn't
		var nextListenPort uint16 = 10000

		return &orchestration.Engine{
			Launcher: &Driver{storage, cfg, nextListenPort},
			DB:       db,
		}, nil
	})
}

//LaunchRegistry implements the orchestration.RegistryLauncher interface.
func (d *Driver) LaunchRegistry(processCtx, accountCtx context.Context, account keppel.Account, wg *sync.WaitGroup, notifyTerminated func()) (string, error) {
	d.NextListenPort++
	port := d.NextListenPort
	logg.Info("[account=%s] starting keppel-registry on port %d", account.Name, port)

	cmd := exec.Command("keppel-registry", "serve", baseConfigPath)
	cmd.Env = os.Environ()

	storageEnv, err := d.StorageDriver.GetEnvironment(account)
	if err != nil {
		return "", err
	}
	for k, v := range storageEnv {
		cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", k, v))
	}
	for k, v := range d.Config.ToRegistryEnvironment() {
		cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", k, v))
	}

	cmd.Env = append(cmd.Env,
		fmt.Sprintf("REGISTRY_HTTP_ADDR=:%d", port),
		"REGISTRY_LOG_FIELDS_KEPPEL.ACCOUNT="+account.Name,
		"REGISTRY_AUTH_TOKEN_ROOTCERTBUNDLE="+issuerCertBundlePath,
	)

	//the REGISTRY_LOG_FIELDS_KEPPEL.ACCOUNT variable (see above) adds the account
	//name to all log messages produced by the keppel-registry (it is therefore
	//safe to send its log directly to our own stdout)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	err = cmd.Start()
	if err != nil {
		return "", err
	}

	//manage the process during its lifetime
	wg.Add(2)
	processResult := make(chan error)
	go func() {
		defer wg.Done()
		processResult <- cmd.Wait()
		notifyTerminated()
	}()
	go func() {
		defer wg.Done()
		waitOnProcess(processCtx, accountCtx, account.Name, cmd, processResult)
	}()

	//give the registry process some time to come up
	host := fmt.Sprintf("localhost:%d", port)
	waitUntilRegistryRunning(host)
	return host, nil
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

func waitOnProcess(processCtx, accountCtx context.Context, accountName string, cmd *exec.Cmd, processResult <-chan error) {
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
	case <-processCtx.Done():
		logg.Debug("[account=%s] sending SIGINT to keppel-registry because of process shutdown...", accountName)
		cmd.Process.Signal(os.Interrupt)
	case <-accountCtx.Done():
		logg.Debug("[account=%s] sending SIGINT to keppel-registry because of account deletion...", accountName)
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
