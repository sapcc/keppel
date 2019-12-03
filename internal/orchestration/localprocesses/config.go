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
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

	"github.com/sapcc/go-bits/logg"
	"github.com/sapcc/keppel/internal/keppel"
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
	relativeurls: true
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
	delete:
		enabled: true
compatibility:
	schema1:
		enabled: false
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
	//note to self: YAML does not allow tabs for indentation
	cfg := strings.Replace(baseConfig, "\t", "    ", -1)

	err := os.MkdirAll(filepath.Dir(baseConfigPath), 0700)
	if err == nil {
		err = ioutil.WriteFile(baseConfigPath, []byte(cfg), 0644)
	}
	if err != nil {
		logg.Fatal(err.Error())
	}
}

func prepareCertBundle(cfg keppel.Configuration) {
	err := ioutil.WriteFile(issuerCertBundlePath, []byte(cfg.JWTIssuerCertPEM), 0600)
	if err != nil {
		logg.Fatal("cannot write issuer certificate bundle: " + err.Error())
	}
}
