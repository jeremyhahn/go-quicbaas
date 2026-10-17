package iam

import (
	"context"
	"errors"
	"testing"
)

// A system manager with no current signer must fail closed with
// SigningKeyUnavailableError rather than sign with any other key.
func TestSigningMaterial_SystemManagerWithoutSignerFailsClosed(t *testing.T) {
	s := &TokenService{keyManager: &SigningKeyManager{}, config: &OIDCConfig{}}

	signer, _, _, err := s.signingMaterial(context.Background(), "")
	if !errors.As(err, new(*SigningKeyUnavailableError)) {
		t.Fatalf("want *SigningKeyUnavailableError, got %T: %v", err, err)
	}
	if signer != nil {
		t.Fatal("no signer may be returned when the manager has none")
	}
}

// A token service with no manager at all has nothing to sign with.
func TestSigningMaterial_NoManagerFailsClosed(t *testing.T) {
	s := &TokenService{config: &OIDCConfig{}}

	if _, _, _, err := s.signingMaterial(context.Background(), ""); !errors.As(err, new(*SigningKeyUnavailableError)) {
		t.Fatalf("want *SigningKeyUnavailableError, got %T: %v", err, err)
	}
}

// A dedicated tenant whose manager has no current signer must fail closed with
// TenantSigningKeyUnavailableError; the system key is never used in its place.
func TestSigningMaterial_DedicatedTenantWithoutSignerFailsClosed(t *testing.T) {
	setupTestXKMS(t)
	settings := &fakeOIDCSettings{dedicated: map[string]bool{"sealed-tenant": true}}
	r, global := newResolverWithGlobal(t, settings)
	r.build = func(string) (*SigningKeyManager, error) {
		return &SigningKeyManager{}, nil
	}
	s := &TokenService{keyManager: global, tenantKeys: r, config: &OIDCConfig{}}

	signer, _, kid, err := s.signingMaterial(context.Background(), "sealed-tenant")
	var unavailable *TenantSigningKeyUnavailableError
	if !errors.As(err, &unavailable) {
		t.Fatalf("want *TenantSigningKeyUnavailableError, got %T: %v", err, err)
	}
	if unavailable.TenantID != "sealed-tenant" {
		t.Fatalf("error tenant id = %q, want sealed-tenant", unavailable.TenantID)
	}
	globalKID, _, _ := global.Current()
	if signer != nil || kid == globalKID {
		t.Fatal("the system key must never sign for a dedicated tenant")
	}
}

// The happy path signs with the resolved manager's current key and kid.
func TestSigningMaterial_UsesManagerCurrentKID(t *testing.T) {
	setupTestXKMS(t)
	mgr, err := NewSigningKeyManagerFromXKMS(OIDCSigningKeyCN, "")
	if err != nil {
		t.Fatalf("signing key manager: %v", err)
	}
	s := &TokenService{keyManager: mgr, config: &OIDCConfig{}}

	signer, alg, kid, err := s.signingMaterial(context.Background(), "")
	if err != nil {
		t.Fatalf("signingMaterial: %v", err)
	}
	wantKID, wantSigner, wantAlg := mgr.Current()
	if kid != wantKID || alg != wantAlg || signer != wantSigner {
		t.Fatalf("signingMaterial = (%q, %q), want the manager's current (%q, %q)", kid, alg, wantKID, wantAlg)
	}
}
