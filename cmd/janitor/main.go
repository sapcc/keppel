/******************************************************************************
*
*  Copyright 2020 SAP SE
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

package janitorcmd

import (
	"context"
	"database/sql"
	"net/http"
	"time"

	"github.com/dlmiddlecote/sqlstats"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/sapcc/go-bits/httpapi"
	"github.com/sapcc/go-bits/httpext"
	"github.com/sapcc/go-bits/logg"
	"github.com/sapcc/go-bits/must"
	"github.com/sapcc/go-bits/osext"
	"github.com/spf13/cobra"

	"github.com/sapcc/keppel/internal/keppel"
	"github.com/sapcc/keppel/internal/tasks"
)

// AddCommandTo mounts this command into the command hierarchy.
func AddCommandTo(parent *cobra.Command) {
	cmd := &cobra.Command{
		Use:   "janitor",
		Short: "Run the keppel-janitor server component.",
		Long:  "Run the keppel-janitor server component. Configuration is read from environment variables as described in README.md.",
		Args:  cobra.NoArgs,
		Run:   run,
	}
	parent.AddCommand(cmd)
}

func run(cmd *cobra.Command, args []string) {
	keppel.SetTaskName("janitor")

	cfg := keppel.ParseConfiguration()
	auditor := keppel.InitAuditTrail()

	db := must.Return(keppel.InitDB(cfg.DatabaseURL))
	ad := must.Return(keppel.NewAuthDriver(osext.MustGetenv("KEPPEL_DRIVER_AUTH"), nil))
	fd := must.Return(keppel.NewFederationDriver(osext.MustGetenv("KEPPEL_DRIVER_FEDERATION"), ad, cfg))
	sd := must.Return(keppel.NewStorageDriver(osext.MustGetenv("KEPPEL_DRIVER_STORAGE"), ad, cfg))
	icd := must.Return(keppel.NewInboundCacheDriver(osext.MustGetenv("KEPPEL_DRIVER_INBOUND_CACHE"), cfg))

	prometheus.MustRegister(sqlstats.NewStatsCollector("keppel", db.DbMap.Db))

	ctx := httpext.ContextWithSIGINT(context.Background(), 10*time.Second)

	//start task loops
	janitor := tasks.NewJanitor(cfg, fd, sd, icd, db, auditor)
	go jobLoop(janitor.AnnounceNextAccountToFederation)
	go jobLoop(janitor.DeleteNextAbandonedUpload)
	go jobLoop(janitor.GarbageCollectManifestsInNextRepo)
	go jobLoop(janitor.SweepBlobMountsInNextRepo)
	go jobLoop(janitor.SweepBlobsInNextAccount)
	go jobLoop(janitor.SweepStorageInNextAccount)
	go jobLoop(janitor.SyncManifestsInNextRepo)
	go jobLoop(janitor.ValidateNextBlob)
	go jobLoop(janitor.ValidateNextManifest)
	if cfg.ClairClient != nil {
		go tasks.GoQueuedJobLoop(ctx, 3, janitor.CheckVulnerabilitiesForNextManifest())
	}

	//start HTTP server for Prometheus metrics and health check
	handler := httpapi.Compose(httpapi.HealthCheckAPI{SkipRequestLog: true})
	http.Handle("/", handler)
	http.Handle("/metrics", promhttp.Handler())
	listenAddress := osext.GetenvOrDefault("KEPPEL_JANITOR_LISTEN_ADDRESS", ":8080")
	must.Succeed(httpext.ListenAndServeContext(ctx, listenAddress, nil))
}

// Execute a task repeatedly, but slow down when sql.ErrNoRows is returned by it.
// (Tasks use this error value to indicate that nothing needs scraping, so we
// can back off a bit to avoid useless database load.)
func jobLoop(task func() error) {
	for {
		err := task()
		switch err {
		case nil:
			//nothing to do here
		case sql.ErrNoRows:
			//nothing to do right now - slow down a bit to avoid useless DB polling
			time.Sleep(10 * time.Second)
		default:
			logg.Error(err.Error())
			//slow down a bit after an error to avoid hammering the DB during outages
			time.Sleep(2 * time.Second)
		}
	}
}
