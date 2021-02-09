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

package apicmd

import (
	"context"
	"fmt"
	"net/http"
	"os"

	"github.com/dlmiddlecote/sqlstats"
	"github.com/go-redis/redis"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rs/cors"
	"github.com/sapcc/go-bits/httpee"
	"github.com/sapcc/go-bits/logg"
	"github.com/sapcc/keppel/internal/api"
	auth "github.com/sapcc/keppel/internal/api/auth"
	"github.com/sapcc/keppel/internal/api/clairproxy"
	keppelv1 "github.com/sapcc/keppel/internal/api/keppel"
	registryv2 "github.com/sapcc/keppel/internal/api/registry"
	"github.com/sapcc/keppel/internal/keppel"
	"github.com/spf13/cobra"
	"github.com/throttled/throttled/v2/store/goredisstore"
)

//AddCommandTo mounts this command into the command hierarchy.
func AddCommandTo(parent *cobra.Command) {
	cmd := &cobra.Command{
		Use:   "api",
		Short: "Run the keppel-api server component.",
		Long:  "Run the keppel-api server component. Configuration is read from environment variables as described in README.md.",
		Args:  cobra.NoArgs,
		Run:   run,
	}
	parent.AddCommand(cmd)
}

func run(cmd *cobra.Command, args []string) {
	keppel.Component = "keppel-api"
	logg.Info("starting keppel-api %s", keppel.Version)

	cfg := keppel.ParseConfiguration()
	auditor := initAuditTrail()

	db, err := keppel.InitDB(cfg.DatabaseURL)
	must(err)
	rc, err := initRedis()
	must(err)
	ad, err := keppel.NewAuthDriver(keppel.MustGetenv("KEPPEL_DRIVER_AUTH"), rc)
	must(err)
	fd, err := keppel.NewFederationDriver(keppel.MustGetenv("KEPPEL_DRIVER_FEDERATION"), ad, cfg)
	must(err)
	sd, err := keppel.NewStorageDriver(keppel.MustGetenv("KEPPEL_DRIVER_STORAGE"), ad, cfg)
	must(err)

	prometheus.MustRegister(sqlstats.NewStatsCollector("keppel", db.DbMap.Db))

	rle := (*keppel.RateLimitEngine)(nil)
	if rc != nil {
		store, err := goredisstore.New(rc, "keppel-")
		must(err)
		rld, err := keppel.NewRateLimitDriver(keppel.MustGetenv("KEPPEL_DRIVER_RATELIMIT"), ad, cfg)
		must(err)
		rle = &keppel.RateLimitEngine{Driver: rld, Store: store}
	}

	//start background goroutines
	ctx := httpee.ContextWithSIGINT(context.Background())
	runPeering(ctx, cfg, db)

	//wire up HTTP handlers
	handler := api.Compose(
		keppelv1.NewAPI(cfg, ad, fd, sd, db, auditor),
		auth.NewAPI(cfg, ad, fd, db),
		registryv2.NewAPI(cfg, ad, fd, sd, db, rle),
		clairproxy.NewAPI(cfg, ad),
		&headerReflector{logg.ShowDebug}, //the header reflection endpoint is only enabled where debugging is enabled (i.e. usually in dev/QA only)
		&guiRedirecter{db, os.Getenv("KEPPEL_GUI_URI")},
	)
	handler = logg.Middleware{}.Wrap(handler)
	handler = cors.New(cors.Options{
		AllowedOrigins: []string{"*"},
		AllowedMethods: []string{"HEAD", "GET", "POST", "PUT", "DELETE"},
		AllowedHeaders: []string{"Content-Type", "User-Agent", "X-Auth-Token", "X-Keppel-Sublease-Token"},
	}).Handler(handler)
	http.Handle("/", handler)
	http.Handle("/metrics", promhttp.Handler())
	http.HandleFunc("/healthcheck", api.HealthCheckHandler)

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

func must(err error) {
	if err != nil {
		logg.Fatal(err.Error())
	}
}

//Note that, since Redis is optional, this may return (nil, nil).
func initRedis() (*redis.Client, error) {
	if !keppel.ParseBool("KEPPEL_REDIS_ENABLE") {
		return nil, nil
	}
	opts, err := keppel.GetRedisOptions("KEPPEL")
	if err != nil {
		return nil, fmt.Errorf("cannot parse Redis URL: %s", err.Error())
	}
	return redis.NewClient(opts), nil
}
