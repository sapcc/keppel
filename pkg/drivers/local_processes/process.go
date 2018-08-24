/******************************************************************************
*
*  Copyright 2018 SAP SE
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

package localprocesses

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"

	"github.com/sapcc/go-bits/logg"
	"github.com/sapcc/keppel/pkg/keppel"
)

//The base configuration file for keppel-registry, which contains all values
//that are shared among all keppel-registry processes.
const baseConfig = `
version: 0.1
log:
	accesslog:
		disabled: true
	level: info
http:
	addr: :10000
	headers:
		X-Content-Type-Options: [nosniff]
health:
	storagedriver:
		enabled: true
		interval: 10s
		threshold: 3
storage:
	cache:
		blobdescriptor: inmemory
`

var baseConfigPath = filepath.Join(chooseRuntimeDir(), "keppel/registry-base.yaml")
var issuerCertBundlePath = filepath.Join(chooseRuntimeDir(), "keppel/issuer-cert-bundle.pem")

func chooseRuntimeDir() string {
	if val := os.Getenv("XDG_RUNTIME_DIR"); val != "" {
		return val
	}
	return "/run"
}

func prepareBaseConfig() {
	cfg := strings.Replace(baseConfig, "\t", "    ", -1)

	err := os.MkdirAll(filepath.Dir(baseConfigPath), 0700)
	if err == nil {
		err = ioutil.WriteFile(baseConfigPath, []byte(cfg), 0644)
	}
	if err != nil {
		logg.Fatal(err.Error())
	}
}

func prepareCertBundle() {
	err := ioutil.WriteFile(issuerCertBundlePath, []byte(keppel.State.JWTIssuerCertPEM), 0600)
	if err != nil {
		logg.Fatal("cannot write issuer certificate bundle: " + err.Error())
	}
}

//Context state for launching keppel-registry processes.
type processContext struct {
	Context         context.Context
	WaitGroup       sync.WaitGroup
	ProcessExitChan chan<- processExitMessage
}

func (pc *processContext) startRegistry(account keppel.Account, port uint16) error {
	logg.Info("[account=%s] starting keppel-registry on port %d",
		account.Name, port)
	cmd := exec.Command("keppel-registry", "serve", baseConfigPath)
	cmd.Env = os.Environ()

	storageEnv, err := keppel.State.StorageDriver.GetEnvironment(account, keppel.State.AuthDriver)
	if err != nil {
		return err
	}
	cmd.Env = append(cmd.Env, storageEnv...)

	publicURL := keppel.State.Config.APIPublicURL.String()
	publicHost := keppel.State.Config.APIPublicHostname()
	cmd.Env = append(cmd.Env,
		fmt.Sprintf("REGISTRY_HTTP_ADDR=:%d", port),
		"REGISTRY_LOG_FIELDS_KEPPEL.ACCOUNT="+account.Name,
		fmt.Sprintf("REGISTRY_AUTH_TOKEN_REALM=%s/keppel/v1/auth", publicURL),
		"REGISTRY_AUTH_TOKEN_SERVICE="+publicHost,
		fmt.Sprintf("REGISTRY_AUTH_TOKEN_ISSUER=keppel-api@%s", publicHost),
		"REGISTRY_AUTH_TOKEN_ROOTCERTBUNDLE="+issuerCertBundlePath,
	)

	//the REGISTRY_LOG_FIELDS_KEPPEL.ACCOUNT variable (see above) adds the account
	//name to all log messages produced by the keppel-registry (it is therefore
	//safe to send its log directly to our own stdout)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	url := keppel.State.Config.DatabaseURL
	url.Path = "/" + account.PostgresDatabaseName()
	cmd.Env = append(cmd.Env, "REGISTRY_STORAGE_SWIFT-PLUS_POSTGRESURI="+url.String())

	err = cmd.Start()
	if err != nil {
		return err
	}

	//manage the process during its lifetime (see big comment in
	//driver.Run() for a high-level explanation)
	pc.WaitGroup.Add(2)
	processResult := make(chan error)
	go func() {
		defer pc.WaitGroup.Done()
		processResult <- cmd.Wait()
	}()
	go pc.waitOnProcess(account.Name, cmd, processResult)

	return nil
}

func (pc *processContext) waitOnProcess(accountName string, cmd *exec.Cmd, processResult <-chan error) {
	defer pc.WaitGroup.Done()
	var err error
	receivedProcessResult := false

	//Two options:
	//1. Subprocess terminates abnormally. -> recv from processResult completes
	//   before pc.Interrupt is fired.
	//2. Subprocess does not terminate. -> At some point, pc.Context expires (to
	//   start the shutdown of keppel-api itself). Send SIGINT to the subprocess,
	//   then recv its processResult.
	select {
	case <-pc.Context.Done():
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
	}
	if pc.Context.Err() == nil {
		//only send if someone is going to recv this
		pc.ProcessExitChan <- processExitMessage{accountName}
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
