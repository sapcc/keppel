// SPDX-FileCopyrightText: 2020 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package models

import (
	"strings"
	"testing"

	"github.com/sapcc/go-bits/assert"
)

func TestParseImageReferenceSuccess(t *testing.T) {
	// to generate a lot of test cases quickly, we start from the elements of
	// ImageReference and do a round-trip test: ParseImageReference(ref.String())
	// should yield the same Ref again
	hostNames := []string{
		defaultHostName,
		defaultHostName + ":5000",
		"registry.example.org",
		"registry.example.org:5000",
		"localhost",
		"localhost:5000",
	}
	repoNames := []string{
		"foo",
		"foo/bar123",
		"library/alpine",
	}
	references := []string{
		"sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
		"qux",
		"123",
		"latest",
		"something-else",
	}

	for _, hostName := range hostNames {
		for _, repoName := range repoNames {
			// skip repo names without slashes when considering the default registry
			// (on that one, repo names are always "user/repo", and if no user is
			// given, "library" is implied)
			if hostName == defaultHostName && !strings.Contains(repoName, "/") {
				continue
			}

			for _, reference := range references {
				ref := ImageReference{hostName, repoName, ParseManifestReference(reference)}
				parsedRef, interpretation, err := ParseImageReference(ref.String())
				if err == nil {
					if !assert.DeepEqual(t, "parse of %s", parsedRef, ref) {
						t.Logf("input interpretation was: %s", interpretation)
					}
				} else {
					t.Errorf("expected %s to parse, but got error: %s", ref, err.Error())
					t.Logf("input interpretation was: %s", interpretation)
				}
			}
		}
	}
}

func TestParseImageReferenceLabelDigestSuccess(t *testing.T) {
	registry := "localhost:5000"
	repo := "library/alpine"
	digest := "sha256:e9707504ad0d4c119036b6d41ace4a33596139d3feb9ccb6617813ce48c3eeef"
	// Check that the manifest reference :nonsense@digest is equal to @digest where :nonsense can be anything and is NOT checked.
	// This mirrors the behaviour of the official docker client to maintain compatibility.
	refActual := ImageReference{registry, repo, ParseManifestReference("nonsense@" + digest)}
	refExpected := ImageReference{registry, repo, ParseManifestReference(digest)}

	parsedRef, interpretation, err := ParseImageReference(refActual.String())
	if err == nil {
		if !assert.DeepEqual(t, "parse of %s", parsedRef, refExpected) {
			t.Logf("input interpretation was: %s", interpretation)
		}
	} else {
		t.Errorf("expected %s to parse, but got error: %s", refActual, err.Error())
		t.Logf("input interpretation was: %s", interpretation)
	}
}
