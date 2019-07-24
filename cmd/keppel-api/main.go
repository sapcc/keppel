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
	"net/url"
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
	logg.ShowDebug, _ = strconv.ParseBool(os.Getenv("KEPPEL_DEBUG"))
	logg.Info("starting keppel-api %s", keppel.Version)

	//TODO get rid of global state
	keppel.State = setupState()

	//wire up HTTP handlers
	r := mux.NewRouter()
	keppelv1api.AddTo(r)
	authapi.AddTo(r)
	registryv2api.AddTo(r)

	//TODO Prometheus instrumentation
	http.Handle("/", logg.Middleware{}.Wrap(r))
	http.HandleFunc("/healthcheck", healthCheckHandler)

	//start HTTP server
	apiListenAddress := os.Getenv("KEPPEL_API_LISTEN_ADDRESS")
	if apiListenAddress == "" {
		apiListenAddress = ":8080"
	}
	logg.Info("listening on " + apiListenAddress)
	go func() {
		err := http.ListenAndServe(apiListenAddress, nil)
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

func setupState() *keppel.StateStruct {
	cfg := keppel.Configuration{
		APIPublicURL: mustGetenvURL("KEPPEL_API_PUBLIC_URL"),
		DatabaseURL:  mustGetenvURL("KEPPEL_DB_URI"),
	}

	var err error
	cfg.JWTIssuerKey, err = keppel.ParseIssuerKey(mustGetenv("KEPPEL_ISSUER_KEY"))
	must(err)
	cfg.JWTIssuerCertPEM, err = keppel.ParseIssuerCertPEM(mustGetenv("KEPPEL_ISSUER_CERT"))
	must(err)

	db, err := keppel.InitDB(cfg.DatabaseURL)
	must(err)
	ad, err := keppel.NewAuthDriver(mustGetenv("KEPPEL_DRIVER_AUTH"))
	must(err)
	sd, err := keppel.NewStorageDriver(mustGetenv("KEPPEL_DRIVER_STORAGE"), ad)
	must(err)
	od, err := keppel.NewOrchestrationDriver(mustGetenv("KEPPEL_DRIVER_ORCHESTRATION"), sd)
	must(err)

	return &keppel.StateStruct{
		Config:              cfg,
		DB:                  db,
		AuthDriver:          ad,
		OrchestrationDriver: od,
		StorageDriver:       sd,
	}
}

func must(err error) {
	if err != nil {
		logg.Fatal(err.Error())
	}
}

func mustGetenv(key string) string {
	val := os.Getenv(key)
	if val == "" {
		logg.Fatal("missing environment variable: %s", key)
	}
	return val
}

func mustGetenvURL(key string) url.URL {
	val := mustGetenv(key)
	parsed, err := url.Parse(val)
	if err != nil {
		logg.Fatal("malformed %s: %s", key, err.Error())
	}
	return *parsed
}
