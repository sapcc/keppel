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
	"net/http"
	"os"

	"github.com/gophercloud/gophercloud"
	"github.com/gophercloud/gophercloud/openstack"
	"github.com/gophercloud/utils/openstack/clientconfig"
	"github.com/gorilla/mux"
	"github.com/sapcc/go-bits/logg"
	"github.com/sapcc/keppel/pkg/api"
	"github.com/sapcc/keppel/pkg/database"
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
	identityV3, err := openstack.NewIdentityV3(provider, gophercloud.EndpointOpts{})
	if err != nil {
		logg.Fatal("cannot find Identity v3 API in Keystone catalog: %s", err.Error())
	}
	keppelV1, err := api.NewKeppelV1(db, identityV3)
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
	err = http.ListenAndServe(listenAddress, nil)
	if err != nil {
		logg.Fatal("error returned from http.ListenAndServe(): %s", err.Error())
	}
}
