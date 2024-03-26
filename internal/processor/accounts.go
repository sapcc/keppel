/*******************************************************************************
*
* Copyright 2024 SAP SE
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

package processor

import (
	"context"

	"github.com/sapcc/keppel/internal/auth"
	peerclient "github.com/sapcc/keppel/internal/client/peer"
	"github.com/sapcc/keppel/internal/keppel"
	"github.com/sapcc/keppel/internal/models"
)

// GetPlatformFilterFromPrimaryAccount takes a replica account and queries the
// peer holding the primary account for that account's platform filter.
//
// Returns sql.ErrNoRows if the configured peer does not exist.
func (p *Processor) GetPlatformFilterFromPrimaryAccount(ctx context.Context, replicaAccount models.Account) (models.PlatformFilter, error) {
	var peer models.Peer
	err := p.db.SelectOne(&peer, `SELECT * FROM peers WHERE hostname = $1`, replicaAccount.UpstreamPeerHostName)
	if err != nil {
		return nil, err
	}

	viewScope := auth.Scope{
		ResourceType: "keppel_account",
		ResourceName: replicaAccount.Name,
		Actions:      []string{"view"},
	}
	client, err := peerclient.New(ctx, p.cfg, peer, viewScope)
	if err != nil {
		return nil, err
	}

	//TODO: use type keppelv1.Account once it is moved to package keppel
	var upstreamAccount struct {
		Name              string                    `json:"name"`
		AuthTenantID      string                    `json:"auth_tenant_id"`
		GCPolicies        []keppel.GCPolicy         `json:"gc_policies,omitempty"`
		InMaintenance     bool                      `json:"in_maintenance"`
		Metadata          map[string]string         `json:"metadata"`
		RBACPolicies      []keppel.RBACPolicy       `json:"rbac_policies"`
		ReplicationPolicy *keppel.ReplicationPolicy `json:"replication,omitempty"`
		ValidationPolicy  *keppel.ValidationPolicy  `json:"validation,omitempty"`
		PlatformFilter    models.PlatformFilter     `json:"platform_filter,omitempty"`
	}
	err = client.GetForeignAccountConfigurationInto(ctx, &upstreamAccount, replicaAccount.Name)
	if err != nil {
		return nil, err
	}
	return upstreamAccount.PlatformFilter, nil
}
