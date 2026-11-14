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
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"errors"
	"testing"

	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/xkms"
)

// setupTestXKMS initializes the xkms package with a real, in-memory
// software-backed keystore so tests exercise genuine ECDSA P-256 signing key
// generation and persistence (no mocked signer). The backend is reset when the
// test finishes.
func setupTestXKMS(t *testing.T) {
	t.Helper()

	xkms.Reset()

	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	if err != nil {
		t.Fatalf("failed to create logger: %v", err)
	}

	keyStorage, err := xkmssdk.NewMemoryBackend()
	if err != nil {
		t.Fatalf("failed to create key storage: %v", err)
	}
	certStorage := xkmssdk.NewStorageMemory()

	softwareBackend, err := xkmssdk.NewSoftwareBackend(&xkmssdk.SoftwareBackendConfig{
		KeyStorage: keyStorage,
	})
	if err != nil {
		t.Fatalf("failed to create software backend: %v", err)
	}

	ks, err := xkmssdk.NewServiceBackend(&xkmssdk.ServiceBackendConfig{
		Backend:     softwareBackend,
		CertStorage: certStorage,
	})
	if err != nil {
		t.Fatalf("failed to create service backend: %v", err)
	}

	// Key-version records, as the server wires them: rotation creates the
	// key's next version and records it as current.
	keyVersions, err := xkmssdk.NewKeyVersionStore(xkmssdk.NewStorageMemory())
	if err != nil {
		t.Fatalf("failed to create key version store: %v", err)
	}

	const backendName = "software"
	if err := xkms.Initialize(&xkms.Config{
		DefaultBackend: backendName,
		Backends: map[string]xkmssdk.ServiceBackend{
			backendName: ks,
		},
		KeyVersions: keyVersions,
		Logger:      logger.NewAdapter(log),
	}); err != nil {
		t.Fatalf("failed to initialize xkms: %v", err)
	}

	t.Cleanup(func() { xkms.Reset() })
}

func TestComputeKIDDeterministic(t *testing.T) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}

	kid1, err := computeKID(priv.Public())
	if err != nil {
		t.Fatalf("computeKID failed: %v", err)
	}
	kid2, err := computeKID(priv.Public())
	if err != nil {
		t.Fatalf("computeKID failed: %v", err)
	}

	if kid1 == "" {
		t.Fatal("computeKID returned empty kid")
	}
	if kid1 != kid2 {
		t.Fatalf("computeKID not deterministic: %q != %q", kid1, kid2)
	}
}

func TestComputeKIDDistinctKeys(t *testing.T) {
	a, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}
	b, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}

	kidA, err := computeKID(a.Public())
	if err != nil {
		t.Fatalf("computeKID failed: %v", err)
	}
	kidB, err := computeKID(b.Public())
	if err != nil {
		t.Fatalf("computeKID failed: %v", err)
	}

	if kidA == kidB {
		t.Fatal("distinct keys produced the same kid")
	}
}

func TestComputeKIDInvalidKey(t *testing.T) {
	// A non-public-key value cannot be marshaled to SPKI.
	_, err := computeKID("not-a-key")
	if err == nil {
		t.Fatal("expected error for non-public-key input")
	}
	var marshalErr *PublicKeyMarshalError
	if !errors.As(err, &marshalErr) {
		t.Fatalf("expected PublicKeyMarshalError, got %T", err)
	}
}

func TestNewSigningKeyManagerFromXKMSGetOrCreate(t *testing.T) {
	setupTestXKMS(t)

	const cn = "oidc-signing-test"

	mgr1, err := NewSigningKeyManagerFromXKMS(cn, "")
	if err != nil {
		t.Fatalf("first NewSigningKeyManagerFromXKMS failed: %v", err)
	}

	kid1, signer1, alg1 := mgr1.Current()
	if kid1 == "" {
		t.Fatal("current kid is empty")
	}
	if signer1 == nil {
		t.Fatal("current signer is nil")
	}
	if alg1 != SigningAlgorithmES256 {
		t.Fatalf("expected ES256, got %q", alg1)
	}
	if _, ok := signer1.Public().(*ecdsa.PublicKey); !ok {
		t.Fatalf("expected ECDSA public key, got %T", signer1.Public())
	}

	// A second manager for the same CN must resolve the persisted key and
	// derive the same kid (get-or-create idempotency / restart stability).
	mgr2, err := NewSigningKeyManagerFromXKMS(cn, "")
	if err != nil {
		t.Fatalf("second NewSigningKeyManagerFromXKMS failed: %v", err)
	}
	kid2, _, _ := mgr2.Current()
	if kid2 != kid1 {
		t.Fatalf("kid changed across get-or-create: %q != %q", kid1, kid2)
	}
}

func TestNewSigningKeyManagerFromXKMSMissingCN(t *testing.T) {
	setupTestXKMS(t)

	_, err := NewSigningKeyManagerFromXKMS("", "")
	if err == nil {
		t.Fatal("expected error for empty common name")
	}
}

func TestSignerForKIDCurrentAndUnknown(t *testing.T) {
	setupTestXKMS(t)

	mgr, err := NewSigningKeyManagerFromXKMS("oidc-signing-test", "")
	if err != nil {
		t.Fatalf("manager init failed: %v", err)
	}

	kid, _, _ := mgr.Current()

	signer, err := mgr.SignerForKID(kid)
	if err != nil {
		t.Fatalf("SignerForKID(current) failed: %v", err)
	}
	if signer == nil {
		t.Fatal("SignerForKID returned nil signer")
	}

	_, err = mgr.SignerForKID("not-a-real-kid")
	if err == nil {
		t.Fatal("expected error for unknown kid")
	}
	var unknownErr *UnknownKeyIDError
	if !errors.As(err, &unknownErr) {
		t.Fatalf("expected UnknownKeyIDError, got %T", err)
	}
}

func TestPublicForKIDCurrentAndUnknown(t *testing.T) {
	setupTestXKMS(t)

	mgr, err := NewSigningKeyManagerFromXKMS("oidc-signing-test", "")
	if err != nil {
		t.Fatalf("manager init failed: %v", err)
	}

	kid, _, _ := mgr.Current()

	pub, err := mgr.PublicForKID(kid)
	if err != nil {
		t.Fatalf("PublicForKID(current) failed: %v", err)
	}
	if _, ok := pub.(*ecdsa.PublicKey); !ok {
		t.Fatalf("expected ECDSA public key, got %T", pub)
	}

	_, err = mgr.PublicForKID("missing")
	var unknownErr *UnknownKeyIDError
	if !errors.As(err, &unknownErr) {
		t.Fatalf("expected UnknownKeyIDError, got %T", err)
	}
}

func TestJWKSContainsCurrentKID(t *testing.T) {
	setupTestXKMS(t)

	mgr, err := NewSigningKeyManagerFromXKMS("oidc-signing-test", "")
	if err != nil {
		t.Fatalf("manager init failed: %v", err)
	}

	kid, _, _ := mgr.Current()
	jwks := mgr.JWKS()
	if len(jwks.Keys) != 1 {
		t.Fatalf("expected 1 JWKS key, got %d", len(jwks.Keys))
	}
	if jwks.Keys[0].KeyID != kid {
		t.Fatalf("JWKS kid %q != current kid %q", jwks.Keys[0].KeyID, kid)
	}
	if jwks.Keys[0].Use != "sig" {
		t.Fatalf("expected Use=sig, got %q", jwks.Keys[0].Use)
	}
	if jwks.Keys[0].Algorithm != SigningAlgorithmES256 {
		t.Fatalf("expected ES256, got %q", jwks.Keys[0].Algorithm)
	}
}

func TestRotateOverlapAndJWKS(t *testing.T) {
	setupTestXKMS(t)

	mgr, err := NewSigningKeyManagerFromXKMS("oidc-signing-test", "")
	if err != nil {
		t.Fatalf("manager init failed: %v", err)
	}

	oldKID, oldSigner, _ := mgr.Current()

	if err := mgr.Rotate(); err != nil {
		t.Fatalf("Rotate failed: %v", err)
	}

	newKID, newSigner, _ := mgr.Current()
	if newKID == oldKID {
		t.Fatal("rotation did not change the current kid")
	}
	if newSigner == oldSigner {
		t.Fatal("rotation did not change the signer")
	}

	// The previous key must still resolve during the overlap window.
	prevSigner, err := mgr.SignerForKID(oldKID)
	if err != nil {
		t.Fatalf("previous kid did not resolve after rotation: %v", err)
	}
	if prevSigner == nil {
		t.Fatal("previous signer is nil after rotation")
	}
	if _, err := mgr.PublicForKID(oldKID); err != nil {
		t.Fatalf("previous public key did not resolve after rotation: %v", err)
	}

	// The JWKS must publish both the current and the previous public keys.
	jwks := mgr.JWKS()
	if len(jwks.Keys) != 2 {
		t.Fatalf("expected 2 JWKS keys after rotation, got %d", len(jwks.Keys))
	}
	found := map[string]bool{}
	for _, k := range jwks.Keys {
		found[k.KeyID] = true
	}
	if !found[oldKID] || !found[newKID] {
		t.Fatalf("JWKS missing keys after rotation: have %v want %s and %s", found, oldKID, newKID)
	}
}

func TestRotatePrunesBeyondOverlap(t *testing.T) {
	setupTestXKMS(t)

	mgr, err := NewSigningKeyManagerFromXKMS("oidc-signing-test", "")
	if err != nil {
		t.Fatalf("manager init failed: %v", err)
	}

	firstKID, _, _ := mgr.Current()

	if err := mgr.Rotate(); err != nil {
		t.Fatalf("first rotate failed: %v", err)
	}
	if err := mgr.Rotate(); err != nil {
		t.Fatalf("second rotate failed: %v", err)
	}

	// After two rotations, the original (oldest) key must have been pruned
	// because the overlap window keeps only one previous key.
	if _, err := mgr.SignerForKID(firstKID); err == nil {
		t.Fatal("expected oldest kid to be pruned after two rotations")
	}

	jwks := mgr.JWKS()
	if len(jwks.Keys) != 1+rotationOverlap {
		t.Fatalf("expected %d JWKS keys after two rotations, got %d", 1+rotationOverlap, len(jwks.Keys))
	}
}

// TestRotate_SurvivesARestart is the regression guard for go-xkms key
// versions: a rotation creates the key's next version under its own backend
// name, so a manager rebuilt from the same key (a restart) must load the
// CURRENT version. Loading version 1 would silently sign with the key the
// operator rotated away from.
func TestRotate_SurvivesARestart(t *testing.T) {
	setupTestXKMS(t)

	mgr, err := NewSigningKeyManagerFromXKMS("oidc-signing-restart", "")
	if err != nil {
		t.Fatalf("manager init failed: %v", err)
	}
	if err := mgr.Rotate(); err != nil {
		t.Fatalf("Rotate failed: %v", err)
	}
	rotatedKID, _, _ := mgr.Current()

	restarted, err := NewSigningKeyManagerFromXKMS("oidc-signing-restart", "")
	if err != nil {
		t.Fatalf("manager re-init failed: %v", err)
	}
	if kid, _, _ := restarted.Current(); kid != rotatedKID {
		t.Fatalf("restarted manager signs with kid %q, want the rotated %q", kid, rotatedKID)
	}
}

// TestNewSigningKeyManagerFromXKMSBackendRequiredFields proves the explicit
// backend constructor rejects an empty common name and an empty store type
// with a typed config error.
func TestNewSigningKeyManagerFromXKMSBackendRequiredFields(t *testing.T) {
	cases := []struct {
		name, cn, storeType, field string
	}{
		{name: "empty cn", cn: "", storeType: "software", field: "signing key common name"},
		{name: "empty store type", cn: OIDCSigningKeyCN, storeType: "", field: "signing key store type"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewSigningKeyManagerFromXKMSBackend(tc.cn, "", tc.storeType)
			var cfgErr *OIDCConfigRequiredError
			if !errors.As(err, &cfgErr) {
				t.Fatalf("want *OIDCConfigRequiredError, got %v", err)
			}
			if cfgErr.Field != tc.field {
				t.Fatalf("field = %q, want %q", cfgErr.Field, tc.field)
			}
		})
	}
}
