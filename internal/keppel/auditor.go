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
	"fmt"

	"github.com/sapcc/go-bits/audittools"
	"github.com/sapcc/hermes/pkg/cadf"
)

//Auditor is a component that forwards audit events to the appropriate logs.
//It is used by some of the API modules.
type Auditor interface {
	//Record forwards the given audit event to the audit log.
	//EventParameters.Observer will be filled by the auditor.
	Record(params audittools.EventParameters)
}

//AuditManifest is an audittools.TargetRenderer.
type AuditManifest struct {
	Account    Account
	Repository Repository
	Digest     string
}

//Render implements the audittools.TargetRenderer interface.
func (a AuditManifest) Render() cadf.Resource {
	return cadf.Resource{
		TypeURI:   "docker-registry/account/repository/manifest",
		ID:        fmt.Sprintf("%s@%s", a.Repository.FullName(), a.Digest),
		ProjectID: a.Account.AuthTenantID,
	}
}

//AuditTag is an audittools.TargetRenderer.
type AuditTag struct {
	Account    Account
	Repository Repository
	Digest     string
	TagName    string
}

//Render implements the audittools.TargetRenderer interface.
func (a AuditTag) Render() cadf.Resource {
	return cadf.Resource{
		TypeURI:   "docker-registry/account/repository/tag",
		ID:        fmt.Sprintf("%s:%s", a.Repository.FullName(), a.TagName),
		ProjectID: a.Account.AuthTenantID,
		Attachments: []cadf.Attachment{{
			Name:    "digest",
			TypeURI: "mime:text/plain",
			Content: a.Digest,
		}},
	}
}
