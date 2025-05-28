// SPDX-FileCopyrightText: 2020 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package tasks

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/gofrs/uuid/v5"
	. "github.com/majewsky/gg/option"
	"github.com/sapcc/go-bits/easypg"

	"github.com/sapcc/keppel/internal/keppel"
	"github.com/sapcc/keppel/internal/models"
	"github.com/sapcc/keppel/internal/test"
)

var (
	testUploadUUID = uuid.Must(uuid.NewV4()).String()
	testStorageID  = keppel.GenerateStorageID()
)

func TestDeleteAbandonedUploadWithZeroChunks(t *testing.T) {
	testDeleteUpload(t, func(_ context.Context, sd keppel.StorageDriver, account models.ReducedAccount) models.Upload {
		return models.Upload{
			SizeBytes: 0,
			Digest:    "",
			NumChunks: 0,
		}
	})
}

func TestDeleteAbandonedUploadWithOneChunk(t *testing.T) {
	testDeleteUpload(t, func(ctx context.Context, sd keppel.StorageDriver, account models.ReducedAccount) models.Upload {
		data := "just some test data"
		test.MustDo(t, sd.AppendToBlob(ctx, account, testStorageID, 1, Some(uint64(len(data))), strings.NewReader(data)))

		return models.Upload{
			SizeBytes: uint64(len(data)),
			Digest:    sha256Of([]byte(data)),
			NumChunks: 1,
		}
	})
}

func TestDeleteAbandonedUploadWithManyChunks(t *testing.T) {
	testDeleteUpload(t, func(ctx context.Context, sd keppel.StorageDriver, account models.ReducedAccount) models.Upload {
		chunks := []string{"just", "some", "test", "data"}
		for idx, data := range chunks {
			err := sd.AppendToBlob(ctx, account, testStorageID, uint32(idx+1), Some(uint64(len(data))), strings.NewReader(data)) //nolint:gosec // chunks has a fixed size of 4
			if err != nil {
				t.Fatalf("AppendToBlob %d failed: %s", idx, err.Error())
			}
		}

		fullData := strings.Join(chunks, "")
		return models.Upload{
			SizeBytes: uint64(len(fullData)),
			Digest:    sha256Of([]byte(fullData)),
			NumChunks: 1,
		}
	})
}

func testDeleteUpload(t *testing.T, setupUploadObject func(context.Context, keppel.StorageDriver, models.ReducedAccount) models.Upload) {
	j, s := setup(t)
	account := models.ReducedAccount{Name: "test1"}
	uploadJob := j.AbandonedUploadCleanupJob(s.Registry)

	// right now, there are no upload objects, so DeleteNextAbandonedUpload should indicate that
	s.Clock.StepBy(48 * time.Hour)
	expectNoRows(t, uploadJob.ProcessOne(s.Ctx))

	// create the upload object for this test
	upload := setupUploadObject(s.Ctx, s.SD, account)
	// apply common attributes
	upload.RepositoryID = 1
	upload.UUID = testUploadUUID
	upload.StorageID = testStorageID
	upload.UpdatedAt = s.Clock.Now()
	test.MustInsert(t, s.DB, &upload)

	// DeleteNextAbandonedUpload should not do anything since this upload is fairly recent
	s.Clock.StepBy(3 * time.Hour)
	expectNoRows(t, uploadJob.ProcessOne(s.Ctx))

	// after a day has passed, DeleteNextAbandonedUpload should clean up this upload
	s.Clock.StepBy(24 * time.Hour)
	err := uploadJob.ProcessOne(s.Ctx)
	if err != nil {
		t.Errorf("expected no error, but got: %s", err.Error())
	}

	// now the DB should not contain any traces of the upload, only the account and repo
	easypg.AssertDBContent(t, s.DB.Db, "fixtures/after-delete-upload.sql")

	// and once again, DeleteNextAbandonedUpload should indicate that there's nothing to do
	expectNoRows(t, uploadJob.ProcessOne(s.Ctx))
}

func expectNoRows(t *testing.T, err error) {
	t.Helper()
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return
	case err == nil:
		t.Error("expected sql.ErrNoRows, but got no error")
	default:
		t.Errorf("expected sql.ErrNoRows, but got: %s", err.Error())
	}
}

func sha256Of(data []byte) string {
	sha256Hash := sha256.Sum256(data)
	return hex.EncodeToString(sha256Hash[:])
}
