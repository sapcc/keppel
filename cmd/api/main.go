// SPDX-FileCopyrightText: 2018 SAP SE
// SPDX-License-Identifier: Apache-2.0

package apicmd

import (
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/dlmiddlecote/sqlstats"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/redis/go-redis/v9"
	"github.com/rs/cors"
	"github.com/sapcc/go-bits/easypg"
	"github.com/sapcc/go-bits/httpapi"
	"github.com/sapcc/go-bits/httpapi/pprofapi"
	"github.com/sapcc/go-bits/httpext"
	"github.com/sapcc/go-bits/logg"
	"github.com/sapcc/go-bits/must"
	"github.com/sapcc/go-bits/osext"
	"github.com/spf13/cobra"

	auth "github.com/sapcc/keppel/internal/api/auth"
	keppelv1 "github.com/sapcc/keppel/internal/api/keppel"
	peerv1 "github.com/sapcc/keppel/internal/api/peer"
	registryv2 "github.com/sapcc/keppel/internal/api/registry"
	"github.com/sapcc/keppel/internal/keppel"
)

// AddCommandTo mounts this command into the command hierarchy.
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
	_, _ = cmd, args

	keppel.SetTaskName("api")

	cfg := keppel.ParseConfiguration()
	ctx := httpext.ContextWithSIGINT(cmd.Context(), 10*time.Second)
	auditor := must.Return(keppel.InitAuditTrail(ctx))

	dbURL, dbName := keppel.GetDatabaseURLFromEnvironment()
	dbConn := must.Return(easypg.Connect(dbURL, keppel.DBConfiguration()))
	prometheus.MustRegister(sqlstats.NewStatsCollector(dbName, dbConn))
	db := keppel.InitORM(dbConn)
	must.Succeed(setupDBIfRequested(db))

	rc := must.Return(initRedis())
	ad := must.Return(keppel.NewAuthDriver(ctx, osext.MustGetenv("KEPPEL_DRIVER_AUTH"), rc))
	fd := must.Return(keppel.NewFederationDriver(ctx, osext.MustGetenv("KEPPEL_DRIVER_FEDERATION"), ad, cfg))
	sd := must.Return(keppel.NewStorageDriver(osext.MustGetenv("KEPPEL_DRIVER_STORAGE"), ad, cfg))
	icd := must.Return(keppel.NewInboundCacheDriver(ctx, osext.MustGetenv("KEPPEL_DRIVER_INBOUND_CACHE"), cfg))

	rle := (*keppel.RateLimitEngine)(nil)
	if rc != nil {
		rld := must.Return(keppel.NewRateLimitDriver(osext.MustGetenv("KEPPEL_DRIVER_RATELIMIT"), ad, cfg))
		rle = &keppel.RateLimitEngine{Driver: rld, Client: rc}
	}

	// start background goroutines
	runPeering(ctx, cfg, db)

	// wire up HTTP handlers
	corsMiddleware := cors.New(cors.Options{
		AllowedOrigins: []string{"*"},
		AllowedMethods: []string{"HEAD", "GET", "POST", "PUT", "DELETE"},
		AllowedHeaders: []string{"Content-Type", "User-Agent", "Authorization", "X-Auth-Token", keppelv1.SubleaseHeader},
	})
	handler := httpapi.Compose(
		keppelv1.NewAPI(cfg, ad, fd, sd, icd, db, auditor, rle),
		auth.NewAPI(cfg, ad, fd, db),
		registryv2.NewAPI(cfg, ad, fd, sd, icd, db, auditor, rle),
		peerv1.NewAPI(cfg, ad, db),
		&headerReflector{logg.ShowDebug}, // the header reflection endpoint is only enabled where debugging is enabled (i.e. usually in dev/QA only)
		httpapi.HealthCheckAPI{
			SkipRequestLog: true,
			Check: func() error {
				return db.Db.PingContext(ctx)
			},
		},
		httpapi.WithGlobalMiddleware(reportClientIP),
		httpapi.WithGlobalMiddleware(corsMiddleware.Handler),
		pprofapi.API{IsAuthorized: pprofapi.IsRequestFromLocalhost},
		// This needs to be at the end because it is the fallback match for all
		// paths that are not otherwise defined.
		&guiRedirecter{db, os.Getenv("KEPPEL_GUI_URI")},
	)
	mux := http.NewServeMux()
	mux.Handle("/", handler)
	mux.Handle("/metrics", promhttp.Handler())

	// start HTTP server
	apiListenAddress := osext.GetenvOrDefault("KEPPEL_API_LISTEN_ADDRESS", ":8080")
	must.Succeed(httpext.ListenAndServeContext(ctx, apiListenAddress, mux))
}

// Note that, since Redis is optional, this may return (nil, nil).
func initRedis() (*redis.Client, error) {
	if !osext.GetenvBool("KEPPEL_REDIS_ENABLE") {
		return nil, nil
	}
	logg.Debug("initializing Redis connection...")

	opts, err := keppel.GetRedisOptions("KEPPEL")
	if err != nil {
		return nil, fmt.Errorf("cannot parse Redis URL: %s", err.Error())
	}
	return redis.NewClient(opts), nil
}

func setupDBIfRequested(db *keppel.DB) error {
	// This method performs specialized first-time setup for conformance test
	// scenarios where we always start with a fresh empty database.
	//
	// This setup cannot be done before keppel-api has been started, because the
	// DB schema has not been populated yet at that point.
	if osext.GetenvBool("KEPPEL_RUN_DB_SETUP_FOR_CONFORMANCE_TEST") {
		// clear out database before running conformance tests to be not out of sync with cleared out storage filedriver
		// borrowed from test setup
		for {
			result := must.Return(db.Exec(`DELETE FROM manifest_manifest_refs WHERE parent_digest NOT IN (SELECT child_digest FROM manifest_manifest_refs)`))
			rowsDeleted := must.Return(result.RowsAffected())
			if rowsDeleted == 0 {
				break
			}
		}

		queries := []string{
			// clean out all other tables before inserting account
			"DELETE FROM manifest_blob_refs",
			"DELETE FROM accounts",
			"DELETE FROM peers",
			"DELETE FROM quotas",

			"INSERT INTO accounts (name, auth_tenant_id) VALUES ('conformance-test', 'bogus')",
			"INSERT INTO quotas (auth_tenant_id, manifests) VALUES ('bogus', 100)",
		}

		for _, query := range queries {
			_, err := db.Exec(query)
			if err != nil {
				return fmt.Errorf("while performing DB setup for conformance test: %w", err)
			}
		}
	}

	return nil
}

func reportClientIP(inner http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// This middleware adds the X-Keppel-Your-Ip header to all requests, which is used:
		// 1. by end users to understand which IPs they need to put in their RBAC policies
		// 2. by Keppel operators to check if X-Forwarded-For is transported correctly through reverse proxies
		w.Header().Set("X-Keppel-Your-Ip", httpext.GetRequesterIPFor(r))
		inner.ServeHTTP(w, r)
	})
}
