// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// go-quicbaas is dual-licensed:
//
// 1. GNU Affero General Public License v3.0 (AGPL-3.0)
//    See LICENSE file or visit https://www.gnu.org/licenses/agpl-3.0.html
//
// 2. Commercial License
//    Contact licensing@automatethethings.com for commercial licensing options.

package ociregistry

import (
	"context"
	"strconv"
	"testing"
)

const manifestV1 = "application/vnd.oci.image.manifest.v1+json"

// TestOCIDistBackend_ListReferrers pushes an image and two artifacts
// whose subject is the image, then lists them through the registry:
// every referrer once, filtered by artifactType, by config media type
// when the artifact has no artifactType, and none for another subject
// or another repository.
func TestOCIDistBackend_ListReferrers(t *testing.T) {
	reg := newTestRegistry(t)
	ctx := context.Background()

	image := []byte(`{"schemaVersion":2,"mediaType":"` + manifestV1 + `",` +
		`"config":{"mediaType":"application/vnd.oci.image.config.v1+json","digest":"sha256:` + repeat64("c") + `","size":4},"layers":[]}`)
	subject, err := reg.PutManifest(ctx, "apps/web", "v1", manifestV1, image)
	if err != nil {
		t.Fatalf("PutManifest image: %v", err)
	}
	subjectJSON := `"subject":{"mediaType":"` + manifestV1 + `","digest":"` + subject + `","size":` + strconv.Itoa(len(image)) + `}`
	sig := []byte(`{"schemaVersion":2,"mediaType":"` + manifestV1 + `","artifactType":"application/vnd.example.signature",` +
		`"config":{"mediaType":"application/vnd.oci.empty.v1+json","digest":"sha256:` + repeat64("e") + `","size":2},"layers":[],` +
		`"annotations":{"org.example":"sig"},` + subjectJSON + `}`)
	sbom := []byte(`{"schemaVersion":2,"mediaType":"` + manifestV1 + `",` +
		`"config":{"mediaType":"application/vnd.example.sbom","digest":"sha256:` + repeat64("d") + `","size":2},"layers":[],` + subjectJSON + `}`)
	sigDigest, err := reg.PutManifest(ctx, "apps/web", sha256Digest(sig), manifestV1, sig)
	if err != nil {
		t.Fatalf("PutManifest sig: %v", err)
	}
	// Tagged as well as pushed by digest: still listed once.
	sbomDigest, err := reg.PutManifest(ctx, "apps/web", "sbom", manifestV1, sbom)
	if err != nil {
		t.Fatalf("PutManifest sbom: %v", err)
	}

	all, err := reg.ListReferrers(ctx, "apps/web", subject, "")
	if err != nil {
		t.Fatalf("ListReferrers: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("ListReferrers = %+v, want the signature and the SBOM", all)
	}
	byDigest := map[string]Descriptor{}
	for _, d := range all {
		byDigest[d.Digest] = d
	}
	s, ok := byDigest[sigDigest]
	if !ok || s.ArtifactType != "application/vnd.example.signature" || s.Size != int64(len(sig)) ||
		s.MediaType != manifestV1 || s.Annotations["org.example"] != "sig" {
		t.Errorf("signature descriptor = %+v", s)
	}
	if b, ok := byDigest[sbomDigest]; !ok || b.ArtifactType != "application/vnd.example.sbom" {
		t.Errorf("SBOM descriptor = %+v (artifactType falls back to the config media type)", b)
	}

	only, err := reg.ListReferrers(ctx, "apps/web", subject, "application/vnd.example.sbom")
	if err != nil {
		t.Fatalf("ListReferrers filtered: %v", err)
	}
	if len(only) != 1 || only[0].Digest != sbomDigest {
		t.Errorf("filtered ListReferrers = %+v, want only the SBOM", only)
	}

	for _, tc := range []struct{ name, digest string }{
		{"apps/web", "sha256:" + repeat64("0")},
		{"apps/other", subject},
	} {
		got, err := reg.ListReferrers(ctx, tc.name, tc.digest, "")
		if err != nil {
			t.Fatalf("ListReferrers %s@%s: %v", tc.name, tc.digest, err)
		}
		if got == nil || len(got) != 0 {
			t.Errorf("ListReferrers %s@%s = %#v, want an empty list", tc.name, tc.digest, got)
		}
	}
}
