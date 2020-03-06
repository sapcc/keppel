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

package tasks

import (
	"database/sql"
	"testing"
	"time"

	"github.com/sapcc/go-bits/easypg"
	"github.com/sapcc/keppel/internal/keppel"
	"github.com/sapcc/keppel/internal/test"
)

func TestValidateNextManifestError(t *testing.T) {
	j, _, db, sd, clock := setup(t)

	//setup a manifest that does not exist in the backing storage
	clock.StepBy(1 * time.Hour)
	image := test.GenerateImage( /* no layers */ )
	imageSize := len(image.Manifest.Contents) + len(image.Config.Contents)
	must(t, db.Insert(&keppel.Manifest{
		RepositoryID: 1,
		Digest:       image.Manifest.Digest.String(),
		MediaType:    image.Manifest.MediaType,
		SizeBytes:    uint64(imageSize),
		PushedAt:     clock.Now(),
		ValidatedAt:  clock.Now(),
	}))

	//validation should yield an error ("no such manifest" is returned by test.StorageDriver)
	clock.StepBy(12 * time.Hour)
	expectError(t, "while validating a manifest: no such manifest",
		j.ValidateNextManifest())

	//check that validation error to be recorded in the DB
	easypg.AssertDBContent(t, db.DbMap.Db, "fixtures/manifest-validate-error-001.sql")

	//expect next ValidateNextManifest run to skip over this manifest since it
	//was recently validated
	expectError(t, sql.ErrNoRows.Error(), j.ValidateNextManifest())

	//upload manifest and blob so that we can test recovering from the validation error
	uploadBlob(t, db, sd, clock, image.Config)
	mustExec(t, db, `DELETE FROM manifests WHERE digest = $1`, image.Manifest.Digest.String())
	uploadManifest(t, db, sd, clock, image.Manifest, imageSize)

	//next validation should be happy (and also create the missing refs)
	clock.StepBy(12 * time.Hour)
	expectSuccess(t, j.ValidateNextManifest())
	easypg.AssertDBContent(t, db.DbMap.Db, "fixtures/manifest-validate-error-002.sql")
}
