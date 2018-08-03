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

package main

import (
	"context"
	"crypto/tls"
	"net/http"
	"os"
	"os/signal"
	"regexp"
	"syscall"

	"github.com/gorilla/mux"
	"github.com/sapcc/go-bits/logg"
	"github.com/sapcc/keppel/pkg/api"
	"github.com/sapcc/keppel/pkg/keppel"
	orchestrator_pkg "github.com/sapcc/keppel/pkg/orchestrator"
)

func main() {
	logg.Info("starting keppel-api %s", keppel.Version)
	if os.Getenv("KEPPEL_DEBUG") == "1" {
		logg.ShowDebug = true
	}

	//I have some trouble getting Keppel to connect to our staging OpenStack
	//through mitmproxy (which is very useful for development and debugging) when
	//TLS certificate verification is enabled. Therefore, allow to turn it off
	//with an env variable. (It's very important that this is not the standard
	//"KEPPEL_DEBUG" variable. That one is meant to be useful for production
	//systems, where you definitely don't want to turn off certificate
	//verification.)
	if os.Getenv("KEPPEL_INSECURE") == "1" {
		http.DefaultTransport.(*http.Transport).TLSClientConfig = &tls.Config{
			InsecureSkipVerify: true,
		}
		http.DefaultClient.Transport = http.DefaultTransport
	}

	if len(os.Args) != 2 {
		logg.Fatal("usage: keppel-api <config-path>")
	}
	keppel.ReadConfig(os.Args[1]) //exits on error

	orchestrator, orchestratorAPI := orchestrator_pkg.NewOrchestrator()
	keppelV1, err := api.NewKeppelV1(orchestratorAPI)
	if err != nil {
		logg.Fatal(err.Error())
	}

	//wire up HTTP handlers
	r := mux.NewRouter()
	kv1, rv2 := keppelV1.Routers()
	r.PathPrefix("/keppel/v1/").Handler(kv1)
	r.PathPrefix("/v2/").Handler(rv2)
	r.Methods("GET").Path("/health").HandlerFunc(handleHealthcheck)

	//TODO Prometheus instrumentation
	loggm := logg.Middleware{
		ExceptURLPath: regexp.MustCompile(`^/health`),
	}
	http.Handle("/",
		loggm.Wrap(r),
	)

	//start HTTP server
	logg.Info("listening on " + keppel.State.Config.APIListenAddress)
	go func() {
		err = http.ListenAndServe(keppel.State.Config.APIListenAddress, nil)
		if err != nil {
			logg.Fatal("error returned from http.ListenAndServe(): %s", err.Error())
		}
	}()

	//start orchestrator workers
	go orchestratorAPI.EnsureAllRegistriesAreRunning()

	//enter orchestrator main loop
	ok := orchestrator.Run(contextWithSIGINT(context.Background()))
	if !ok {
		os.Exit(1)
	}
}

func contextWithSIGINT(ctx context.Context) context.Context {
	ctx, cancel := context.WithCancel(ctx)
	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-signalChan
		signal.Reset(os.Interrupt, syscall.SIGTERM)
		close(signalChan)
		cancel()
	}()
	return ctx
}

func handleHealthcheck(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("ok"))
}
