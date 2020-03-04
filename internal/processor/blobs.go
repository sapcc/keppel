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
	"database/sql"
	"time"

	"github.com/docker/distribution"
	"github.com/sapcc/keppel/internal/keppel"
	"gopkg.in/gorp.v2"
)

//FindBlobOrInsertUnbackedBlob is used by the replication code path. If the
//requested blob does not exist, a blob record with an empty storage ID will be
//inserted into the DB. This indicates to the registry API handler that this
//blob shall be replicated when it is first pulled.
func (p *Processor) FindBlobOrInsertUnbackedBlob(desc distribution.Descriptor, account keppel.Account) (*keppel.Blob, error) {
	var blob *keppel.Blob
	err := p.insideTransaction(func(tx *gorp.Transaction) error {
		var err error
		blob, err = keppel.FindBlobByAccountName(tx, desc.Digest, account)
		if err != sql.ErrNoRows { //either success or unexpected error
			return err
		}

		blob = &keppel.Blob{
			AccountName: account.Name,
			Digest:      desc.Digest.String(),
			SizeBytes:   uint64(desc.Size),
			StorageID:   "", //unbacked
			PushedAt:    time.Unix(0, 0),
			ValidatedAt: time.Unix(0, 0),
		}
		return tx.Insert(blob)
	})
	return blob, err
}
