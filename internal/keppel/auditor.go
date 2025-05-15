// SPDX-FileCopyrightText: 2019 SAP SE
// SPDX-License-Identifier: Apache-2.0

package keppel

import (
	"context"
	"net/http"
	"os"

	"github.com/sapcc/go-api-declarations/bininfo"
	"github.com/sapcc/go-bits/audittools"
	"github.com/sapcc/go-bits/logg"
)

// AuditContext collects arguments that business logic methods need only for
// generating audit events.
type AuditContext struct {
	UserIdentity UserIdentity
	Request      *http.Request
}

// InitAuditTrail initializes a Auditor from the configuration variables
// found in the environment.
func InitAuditTrail(ctx context.Context) (audittools.Auditor, error) {
	logg.Debug("initializing audit trail...")

	if os.Getenv("KEPPEL_AUDIT_RABBITMQ_QUEUE_NAME") == "" {
		return audittools.NewMockAuditor(), nil
	} else {
		return audittools.NewAuditor(ctx, audittools.AuditorOpts{
			EnvPrefix: "KEPPEL_AUDIT_RABBITMQ",
			Observer: audittools.Observer{
				TypeURI: "service/docker-registry",
				Name:    bininfo.Component(),
				ID:      audittools.GenerateUUID(),
			},
		})
	}
}
