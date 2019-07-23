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
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	"github.com/gorilla/mux"
	"github.com/sapcc/go-bits/logg"
	authapi "github.com/sapcc/keppel/internal/api/auth"
	keppelv1api "github.com/sapcc/keppel/internal/api/keppel"
	registryv2api "github.com/sapcc/keppel/internal/api/registry"
	"github.com/sapcc/keppel/internal/keppel"

	_ "github.com/sapcc/keppel/internal/drivers/local_processes"
	_ "github.com/sapcc/keppel/internal/drivers/openstack"
)

func main() {
	logg.ShowDebug, _ = strconv.ParseBool(os.Getenv("CASTELLUM_DEBUG"))

	logg.Info("starting keppel-api %s", keppel.Version)
	if os.Getenv("KEPPEL_DEBUG") == "1" {
		logg.ShowDebug = true
	}

	if len(os.Args) != 2 {
		logg.Fatal("usage: keppel-api <config-path>")
	}
	cfgFile, err := os.Open(os.Args[1])
	if err == nil {
		err = keppel.ReadConfig(cfgFile)
	}
	if err == nil {
		err = cfgFile.Close()
	}
	if err != nil {
		logg.Fatal(err.Error())
	}

	//wire up HTTP handlers
	r := mux.NewRouter()
	keppelv1api.AddTo(r)
	authapi.AddTo(r)
	registryv2api.AddTo(r)

	//TODO Prometheus instrumentation
	http.Handle("/", logg.Middleware{}.Wrap(r))
	http.HandleFunc("/healthcheck", healthCheckHandler)

	//start HTTP server
	logg.Info("listening on " + keppel.State.Config.APIListenAddress)
	go func() {
		err := http.ListenAndServe(keppel.State.Config.APIListenAddress, nil)
		if err != nil {
			logg.Fatal("error returned from http.ListenAndServe(): %s", err.Error())
		}
	}()

	//enter orchestrator main loop
	ok := keppel.State.OrchestrationDriver.Run(contextWithSIGINT(context.Background()))
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

func healthCheckHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	if r.URL.Path == "/healthcheck" && r.Method == "GET" {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	} else {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte("not found"))
	}
}
