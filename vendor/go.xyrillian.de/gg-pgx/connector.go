// SPDX-FileCopyrightText: 2026 Stefan Majewsky <majewsky@gmx.net>
// SPDX-License-Identifier: Apache-2.0

package gg_pgx

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.xyrillian.de/gg/pgruntime"
)

// SingleConnector returns a [pgruntime.Connector] that spawns individual database connections.
func SingleConnector() pgruntime.Connector[*Conn] {
	return func(ctx context.Context, target pgruntime.ConnectionTarget) (*Conn, error) {
		dbURL, err := target.IntoURL()
		if err != nil {
			return nil, err
		}
		conn, err := pgx.Connect(ctx, dbURL.String())
		return maybe(NewConn, conn), err
	}
}

// PoolConnector returns a [pgruntime.Connector] that spawns database connection pools.
func PoolConnector() pgruntime.Connector[*Pool] {
	return func(ctx context.Context, target pgruntime.ConnectionTarget) (*Pool, error) {
		dbURL, err := target.IntoURL()
		if err != nil {
			return nil, err
		}
		pool, err := pgxpool.New(ctx, dbURL.String())
		return maybe(NewPool, pool), err
	}
}
