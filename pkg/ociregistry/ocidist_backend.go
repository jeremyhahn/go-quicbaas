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
	"errors"
	"sort"
	"strings"
)

// OCIDistBackend exposes the surface the OCI distribution-spec /v2/
// router needs. It is implemented by *Registry but kept as an
// interface in the rest/registry package so that package doesn't
// directly import pkg/registry/Registry — wiring crosses packages via
// the daemon command init code.
//
// The shape is intentionally narrower than the public Registry API:
// the spec speaks in (name, reference, digest) tuples and doesn't
// touch the OCI-image-as-a-whole concepts (Image, Layer) the
// platform-curated API uses.

// PutBlob persists a content-addressable blob. The digest is supplied
// by the caller and trusted (the /v2/ router does the integrity
// check before invoking this).
func (r *Registry) PutBlob(ctx context.Context, digest string, content []byte) error {
	if err := r.checkState(); err != nil {
		return err
	}
	if r.storage == nil {
		return StorageNotConfiguredError{Op: "PutBlob"}
	}
	if r.config.Encryption.EncryptByDefault {
		return DistributionEncryptionUnsupportedError{Op: "PutBlob"}
	}
	return r.storage.StoreBlob(ctx, digest, content)
}

// GetBlob returns the bytes at the supplied digest.
func (r *Registry) GetBlob(ctx context.Context, digest string) ([]byte, error) {
	if err := r.checkState(); err != nil {
		return nil, err
	}
	if r.storage == nil {
		return nil, StorageNotConfiguredError{Op: "GetBlob"}
	}
	return r.storage.GetBlob(ctx, digest)
}

// BlobExists reports whether the blob is present.
func (r *Registry) BlobExists(ctx context.Context, digest string) (bool, error) {
	if err := r.checkState(); err != nil {
		return false, err
	}
	if r.storage == nil {
		return false, StorageNotConfiguredError{Op: "BlobExists"}
	}
	return r.storage.BlobExists(ctx, digest)
}

// PutManifest persists a manifest under name:reference. The returned
// digest is the content-address of the manifest bytes themselves
// (sha256 of the raw body) — clients use this as the immutable
// reference for the artifact.
func (r *Registry) PutManifest(ctx context.Context, name, reference, mediaType string, content []byte) (string, error) {
	if err := r.checkState(); err != nil {
		return "", err
	}
	if r.storage == nil {
		return "", StorageNotConfiguredError{Op: "PutManifest"}
	}
	if r.config.Encryption.EncryptByDefault {
		return "", DistributionEncryptionUnsupportedError{Op: "PutManifest"}
	}
	digest := sha256Digest(content)
	// A digest reference names content: the body must hash to it, or a
	// client could replace what another reference points at.
	if strings.HasPrefix(reference, "sha256:") {
		if reference != digest {
			return "", DigestMismatchError{Expected: reference, Actual: digest}
		}
	} else if strings.Contains(reference, ":") {
		return "", InvalidDigestError{Digest: reference, Reason: "only sha256 digests are supported"}
	}
	ref := refJoin(name, reference)
	// Parse the manifest so we can store it in the platform's
	// canonical ImageManifest shape. /v2/ clients send the OCI
	// manifest JSON directly; ImageManifest is a superset that
	// matches the OCI structure for SchemaVersion=2.
	manifest, err := decodeOCIManifest(content, mediaType)
	if err != nil {
		return "", err
	}
	manifest.Reference = ref
	manifest.ContentSize = int64(len(content))
	// The pushed bytes are what clients digest and sign, so they are kept
	// as a blob and served as-is rather than re-encoded.
	if err := r.storage.StoreBlob(ctx, digest, content); err != nil {
		return "", err
	}
	manifest.ContentDigest = digest
	// A push of the same reference holds it across its no-overwrite
	// check; this store waits for it rather than landing in between.
	unlock := r.refLocks.lock(ref)
	defer unlock()
	if err := r.storage.StoreManifest(ctx, ref, manifest); err != nil {
		return "", err
	}
	// Also store the raw manifest bytes under the digest reference so
	// GET /v2/{name}/manifests/<sha256:...> works.
	if err := r.storage.StoreManifest(ctx, refJoin(name, digest), manifest); err != nil {
		return "", err
	}
	return digest, nil
}

// GetManifest returns the manifest bytes + media type for name:reference.
func (r *Registry) GetManifest(ctx context.Context, name, reference string) (string, []byte, error) {
	if err := r.checkState(); err != nil {
		return "", nil, err
	}
	if r.storage == nil {
		return "", nil, StorageNotConfiguredError{Op: "GetManifest"}
	}
	ref := refJoin(name, reference)
	manifest, err := r.storage.GetManifest(ctx, ref)
	if err != nil {
		return "", nil, err
	}
	body, err := manifestBody(ctx, r.storage, manifest)
	if err != nil {
		return "", nil, err
	}
	// Only a manifest media type is ever served as Content-Type: a
	// manifest stored through another path keeps its bytes, not a type a
	// browser would render.
	mediaType := manifest.MediaType
	if !isManifestMediaType(mediaType) {
		mediaType = defaultManifestMediaType
	}
	return mediaType, body, nil
}

// DeleteManifest removes a tag mapping. Blob GC happens via the
// platform-curated Delete; OCI clients expect this endpoint to be
// fast and not cascade.
func (r *Registry) DeleteManifest(ctx context.Context, name, reference string) error {
	if err := r.checkState(); err != nil {
		return err
	}
	if r.storage == nil {
		return StorageNotConfiguredError{Op: "DeleteManifest"}
	}
	if !strings.HasPrefix(reference, "sha256:") {
		return r.storage.DeleteManifest(ctx, refJoin(name, reference))
	}
	// Deleting by digest deletes the content, so every tag that names it
	// goes too. The whole repository is listed before anything is deleted:
	// a listing that fails leaves every record in place.
	var tags []string
	cursor := ""
	for {
		res, err := r.storage.ListManifests(ctx, ListOptions{Repository: name, Limit: 500, Cursor: cursor, Strict: true})
		if err != nil {
			return err
		}
		for i := range res.Manifests {
			m := &res.Manifests[i]
			tag := strings.TrimPrefix(m.Reference, name+":")
			if tag == m.Reference || isDigestReference(tag) || m.ContentDigest != reference {
				continue
			}
			tags = append(tags, m.Reference)
		}
		if !res.HasMore || res.NextCursor == "" {
			break
		}
		cursor = res.NextCursor
	}
	for _, ref := range tags {
		if err := r.deleteTagIfDigest(ctx, ref, reference); err != nil {
			return err
		}
	}
	return r.storage.DeleteManifest(ctx, refJoin(name, reference))
}

// deleteTagIfDigest deletes the tag ref only while it still names
// digest. It holds ref's lock across the re-read and the delete, so a
// push or PUT that moved the tag to other content since the listing
// keeps it.
func (r *Registry) deleteTagIfDigest(ctx context.Context, ref, digest string) error {
	unlock := r.refLocks.lock(ref)
	defer unlock()
	m, err := r.storage.GetManifest(ctx, ref)
	if err != nil {
		var notFound ManifestNotFoundError
		if errors.As(err, &notFound) {
			return nil
		}
		return err
	}
	if m.ContentDigest != digest {
		return nil
	}
	return r.storage.DeleteManifest(ctx, ref)
}

// ListRepositories enumerates known repositories. We walk all
// manifests and project to the unique set of "name" prefixes.
func (r *Registry) ListRepositories(ctx context.Context, limit int) ([]string, error) {
	if err := r.checkState(); err != nil {
		return nil, err
	}
	if r.storage == nil {
		return nil, StorageNotConfiguredError{Op: "ListRepositories"}
	}
	seen := map[string]struct{}{}
	cursor := ""
	for {
		res, err := r.storage.ListManifests(ctx, ListOptions{
			Limit:  500,
			Cursor: cursor,
		})
		if err != nil {
			return nil, err
		}
		for _, m := range res.Manifests {
			name, tag := splitOCIRef(m.Reference)
			if name == "" {
				continue
			}
			// The PutManifest path stores each manifest under
			// name:sha256:<digest>, and a tagged push also under
			// name:tag. A digest-keyed record splits as
			// ("name:sha256", "<hex>"), so it is mapped back to name
			// rather than skipped: a repository pushed only by digest,
			// or whose last tag was deleted, is still listed, and
			// `alpine:sha256` never shows up as a phantom repo.
			if strings.HasSuffix(name, ":sha256") && isHex64(tag) {
				name = strings.TrimSuffix(name, ":sha256")
			}
			seen[name] = struct{}{}
			if limit > 0 && len(seen) >= limit {
				break
			}
		}
		if !res.HasMore || res.NextCursor == "" {
			break
		}
		cursor = res.NextCursor
	}
	out := make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	sort.Strings(out)
	return out, nil
}

// ListTags enumerates the tags within a repository. It walks every
// storage page by following the ListManifests cursor, so a repository
// whose manifests sort beyond the first page is still listed in full.
func (r *Registry) ListTags(ctx context.Context, name string) ([]string, error) {
	if err := r.checkState(); err != nil {
		return nil, err
	}
	if r.storage == nil {
		return nil, StorageNotConfiguredError{Op: "ListTags"}
	}
	tags := []string{}
	cursor := ""
	for {
		res, err := r.storage.ListManifests(ctx, ListOptions{
			Limit:  500,
			Cursor: cursor,
		})
		if err != nil {
			return nil, err
		}
		for _, m := range res.Manifests {
			repo, tag := splitOCIRef(m.Reference)
			if repo == name && tag != "" && !strings.HasPrefix(tag, "sha256:") {
				tags = append(tags, tag)
			}
		}
		if !res.HasMore || res.NextCursor == "" || res.NextCursor == cursor {
			break
		}
		cursor = res.NextCursor
	}
	sort.Strings(tags)
	return tags, nil
}

// ListReferrers returns a descriptor for each manifest in repository
// name whose subject is digest (OCI distribution spec 1.1, referrers
// API). A non-empty artifactType keeps only the referrers of that
// type. Each manifest is stored once under its digest, so only those
// digest-keyed copies are considered and no referrer is listed twice.
// A subject with no referrers yields an empty list, not an error. The
// repository's digest-keyed manifests are listed strictly: a referrer
// manifest that cannot be read or decoded fails the listing, so a 200
// never hides an existing artifact.
func (r *Registry) ListReferrers(ctx context.Context, name, digest, artifactType string) ([]Descriptor, error) {
	if err := r.checkState(); err != nil {
		return nil, err
	}
	if r.storage == nil {
		return nil, StorageNotConfiguredError{Op: "ListReferrers"}
	}
	out := []Descriptor{}
	cursor := ""
	for {
		res, err := r.storage.ListManifests(ctx, ListOptions{
			Repository: name,
			Prefix:     "sha256:",
			Limit:      500,
			Cursor:     cursor,
			Strict:     true,
		})
		if err != nil {
			return nil, err
		}
		for i := range res.Manifests {
			m := &res.Manifests[i]
			if m.Subject == nil || m.Subject.Digest != digest {
				continue
			}
			repo, hex := splitOCIRef(m.Reference)
			if repo != name+":sha256" || !isHex64(hex) {
				continue
			}
			desc := Descriptor{
				MediaType:    m.MediaType,
				Digest:       "sha256:" + hex,
				Size:         m.ContentSize,
				Annotations:  m.Annotations,
				ArtifactType: referrerArtifactType(m),
			}
			if artifactType != "" && desc.ArtifactType != artifactType {
				continue
			}
			out = append(out, desc)
		}
		if !res.HasMore || res.NextCursor == "" || res.NextCursor == cursor {
			break
		}
		cursor = res.NextCursor
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Digest < out[j].Digest })
	return out, nil
}

// referrerArtifactType is the artifact type the referrers API reports
// for m: its artifactType, or else its config's media type, as the
// OCI distribution spec 1.1 directs.
func referrerArtifactType(m *ImageManifest) string {
	if m.ArtifactType != "" {
		return m.ArtifactType
	}
	return m.Config.MediaType
}

// refJoin joins (name, tag) into the colon-delimited form used by
// the storage layer. If reference already encodes a digest (begins
// with sha256:) it is preserved as-is.
func refJoin(name, reference string) string {
	if reference == "" {
		return name
	}
	return name + ":" + reference
}

// splitOCIRef inverts refJoin. It uses the LAST colon as separator
// since references may contain port numbers (e.g.
// registry.example:5000/foo). The sha256: digest is treated as a
// regular tag here for storage purposes.
func splitOCIRef(ref string) (string, string) {
	for i := len(ref) - 1; i >= 0; i-- {
		if ref[i] == ':' {
			repo := ref[:i]
			tag := ref[i+1:]
			if strings.Contains(tag, "/") {
				return ref, ""
			}
			return repo, tag
		}
	}
	return ref, ""
}

// decodeOCIManifest parses the spec's OCI image manifest JSON into
// the platform's ImageManifest shape. Schema differences are minimal
// since ImageManifest mirrors the OCI structure.
func decodeOCIManifest(body []byte, mediaType string) (*ImageManifest, error) {
	if len(body) == 0 {
		return nil, &MessageError{Msg: "registry: empty manifest body"}
	}
	var m ImageManifest
	if err := jsonUnmarshal(body, &m); err != nil {
		return nil, &WrappedError{Msg: "registry: decode manifest", Err: err}
	}
	// The body's mediaType is what GetManifest serves as Content-Type, so
	// it must be a manifest type and agree with the push's header: a
	// body declaring text/html would otherwise be served as HTML.
	if m.MediaType != "" {
		if !isManifestMediaType(m.MediaType) {
			return nil, InvalidManifestError{Reason: "unsupported manifest mediaType: " + m.MediaType}
		}
		if mediaType != "" && manifestBaseType(m.MediaType) != manifestBaseType(mediaType) {
			return nil, InvalidManifestError{Reason: "manifest mediaType " + m.MediaType + " does not match Content-Type " + mediaType}
		}
	} else {
		m.MediaType = mediaType
	}
	if m.MediaType == "" {
		m.MediaType = defaultManifestMediaType
	}
	if !isManifestMediaType(m.MediaType) {
		return nil, InvalidManifestError{Reason: "unsupported manifest mediaType: " + m.MediaType}
	}
	return &m, nil
}

// defaultManifestMediaType is served for a manifest that names no type.
const defaultManifestMediaType = "application/vnd.oci.image.manifest.v1+json"

// manifestMediaTypes are the media types a manifest is stored and served
// under; anything else is refused on push and never used as Content-Type.
var manifestMediaTypes = map[string]bool{
	"application/vnd.oci.image.manifest.v1+json":                true,
	"application/vnd.oci.image.index.v1+json":                   true,
	"application/vnd.docker.distribution.manifest.v2+json":      true,
	"application/vnd.docker.distribution.manifest.list.v2+json": true,
}

// manifestBaseType is mediaType without parameters, lower-cased.
func manifestBaseType(mediaType string) string {
	base, _, _ := strings.Cut(mediaType, ";")
	return strings.ToLower(strings.TrimSpace(base))
}

// isManifestMediaType reports whether mediaType is a manifest media type.
func isManifestMediaType(mediaType string) bool {
	return manifestMediaTypes[manifestBaseType(mediaType)]
}

// encodeOCIManifest emits the manifest in the spec's wire shape. We
// don't round-trip the original bytes because the platform mutates
// the manifest on store (CreatedAt/UpdatedAt timestamps). For
// digest-stable distribution-spec behavior we'd need to keep the
// raw push body alongside the parsed form — a follow-up if a
// content-addressed client tries to verify the manifest digest.
func encodeOCIManifest(m *ImageManifest) ([]byte, error) {
	if m == nil {
		return nil, &MessageError{Msg: "registry: nil manifest"}
	}
	return jsonMarshalIndent(m)
}

// manifestBody returns the bytes to serve for m: the pushed bytes when
// the distribution API stored them, checked against their digest, else
// m encoded (a manifest written by Push).
func manifestBody(ctx context.Context, storage *QuicBaaSStorage, m *ImageManifest) ([]byte, error) {
	if m.ContentDigest == "" {
		return encodeOCIManifest(m)
	}
	body, err := storage.GetBlob(ctx, m.ContentDigest)
	if err != nil {
		return nil, err
	}
	if got := sha256Digest(body); got != m.ContentDigest {
		return nil, DigestMismatchError{Expected: m.ContentDigest, Actual: got}
	}
	return body, nil
}

// jsonUnmarshal + jsonMarshalIndent are wrappers that keep the
// stdlib imports out of this file's surface so future swaps to a
// faster encoder are a single-file change.
func jsonUnmarshal(data []byte, v interface{}) error {
	return jsonDecoder()(data, v)
}

func jsonMarshalIndent(v interface{}) ([]byte, error) {
	return jsonEncoder()(v)
}

// sha256Digest returns the canonical OCI digest string for the
// supplied bytes.
func sha256Digest(content []byte) string {
	return "sha256:" + hexSha256(content)
}

// isHex64 reports whether s is a 64-character lowercase hex string —
// the canonical encoding of a SHA-256 digest. Used to recognize
// digest-keyed manifest references during repository enumeration.
func isHex64(s string) bool {
	if len(s) != 64 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}
