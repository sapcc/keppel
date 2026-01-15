// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package client_test

import (
	"io"
	"testing"

	"github.com/sapcc/go-bits/assert"
	"github.com/sapcc/go-bits/easypg"
	"github.com/sapcc/go-bits/must"

	"github.com/sapcc/keppel/internal/client"
	"github.com/sapcc/keppel/internal/models"
	"github.com/sapcc/keppel/internal/test"
)

func TestMain(m *testing.M) {
	easypg.WithTestDB(m, func() int { return m.Run() })
}

func TestRepoClientBasic(t *testing.T) {
	ctx := t.Context()
	test.WithRoundTripper(func(tt *test.RoundTripper) {
		s := test.NewSetup(t,
			test.WithAccount(models.Account{Name: "test1", AuthTenantID: "test1authtenant"}),
			test.WithQuotas,
		)
		const hostName = "registry.example.org"
		tt.Handlers[hostName] = s.Handler
		s.AD.ExpectedUserName = "alice"
		s.AD.ExpectedPassword = "swordfish"
		s.AD.GrantedPermissions = "pull:test1authtenant,push:test1authtenant"

		rc := &client.RepoClient{
			Scheme:   "http",
			Host:     "registry.example.org",
			RepoName: "test1/foo",
			UserName: s.AD.ExpectedUserName,
			Password: s.AD.ExpectedPassword,
		}

		// test uploading an image using RepoClient
		img := test.GenerateImage(
			test.GenerateExampleLayer(1),
		)

		digest := must.ReturnT(rc.UploadMonolithicBlob(ctx, img.Layers[0].Contents))(t)
		assert.Equal(t, digest, img.Layers[0].Digest)

		digest = must.ReturnT(rc.UploadMonolithicBlob(ctx, img.Config.Contents))(t)
		assert.Equal(t, digest, img.Config.Digest)

		digest = must.ReturnT(rc.UploadManifest(ctx, img.Manifest.Contents, img.Manifest.MediaType, "latest"))(t)
		assert.Equal(t, digest, img.Manifest.Digest)

		// test downloading the same image using RepoClient
		buf, mediaType, err := rc.DownloadManifest(ctx, models.ManifestReference{Tag: "latest"}, nil)
		must.SucceedT(t, err)
		assert.Equal(t, string(buf), string(img.Manifest.Contents))
		assert.Equal(t, mediaType, img.Manifest.MediaType)

		readCloser, sizeBytes, err := rc.DownloadBlob(ctx, img.Config.Digest)
		must.SucceedT(t, err)
		buf = must.ReturnT(io.ReadAll(readCloser))(t)
		must.SucceedT(t, readCloser.Close())
		assert.Equal(t, sizeBytes, uint64(len(img.Config.Contents)))
		assert.Equal(t, string(buf), string(img.Config.Contents))

		readCloser, sizeBytes, err = rc.DownloadBlob(ctx, img.Layers[0].Digest)
		must.SucceedT(t, err)
		buf = must.ReturnT(io.ReadAll(readCloser))(t)
		must.SucceedT(t, readCloser.Close())
		assert.Equal(t, sizeBytes, uint64(len(img.Layers[0].Contents)))
		assert.Equal(t, string(buf), string(img.Layers[0].Contents))
	})
}
