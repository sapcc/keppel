// SPDX-FileCopyrightText: 2020 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package test

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"time"

	"github.com/containers/image/v5/manifest"
	"github.com/opencontainers/go-digest"
	"github.com/opencontainers/image-spec/specs-go"
	imgspecv1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/sapcc/go-bits/assert"
	"github.com/sapcc/go-bits/must"

	"maps"

	"github.com/sapcc/keppel/internal/models"
)

// Bytes groups a bytestring with its digest.
type Bytes struct {
	Annotations map[string]string
	Contents    []byte
	Digest      digest.Digest
	MediaType   string
}

// NewBytes makes a new Bytes instance.
func NewBytes(contents []byte) Bytes {
	return newBytesWithMediaType(contents, "application/octet-stream")
}

func newBytesWithMediaType(contents []byte, mediaType string) Bytes {
	return Bytes{map[string]string{}, contents, digest.Canonical.FromBytes(contents), mediaType}
}

// NewBytesFromFile creates a Bytes instance with the contents of the given file.
func NewBytesFromFile(path string) (Bytes, error) {
	buf, err := os.ReadFile(path)
	return NewBytes(buf), err
}

// Results from GenerateExampleLayer are frequently recalculated between tests,
// so it makes sense to memoize them to shave a bit of runtime off the tests.
var exampleLayerCache = make(map[int64]Bytes)

// GenerateExampleLayer generates a blob of 1 MiB that can be used like an image
// layer when constructing image manifests for unit tests. The contents are
// generated deterministically from the given seed.
func GenerateExampleLayer(seed int64) Bytes {
	layer, ok := exampleLayerCache[seed]
	if !ok {
		layer = GenerateExampleLayerSize(seed, 1)
		if seed >= 0 && seed < 10 {
			// only the most commonly requested layers are cached to avoid excessive memory usage
			exampleLayerCache[seed] = layer
		}
	}
	return layer
}

// GenerateExampleLayerSize generates a blob of a configurable size that can be used like an image
// layer when constructing image manifests for unit tests. The contents are
// generated deterministically from the given seed.
func GenerateExampleLayerSize(seed, sizeMiB int64) Bytes {
	r := rand.New(rand.NewSource(seed)) //nolint:gosec // random data from hardcoded seed to generate data for tests
	buf := make([]byte, sizeMiB<<20)
	r.Read(buf)

	var byteBuffer bytes.Buffer
	w := gzip.NewWriter(&byteBuffer)
	w.Write(buf) //nolint: errcheck
	w.Close()

	return newBytesWithMediaType(byteBuffer.Bytes(), manifest.DockerV2Schema2LayerMediaType)
}

// Image contains all the pieces of a Docker image. The Layers and Config must
// be uploaded to the registry as blobs.
type Image struct {
	Layers   []Bytes
	Config   Bytes
	Manifest Bytes
}

// GenerateImage makes an Image from the given bytes in a deterministic manner.
func GenerateImage(layers ...Bytes) Image {
	return GenerateImageWithCustomConfig(nil, layers...)
}

func GenerateImageWithCustomConfig(change func(map[string]any), layers ...Bytes) Image {
	config := map[string]any{
		"architecture": "amd64",
		"config": map[string]any{
			"Hostname":     "",
			"Domainname":   "",
			"User":         "",
			"AttachStdin":  false,
			"AttachStdout": false,
			"AttachStderr": false,
			"Tty":          false,
			"OpenStdin":    false,
			"StdinOnce":    false,
			"Env": []string{
				"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
				"test_for=keppel",
			},
			"Cmd":        nil,
			"Image":      "",
			"Volumes":    nil,
			"WorkingDir": "",
			"Entrypoint": nil,
			"OnBuild":    nil,
			"Labels":     nil,
		},
		"container": "efd768c7229cf5030d391fb572f60cf4e22d5d85828fafb3aa5ff37997523c60",
		"container_config": map[string]any{
			"Hostname":     "efd768c7229c",
			"Domainname":   "",
			"User":         "",
			"AttachStdin":  false,
			"AttachStdout": false,
			"AttachStderr": false,
			"Tty":          false,
			"OpenStdin":    false,
			"StdinOnce":    false,
			"Env": []string{
				"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
				"test_for=keppel",
			},
			"Cmd": []string{
				"/bin/sh",
				"-c",
				"#(nop) ",
				"ENV test_for=keppel",
			},
			"Image":      "",
			"Volumes":    nil,
			"WorkingDir": "",
			"Entrypoint": nil,
			"OnBuild":    nil,
			"Labels":     nil,
		},
		"created":        makeTimestamp(86400),
		"docker_version": "19.03.1-ce",
		"history": []map[string]any{
			{
				"created":     makeTimestamp(0),
				"created_by":  "/bin/sh -c #(nop)  ENV test_for=keppel",
				"empty_layer": true,
			},
		},
		"os": "linux",
		"rootfs": map[string]any{
			"type": "layers",
		},
	}

	if change != nil {
		change(config)
	}

	// build image config referencing the given layers
	imageConfig := make(map[string]any)
	maps.Copy(imageConfig, config)
	history := []map[string]any{imageConfig["history"].([]map[string]any)[0]}
	for idx, layer := range layers {
		history = append(history, map[string]any{
			"created":    makeTimestamp(idx),
			"created_by": fmt.Sprintf("/bin/sh -c #(nop) ADD file:%s in / ", layer.Digest),
		})
	}
	imageConfig["history"] = history
	imageConfigBytes, err := json.Marshal(imageConfig)
	if err != nil {
		panic(err.Error())
	}
	imageConfigBytesObj := newBytesWithMediaType(imageConfigBytes, manifest.DockerV2Schema2ConfigMediaType)

	// build a manifest
	layerDescs := []map[string]any{}
	for _, layer := range layers {
		layerDescs = append(layerDescs, map[string]any{
			"mediaType": layer.MediaType,
			"size":      len(layer.Contents),
			"digest":    layer.Digest,
		})
	}
	manifestData := map[string]any{
		"schemaVersion": 2,
		"mediaType":     manifest.DockerV2Schema2MediaType,
		"config": assert.JSONObject{
			"mediaType": imageConfigBytesObj.MediaType,
			"size":      len(imageConfigBytes),
			"digest":    imageConfigBytesObj.Digest,
		},
		"layers": layerDescs,
	}
	manifestBytes, err := json.Marshal(manifestData)
	if err != nil {
		panic(err.Error())
	}

	return Image{
		Layers:   layers,
		Config:   imageConfigBytesObj,
		Manifest: newBytesWithMediaType(manifestBytes, manifest.DockerV2Schema2MediaType),
	}
}

// SizeBytes returns the value that we expect in the DB column
// `manifests.size_bytes` for this image.
func (i Image) SizeBytes() uint64 {
	imageSize := uint64(len(i.Manifest.Contents)) + uint64(len(i.Config.Contents))
	for _, layer := range i.Layers {
		imageSize += uint64(len(layer.Contents))
	}
	return imageSize
}

// DigestRef returns the ManifestReference for this manifest's digest.
func (i Image) DigestRef() models.ManifestReference {
	return models.ManifestReference{
		Digest: i.Manifest.Digest,
	}
}

// ImageRef returns the ImageReference for this images.
func (i Image) ImageRef(s Setup, repo models.Repository) models.ImageReference {
	return models.ImageReference{
		Host:      s.Config.APIPublicHostname,
		RepoName:  fmt.Sprintf("%s/%s", repo.AccountName, repo.Name),
		Reference: i.DigestRef(),
	}
}

// ImageList contains all the pieces of a multi-architecture Docker image. This
// type is used for testing the behavior of Keppel with manifests that reference
// other manifests.
type ImageList struct {
	Images   []Image
	Manifest Bytes
}

// GenerateImageList makes an ImageList containing the given images in a
// deterministic manner.
func GenerateImageList(images ...Image) ImageList {
	manifestDescs := []map[string]any{}
	testArchStrings := []string{"amd64", "arm", "arm64", "386", "ppc64le", "s390x"}
	for idx, img := range images {
		manifestDescs = append(manifestDescs, map[string]any{
			"mediaType": img.Manifest.MediaType,
			"size":      len(img.Manifest.Contents),
			"digest":    img.Manifest.Digest,
			"platform": map[string]string{
				"os":           "linux",
				"architecture": testArchStrings[idx],
			},
		})
	}

	manifestListBytes, err := json.Marshal(map[string]any{
		"schemaVersion": 2,
		"mediaType":     manifest.DockerV2ListMediaType,
		"manifests":     manifestDescs,
	})
	if err != nil {
		panic(err.Error())
	}

	return ImageList{
		Images:   images,
		Manifest: newBytesWithMediaType(manifestListBytes, manifest.DockerV2ListMediaType),
	}
}

// SizeBytes returns the value that we expect in the DB column
// `manifests.size_bytes` for this image.
func (l ImageList) SizeBytes() uint64 {
	imageSize := uint64(len(l.Manifest.Contents))
	for _, i := range l.Images {
		imageSize += uint64(len(i.Manifest.Contents))
	}
	return imageSize
}

// DigestRef returns the ManifestReference for this manifest's digest.
func (l ImageList) DigestRef() models.ManifestReference {
	return models.ManifestReference{
		Digest: l.Manifest.Digest,
	}
}

// ImageRef returns the ImageReference for this ImageList.
func (l ImageList) ImageRef(s Setup, repo models.Repository) models.ImageReference {
	return models.ImageReference{
		Host:      s.Config.APIPublicHostname,
		RepoName:  fmt.Sprintf("%s/%s", repo.AccountName, repo.Name),
		Reference: l.DigestRef(),
	}
}

func makeTimestamp(seconds int) string {
	return time.Unix(int64(seconds), 0).UTC().Format(time.RFC3339Nano)
}

func DeterministicDummyDigest(counter int) digest.Digest {
	return digest.SHA256.FromBytes(bytes.Repeat([]byte{1}, counter))
}

type OCIArgs struct {
	Config          map[string]any
	ConfigMediaType string
	Annotations     map[string]string
	ArtifactType    string
	SubjectDigest   digest.Digest
}

func GenerateOCIImage(ociArgs OCIArgs, layers ...Bytes) Image {
	configBytes := must.Return(json.Marshal(ociArgs.Config))

	layerDescs := []imgspecv1.Descriptor{}
	for _, layer := range layers {
		layerDescs = append(layerDescs, imgspecv1.Descriptor{
			Annotations: layer.Annotations,
			Digest:      layer.Digest,
			MediaType:   layer.MediaType,
			Size:        int64(len(layer.Contents)),
		})
	}

	// default to the standard media type for config blobs
	if ociArgs.ConfigMediaType == "" {
		ociArgs.ConfigMediaType = imgspecv1.MediaTypeImageConfig
	}

	ociManifest := manifest.OCI1{
		Manifest: imgspecv1.Manifest{
			Versioned: specs.Versioned{SchemaVersion: 2},
			MediaType: imgspecv1.MediaTypeImageManifest,
			Config: imgspecv1.Descriptor{
				MediaType: ociArgs.ConfigMediaType,
				Size:      int64(len(configBytes)),
				Digest:    digest.FromBytes(configBytes),
			},
			Layers:      layerDescs,
			Annotations: ociArgs.Annotations,
		},
	}

	if ociArgs.SubjectDigest != "" {
		ociManifest.Subject = &imgspecv1.Descriptor{
			MediaType: imgspecv1.MediaTypeImageManifest,
			Digest:    ociArgs.SubjectDigest,
		}
	}

	if ociArgs.ArtifactType != "" {
		ociManifest.ArtifactType = ociArgs.ArtifactType
	}

	return Image{
		Layers:   layers,
		Config:   newBytesWithMediaType(must.Return(json.Marshal(ociArgs.Config)), ociManifest.Config.MediaType),
		Manifest: newBytesWithMediaType(must.Return(json.Marshal(ociManifest)), ociManifest.MediaType),
	}
}
