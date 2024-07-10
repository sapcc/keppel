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
	"net/http"
	"time"

	"github.com/dlmiddlecote/sqlstats"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/sapcc/go-bits/httpapi"
	"github.com/sapcc/go-bits/httpapi/pprofapi"
	"github.com/sapcc/go-bits/httpext"
	"github.com/sapcc/go-bits/jobloop"
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
	ctx := httpext.ContextWithSIGINT(cmd.Context(), 10*time.Second)
	auditor := keppel.InitAuditTrail(ctx)

	ad := must.Return(keppel.NewAuthDriver(ctx, osext.MustGetenv("KEPPEL_DRIVER_AUTH"), nil))
	amd := must.Return(keppel.NewAccountManagementDriver(osext.MustGetenv("KEPPEL_DRIVER_ACCOUNT_MANAGEMENT")))
	db := must.Return(keppel.InitDB(cfg.DatabaseURL))
	fd := must.Return(keppel.NewFederationDriver(ctx, osext.MustGetenv("KEPPEL_DRIVER_FEDERATION"), ad, cfg))
	sd := must.Return(keppel.NewStorageDriver(osext.MustGetenv("KEPPEL_DRIVER_STORAGE"), ad, cfg))
	icd := must.Return(keppel.NewInboundCacheDriver(ctx, osext.MustGetenv("KEPPEL_DRIVER_INBOUND_CACHE"), cfg))

	prometheus.MustRegister(sqlstats.NewStatsCollector("keppel", db.DbMap.Db))

	// start task loops
	janitor := tasks.NewJanitor(cfg, fd, sd, icd, db, amd, auditor)
	go janitor.AccountFederationAnnouncementJob(nil).Run(ctx)
	go janitor.AbandonedUploadCleanupJob(nil).Run(ctx)
	go janitor.EnforceManagedAccountsJob(nil).Run(ctx)
	go janitor.ManifestGarbageCollectionJob(nil).Run(ctx)
	go janitor.BlobMountSweepJob(nil).Run(ctx)
	go janitor.BlobSweepJob(nil).Run(ctx)
	go janitor.StorageSweepJob(nil).Run(ctx)
	go janitor.ManifestSyncJob(nil).Run(ctx)
	go janitor.BlobValidationJob(nil).Run(ctx)
	go janitor.ManifestValidationJob(nil).Run(ctx)
	if cfg.Trivy != nil {
		go janitor.CheckTrivySecurityStatusJob(nil).Run(ctx, jobloop.NumGoroutines(3))
	}

	// start HTTP server for Prometheus metrics and health check
	handler := httpapi.Compose(
		httpapi.HealthCheckAPI{SkipRequestLog: true},
		pprofapi.API{IsAuthorized: pprofapi.IsRequestFromLocalhost},
	)
	mux := http.NewServeMux()
	mux.Handle("/", handler)
	mux.Handle("/metrics", promhttp.Handler())
	listenAddress := osext.GetenvOrDefault("KEPPEL_JANITOR_LISTEN_ADDRESS", ":8080")
	must.Succeed(httpext.ListenAndServeContext(ctx, listenAddress, mux))
}
