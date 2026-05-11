// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package liquidapicmd

import (
	"net/http"
	"time"

	"github.com/dlmiddlecote/sqlstats"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/sapcc/go-bits/easypg"
	"github.com/sapcc/go-bits/httpapi"
	"github.com/sapcc/go-bits/httpapi/pprofapi"
	"github.com/sapcc/go-bits/httpext"
	"github.com/sapcc/go-bits/must"
	"github.com/sapcc/go-bits/osext"
	"github.com/spf13/cobra"

	"github.com/sapcc/keppel/internal/api/liquid"
	"github.com/sapcc/keppel/internal/keppel"
)

// AddCommandTo mounts this command into the command hierarchy.
func AddCommandTo(parent *cobra.Command) {
	cmd := &cobra.Command{
		Use:   "liquidapi",
		Short: "Run the keppel-liquidapi server component.",
		Long:  "Run the keppel-liquidapi server component. Configuration is read from environment variables as described in README.md.",
		Args:  cobra.NoArgs,
		Run:   run,
	}
	parent.AddCommand(cmd)
}

func run(cmd *cobra.Command, args []string) {
	_, _ = cmd, args

	keppel.SetTaskName("liquidapi")

	cfg := keppel.ParseConfiguration()
	ctx := httpext.ContextWithSIGINT(cmd.Context(), 10*time.Second)
	auditor := must.Return(keppel.InitAuditTrail(ctx))

	dbURL, dbName := keppel.GetDatabaseURLFromEnvironment()
	dbConn := must.Return(easypg.Connect(dbURL, keppel.DBConfiguration()))
	prometheus.MustRegister(sqlstats.NewStatsCollector(dbName, dbConn))
	db := keppel.InitORM(dbConn)

	ad := must.Return(keppel.NewAuthDriver(ctx, osext.MustGetenv("KEPPEL_DRIVER_AUTH"), nil))
	sd := must.Return(keppel.NewStorageDriver(osext.MustGetenv("KEPPEL_DRIVER_STORAGE"), ad, cfg))

	// wire up HTTP handlers
	handler := httpapi.Compose(
		liquid.NewLiquidAPI(cfg, ad, sd, db, auditor),
		httpapi.HealthCheckAPI{
			SkipRequestLog: true,
			Check: func() error {
				return db.Db.PingContext(ctx)
			},
		},
		pprofapi.API{IsAuthorized: pprofapi.IsRequestFromLocalhost},
	)
	mux := http.NewServeMux()
	mux.Handle("/", handler)
	mux.Handle("/metrics", promhttp.Handler())

	// start HTTP server
	apiListenAddress := osext.GetenvOrDefault("KEPPEL_LIQUIDAPI_LISTEN_ADDRESS", ":8080")
	must.Succeed(httpext.ListenAndServeContext(ctx, apiListenAddress, mux))
}
