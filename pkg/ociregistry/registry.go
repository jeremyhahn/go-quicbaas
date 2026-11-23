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
	"encoding/base64"
	"errors"
	"log/slog"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// TrustedRegistry is a container registry requiring host attestation.
// It provides TPM-based attestation verification, encrypted image storage,
// and policy-based access control via QuicBaaS integration.
type TrustedRegistry interface {
	// Push stores a container image in the registry.
	// If attestation is required, the client must be verified first.
	Push(ctx context.Context, ref string, image *Image, opts PushOptions) error

	// Pull retrieves a container image from the registry.
	// If the image is encrypted, it will be decrypted transparently.
	Pull(ctx context.Context, ref string, opts PullOptions) (*Image, error)

	// Delete removes a container image from the registry.
	Delete(ctx context.Context, ref string) error

	// List returns image manifests matching the given options.
	List(ctx context.Context, opts RegistryListOptions) ([]*ImageManifest, error)

	// VerifyClient verifies a client's attestation evidence.
	// Returns nil if the client is authorized to access the registry.
	VerifyClient(ctx context.Context, attestation *AttestationEvidence) error

	// EncryptImage encrypts an image in place using the specified key.
	// The image must already exist in the registry.
	EncryptImage(ctx context.Context, ref string, keyID string) error

	// DecryptImage retrieves and decrypts an encrypted image.
	DecryptImage(ctx context.Context, ref string) (*Image, error)

	// Close releases all resources held by the registry.
	Close() error
}

// PushOptions configures image push behavior.
type PushOptions struct {
	// Encrypt indicates whether to encrypt the image
	Encrypt bool

	// KeyID is the encryption key ID (required if Encrypt is true)
	KeyID string

	// AllowOverwrite allows overwriting an existing image
	AllowOverwrite bool

	// SignImage indicates whether to sign the image
	SignImage bool

	// SigningKeyID is the key ID for image signing
	SigningKeyID string

	// Annotations are metadata to add to the manifest
	Annotations map[string]string

	// Timeout overrides the default operation timeout
	Timeout time.Duration
}

// PullOptions configures image pull behavior.
type PullOptions struct {
	// DecryptIfEncrypted will attempt to decrypt encrypted images
	DecryptIfEncrypted bool

	// VerifySignature will verify the image signature if signed
	VerifySignature bool

	// Platform specifies the target platform (e.g., "linux/amd64")
	Platform string

	// Timeout overrides the default operation timeout
	Timeout time.Duration
}

// RegistryListOptions configures image listing behavior for TrustedRegistry.
// This is separate from storage.ListOptions to allow for registry-specific options.
type RegistryListOptions struct {
	// Repository filters by repository name
	Repository string

	// TagPrefix filters tags by prefix
	TagPrefix string

	// Limit limits the number of results
	Limit int

	// Offset is the starting offset for pagination
	Offset int

	// IncludeEncrypted includes encrypted images
	IncludeEncrypted bool

	// SortBy specifies the sort field
	SortBy string

	// SortDesc reverses the sort order
	SortDesc bool
}

// PlatformInfo contains platform identification.
type PlatformInfo struct {
	// Manufacturer is the platform manufacturer
	Manufacturer string

	// Model is the platform model
	Model string

	// Version is the firmware version
	Version string

	// Serial is the platform serial number
	Serial string
}

// RegistrySessionView is a simplified view of RegistrySession for external use.
// This is used by Registry.GetSession to avoid exposing the full session details.
type RegistrySessionView struct {
	// ClientID identifies the client
	ClientID string

	// AIK is the client's Attestation Identity Key fingerprint
	AIK string

	// PCRs are the verified PCR values
	PCRs map[int][]byte

	// ValidUntil is when the session expires
	ValidUntil time.Time

	// Permissions are the granted permissions
	Permissions []string
}

// Registry implements TrustedRegistry with QuicBaaS storage.
// refLocker serialises the writers of one manifest reference, so a
// no-overwrite push's existence check and its store are one step against
// every other push or distribution PUT of the same reference.
type refLocker struct {
	mu    sync.Mutex
	locks map[string]*refLock
	// waiting counts callers blocked on a held reference; tests read it.
	waiting atomic.Int64
}

type refLock struct {
	mu   sync.Mutex
	refs int
}

// lock takes the lock for ref and returns its release.
func (l *refLocker) lock(ref string) func() {
	l.mu.Lock()
	if l.locks == nil {
		l.locks = make(map[string]*refLock)
	}
	rl := l.locks[ref]
	if rl == nil {
		rl = &refLock{}
		l.locks[ref] = rl
	}
	rl.refs++
	l.mu.Unlock()

	if !rl.mu.TryLock() {
		l.waiting.Add(1)
		rl.mu.Lock()
		l.waiting.Add(-1)
	}
	return func() {
		rl.mu.Unlock()
		l.mu.Lock()
		rl.refs--
		if rl.refs == 0 {
			delete(l.locks, ref)
		}
		l.mu.Unlock()
	}
}

type Registry struct {
	config RegistryConfig
	logger *slog.Logger

	// storage is the backing blob+manifest store. It is set via the
	// WithStorage option at construction time; without it the
	// Registry remains in an "unconfigured" state where Push/Pull/
	// List return StorageNotConfiguredError rather than silently
	// no-oping (the pre-Phase-5 behavior).
	storage *QuicBaaSStorage

	// encryptor handles per-layer encryption when Push(opts.Encrypt)
	// is requested. Set via WithEncryptor; nil disables encryption
	// (Push with Encrypt=true is then refused).
	encryptor *ImageEncryptor

	// signer signs the manifest when Push(opts.SignImage) is requested.
	// Set via WithSigner; nil disables signing (Push with SignImage=true
	// is then refused).
	signer SigningKMSClient

	// verifier checks manifest signatures when Pull(opts.VerifySignature)
	// is requested. Set via WithVerifier; nil refuses verified pulls.
	verifier ManifestVerifier

	// attestor verifies client TPM quotes in VerifyClient. Set via
	// WithAttestor; nil refuses every client when attestation is required.
	attestor Attestor

	// initialized tracks whether the registry is ready
	initialized atomic.Bool

	// closed tracks whether the registry has been closed
	closed atomic.Bool

	// sessions stores authenticated client sessions (clientID -> *RegistrySessionView)
	sessions sync.Map

	// pullSemaphore limits concurrent pulls
	pullSemaphore chan struct{}

	// pushSemaphore limits concurrent pushes
	pushSemaphore chan struct{}

	// refLocks serialises the writers of each manifest reference.
	refLocks refLocker

	// now is time.Now in production; tests replace it to advance time (#284)
	now func() time.Time
}

// RegistryOption is a functional option for constructing a Registry.
type RegistryOption func(*Registry)

// WithStorage installs the QuicBaaSStorage backend the Registry uses
// for blob + manifest + tag persistence. Required for Push/Pull/List/
// Delete to function. The Registry takes ownership of the storage
// (Close on the registry closes the storage).
func WithStorage(s *QuicBaaSStorage) RegistryOption {
	return func(r *Registry) {
		r.storage = s
	}
}

// WithEncryptor installs an ImageEncryptor so the Registry can fulfill
// Push(opts.Encrypt=true). When unset, encrypted pushes return
// EncryptionNotConfiguredError. The encryptor's underlying KMSClient is
// usually NewSDKKMSClient over the SDK's KMS and crypto clients, so the
// per-layer DEKs are wrapped by a platform KMS key.
func WithEncryptor(e *ImageEncryptor) RegistryOption {
	return func(r *Registry) {
		r.encryptor = e
	}
}

// WithSigner wires the KMS that signs manifests (SDKSigner in the daemon:
// the key a push names is the SDK KMS key of that ID). Without one, a push
// that asks for signing is refused with SigningNotConfiguredError.
func WithSigner(s SigningKMSClient) RegistryOption {
	return func(r *Registry) {
		r.signer = s
	}
}

// WithAttestor sets the TPM quote verifier VerifyClient uses when
// attestation is required.
func WithAttestor(a Attestor) RegistryOption {
	return func(r *Registry) {
		r.attestor = a
	}
}

// ManifestVerifier checks a manifest signature over a SHA-256 digest.
type ManifestVerifier interface {
	Verify(ctx context.Context, keyID, algorithm string, digest, signature []byte) error
}

// WithVerifier sets the manifest signature verifier Pull uses when
// PullOptions.VerifySignature is set.
func WithVerifier(v ManifestVerifier) RegistryOption {
	return func(r *Registry) {
		r.verifier = v
	}
}

// NewRegistry creates a new TrustedRegistry with the given configuration.
func NewRegistry(config RegistryConfig, logger *slog.Logger, opts ...RegistryOption) (*Registry, error) {
	if logger == nil {
		logger = slog.Default()
	}

	if err := config.Validate(); err != nil {
		return nil, err
	}

	r := &Registry{
		config:        config,
		logger:        logger.With("component", "registry"),
		pullSemaphore: make(chan struct{}, config.MaxConcurrentPulls),
		pushSemaphore: make(chan struct{}, config.MaxConcurrentPushes),
		now:           time.Now,
	}
	for _, opt := range opts {
		opt(r)
	}

	r.initialized.Store(true)

	r.logger.Info("trusted registry initialized",
		"listen_addr", config.ListenAddr,
		"attestation_required", config.Attestation.RequireAttestation,
		"storage_addr", config.Storage.Addr,
		"storage_namespace", config.Storage.Namespace,
		"encryption_enabled", config.Encryption.EncryptByDefault,
		"storage_configured", r.storage != nil,
	)

	return r, nil
}

// Push stores a container image in the registry. Layers are persisted
// blob-by-blob keyed by digest, the manifest is persisted at the
// supplied reference, and a tag entry is written so List() can find
// the image by ref.
//
// Every layer digest, and the config digest when a config is given,
// must be the sha256 of the pushed bytes, and the manifest's layer
// descriptors must name the pushed layers in order. The image is
// encrypted when opts.Encrypt or the EncryptByDefault setting asks for
// it; without an encryptor that push is refused, never stored in plain.
func (r *Registry) Push(ctx context.Context, ref string, image *Image, opts PushOptions) error {
	if err := r.checkState(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := validateImageRef(ref); err != nil {
		return err
	}
	if image == nil {
		return InvalidManifestError{Reference: ref, Reason: "image cannot be nil"}
	}
	if image.Manifest == nil {
		return InvalidManifestError{Reference: ref, Reason: "manifest cannot be nil"}
	}
	if r.storage == nil {
		return StorageNotConfiguredError{Op: "Push"}
	}
	encrypt := opts.Encrypt || r.config.Encryption.EncryptByDefault
	if encrypt && r.encryptor == nil {
		return EncryptionNotConfiguredError{Reference: ref}
	}
	if opts.SignImage && r.signer == nil {
		return SigningNotConfiguredError{Reference: ref}
	}
	// An image without layers has no content to serve; refuse it here so
	// every transport that pushes through the registry inherits the check.
	if len(image.Layers) == 0 {
		return ImageEmptyError{}
	}
	if err := validateImageDigests(ref, image); err != nil {
		return err
	}
	// Only the registry's signer sets the signature annotations.
	image.Manifest.Annotations = withoutSignatureAnnotations(image.Manifest.Annotations)

	select {
	case r.pushSemaphore <- struct{}{}:
		defer func() { <-r.pushSemaphore }()
	case <-ctx.Done():
		return ctx.Err()
	}
	// Close may have run while this push waited for a slot.
	if err := r.checkState(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	r.logger.Debug("pushing image",
		"ref", ref,
		"overwrite", opts.AllowOverwrite,
		"layers", len(image.Layers),
	)

	// Hold the reference from the existence check to the manifest store,
	// so two pushes (or a push and a distribution PUT) of the same absent
	// reference cannot both pass the check.
	unlock := r.refLocks.lock(ref)
	defer unlock()

	if !opts.AllowOverwrite {
		exists, err := r.storage.ManifestExists(ctx, ref)
		if err != nil {
			return err
		}
		if exists {
			return ImageAlreadyExistsError{Reference: ref}
		}
	}

	// Encrypted push: route each layer through the encryptor before
	// persisting. The encrypted blob bytes land at the layer digest
	// in storage; the manifest carries an "encrypted=true" annotation
	// plus the wrapped DEK + nonce so Pull can reverse the operation.
	var encLayers []EncryptedLayer
	var envelopes [][]byte
	if encrypt {
		keyID := opts.KeyID
		if keyID == "" {
			keyID = "registry-default"
		}
		encImage, err := r.encryptor.EncryptImage(ctx, image, keyID)
		if err != nil {
			return err
		}
		encLayers = encImage.EncryptedLayers
		if image.Manifest.Annotations == nil {
			image.Manifest.Annotations = make(map[string]string)
		}
		image.Manifest.Annotations[ManifestAnnotationEncrypted] = "true"
		image.Manifest.Annotations[ManifestAnnotationKeyID] = encImage.KeyID

		// Each envelope is stored under its own digest, never the
		// plaintext layer digest: another image may hold the same layer
		// in the clear or under another key. The layer descriptor
		// records where its envelope lives, and the signature covers it.
		envelopes = make([][]byte, len(encLayers))
		for i := range encLayers {
			env, err := encodeEncryptedLayer(&encLayers[i])
			if err != nil {
				return err
			}
			envelopes[i] = env
			d := &image.Manifest.Layers[i]
			if d.Annotations == nil {
				d.Annotations = make(map[string]string)
			}
			d.Annotations[LayerAnnotationEncryptedBlob] = sha256Digest(env)
		}
	}

	// Sign after the encryption annotations are set, so the signature
	// covers them, and before anything is stored.
	if opts.SignImage {
		if err := r.signManifest(ctx, ref, image.Manifest, opts.SigningKeyID); err != nil {
			return err
		}
	}

	for i := range image.Layers {
		l := &image.Layers[i]
		if l.Digest == "" {
			return InvalidManifestError{Reference: ref, Reason: "layer with empty digest"}
		}
		digest, blob := l.Digest, l.Content
		if encrypt {
			digest = image.Manifest.Layers[i].Annotations[LayerAnnotationEncryptedBlob]
			blob = envelopes[i]
		}
		if err := r.storage.StoreBlob(ctx, digest, blob); err != nil {
			return err
		}
	}

	// Persist the config blob if present so it survives independent of
	// the manifest. The manifest references it by digest.
	if len(image.Config) > 0 && image.Manifest.Config.Digest != "" {
		if err := r.storage.StoreBlob(ctx, image.Manifest.Config.Digest, image.Config); err != nil {
			return err
		}
	}

	now := r.now().UTC()
	if image.Manifest.CreatedAt.IsZero() {
		image.Manifest.CreatedAt = now
	}
	image.Manifest.UpdatedAt = now
	image.Manifest.Reference = ref

	return r.storage.StoreManifest(ctx, ref, image.Manifest)
}

// signManifest signs m's signing payload for ref with keyID (the default
// signing key when empty) and records the signature as annotations.
func (r *Registry) signManifest(ctx context.Context, ref string, m *ImageManifest, keyID string) error {
	if keyID == "" {
		keyID = defaultSigningKeyID
	}
	payload, err := manifestSigningPayload(ref, m)
	if err != nil {
		return ManifestSigningFailedError{KeyID: keyID, Err: err}
	}
	digest := sha256.Sum256(payload)
	sig, algorithm, err := r.signer.Sign(ctx, keyID, digest[:])
	if err != nil {
		return ManifestSigningFailedError{KeyID: keyID, Err: err}
	}
	if m.Annotations == nil {
		m.Annotations = make(map[string]string, len(signatureAnnotations))
	}
	m.Annotations[ManifestAnnotationSigned] = "true"
	m.Annotations[ManifestAnnotationSigningKeyID] = keyID
	m.Annotations[ManifestAnnotationSignatureAlgorithm] = algorithm
	m.Annotations[ManifestAnnotationSignature] = base64.StdEncoding.EncodeToString(sig)
	return nil
}

// verifyManifest checks the signature signManifest put on m, over the
// same payload, with the configured verifier. Any failure, including a
// missing verifier or an unsigned manifest, is a
// SignatureVerificationFailedError.
func (r *Registry) verifyManifest(ctx context.Context, ref string, m *ImageManifest) error {
	if r.verifier == nil {
		return SignatureVerificationFailedError{Reason: "no signature verifier configured"}
	}
	if m.Annotations[ManifestAnnotationSigned] != "true" || m.Annotations[ManifestAnnotationSignature] == "" {
		return SignatureVerificationFailedError{Reason: "manifest is not signed"}
	}
	sig, err := base64.StdEncoding.DecodeString(m.Annotations[ManifestAnnotationSignature])
	if err != nil {
		return SignatureVerificationFailedError{Reason: "signature is not valid base64", Cause: err}
	}
	payload, err := manifestSigningPayload(ref, m)
	if err != nil {
		return SignatureVerificationFailedError{Reason: "cannot build the signing payload", Cause: err}
	}
	digest := sha256.Sum256(payload)
	keyID := m.Annotations[ManifestAnnotationSigningKeyID]
	algorithm := m.Annotations[ManifestAnnotationSignatureAlgorithm]
	if err := r.verifier.Verify(ctx, keyID, algorithm, digest[:], sig); err != nil {
		return SignatureVerificationFailedError{Reason: "signature does not verify", Cause: err}
	}
	return nil
}

// Annotation keys recorded on encrypted manifests. Kept namespaced so
// they round-trip through generic OCI tooling without collision.
const (
	// ManifestAnnotationEncrypted is "true" on a manifest whose layers
	// were encrypted on push.
	ManifestAnnotationEncrypted = "org.letstrust.registry.encrypted"
	// ManifestAnnotationKeyID names the key the layers were encrypted with.
	ManifestAnnotationKeyID = "org.letstrust.registry.key-id"
	// LayerAnnotationEncryptedBlob, on a layer descriptor of an encrypted
	// manifest, is the digest the layer's envelope is stored under.
	LayerAnnotationEncryptedBlob = "org.letstrust.registry.encrypted-blob"

	// ManifestAnnotationSigned is "true" on a manifest the registry signed
	// on push.
	ManifestAnnotationSigned = "org.letstrust.registry.signed"
	// ManifestAnnotationSigningKeyID names the key the manifest was signed
	// with.
	ManifestAnnotationSigningKeyID = "org.letstrust.registry.signing-key-id"
	// ManifestAnnotationSignatureAlgorithm names the signature algorithm.
	ManifestAnnotationSignatureAlgorithm = "org.letstrust.registry.signature-algorithm"
	// ManifestAnnotationSignature is the base64 (standard encoding)
	// signature over the manifest's signing payload.
	ManifestAnnotationSignature = "org.letstrust.registry.signature"
)

// signatureAnnotations are the annotations only the registry's signer may
// set: Push strips them from every incoming manifest, so a caller cannot
// present an unsigned image as signed.
var signatureAnnotations = map[string]struct{}{
	ManifestAnnotationSigned:             {},
	ManifestAnnotationSigningKeyID:       {},
	ManifestAnnotationSignatureAlgorithm: {},
	ManifestAnnotationSignature:          {},
}

// Pull retrieves a container image from the registry. The manifest is
// looked up at the supplied reference, then each layer is fetched
// blob-by-blob using the digest recorded in the manifest. The
// returned Image always references the on-disk content directly —
// no decryption is performed today; encrypted images surface
// EncryptionNotConfiguredError.
func (r *Registry) Pull(ctx context.Context, ref string, opts PullOptions) (*Image, error) {
	if err := r.checkState(); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := validateImageRef(ref); err != nil {
		return nil, err
	}
	if r.storage == nil {
		return nil, StorageNotConfiguredError{Op: "Pull"}
	}

	select {
	case r.pullSemaphore <- struct{}{}:
		defer func() { <-r.pullSemaphore }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	// Close may have run while this pull waited for a slot.
	if err := r.checkState(); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	r.logger.Debug("pulling image", "ref", ref, "platform", opts.Platform)

	manifest, err := r.storage.GetManifest(ctx, ref)
	if err != nil {
		// Surface the public ImageNotFoundError at the registry boundary
		// so callers don't need to know about the storage-layer
		// ManifestNotFoundError, which is an internal type. Other
		// storage errors (transport, validation) pass through.
		var notFound ManifestNotFoundError
		if errors.As(err, &notFound) {
			return nil, ImageNotFoundError{Reference: ref}
		}
		return nil, err
	}
	if manifest == nil {
		return nil, ImageNotFoundError{Reference: ref}
	}
	if opts.VerifySignature {
		if err := r.verifyManifest(ctx, ref, manifest); err != nil {
			return nil, err
		}
	}

	img := &Image{Manifest: manifest}

	if manifest.Config.Digest != "" {
		data, err := r.storage.GetBlob(ctx, manifest.Config.Digest)
		if err != nil {
			return nil, err
		}
		img.Config = data
	}

	encrypted := manifest.Annotations != nil &&
		manifest.Annotations[ManifestAnnotationEncrypted] == "true"
	if encrypted && r.encryptor == nil {
		return nil, EncryptionNotConfiguredError{Reference: ref}
	}

	img.Layers = make([]Layer, 0, len(manifest.Layers))
	for _, d := range manifest.Layers {
		blobDigest := d.Digest
		if encrypted {
			// The envelope lives under its own digest; the plaintext
			// digest slot may hold another image's copy of the layer.
			blobDigest = d.Annotations[LayerAnnotationEncryptedBlob]
			if blobDigest == "" {
				return nil, InvalidManifestError{Reference: ref, Reason: "encrypted layer " + d.Digest + " names no envelope blob"}
			}
		}
		data, err := r.storage.GetBlob(ctx, blobDigest)
		if err != nil {
			return nil, err
		}
		if !encrypted {
			img.Layers = append(img.Layers, Layer{
				Digest:    d.Digest,
				MediaType: d.MediaType,
				Size:      d.Size,
				Content:   data,
			})
			continue
		}
		encLayer, err := decodeEncryptedLayer(data)
		if err != nil {
			return nil, err
		}
		if encLayer.Digest != d.Digest {
			return nil, DigestMismatchError{Expected: d.Digest, Actual: encLayer.Digest}
		}
		layer, err := r.encryptor.decryptLayer(ctx, encLayer)
		if err != nil {
			return nil, err
		}
		img.Layers = append(img.Layers, *layer)
	}
	return img, nil
}

// Delete removes a container image's manifest from the registry. Its
// layer and config blobs are left in place: a push running alongside the
// delete may be storing the same blobs, and a manifest listing cannot
// prove a blob is unreferenced, so deleting them here could lose data.
func (r *Registry) Delete(ctx context.Context, ref string) error {
	if err := r.checkState(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := validateImageRef(ref); err != nil {
		return err
	}
	if r.storage == nil {
		return StorageNotConfiguredError{Op: "Delete"}
	}

	r.logger.Debug("deleting image", "ref", ref)

	manifest, err := r.storage.GetManifest(ctx, ref)
	if err != nil {
		var notFound ManifestNotFoundError
		if errors.As(err, &notFound) {
			return ImageNotFoundError{Reference: ref}
		}
		return err
	}
	if manifest == nil {
		return ImageNotFoundError{Reference: ref}
	}

	return r.storage.DeleteManifest(ctx, ref)
}

// List returns image manifests matching the given options. The
// underlying QuicBaaSStorage performs the prefix-scan and pagination;
// IncludeEncrypted is honored at the storage layer (Phase 5 ignores
// it since encryption is deferred).
func (r *Registry) List(ctx context.Context, opts RegistryListOptions) ([]*ImageManifest, error) {
	if err := r.checkState(); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if r.storage == nil {
		return nil, StorageNotConfiguredError{Op: "List"}
	}

	r.logger.Debug("listing images",
		"repository", opts.Repository,
		"tag_prefix", opts.TagPrefix,
		"limit", opts.Limit,
	)

	sopts := ListOptions{
		Repository:  opts.Repository,
		Prefix:      opts.TagPrefix,
		Limit:       opts.Limit,
		IncludeTags: true,
	}
	res, err := r.storage.ListManifests(ctx, sopts)
	if err != nil {
		return nil, err
	}
	out := make([]*ImageManifest, len(res.Manifests))
	for i := range res.Manifests {
		m := res.Manifests[i]
		out[i] = &m
	}
	return out, nil
}

// VerifyClient verifies a client's attestation evidence.
func (r *Registry) VerifyClient(ctx context.Context, attestation *AttestationEvidence) error {
	if err := r.checkState(); err != nil {
		return err
	}

	if !r.config.Attestation.RequireAttestation {
		return nil
	}

	if attestation == nil {
		return AttestationRequiredError{}
	}

	if r.attestor == nil {
		return AttestationFailedError{Reason: "no attestation verifier configured"}
	}
	switch {
	case len(attestation.Quote) == 0:
		return AttestationFailedError{Reason: "missing quote"}
	case len(attestation.Signature) == 0:
		return AttestationFailedError{Reason: "missing signature"}
	case len(attestation.AIK) == 0:
		return AttestationFailedError{Reason: "missing AIK"}
	case attestation.ClientID == "":
		return AttestationFailedError{Reason: "missing client ID"}
	}

	r.logger.Debug("verifying client attestation",
		"client_id", attestation.ClientID,
	)

	// The quote signature is checked against the AIK; only the PCR values
	// the attestor verified are trusted, never the caller's own claim.
	verified, err := r.attestor.VerifyQuote(ctx, attestation.Quote, attestation.Signature, attestation.AIK)
	if err != nil {
		return InvalidQuoteError{Cause: err}
	}

	// Check if AIK is allowed
	if len(r.config.Attestation.AllowedAIKs) > 0 {
		allowed := false
		aikStr := aikToString(attestation.AIK)
		for _, aik := range r.config.Attestation.AllowedAIKs {
			if aik == aikStr {
				allowed = true
				break
			}
		}
		if !allowed {
			return AIKNotAllowedError{AIK: aikStr}
		}
	}

	// Check quote age
	if r.config.Attestation.QuoteMaxAge > 0 {
		if r.now().Sub(attestation.Timestamp) > r.config.Attestation.QuoteMaxAge {
			return InvalidQuoteError{Reason: "quote has expired"}
		}
	}

	// Verify required PCR values against the verified quote
	for pcrIndex, expectedValue := range r.config.Attestation.RequiredPCRs {
		actualValue, ok := verified[pcrIndex]
		if !ok {
			return PCRMismatchError{
				PCRIndex: pcrIndex,
				Expected: expectedValue,
				Actual:   nil,
			}
		}
		if !bytesEqual(expectedValue, actualValue) {
			return PCRMismatchError{
				PCRIndex: pcrIndex,
				Expected: expectedValue,
				Actual:   actualValue,
			}
		}
	}

	// Cache the session
	session := &RegistrySessionView{
		ClientID:   attestation.ClientID,
		AIK:        aikToString(attestation.AIK),
		PCRs:       verified,
		ValidUntil: r.now().Add(r.config.Attestation.CacheSessionDuration),
	}
	r.sessions.Store(attestation.ClientID, session)

	r.logger.Info("client attestation verified",
		"client_id", attestation.ClientID,
		"valid_until", session.ValidUntil,
	)

	return nil
}

// EncryptImage encrypts an image in place using the specified key.
func (r *Registry) EncryptImage(ctx context.Context, ref string, keyID string) error {
	if err := r.checkState(); err != nil {
		return err
	}

	if err := validateImageRef(ref); err != nil {
		return err
	}

	if keyID == "" {
		return InvalidConfigError{Field: "key_id", Reason: "encryption key ID required"}
	}

	r.logger.Debug("encrypting image",
		"ref", ref,
		"key_id", keyID,
	)

	// Implementation would:
	// 1. Load image manifest
	// 2. Check if already encrypted
	// 3. For each layer:
	//    a. Generate DEK
	//    b. Encrypt layer with DEK
	//    c. Wrap DEK with KEK from QuicBaaS
	//    d. Store encrypted layer
	// 4. Update manifest with encryption metadata
	// 5. Store updated manifest

	return ImageNotFoundError{Reference: ref}
}

// DecryptImage retrieves and decrypts an encrypted image.
func (r *Registry) DecryptImage(ctx context.Context, ref string) (*Image, error) {
	if err := r.checkState(); err != nil {
		return nil, err
	}

	if err := validateImageRef(ref); err != nil {
		return nil, err
	}

	r.logger.Debug("decrypting image", "ref", ref)

	// Implementation would:
	// 1. Load image manifest
	// 2. Check if encrypted
	// 3. For each layer:
	//    a. Load encrypted layer
	//    b. Unwrap DEK from QuicBaaS
	//    c. Decrypt layer with DEK
	// 4. Return decrypted image

	return nil, ImageNotFoundError{Reference: ref}
}

// Close releases all resources held by the registry.
func (r *Registry) Close() error {
	if !r.closed.CompareAndSwap(false, true) {
		return RegistryClosedError{}
	}

	r.logger.Info("closing trusted registry")

	// Clear sessions
	r.sessions.Range(func(key, value interface{}) bool {
		r.sessions.Delete(key)
		return true
	})

	// The semaphores stay open: a Push or Pull waiting on one would panic
	// on a closed channel. Each re-checks the state once it has a slot.

	r.initialized.Store(false)

	r.logger.Info("trusted registry closed")

	return nil
}

// checkState verifies the registry is ready for operations.
func (r *Registry) checkState() error {
	// Check closed first - a closed registry should return RegistryClosedError
	// even if initialized is also false
	if r.closed.Load() {
		return RegistryClosedError{}
	}
	if !r.initialized.Load() {
		return RegistryNotInitializedError{}
	}
	return nil
}

// GetSession retrieves a cached session for a client.
func (r *Registry) GetSession(clientID string) (*RegistrySessionView, bool) {
	value, ok := r.sessions.Load(clientID)
	if !ok {
		return nil, false
	}

	session, isView := value.(*RegistrySessionView)
	if !isView || r.now().After(session.ValidUntil) {
		r.sessions.Delete(clientID)
		return nil, false
	}

	return session, true
}

// InvalidateSession removes a session from the cache.
func (r *Registry) InvalidateSession(clientID string) {
	r.sessions.Delete(clientID)
}

// imageRefRegex validates image references
// Supports:
// - Simple names: alpine
// - With tag: alpine:latest
// - With repository: myrepo/myimage, myrepo/myimage:v1
// - With registry: registry.example.com/myrepo/myimage:v1
// - With port: localhost:5000/myimage:v1, registry.io:443/myrepo/myimage:tag
// - With digest: myrepo/myimage@sha256:abc123...
//
// Repository path components and tags follow the OCI distribution-spec
// grammar ("Pulling manifests"): a component is
// [a-z0-9]+((\.|_|__|-+)[a-z0-9]+)* and a tag is
// [a-zA-Z0-9_][a-zA-Z0-9._-]{0,127}.
var imageRefRegex = regexp.MustCompile(`^(?:(?:[a-z0-9]+(?:[._-][a-z0-9]+)*(?::[0-9]+)?)/)?` +
	ociPathComponent + `(?:/` + ociPathComponent + `)*` +
	`(?::` + ociTag + `)?(?:@sha256:[a-f0-9]{64})?$`)

const (
	// ociPathComponent is one OCI distribution-spec repository path component.
	ociPathComponent = `[a-z0-9]+(?:(?:[._]|__|-+)[a-z0-9]+)*`
	// ociTag is an OCI distribution-spec tag (at most 128 characters).
	ociTag = `[a-zA-Z0-9_][a-zA-Z0-9._-]{0,127}`
)

// validateImageRef validates an image reference format.
func validateImageRef(ref string) error {
	if ref == "" {
		return InvalidImageRefError{Reference: ref, Reason: "reference cannot be empty"}
	}

	// Check for overly long references
	if len(ref) > 256 {
		return InvalidImageRefError{Reference: ref, Reason: "reference too long"}
	}

	// Validate the reference
	if !imageRefRegex.MatchString(ref) {
		return InvalidImageRefError{Reference: ref, Reason: "invalid format"}
	}

	return nil
}

// validateImageDigests checks that every layer and the config are stored
// under the sha256 of their plaintext bytes and that the manifest's layer
// descriptors name exactly the pushed layers, so a push cannot write
// bytes under a digest another image references.
func validateImageDigests(ref string, image *Image) error {
	for i := range image.Layers {
		l := &image.Layers[i]
		if !strings.HasPrefix(l.Digest, "sha256:") {
			return InvalidDigestError{Digest: l.Digest, Reason: "layer digest must be sha256"}
		}
		if actual := sha256Digest(l.Content); l.Digest != actual {
			return DigestMismatchError{Expected: l.Digest, Actual: actual}
		}
	}
	if len(image.Manifest.Layers) != len(image.Layers) {
		return InvalidManifestError{Reference: ref, Reason: "manifest layers do not match the pushed layers"}
	}
	for i := range image.Layers {
		if image.Manifest.Layers[i].Digest != image.Layers[i].Digest {
			return InvalidManifestError{Reference: ref, Reason: "manifest layer descriptor does not match the pushed layer"}
		}
	}
	if len(image.Config) > 0 {
		if actual := sha256Digest(image.Config); image.Manifest.Config.Digest != actual {
			return DigestMismatchError{Expected: image.Manifest.Config.Digest, Actual: actual}
		}
	}
	return nil
}

// aikToString converts an AIK byte slice to a string representation.
func aikToString(aik []byte) string {
	if len(aik) == 0 {
		return ""
	}
	return computeAIKFingerprint(aik)
}
