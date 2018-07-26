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
	"syscall"

	"github.com/gophercloud/utils/openstack/clientconfig"
	"github.com/gorilla/mux"
	"github.com/sapcc/go-bits/logg"
	"github.com/sapcc/keppel/pkg/api"
	"github.com/sapcc/keppel/pkg/database"
	"github.com/sapcc/keppel/pkg/openstack"
	orchestrator_pkg "github.com/sapcc/keppel/pkg/orchestrator"
	"github.com/sapcc/keppel/pkg/version"
)

func main() {
	logg.Info("starting keppel-api %s", version.Version)
	if os.Getenv("KEPPEL_DEBUG") == "1" {
		logg.ShowDebug = true
	}

	//connect to Postgres
	db, err := database.Init()
	if err != nil {
		logg.Fatal(err.Error())
	}

	//connect to Keystone
	provider, err := clientconfig.AuthenticatedClient(nil)
	if err != nil {
		logg.Fatal("cannot connect to Keystone: %s", err.Error())
	}
	serviceUser, err := openstack.NewServiceUser(provider)
	if err != nil {
		logg.Fatal(err.Error())
	}

	orchestrator, orchestratorAPI := orchestrator_pkg.NewOrchestrator()
	keppelV1, err := api.NewKeppelV1(db, serviceUser, orchestratorAPI)
	if err != nil {
		logg.Fatal(err.Error())
	}

	//wire up HTTP handlers
	r := mux.NewRouter()
	r.PathPrefix("/keppel/v1/").Handler(keppelV1.Router())
	http.Handle("/", r)

	//start HTTP server (TODO Prometheus instrumentation, TODO log middleware)
	listenAddress := os.Getenv("KEPPEL_LISTEN_ADDRESS")
	if listenAddress == "" {
		listenAddress = ":8080"
	}
	logg.Info("listening on " + listenAddress)
	go func() {
		err = http.ListenAndServe(listenAddress, nil)
		if err != nil {
			logg.Fatal("error returned from http.ListenAndServe(): %s", err.Error())
		}
	}()

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
