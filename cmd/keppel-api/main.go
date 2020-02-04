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
	"net/url"
	"os"
	"strconv"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rs/cors"
	"github.com/sapcc/go-bits/httpee"
	"github.com/sapcc/go-bits/logg"
	"github.com/sapcc/keppel/internal/api"
	auth "github.com/sapcc/keppel/internal/api/auth"
	keppelv1 "github.com/sapcc/keppel/internal/api/keppel"
	registryv2 "github.com/sapcc/keppel/internal/api/registry"
	"github.com/sapcc/keppel/internal/keppel"

	_ "github.com/sapcc/keppel/internal/drivers/openstack"
	_ "github.com/sapcc/keppel/internal/drivers/trivial"
)

func main() {
	logg.ShowDebug, _ = strconv.ParseBool(os.Getenv("KEPPEL_DEBUG"))
	logg.Info("starting keppel-api %s", keppel.Version)

	//The KEPPEL_INSECURE flag can be used to get Keppel to work through
	//mitmproxy (which is very useful for development and debugging). (It's very
	//important that this is not the standard "KEPPEL_DEBUG" variable. That one
	//is meant to be useful for production systems, where you definitely don't
	//want to turn off certificate verification.)
	if insecure, _ := strconv.ParseBool(os.Getenv("KEPPEL_INSECURE")); insecure {
		http.DefaultTransport.(*http.Transport).TLSClientConfig = &tls.Config{
			InsecureSkipVerify: true,
		}
		http.DefaultClient.Transport = http.DefaultTransport
	}

	cfg := parseConfig()
	auditor := initAuditTrail()

	db, err := keppel.InitDB(cfg.DatabaseURL)
	must(err)
	ad, err := keppel.NewAuthDriver(mustGetenv("KEPPEL_DRIVER_AUTH"))
	must(err)
	ncd, err := keppel.NewNameClaimDriver(mustGetenv("KEPPEL_DRIVER_NAMECLAIM"), ad, cfg)
	must(err)
	sd, err := keppel.NewStorageDriver(mustGetenv("KEPPEL_DRIVER_STORAGE"), ad, cfg)
	must(err)

	//start background goroutines
	ctx := httpee.ContextWithSIGINT(context.Background())
	runPeering(ctx, cfg, db)

	//wire up HTTP handlers
	handler := api.Compose(
		keppelv1.NewAPI(cfg, ad, ncd, sd, db, auditor),
		auth.NewAPI(cfg, ad, db),
		registryv2.NewAPI(cfg, sd, db),
	)
	handler = logg.Middleware{}.Wrap(handler)
	handler = cors.New(cors.Options{
		AllowedOrigins: []string{"*"},
		AllowedMethods: []string{"HEAD", "GET", "POST", "PUT", "DELETE"},
		AllowedHeaders: []string{"Content-Type", "User-Agent", "X-Auth-Token"},
	}).Handler(handler)
	http.Handle("/", handler)
	http.Handle("/metrics", promhttp.Handler())
	http.HandleFunc("/healthcheck", healthCheckHandler)

	//start HTTP server
	apiListenAddress := os.Getenv("KEPPEL_API_LISTEN_ADDRESS")
	if apiListenAddress == "" {
		apiListenAddress = ":8080"
	}
	logg.Info("listening on " + apiListenAddress)
	err = httpee.ListenAndServeContext(ctx, apiListenAddress, nil)
	if err != nil {
		logg.Fatal("error returned from httpee.ListenAndServeContext(): %s", err.Error())
	}
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

func parseConfig() keppel.Configuration {
	cfg := keppel.Configuration{
		APIPublicURL: mustGetenvURL("KEPPEL_API_PUBLIC_URL"),
		DatabaseURL:  mustGetenvURL("KEPPEL_DB_URI"),
	}

	var err error
	cfg.JWTIssuerKey, err = keppel.ParseIssuerKey(mustGetenv("KEPPEL_ISSUER_KEY"))
	must(err)
	cfg.JWTIssuerCertPEM, err = keppel.ParseIssuerCertPEM(mustGetenv("KEPPEL_ISSUER_CERT"))
	must(err)

	return cfg
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
