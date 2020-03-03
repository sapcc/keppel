/*******************************************************************************
*
* Copyright 2020 SAP SE
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
	"fmt"
	"net/http"

	"github.com/sapcc/go-bits/logg"
	"github.com/sapcc/keppel/internal/keppel"
	"gopkg.in/gorp.v2"
)

//Processor is a higher-level interface wrapping keppel.DB and keppel.StorageDriver.
//It abstracts DB accesses into high-level interactions and keeps DB updates in
//lockstep with StorageDriver accesses.
type Processor struct {
	db *keppel.DB
	sd keppel.StorageDriver
}

//New creates a new Processor.
func New(db *keppel.DB, sd keppel.StorageDriver) *Processor {
	return &Processor{db, sd}
}

//WithLowlevelAccess lets the caller access the low-level interfaces wrapped by
//this Processor instance. The existence of this method means that the
//low-level interfaces are basically public, but having to use this method
//makes it more obvious when code bypasses the interface of Processor.
//
//NOTE: This method is not used widely at the moment because callers usually
//have direct access to `db` and `sd`, but my plan is to convert most or all DB
//accesses into methods on type Processor eventually.
func (p *Processor) WithLowlevelAccess(action func(*keppel.DB, keppel.StorageDriver) error) error {
	return action(p.db, p.sd)
}

//Executes the action callback within a database transaction.  If the action
//callback returns success (i.e. a nil error), the transaction will be
//committed.  If it returns an error or panics, the transaction will be rolled
//back.
func (p *Processor) insideTransaction(action func(*gorp.Transaction) error) error {
	tx, err := p.db.Begin()
	if err != nil {
		return err
	}
	isCommitted := false

	defer func() {
		if !isCommitted {
			err := tx.Rollback()
			if err != nil {
				logg.Error("implicit rollback failed: " + err.Error())
			}
		}
	}()

	err = action(tx)
	if err != nil {
		return err
	}
	err = tx.Commit()
	if err != nil {
		return err
	}
	isCommitted = true
	return nil
}

////////////////////////////////////////////////////////////////////////////////
// helper functions used by multiple Processor methods

//Returns nil if and only if the user can push another manifest.
func (p *Processor) checkQuotaForManifestPush(account keppel.Account) error {
	//check if user has enough quota to push a manifest
	quotas, err := keppel.FindQuotas(p.db, account.AuthTenantID)
	if err != nil {
		return err
	}
	if quotas == nil {
		quotas = keppel.DefaultQuotas(account.AuthTenantID)
	}
	manifestUsage, err := quotas.GetManifestUsage(p.db)
	if err != nil {
		return err
	}
	if manifestUsage >= quotas.ManifestCount {
		msg := fmt.Sprintf("manifest quota exceeded (quota = %d, usage = %d)",
			quotas.ManifestCount, manifestUsage,
		)
		return keppel.ErrDenied.With(msg).WithStatus(http.StatusConflict)
	}
	return nil
}
