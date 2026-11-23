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
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
)

// TestOCIDistBackend_BlobPutGet verifies the blob primitives the
// /v2/ router needs.
func TestOCIDistBackend_BlobPutGet(t *testing.T) {
	reg := newTestRegistry(t)
	ctx := context.Background()

	content := []byte("layer bytes")
	sum := sha256.Sum256(content)
	digest := "sha256:" + hex.EncodeToString(sum[:])

	if err := reg.PutBlob(ctx, digest, content); err != nil {
		t.Fatalf("PutBlob: %v", err)
	}
	exists, err := reg.BlobExists(ctx, digest)
	if err != nil {
		t.Fatalf("BlobExists: %v", err)
	}
	if !exists {
		t.Fatal("expected blob to exist after PutBlob")
	}
	got, err := reg.GetBlob(ctx, digest)
	if err != nil {
		t.Fatalf("GetBlob: %v", err)
	}
	if string(got) != string(content) {
		t.Errorf("GetBlob = %q, want %q", got, content)
	}
}

// TestOCIDistBackend_ManifestRoundTrip verifies the manifest
// primitives + the dual-reference (tag + digest) storage so GET by
// digest works.
func TestOCIDistBackend_ManifestRoundTrip(t *testing.T) {
	reg := newTestRegistry(t)
	ctx := context.Background()

	manifest := []byte(`{
		"schemaVersion": 2,
		"mediaType": "application/vnd.oci.image.manifest.v1+json",
		"config": {
			"mediaType": "application/vnd.oci.image.config.v1+json",
			"digest": "sha256:` + repeat64("c") + `",
			"size": 4
		},
		"layers": []
	}`)
	digest, err := reg.PutManifest(ctx, "alpine", "3.20", "application/vnd.oci.image.manifest.v1+json", manifest)
	if err != nil {
		t.Fatalf("PutManifest: %v", err)
	}
	if digest == "" {
		t.Fatal("PutManifest returned empty digest")
	}

	// Tag lookup.
	mediaType, body, err := reg.GetManifest(ctx, "alpine", "3.20")
	if err != nil {
		t.Fatalf("GetManifest by tag: %v", err)
	}
	if mediaType == "" {
		t.Errorf("media type missing")
	}
	if len(body) == 0 {
		t.Errorf("manifest body empty")
	}

	// Digest lookup.
	_, _, err = reg.GetManifest(ctx, "alpine", digest)
	if err != nil {
		t.Fatalf("GetManifest by digest: %v", err)
	}
}

// TestOCIDistBackend_PutManifestDigestReferenceMismatch checks that a
// manifest PUT by digest must hash to that digest, so it cannot replace
// the content another digest names.
func TestOCIDistBackend_PutManifestDigestReferenceMismatch(t *testing.T) {
	reg := newTestRegistry(t)
	ctx := context.Background()
	const mt = "application/vnd.oci.image.manifest.v1+json"
	mk := func(c string) []byte {
		return []byte(`{"schemaVersion":2,"mediaType":"` + mt + `","config":{"mediaType":"application/vnd.oci.image.config.v1+json","digest":"sha256:` +
			repeat64(c) + `","size":4},"layers":[]}`)
	}
	manifestA, manifestB := mk("a"), mk("b")
	sumA := sha256.Sum256(manifestA)
	digestA := "sha256:" + hex.EncodeToString(sumA[:])

	got, err := reg.PutManifest(ctx, "alpine", digestA, mt, manifestA)
	if err != nil {
		t.Fatalf("PutManifest A by its digest: %v", err)
	}
	if got != digestA {
		t.Fatalf("digest = %s, want %s", got, digestA)
	}
	_, before, err := reg.GetManifest(ctx, "alpine", digestA)
	if err != nil {
		t.Fatalf("GetManifest A: %v", err)
	}

	_, err = reg.PutManifest(ctx, "alpine", digestA, mt, manifestB)
	var mismatch DigestMismatchError
	if !errors.As(err, &mismatch) {
		t.Fatalf("PUT of B at A's digest: want DigestMismatchError, got %v", err)
	}
	_, after, err := reg.GetManifest(ctx, "alpine", digestA)
	if err != nil {
		t.Fatalf("GetManifest A after rejected PUT: %v", err)
	}
	if string(after) != string(before) {
		t.Errorf("content behind A's digest changed:\n%s\nwant\n%s", after, before)
	}

	_, err = reg.PutManifest(ctx, "alpine", "sha512:"+repeat64("a"), mt, manifestB)
	var invalid InvalidDigestError
	if !errors.As(err, &invalid) {
		t.Errorf("non-sha256 digest reference: want InvalidDigestError, got %v", err)
	}

	sumB := sha256.Sum256(manifestB)
	if _, err := reg.PutManifest(ctx, "alpine", "sha256:"+hex.EncodeToString(sumB[:]), mt, manifestB); err != nil {
		t.Errorf("PUT of B by its own digest: %v", err)
	}
}

// TestOCIDistBackend_PutManifestRefusesBodyMediaType pushes, through the
// real Registry backend, manifests whose JSON mediaType is not a manifest
// type or disagrees with the push's Content-Type: each is refused and
// nothing is served for the tag, so raw JSON is never served as HTML.
func TestOCIDistBackend_PutManifestRefusesBodyMediaType(t *testing.T) {
	reg := newTestRegistry(t)
	ctx := context.Background()
	const ociManifest = "application/vnd.oci.image.manifest.v1+json"
	const ociIndex = "application/vnd.oci.image.index.v1+json"
	body := func(mt string) []byte {
		return []byte(`{"schemaVersion":2,"mediaType":"` + mt + `","config":{"mediaType":"application/vnd.oci.image.config.v1+json","digest":"sha256:` +
			repeat64("a") + `","size":4},"layers":[],"annotations":{"x":"<script>alert(1)</script>"}}`)
	}
	cases := []struct {
		name, header, bodyType string
	}{
		{"html body type", ociManifest, "text/html"},
		{"html body type with params", ociManifest, "text/html; charset=utf-8"},
		{"body disagrees with header", ociManifest, ociIndex},
		{"html header and body", "text/html", "text/html"},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tag := "bad" + string(rune('a'+i))
			_, err := reg.PutManifest(ctx, "alpine", tag, tc.header, body(tc.bodyType))
			var invalid InvalidManifestError
			if !errors.As(err, &invalid) {
				t.Fatalf("PutManifest: want InvalidManifestError, got %v", err)
			}
			if _, _, err := reg.GetManifest(ctx, "alpine", tag); err == nil {
				t.Fatalf("GetManifest of refused tag %s succeeded", tag)
			}
		})
	}

	// An agreeing body type, with parameters on the header, is accepted
	// and served under the body's type.
	if _, err := reg.PutManifest(ctx, "alpine", "good", ociManifest+"; charset=utf-8", body(ociManifest)); err != nil {
		t.Fatalf("PutManifest agreeing type: %v", err)
	}
	mt, _, err := reg.GetManifest(ctx, "alpine", "good")
	if err != nil {
		t.Fatalf("GetManifest good: %v", err)
	}
	if mt != ociManifest {
		t.Errorf("Content-Type = %q, want %q", mt, ociManifest)
	}
}

// TestOCIDistBackend_GetManifestServesOnlyManifestTypes stores a manifest
// with a non-manifest media type behind the distribution API's back and
// checks GetManifest never serves that type as Content-Type.
func TestOCIDistBackend_GetManifestServesOnlyManifestTypes(t *testing.T) {
	reg := newTestRegistry(t)
	ctx := context.Background()
	const ociManifest = "application/vnd.oci.image.manifest.v1+json"
	content := []byte(`{"schemaVersion":2,"mediaType":"` + ociManifest + `","layers":[]}`)
	if _, err := reg.PutManifest(ctx, "alpine", "v1", ociManifest, content); err != nil {
		t.Fatalf("PutManifest: %v", err)
	}
	m, err := reg.storage.GetManifest(ctx, "alpine:v1")
	if err != nil {
		t.Fatalf("storage.GetManifest: %v", err)
	}
	m.MediaType = "text/html"
	if err := reg.storage.StoreManifest(ctx, "alpine:v1", m); err != nil {
		t.Fatalf("StoreManifest: %v", err)
	}
	mt, _, err := reg.GetManifest(ctx, "alpine", "v1")
	if err != nil {
		t.Fatalf("GetManifest: %v", err)
	}
	if mt != ociManifest {
		t.Errorf("Content-Type = %q, want %q", mt, ociManifest)
	}
}

// TestOCIDistBackend_ListReposAndTags walks repositories and tags.
func TestOCIDistBackend_ListReposAndTags(t *testing.T) {
	reg := newTestRegistry(t)
	ctx := context.Background()
	m := []byte(`{
		"schemaVersion": 2,
		"mediaType": "application/vnd.oci.image.manifest.v1+json",
		"config": {"digest": "sha256:` + repeat64("c") + `", "size": 4},
		"layers": []
	}`)
	for _, ref := range []struct{ name, tag string }{
		{"alpine", "3.20"},
		{"alpine", "3.21"},
		{"distroless/static", "latest"},
	} {
		if _, err := reg.PutManifest(ctx, ref.name, ref.tag, "application/vnd.oci.image.manifest.v1+json", m); err != nil {
			t.Fatalf("PutManifest %s:%s: %v", ref.name, ref.tag, err)
		}
	}

	repos, err := reg.ListRepositories(ctx, 0)
	if err != nil {
		t.Fatalf("ListRepositories: %v", err)
	}
	wantRepos := map[string]bool{"alpine": true, "distroless/static": true}
	for _, r := range repos {
		delete(wantRepos, r)
	}
	if len(wantRepos) != 0 {
		t.Errorf("missing repos: %v (got %v)", wantRepos, repos)
	}

	tags, err := reg.ListTags(ctx, "alpine")
	if err != nil {
		t.Fatalf("ListTags: %v", err)
	}
	if len(tags) != 2 {
		t.Errorf("expected 2 alpine tags, got %v", tags)
	}
}

// TestOCIDistBackend_ListTagsBeyondFirstPage puts more manifests than one
// storage page holds ahead of the target repository, so its tags only
// appear on a later page: ListTags must follow the storage cursor.
func TestOCIDistBackend_ListTagsBeyondFirstPage(t *testing.T) {
	reg := newTestRegistry(t)
	ctx := context.Background()
	m := []byte(`{
		"schemaVersion": 2,
		"mediaType": "application/vnd.oci.image.manifest.v1+json",
		"config": {"digest": "sha256:` + repeat64("c") + `", "size": 4},
		"layers": []
	}`)
	const fillers = 520
	for i := 0; i < fillers; i++ {
		tag := fmt.Sprintf("t%04d", i)
		if _, err := reg.PutManifest(ctx, "aaa/filler", tag, "application/vnd.oci.image.manifest.v1+json", m); err != nil {
			t.Fatalf("PutManifest aaa/filler:%s: %v", tag, err)
		}
	}
	for _, tag := range []string{"3.20", "3.21"} {
		if _, err := reg.PutManifest(ctx, "alpine", tag, "application/vnd.oci.image.manifest.v1+json", m); err != nil {
			t.Fatalf("PutManifest alpine:%s: %v", tag, err)
		}
	}

	tags, err := reg.ListTags(ctx, "alpine")
	if err != nil {
		t.Fatalf("ListTags alpine: %v", err)
	}
	if len(tags) != 2 || tags[0] != "3.20" || tags[1] != "3.21" {
		t.Errorf("ListTags alpine = %v, want [3.20 3.21]", tags)
	}

	filler, err := reg.ListTags(ctx, "aaa/filler")
	if err != nil {
		t.Fatalf("ListTags aaa/filler: %v", err)
	}
	if len(filler) != fillers {
		t.Errorf("ListTags aaa/filler returned %d tags, want %d", len(filler), fillers)
	}
}

// TestOCIDistBackend_RefusesPlaintextWhenEncryptByDefault checks that the
// distribution route, which stores the client's bytes as sent, refuses
// writes while encryption by default is on.
func TestOCIDistBackend_RefusesPlaintextWhenEncryptByDefault(t *testing.T) {
	ctx := context.Background()
	content := []byte("layer bytes")
	digest := sha256Digest(content)

	storage, err := NewQuicBaaSStorage(newMockStorageClient())
	if err != nil {
		t.Fatalf("NewQuicBaaSStorage: %v", err)
	}
	cfg := validTestConfig()
	cfg.Encryption.EncryptByDefault = true
	reg, err := NewRegistry(cfg, testLogger(), WithStorage(storage))
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	t.Cleanup(func() { _ = reg.Close() })

	var refused DistributionEncryptionUnsupportedError
	if err := reg.PutBlob(ctx, digest, content); !errors.As(err, &refused) {
		t.Fatalf("PutBlob error = %v, want DistributionEncryptionUnsupportedError", err)
	}
	manifest := []byte(`{"schemaVersion":2,"mediaType":"application/vnd.oci.image.manifest.v1+json","layers":[]}`)
	if _, err := reg.PutManifest(ctx, "alpine", "3.20", "application/vnd.oci.image.manifest.v1+json", manifest); !errors.As(err, &refused) {
		t.Fatalf("PutManifest error = %v, want DistributionEncryptionUnsupportedError", err)
	}
	exists, err := reg.BlobExists(ctx, digest)
	if err != nil {
		t.Fatalf("BlobExists: %v", err)
	}
	if exists {
		t.Fatal("a refused PutBlob stored the blob")
	}
	if _, _, err := reg.GetManifest(ctx, "alpine", "3.20"); err == nil {
		t.Fatal("a refused PutManifest stored the manifest")
	}

	if err := newTestRegistry(t).PutBlob(ctx, digest, content); err != nil {
		t.Fatalf("PutBlob with encryption by default off: %v", err)
	}
}

// TestOCIDistBackend_ManifestBytesRoundTrip checks that a manifest GET
// serves the bytes that were pushed, so they hash to the digest.
func TestOCIDistBackend_ManifestBytesRoundTrip(t *testing.T) {
	reg := newTestRegistry(t)
	ctx := context.Background()
	const mediaType = "application/vnd.oci.image.manifest.v1+json"
	body := []byte("{\n   \"layers\" : [ ],\n\t\"mediaType\":\"" + mediaType + "\",  \"schemaVersion\": 2,\n" +
		"  \"config\": {\"size\": 4, \"digest\": \"sha256:" + repeat64("c") + "\", \"mediaType\": \"application/vnd.oci.image.config.v1+json\"}\n}\n")

	digest, err := reg.PutManifest(ctx, "alpine", "3.20", mediaType, body)
	if err != nil {
		t.Fatalf("PutManifest: %v", err)
	}
	if digest != sha256Digest(body) {
		t.Fatalf("digest = %s, want %s", digest, sha256Digest(body))
	}
	for _, ref := range []string{"3.20", digest} {
		_, got, err := reg.GetManifest(ctx, "alpine", ref)
		if err != nil {
			t.Fatalf("GetManifest %s: %v", ref, err)
		}
		if string(got) != string(body) {
			t.Fatalf("GetManifest %s served\n%s\nwant\n%s", ref, got, body)
		}
		if sha256Digest(got) != digest {
			t.Fatalf("GetManifest %s bytes hash to %s, want %s", ref, sha256Digest(got), digest)
		}
	}
}

// TestOCIDistBackend_IndexRoundTrip checks that an image index keeps its
// child descriptors.
func TestOCIDistBackend_IndexRoundTrip(t *testing.T) {
	reg := newTestRegistry(t)
	ctx := context.Background()
	const mediaType = "application/vnd.oci.image.index.v1+json"
	childA := "sha256:" + repeat64("a")
	childB := "sha256:" + repeat64("b")
	body := []byte(`{"schemaVersion":2,"mediaType":"` + mediaType + `","manifests":[` +
		`{"mediaType":"application/vnd.oci.image.manifest.v1+json","digest":"` + childA + `","size":10,"platform":{"architecture":"amd64","os":"linux"}},` +
		`{"mediaType":"application/vnd.oci.image.manifest.v1+json","digest":"` + childB + `","size":11,"platform":{"architecture":"arm64","os":"linux"}}]}`)

	digest, err := reg.PutManifest(ctx, "alpine", "multi", mediaType, body)
	if err != nil {
		t.Fatalf("PutManifest: %v", err)
	}
	gotType, got, err := reg.GetManifest(ctx, "alpine", "multi")
	if err != nil {
		t.Fatalf("GetManifest: %v", err)
	}
	if string(got) != string(body) {
		t.Fatalf("GetManifest served\n%s\nwant\n%s", got, body)
	}
	if gotType != mediaType {
		t.Errorf("media type = %q, want %q", gotType, mediaType)
	}
	for _, ref := range []string{"multi", digest} {
		m, err := reg.storage.GetManifest(ctx, refJoin("alpine", ref))
		if err != nil {
			t.Fatalf("storage.GetManifest %s: %v", ref, err)
		}
		if len(m.Manifests) != 2 || m.Manifests[0].Digest != childA || m.Manifests[1].Digest != childB {
			t.Fatalf("stored index %s children = %+v, want %s and %s", ref, m.Manifests, childA, childB)
		}
	}
}

// TestOCIDistBackend_DeleteByDigestRemovesTags checks that deleting a
// manifest by digest leaves no tag resolving to it.
func TestOCIDistBackend_DeleteByDigestRemovesTags(t *testing.T) {
	reg := newTestRegistry(t)
	ctx := context.Background()
	const mediaType = "application/vnd.oci.image.manifest.v1+json"
	body := []byte(`{"schemaVersion":2,"mediaType":"` + mediaType + `","layers":[]}`)
	other := []byte(`{"schemaVersion":2,"mediaType":"` + mediaType + `","layers":[],"annotations":{"k":"v"}}`)

	var digest string
	for _, tag := range []string{"a", "b"} {
		d, err := reg.PutManifest(ctx, "alpine", tag, mediaType, body)
		if err != nil {
			t.Fatalf("PutManifest %s: %v", tag, err)
		}
		digest = d
	}
	// A tag naming other content survives.
	if _, err := reg.PutManifest(ctx, "alpine", "c", mediaType, other); err != nil {
		t.Fatalf("PutManifest c: %v", err)
	}

	if err := reg.DeleteManifest(ctx, "alpine", digest); err != nil {
		t.Fatalf("DeleteManifest by digest: %v", err)
	}
	for _, ref := range []string{"a", "b", digest} {
		if _, _, err := reg.GetManifest(ctx, "alpine", ref); err == nil {
			t.Errorf("GetManifest %s still resolves after delete by digest", ref)
		}
	}
	tags, err := reg.ListTags(ctx, "alpine")
	if err != nil {
		t.Fatalf("ListTags: %v", err)
	}
	if len(tags) != 1 || tags[0] != "c" {
		t.Fatalf("ListTags = %v, want [c]", tags)
	}
	if _, _, err := reg.GetManifest(ctx, "alpine", "c"); err != nil {
		t.Fatalf("GetManifest c: %v", err)
	}
}

// retagAfterListClient runs afterList once, right after the first read
// of a manifest key ending in tagKey that follows arming it. Delete by
// digest lists the keys and then reads each manifest, so a test arms it
// before the delete to move the tag between the scan reading it and the
// deletes.
type retagAfterListClient struct {
	*mockStorageClient
	tagKey    string
	armed     atomic.Bool
	afterList func()
}

func (c *retagAfterListClient) Get(ctx context.Context, entityType, key string) ([]byte, bool, error) {
	value, found, err := c.mockStorageClient.Get(ctx, entityType, key)
	if entityType == entityTypeManifest && strings.HasSuffix(key, c.tagKey) && c.armed.CompareAndSwap(true, false) {
		c.afterList()
	}
	return value, found, err
}

// TestOCIDistBackend_DeleteByDigestKeepsRetaggedTag moves a tag to other
// content after delete-by-digest listed it: the tag now names other
// content and must survive.
func TestOCIDistBackend_DeleteByDigestKeepsRetaggedTag(t *testing.T) {
	ctx := context.Background()
	const mediaType = "application/vnd.oci.image.manifest.v1+json"
	body := []byte(`{"schemaVersion":2,"mediaType":"` + mediaType + `","layers":[]}`)
	other := []byte(`{"schemaVersion":2,"mediaType":"` + mediaType + `","layers":[],"annotations":{"k":"v"}}`)

	client := &retagAfterListClient{mockStorageClient: newMockStorageClient(), tagKey: "alpine:v1", afterList: func() {}}
	storage, err := NewQuicBaaSStorage(client)
	if err != nil {
		t.Fatalf("NewQuicBaaSStorage: %v", err)
	}
	reg, err := NewRegistry(validTestConfig(), testLogger(), WithStorage(storage))
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	t.Cleanup(func() { _ = reg.Close() })

	digest, err := reg.PutManifest(ctx, "alpine", "v1", mediaType, body)
	if err != nil {
		t.Fatalf("PutManifest v1: %v", err)
	}
	client.afterList = func() {
		if _, err := reg.PutManifest(ctx, "alpine", "v1", mediaType, other); err != nil {
			t.Errorf("retag v1: %v", err)
		}
	}
	client.armed.Store(true)
	if err := reg.DeleteManifest(ctx, "alpine", digest); err != nil {
		t.Fatalf("DeleteManifest by digest: %v", err)
	}
	_, got, err := reg.GetManifest(ctx, "alpine", "v1")
	if err != nil {
		t.Fatalf("GetManifest v1 after retag: %v", err)
	}
	if string(got) != string(other) {
		t.Fatalf("v1 = %s, want the retagged manifest", got)
	}
	if _, _, err := reg.GetManifest(ctx, "alpine", digest); err == nil {
		t.Error("the deleted digest still resolves")
	}
}
