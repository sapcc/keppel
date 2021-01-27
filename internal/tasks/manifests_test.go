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
	"fmt"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/sapcc/go-bits/easypg"
	"github.com/sapcc/keppel/internal/api"
	"github.com/sapcc/keppel/internal/clair"
	"github.com/sapcc/keppel/internal/keppel"
	"github.com/sapcc/keppel/internal/test"
)

////////////////////////////////////////////////////////////////////////////////
// tests for ValidateNextManifest

//Base behavior for various unit tests that start with the same image list, destroy
//it in various ways, and check that ValidateNextManifest correctly fixes it.
func testValidateNextManifestFixesDisturbance(t *testing.T, disturb func(*keppel.DB, []int64, []string)) {
	j, _, db, _, sd, clock, _ := setup(t)
	clock.StepBy(1 * time.Hour)

	var (
		allBlobIDs         []int64
		allManifestDigests []string
	)

	//setup two image manifests, both with some layers
	images := make([]test.Image, 2)
	for idx := range images {
		image := test.GenerateImage(
			test.GenerateExampleLayer(int64(10*idx+1)),
			test.GenerateExampleLayer(int64(10*idx+2)),
		)
		images[idx] = image

		layer1Blob := uploadBlob(t, db, sd, clock, image.Layers[0])
		layer2Blob := uploadBlob(t, db, sd, clock, image.Layers[1])
		configBlob := uploadBlob(t, db, sd, clock, image.Config)
		uploadManifest(t, db, sd, clock, image.Manifest, image.SizeBytes())
		for _, blobID := range []int64{layer1Blob.ID, layer2Blob.ID, configBlob.ID} {
			mustExec(t, db,
				`INSERT INTO manifest_blob_refs (blob_id, repo_id, digest) VALUES ($1, 1, $2)`,
				blobID, image.Manifest.Digest.String(),
			)
		}
		allBlobIDs = append(allBlobIDs, layer1Blob.ID, layer2Blob.ID, configBlob.ID)
		allManifestDigests = append(allManifestDigests, image.Manifest.Digest.String())
	}

	//also setup an image list manifest containing those images (so that we have
	//some manifest-manifest refs to play with)
	imageList := test.GenerateImageList(images[0].Manifest, images[1].Manifest)
	uploadManifest(t, db, sd, clock, imageList.Manifest, imageList.SizeBytes())
	for _, image := range images {
		mustExec(t, db,
			`INSERT INTO manifest_manifest_refs (repo_id, parent_digest, child_digest) VALUES (1, $1, $2)`,
			imageList.Manifest.Digest.String(), image.Manifest.Digest.String(),
		)
	}
	allManifestDigests = append(allManifestDigests, imageList.Manifest.Digest.String())

	//since these manifests were just uploaded, validated_at is set to right now,
	//so ValidateNextManifest will report that there is nothing to do
	expectError(t, sql.ErrNoRows.Error(), j.ValidateNextManifest())

	//once they need validating, they validate successfully
	clock.StepBy(36 * time.Hour)
	expectSuccess(t, j.ValidateNextManifest())
	expectSuccess(t, j.ValidateNextManifest())
	expectSuccess(t, j.ValidateNextManifest())
	expectError(t, sql.ErrNoRows.Error(), j.ValidateNextManifest())
	easypg.AssertDBContent(t, db.DbMap.Db, "fixtures/manifest-validate-001-before-disturbance.sql")

	//disturb the DB state, then rerun ValidateNextManifest to fix it
	clock.StepBy(36 * time.Hour)
	disturb(db, allBlobIDs, allManifestDigests)
	expectSuccess(t, j.ValidateNextManifest())
	expectSuccess(t, j.ValidateNextManifest())
	expectSuccess(t, j.ValidateNextManifest())
	expectError(t, sql.ErrNoRows.Error(), j.ValidateNextManifest())
	easypg.AssertDBContent(t, db.DbMap.Db, "fixtures/manifest-validate-002-after-fix.sql")
}

func TestValidateNextManifestFixesWrongSize(t *testing.T) {
	testValidateNextManifestFixesDisturbance(t, func(db *keppel.DB, allBlobIDs []int64, allManifestDigests []string) {
		mustExec(t, db, `UPDATE manifests SET size_bytes = 1337`)
	})
}

func TestValidateNextManifestFixesMissingManifestBlobRefs(t *testing.T) {
	testValidateNextManifestFixesDisturbance(t, func(db *keppel.DB, allBlobIDs []int64, allManifestDigests []string) {
		mustExec(t, db, `DELETE FROM manifest_blob_refs WHERE blob_id % 2 = 0`)
	})
}

func TestValidateNextManifestFixesMissingManifestManifestRefs(t *testing.T) {
	testValidateNextManifestFixesDisturbance(t, func(db *keppel.DB, allBlobIDs []int64, allManifestDigests []string) {
		mustExec(t, db, `DELETE FROM manifest_manifest_refs`)
	})
}

func TestValidateNextManifestFixesSuperfluousManifestBlobRefs(t *testing.T) {
	testValidateNextManifestFixesDisturbance(t, func(db *keppel.DB, allBlobIDs []int64, allManifestDigests []string) {
		for _, id := range allBlobIDs {
			for _, d := range allManifestDigests {
				mustExec(t, db, `INSERT INTO manifest_blob_refs (repo_id, digest, blob_id) VALUES (1, $1, $2) ON CONFLICT DO NOTHING`, d, id)
			}
		}
	})
}

func TestValidateNextManifestFixesSuperfluousManifestManifestRefs(t *testing.T) {
	testValidateNextManifestFixesDisturbance(t, func(db *keppel.DB, allBlobIDs []int64, allManifestDigests []string) {
		for _, d1 := range allManifestDigests {
			for _, d2 := range allManifestDigests {
				mustExec(t, db, `INSERT INTO manifest_manifest_refs (repo_id, parent_digest, child_digest) VALUES (1, $1, $2) ON CONFLICT DO NOTHING`, d1, d2)
			}
		}
	})
}

func TestValidateNextManifestError(t *testing.T) {
	j, _, db, _, sd, clock, _ := setup(t)

	//setup a manifest that is missing a referenced blob
	clock.StepBy(1 * time.Hour)
	image := test.GenerateImage( /* no layers */ )
	uploadManifest(t, db, sd, clock, image.Manifest, image.SizeBytes())

	//validation should yield an error
	clock.StepBy(36 * time.Hour)
	expectedError := "while validating a manifest: manifest blob unknown to registry: " + image.Config.Digest.String()
	expectError(t, expectedError, j.ValidateNextManifest())

	//check that validation error to be recorded in the DB
	easypg.AssertDBContent(t, db.DbMap.Db, "fixtures/manifest-validate-error-001.sql")

	//expect next ValidateNextManifest run to skip over this manifest since it
	//was recently validated
	expectError(t, sql.ErrNoRows.Error(), j.ValidateNextManifest())

	//upload missing blob so that we can test recovering from the validation error
	uploadBlob(t, db, sd, clock, image.Config)

	//next validation should be happy (and also create the missing refs)
	clock.StepBy(36 * time.Hour)
	expectSuccess(t, j.ValidateNextManifest())
	easypg.AssertDBContent(t, db.DbMap.Db, "fixtures/manifest-validate-error-002.sql")
}

////////////////////////////////////////////////////////////////////////////////
// tests for SyncManifestsInNextRepo

func TestSyncManifestsInNextRepo(t *testing.T) {
	j1, _, db1, _, sd1, clock, h1 := setup(t)
	j2, _, db2, sd2, _ := setupReplica(t, db1, h1, clock)
	clock.StepBy(1 * time.Hour)

	//upload some manifests...
	images := make([]test.Image, 4)
	for idx := range images {
		image := test.GenerateImage(
			test.GenerateExampleLayer(int64(10*idx+1)),
			test.GenerateExampleLayer(int64(10*idx+2)),
		)
		images[idx] = image

		//...to the primary account...
		layer1Blob := uploadBlob(t, db1, sd1, clock, image.Layers[0])
		layer2Blob := uploadBlob(t, db1, sd1, clock, image.Layers[1])
		configBlob := uploadBlob(t, db1, sd1, clock, image.Config)
		uploadManifest(t, db1, sd1, clock, image.Manifest, image.SizeBytes())
		for _, blobID := range []int64{layer1Blob.ID, layer2Blob.ID, configBlob.ID} {
			mustExec(t, db1,
				`INSERT INTO manifest_blob_refs (blob_id, repo_id, digest) VALUES ($1, 1, $2)`,
				blobID, image.Manifest.Digest.String(),
			)
		}

		//...and most of them also to the replica account (to simulate replication having taken place)
		if idx != 0 {
			layer1Blob := uploadBlob(t, db2, sd2, clock, image.Layers[0])
			layer2Blob := uploadBlob(t, db2, sd2, clock, image.Layers[1])
			configBlob := uploadBlob(t, db2, sd2, clock, image.Config)
			uploadManifest(t, db2, sd2, clock, image.Manifest, image.SizeBytes())
			for _, blobID := range []int64{layer1Blob.ID, layer2Blob.ID, configBlob.ID} {
				mustExec(t, db2,
					`INSERT INTO manifest_blob_refs (blob_id, repo_id, digest) VALUES ($1, 1, $2)`,
					blobID, image.Manifest.Digest.String(),
				)
			}
		}
	}

	//also setup an image list manifest containing some of those images (so that we have
	//some manifest-manifest refs to play with)
	imageList := test.GenerateImageList(images[1].Manifest, images[2].Manifest)
	uploadManifest(t, db1, sd1, clock, imageList.Manifest, imageList.SizeBytes())
	for _, imageManifest := range imageList.ImageManifests {
		mustExec(t, db1,
			`INSERT INTO manifest_manifest_refs (repo_id, parent_digest, child_digest) VALUES (1, $1, $2)`,
			imageList.Manifest.Digest.String(), imageManifest.Digest.String(),
		)
	}
	//this one is replicated as well
	uploadManifest(t, db2, sd2, clock, imageList.Manifest, imageList.SizeBytes())
	for _, imageManifest := range imageList.ImageManifests {
		mustExec(t, db2,
			`INSERT INTO manifest_manifest_refs (repo_id, parent_digest, child_digest) VALUES (1, $1, $2)`,
			imageList.Manifest.Digest.String(), imageManifest.Digest.String(),
		)
	}

	//set a well-known last_pulled_at timestamp on all manifests in the primary
	//DB (we will later verify that this was not touched by the manifest sync)
	expectedLastPulledAt := time.Unix(42, 0)
	mustExec(t, db1, `UPDATE manifests SET last_pulled_at = $1`, expectedLastPulledAt)

	//SyncManifestsInNextRepo on the primary registry should have nothing to do
	//since there are no replica accounts
	expectError(t, sql.ErrNoRows.Error(), j1.SyncManifestsInNextRepo())
	//SyncManifestsInNextRepo on the secondary registry should set the
	//ManifestsSyncedAt timestamp on the repo, but otherwise not do anything
	expectSuccess(t, j2.SyncManifestsInNextRepo())
	easypg.AssertDBContent(t, db2.DbMap.Db, "fixtures/manifest-sync-001.sql")
	expectError(t, sql.ErrNoRows.Error(), j2.SyncManifestsInNextRepo())
	easypg.AssertDBContent(t, db2.DbMap.Db, "fixtures/manifest-sync-001.sql")

	//delete a manifest on the primary side (this one is a simple image not referenced by anyone else)
	clock.StepBy(2 * time.Hour)
	mustExec(t, db1,
		`DELETE FROM manifests WHERE digest = $1`,
		images[3].Manifest.Digest.String(),
	)

	//again, nothing to do on the primary side
	expectError(t, sql.ErrNoRows.Error(), j1.SyncManifestsInNextRepo())
	//SyncManifestsInNextRepo on the replica side should not do anything while
	//the account is in maintenance; only the timestamp is updated to make sure
	//that the job loop progresses to the next repo
	mustExec(t, db2, `UPDATE accounts SET in_maintenance = TRUE`)
	expectSuccess(t, j2.SyncManifestsInNextRepo())
	easypg.AssertDBContent(t, db2.DbMap.Db, "fixtures/manifest-sync-002.sql")
	expectError(t, sql.ErrNoRows.Error(), j2.SyncManifestsInNextRepo())
	easypg.AssertDBContent(t, db2.DbMap.Db, "fixtures/manifest-sync-002.sql")

	//after the end of the maintenance, SyncManifestsInNextRepo on the replica
	//side should delete the same manifest that we deleted in the primary account
	clock.StepBy(2 * time.Hour)
	mustExec(t, db2, `UPDATE accounts SET in_maintenance = FALSE`)
	expectSuccess(t, j2.SyncManifestsInNextRepo())
	easypg.AssertDBContent(t, db2.DbMap.Db, "fixtures/manifest-sync-003.sql")
	expectError(t, sql.ErrNoRows.Error(), j2.SyncManifestsInNextRepo())
	easypg.AssertDBContent(t, db2.DbMap.Db, "fixtures/manifest-sync-003.sql")

	//cause a deliberate inconsistency on the primary side: delete a manifest that
	//*is* referenced by another manifest (this requires deleting the
	//manifest-manifest ref first, otherwise the DB will complain)
	clock.StepBy(2 * time.Hour)
	mustExec(t, db1,
		`DELETE FROM manifest_manifest_refs WHERE child_digest = $1`,
		images[2].Manifest.Digest.String(),
	)
	mustExec(t, db1,
		`DELETE FROM manifests WHERE digest = $1`,
		images[2].Manifest.Digest.String(),
	)

	//SyncManifestsInNextRepo should now complain since it wants to delete
	//images[2].Manifest, but it can't because of the manifest-manifest ref to
	//the image list
	expectedError := fmt.Sprintf(`while syncing manifests in a replica repo: cannot remove deleted manifests [%s] in repo test1/foo because they are still being referenced by other manifests (this smells like an inconsistency on the primary account)`,
		images[2].Manifest.Digest.String(),
	)
	expectError(t, expectedError, j2.SyncManifestsInNextRepo())
	//the DB should not have changed since the operation was aborted
	easypg.AssertDBContent(t, db2.DbMap.Db, "fixtures/manifest-sync-003.sql") //unchanged

	//also remove the image list manifest on the primary side
	clock.StepBy(2 * time.Hour)
	mustExec(t, db1,
		`DELETE FROM manifests WHERE digest = $1`,
		imageList.Manifest.Digest.String(),
	)

	//this makes the primary side consistent again, so SyncManifestsInNextRepo
	//should succeed now and remove both deleted manifests from the DB
	expectSuccess(t, j2.SyncManifestsInNextRepo())
	easypg.AssertDBContent(t, db2.DbMap.Db, "fixtures/manifest-sync-004.sql")
	expectError(t, sql.ErrNoRows.Error(), j2.SyncManifestsInNextRepo())
	easypg.AssertDBContent(t, db2.DbMap.Db, "fixtures/manifest-sync-004.sql")

	//replace the primary registry's API with something that just answers 404 all the time
	clock.StepBy(2 * time.Hour)
	http.DefaultClient.Transport.(*test.RoundTripper).Handlers["registry.example.org"] = http.HandlerFunc(answerWith404)
	//This is particularly devious since 404 is returned by the GET endpoint for
	//a manifest when the manifest was deleted. We want to check that the next
	//SyncManifestsInNextRepo understands that this is a network issue and not
	//caused by the manifest getting deleted, since the 404-generating endpoint
	//does not render a proper MANIFEST_UNKNOWN error.
	expectedError = fmt.Sprintf(`while syncing manifests in a replica repo: cannot check existence of manifest test1/foo/%s on primary account: during GET https://registry.example.org/v2/test1/foo/manifests/%[1]s: expected status 200, but got 404 Not Found`,
		images[1].Manifest.Digest.String(), //the only manifest that is left
	)
	expectError(t, expectedError, j2.SyncManifestsInNextRepo())
	easypg.AssertDBContent(t, db2.DbMap.Db, "fixtures/manifest-sync-004.sql") //unchanged

	//check that the manifest sync did not update the last_pulled_at timestamps
	//in the primary DB (even though there were GET requests for the manifests
	//there)
	var lastPulledAt time.Time
	expectSuccess(t, db1.DbMap.QueryRow(`SELECT MAX(last_pulled_at) FROM manifests`).Scan(&lastPulledAt))
	if !lastPulledAt.Equal(expectedLastPulledAt) {
		t.Error("last_pulled_at timestamps on the primary side were touched")
		t.Logf("  expected = %#v", expectedLastPulledAt)
		t.Logf("  actual   = %#v", lastPulledAt)
	}
}

func answerWith404(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "not found", http.StatusNotFound)
}

func TestCheckVulnerabilitiesForNextManifest(t *testing.T) {
	j, _, db, _, sd, clock, h := setup(t)
	clock.StepBy(1 * time.Hour)

	//setup two image manifests with just one content layer (we don't really care about
	//the content since our Clair double doesn't care either)
	images := make([]test.Image, 2)
	for idx := range images {
		image := test.GenerateImage(test.GenerateExampleLayer(int64(idx)))
		images[idx] = image

		configBlob := uploadBlob(t, db, sd, clock, image.Config)
		layerBlob := uploadBlob(t, db, sd, clock, image.Layers[0])
		uploadManifest(t, db, sd, clock, image.Manifest, image.SizeBytes())
		mustExec(t, db,
			`INSERT INTO manifest_blob_refs (blob_id, repo_id, digest) VALUES ($1, 1, $2)`,
			configBlob.ID, image.Manifest.Digest.String(),
		)
		mustExec(t, db,
			`INSERT INTO manifest_blob_refs (blob_id, repo_id, digest) VALUES ($1, 1, $2)`,
			layerBlob.ID, image.Manifest.Digest.String(),
		)
	}

	//also setup an image list manifest containing those images (so that we have
	//some manifest-manifest refs to play with)
	imageList := test.GenerateImageList(images[0].Manifest, images[1].Manifest)
	uploadManifest(t, db, sd, clock, imageList.Manifest, imageList.SizeBytes())
	for _, image := range images {
		mustExec(t, db,
			`INSERT INTO manifest_manifest_refs (repo_id, parent_digest, child_digest) VALUES (1, $1, $2)`,
			imageList.Manifest.Digest.String(), image.Manifest.Digest.String(),
		)
	}

	easypg.AssertDBContent(t, db.DbMap.Db, "fixtures/vulnerability-check-000.sql")

	//setup our Clair API double
	claird := test.NewClairDouble()
	tt := &test.RoundTripper{
		Handlers: map[string]http.Handler{
			"registry.example.org": h,
			"clair.example.org":    api.Compose(claird),
		},
	}
	http.DefaultClient.Transport = tt
	j.cfg.ClairClient = &clair.Client{
		BaseURL:      mustParseURL("https://clair.example.org/"),
		PresharedKey: []byte("doesnotmatter"), //since the ClairDouble does not check the Authorization header
	}

	//ClairDouble wants to know which image manifests to expect (only the
	//non-list manifests are relevant here; the list manifest does not contain
	//any blobs and thus only aggregates its submanifests' vulnerability
	//statuses)
	for idx, image := range images {
		claird.IndexFixtures[image.Manifest.Digest.String()] = fmt.Sprintf("fixtures/clair/manifest-%03d.json", idx+1)
	}
	//Clair support currently requires a storage driver that can do URLForBlob()
	sd.(*test.StorageDriver).AllowDummyURLs = true

	//first round of CheckVulnerabilitiesForNextManifest should submit manifests
	//to Clair for indexing, but since Clair is not done indexing yet, images
	//stay in vulnerability status "Unknown" for know
	clock.StepBy(30 * time.Minute)
	expectSuccess(t, j.CheckVulnerabilitiesForNextManifest()) //once for each manifest
	expectSuccess(t, j.CheckVulnerabilitiesForNextManifest())
	expectSuccess(t, j.CheckVulnerabilitiesForNextManifest())
	expectError(t, sql.ErrNoRows.Error(), j.CheckVulnerabilitiesForNextManifest())
	easypg.AssertDBContent(t, db.DbMap.Db, "fixtures/vulnerability-check-001.sql")

	//five minutes later, indexing is still not finished
	clock.StepBy(5 * time.Minute)
	expectSuccess(t, j.CheckVulnerabilitiesForNextManifest()) //once for each manifest
	expectSuccess(t, j.CheckVulnerabilitiesForNextManifest())
	expectSuccess(t, j.CheckVulnerabilitiesForNextManifest())
	expectError(t, sql.ErrNoRows.Error(), j.CheckVulnerabilitiesForNextManifest())
	easypg.AssertDBContent(t, db.DbMap.Db, "fixtures/vulnerability-check-002.sql")

	//five minutes later, indexing is finished now and ClairDouble provides vulnerability reports to us
	claird.ReportFixtures[images[0].Manifest.Digest.String()] = "fixtures/clair/report-vulnerable.json"
	claird.ReportFixtures[images[1].Manifest.Digest.String()] = "fixtures/clair/report-clean.json"
	clock.StepBy(5 * time.Minute)
	expectSuccess(t, j.CheckVulnerabilitiesForNextManifest()) //once for each manifest
	expectSuccess(t, j.CheckVulnerabilitiesForNextManifest())
	expectSuccess(t, j.CheckVulnerabilitiesForNextManifest())
	expectError(t, sql.ErrNoRows.Error(), j.CheckVulnerabilitiesForNextManifest())
	easypg.AssertDBContent(t, db.DbMap.Db, "fixtures/vulnerability-check-003.sql")
}

func mustParseURL(in string) url.URL {
	u, err := url.Parse(in)
	if err != nil {
		panic(err.Error())
	}
	return *u
}
