/******************************************************************************
*
*  Copyright 2019 SAP SE
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
