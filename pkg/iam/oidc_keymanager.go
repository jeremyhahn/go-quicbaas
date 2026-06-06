//go:build iam

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

package iam

import (
	"crypto"
	"crypto/elliptic"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"sync"
	"sync/atomic"

	"github.com/go-jose/go-jose/v4"
	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/xkms"
)

// SigningAlgorithmES256 is the only OIDC token signing algorithm produced by
// the xkms-backed key manager. ES256 (ECDSA P-256 + SHA-256) keeps tokens
// small, is widely supported by JWT verifiers, and maps cleanly onto
// hardware-backed crypto.Signer implementations (HSM/TPM) where the private
// key never leaves the device.
const SigningAlgorithmES256 = "ES256"

// keyEntry is an immutable description of one OIDC signing key: its derived
// kid, the crypto.Signer that produces signatures, the public key used for
// verification, and the JWS algorithm.
type keyEntry struct {
	kid    string
	signer crypto.Signer
	pub    crypto.PublicKey
	alg    string
}

// keySnapshot is an immutable view of the active key set. Reads load it
// atomically so the hot signing/verification paths never take a lock. Rotation
// builds a new snapshot and swaps the pointer.
type keySnapshot struct {
	current  keyEntry
	previous []keyEntry
}

// rotationOverlap is the number of superseded keys kept resolvable after a
// rotation. Keeping one previous key lets tokens minted just before the
// rotation continue to verify until they expire, while the JWKS publishes both
// the current and previous public keys.
const rotationOverlap = 1

// SigningKeyManager owns the OIDC token signing key material backed by xkms
// (and therefore by whatever keystore xkms is configured with: software,
// PKCS#11/HSM, TPM2, or a cloud KMS). It exposes the current signer for
// minting tokens, resolves public keys by kid for kid-aware validation, and
// supports rotation with an overlap window so in-flight tokens stay valid.
//
// Reads (Current, SignerForKID, PublicForKID, JWKS) are lock-free: they load
// an immutable snapshot via an atomic pointer. Only Rotate takes the mutex.
//
// Per-tenant seam: NewSigningKeyManagerFromXKMS already accepts a tenantID so
// keys are namespaced per tenant in the xkms keystore. A future per-tenant
// resolver can map tenantID -> *SigningKeyManager with a global ("") fallback;
// the public API here does not need to change to support that.
type SigningKeyManager struct {
	cn        string
	tenantID  string
	storeType string

	snap atomic.Pointer[keySnapshot]

	rotateMu sync.Mutex
}

// UnknownKeyIDError is returned when a token references a kid that is neither
// the current key nor a retained previous key.
type UnknownKeyIDError struct {
	KID string
}

// Error implements the error interface.
func (e *UnknownKeyIDError) Error() string {
	return "iam: unknown signing key id: " + e.KID
}

// SigningKeyUnavailableError is returned when the key manager has no usable
// signing key (for example before initialization completed).
type SigningKeyUnavailableError struct{}

// Error implements the error interface.
func (e *SigningKeyUnavailableError) Error() string {
	return "iam: no signing key available"
}

// PublicKeyMarshalError is returned when a public key cannot be serialized for
// kid derivation.
type PublicKeyMarshalError struct {
	Cause error
}

// Error implements the error interface.
func (e *PublicKeyMarshalError) Error() string {
	return "iam: failed to marshal public key for kid derivation: " + e.Cause.Error()
}

// Unwrap returns the underlying cause.
func (e *PublicKeyMarshalError) Unwrap() error {
	return e.Cause
}

// computeKID derives a stable, deterministic key id from a public key. It is
// the base64url (no padding) encoding of the SHA-256 digest of the key's
// DER-encoded SubjectPublicKeyInfo (SPKI). Because it is a pure function of
// the public key, the same key yields the same kid across process restarts,
// which is exactly what JWKS consumers and cached verifiers expect.
func computeKID(pub crypto.PublicKey) (string, error) {
	der, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		return "", &PublicKeyMarshalError{Cause: err}
	}
	sum := sha256.Sum256(der)
	return base64.RawURLEncoding.EncodeToString(sum[:]), nil
}

// es256KeyAttributes builds the xkms KeyAttributes describing the OIDC signing
// key: an ECDSA P-256 key used for signing with SHA-256. StoreType is set to
// the resolved default backend name because some backends (e.g. the software
// keystore) require an explicit store type during key generation.
func es256KeyAttributes(cn, tenantID, storeType string) *xkmssdk.KeyAttributes {
	return &xkmssdk.KeyAttributes{
		CN:           cn,
		TenantID:     tenantID,
		StoreType:    xkmssdk.StoreType(storeType),
		KeyType:      xkmssdk.KeyTypeSigning,
		KeyAlgorithm: x509.ECDSA,
		Hash:         crypto.SHA256,
		ECCAttributes: &xkmssdk.ECCAttributes{
			Curve: elliptic.P256(),
		},
	}
}

// loadSignerForAttrs performs get-or-create against xkms: it returns the
// existing signer for attrs when the key is already present, otherwise it
// generates the key and returns the new signer. This is idempotent across
// restarts because xkms keystores persist key material; the first Signer call
// succeeds for any previously created key.
func loadSignerForAttrs(attrs *xkmssdk.KeyAttributes) (crypto.Signer, error) {
	signer, err := xkms.Signer(attrs)
	if err == nil {
		return signer, nil
	}

	// Generate on first boot (or when the key was removed). GenerateKey
	// surfaces a real error if the backend cannot create the key.
	if _, genErr := xkms.GenerateKey(attrs); genErr != nil {
		return nil, genErr
	}

	signer, err = xkms.Signer(attrs)
	if err != nil {
		return nil, err
	}
	return signer, nil
}

// newKeyEntry derives the kid for a signer's public key and packages it as a
// keyEntry.
func newKeyEntry(signer crypto.Signer) (keyEntry, error) {
	pub := signer.Public()
	kid, err := computeKID(pub)
	if err != nil {
		return keyEntry{}, err
	}
	return keyEntry{
		kid:    kid,
		signer: signer,
		pub:    pub,
		alg:    SigningAlgorithmES256,
	}, nil
}

// NewSigningKeyManagerFromXKMS returns a key manager whose current signing key
// is the ES256 (ECDSA P-256) key identified by cn within tenantID, creating it
// in the configured xkms backend if it does not already exist. The kid is
// derived from the key's public SPKI so it is stable across restarts.
//
// A tenantID of "" selects the global/single-tenant namespace.
func NewSigningKeyManagerFromXKMS(cn string, tenantID string) (*SigningKeyManager, error) {
	if cn == "" {
		return nil, &OIDCConfigRequiredError{Field: "signing key common name"}
	}

	storeType, err := xkms.DefaultBackendName()
	if err != nil {
		return nil, err
	}

	attrs := es256KeyAttributes(cn, tenantID, storeType)
	signer, err := loadSignerForAttrs(attrs)
	if err != nil {
		return nil, err
	}

	entry, err := newKeyEntry(signer)
	if err != nil {
		return nil, err
	}

	m := &SigningKeyManager{
		cn:        cn,
		tenantID:  tenantID,
		storeType: storeType,
	}
	m.snap.Store(&keySnapshot{current: entry})
	return m, nil
}

// load returns the current immutable snapshot.
func (m *SigningKeyManager) load() *keySnapshot {
	return m.snap.Load()
}

// Current returns the kid, signer, and algorithm of the key tokens are
// currently signed with.
func (m *SigningKeyManager) Current() (kid string, signer crypto.Signer, alg string) {
	snap := m.load()
	if snap == nil {
		return "", nil, ""
	}
	return snap.current.kid, snap.current.signer, snap.current.alg
}

// Algorithm returns the JWS algorithm used for signing (always ES256).
func (m *SigningKeyManager) Algorithm() string {
	snap := m.load()
	if snap == nil {
		return SigningAlgorithmES256
	}
	return snap.current.alg
}

// SignerForKID returns the signer for the given kid, searching the current key
// and any retained previous keys. It returns UnknownKeyIDError when no key
// matches.
func (m *SigningKeyManager) SignerForKID(kid string) (crypto.Signer, error) {
	snap := m.load()
	if snap == nil {
		return nil, &SigningKeyUnavailableError{}
	}
	if snap.current.kid == kid {
		return snap.current.signer, nil
	}
	for i := range snap.previous {
		if snap.previous[i].kid == kid {
			return snap.previous[i].signer, nil
		}
	}
	return nil, &UnknownKeyIDError{KID: kid}
}

// PublicForKID returns the public key for the given kid, searching the current
// key and any retained previous keys. It returns UnknownKeyIDError when no key
// matches.
func (m *SigningKeyManager) PublicForKID(kid string) (crypto.PublicKey, error) {
	snap := m.load()
	if snap == nil {
		return nil, &SigningKeyUnavailableError{}
	}
	if snap.current.kid == kid {
		return snap.current.pub, nil
	}
	for i := range snap.previous {
		if snap.previous[i].kid == kid {
			return snap.previous[i].pub, nil
		}
	}
	return nil, &UnknownKeyIDError{KID: kid}
}

// JWKS returns the JSON Web Key Set the OIDC provider publishes: one entry per
// active kid (current first, then retained previous keys), each with Use "sig",
// its algorithm, and KeyID set to the derived kid.
func (m *SigningKeyManager) JWKS() *jose.JSONWebKeySet {
	snap := m.load()
	if snap == nil {
		return &jose.JSONWebKeySet{}
	}

	keys := make([]jose.JSONWebKey, 0, 1+len(snap.previous))
	keys = append(keys, jose.JSONWebKey{
		Key:       snap.current.pub,
		KeyID:     snap.current.kid,
		Algorithm: snap.current.alg,
		Use:       "sig",
	})
	for i := range snap.previous {
		keys = append(keys, jose.JSONWebKey{
			Key:       snap.previous[i].pub,
			KeyID:     snap.previous[i].kid,
			Algorithm: snap.previous[i].alg,
			Use:       "sig",
		})
	}
	return &jose.JSONWebKeySet{Keys: keys}
}

// Rotate generates a fresh ES256 key in the xkms backend, promotes it to the
// current key, and demotes the previously current key into the overlap window
// (pruned to rotationOverlap entries). Tokens already signed with the demoted
// key remain verifiable until they expire because both keys stay in the JWKS
// and resolvable by kid.
func (m *SigningKeyManager) Rotate() error {
	m.rotateMu.Lock()
	defer m.rotateMu.Unlock()

	prev := m.load()
	if prev == nil {
		return &SigningKeyUnavailableError{}
	}

	attrs := es256KeyAttributes(m.cn, m.tenantID, m.storeType)

	// RotateKey replaces the stored key material under the same identity with
	// fresh material (a new key with a new public key, hence a new derived
	// kid). We then fetch the signer for that material; backends may return a
	// handle whose private key never leaves the device.
	if _, err := xkms.RotateKey(m.rotationKID()); err != nil {
		return err
	}

	signer, err := xkms.Signer(attrs)
	if err != nil {
		return err
	}

	newEntry, err := newKeyEntry(signer)
	if err != nil {
		return err
	}

	// If rotation produced the same key (no-op backend), do not duplicate it.
	if newEntry.kid == prev.current.kid {
		return nil
	}

	previous := make([]keyEntry, 0, rotationOverlap)
	previous = append(previous, prev.current)
	for i := 0; i < len(prev.previous) && len(previous) < rotationOverlap; i++ {
		previous = append(previous, prev.previous[i])
	}

	m.snap.Store(&keySnapshot{
		current:  newEntry,
		previous: previous,
	})
	return nil
}

// rotationKID builds the fully-qualified xkms key reference used to address the
// signing key for in-place rotation. The xkms key id format is
// "backend:type:algo:keyname"; the backend (store type) segment must be present
// so the resolved KeyAttributes carry the StoreType that backends require for
// key deletion/regeneration during rotation.
func (m *SigningKeyManager) rotationKID() string {
	return m.storeType + ":signing:ecdsa-p256:" + m.cn
}
