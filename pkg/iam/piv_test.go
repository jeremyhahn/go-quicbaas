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
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"testing"
	"time"
)

// pivTestCA creates a self-signed CA certificate with CertSign usage.
func pivTestCA(t *testing.T) (*x509.Certificate, *ecdsa.PrivateKey) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate CA key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "PIV Test Root"},
		NotBefore:             time.Now().Add(-time.Minute),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create CA cert: %v", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parse CA cert: %v", err)
	}
	return cert, key
}

// pivTestLeaf creates a leaf certificate signed by the given CA.
// When clientAuth is true the ExtKeyUsageClientAuth usage is included.
func pivTestLeaf(t *testing.T, ca *x509.Certificate, caKey *ecdsa.PrivateKey, clientAuth bool) (*x509.Certificate, []byte) {
	t.Helper()
	leafKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate leaf key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: "PIV Test Client"},
		NotBefore:    time.Now().Add(-time.Minute),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
	}
	if clientAuth {
		tmpl.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, ca, &leafKey.PublicKey, caKey)
	if err != nil {
		t.Fatalf("create leaf cert: %v", err)
	}
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parse leaf cert: %v", err)
	}
	return cert, pemBytes
}

// setupPIVTest creates a ready-to-use PIVActivator with a pending user and a valid invite token.
func setupPIVTest(t *testing.T) (activator *PIVActivator, token string, userStorage *mockUserStorage, inviteStore *MemoryInviteStore) {
	t.Helper()

	userStorage = newMockUserStorage()
	inviteStore = NewMemoryInviteStore()

	// Create a pending user.
	user := &User{
		ID:       "user-piv-1",
		TenantID: "tenant-1",
		Email:    "piv@example.com",
		State:    UserStatePending,
		Created:  time.Now(),
		Updated:  time.Now(),
	}
	if err := userStorage.CreateUser(context.Background(), user); err != nil {
		t.Fatalf("create user: %v", err)
	}

	// Generate an invite token and store the record.
	var genErr error
	token, genErr = GenerateInviteToken()
	if genErr != nil {
		t.Fatalf("generate invite token: %v", genErr)
	}
	record := &InviteRecord{
		TokenHash: tokenHash(token),
		UserID:    "user-piv-1",
		TenantID:  "tenant-1",
		ExpiresAt: time.Now().Add(time.Hour),
	}
	if err := inviteStore.Create(context.Background(), record); err != nil {
		t.Fatalf("store invite: %v", err)
	}

	ca, caKey := pivTestCA(t)
	roots := x509.NewCertPool()
	roots.AddCert(ca)

	// Store CA/caKey in closure for leaf generation in tests.
	_ = caKey // used by individual tests
	_ = ca

	var pivErr error
	activator, pivErr = NewPIVActivator(PIVActivatorDependencies{
		Roots:       roots,
		UserStorage: userStorage,
		InviteStore: inviteStore,
	})
	if pivErr != nil {
		t.Fatalf("NewPIVActivator: %v", pivErr)
	}

	return activator, token, userStorage, inviteStore
}

// TestPIVActivator_Valid verifies a complete successful PIV activation.
func TestPIVActivator_Valid(t *testing.T) {
	ctx := context.Background()

	userStorage := newMockUserStorage()
	inviteStore := NewMemoryInviteStore()

	user := &User{
		ID:       "u1",
		TenantID: "t1",
		Email:    "piv@example.com",
		State:    UserStatePending,
		Created:  time.Now(),
		Updated:  time.Now(),
	}
	_ = userStorage.CreateUser(ctx, user)

	token, _ := GenerateInviteToken()
	_ = inviteStore.Create(ctx, &InviteRecord{
		TokenHash: tokenHash(token),
		UserID:    "u1",
		TenantID:  "t1",
		ExpiresAt: time.Now().Add(time.Hour),
	})

	ca, caKey := pivTestCA(t)
	roots := x509.NewCertPool()
	roots.AddCert(ca)

	_, certPEM := pivTestLeaf(t, ca, caKey, true)

	act, err := NewPIVActivator(PIVActivatorDependencies{
		Roots:       roots,
		UserStorage: userStorage,
		InviteStore: inviteStore,
	})
	if err != nil {
		t.Fatalf("NewPIVActivator: %v", err)
	}

	if err := act.Activate(ctx, token, certPEM); err != nil {
		t.Fatalf("Activate: %v", err)
	}

	updated, err := userStorage.GetUser(ctx, "t1", "u1")
	if err != nil {
		t.Fatalf("GetUser: %v", err)
	}
	if updated.State != UserStateActive {
		t.Errorf("expected state=active, got %q", updated.State)
	}
	if updated.PIVCertThumbprint == "" {
		t.Error("expected PIVCertThumbprint to be set")
	}
}

// TestPIVActivator_ExpiredInvite verifies that an expired invite is rejected.
func TestPIVActivator_ExpiredInvite(t *testing.T) {
	ctx := context.Background()

	userStorage := newMockUserStorage()
	inviteStore := NewMemoryInviteStore()

	user := &User{ID: "u2", TenantID: "t1", Email: "e@x.com", State: UserStatePending, Created: time.Now(), Updated: time.Now()}
	_ = userStorage.CreateUser(ctx, user)

	token, _ := GenerateInviteToken()
	_ = inviteStore.Create(ctx, &InviteRecord{
		TokenHash: tokenHash(token),
		UserID:    "u2",
		TenantID:  "t1",
		ExpiresAt: time.Now().Add(-time.Minute), // already expired
	})

	ca, caKey := pivTestCA(t)
	roots := x509.NewCertPool()
	roots.AddCert(ca)

	_, certPEM := pivTestLeaf(t, ca, caKey, true)

	act, _ := NewPIVActivator(PIVActivatorDependencies{
		Roots:       roots,
		UserStorage: userStorage,
		InviteStore: inviteStore,
	})

	err := act.Activate(ctx, token, certPEM)
	if !errors.Is(err, ErrInviteExpired) {
		t.Fatalf("expected ErrInviteExpired, got %v", err)
	}
}

// TestPIVActivator_UntrustedCert verifies that a certificate from an unknown CA is rejected.
func TestPIVActivator_UntrustedCert(t *testing.T) {
	ctx := context.Background()

	userStorage := newMockUserStorage()
	inviteStore := NewMemoryInviteStore()

	user := &User{ID: "u3", TenantID: "t1", Email: "f@x.com", State: UserStatePending, Created: time.Now(), Updated: time.Now()}
	_ = userStorage.CreateUser(ctx, user)

	token, _ := GenerateInviteToken()
	_ = inviteStore.Create(ctx, &InviteRecord{
		TokenHash: tokenHash(token),
		UserID:    "u3",
		TenantID:  "t1",
		ExpiresAt: time.Now().Add(time.Hour),
	})

	// Trust only ca2, but present a cert signed by ca1.
	ca1, ca1Key := pivTestCA(t)
	ca2, _ := pivTestCA(t)

	roots := x509.NewCertPool()
	roots.AddCert(ca2)

	_, certPEM := pivTestLeaf(t, ca1, ca1Key, true)

	act, _ := NewPIVActivator(PIVActivatorDependencies{
		Roots:       roots,
		UserStorage: userStorage,
		InviteStore: inviteStore,
	})

	err := act.Activate(ctx, token, certPEM)
	if !errors.Is(err, ErrPIVCertRejected) {
		t.Fatalf("expected ErrPIVCertRejected, got %v", err)
	}
}

// TestPIVActivate_RejectsUntrustedIssuer verifies that a certificate whose issuer DN is not
// in the TrustedIssuers allowlist is rejected even when chain verification succeeds.
func TestPIVActivate_RejectsUntrustedIssuer(t *testing.T) {
	ctx := context.Background()

	userStorage := newMockUserStorage()
	inviteStore := NewMemoryInviteStore()

	user := &User{ID: "u5", TenantID: "t1", Email: "h@x.com", State: UserStatePending, Created: time.Now(), Updated: time.Now()}
	_ = userStorage.CreateUser(ctx, user)

	token, _ := GenerateInviteToken()
	_ = inviteStore.Create(ctx, &InviteRecord{
		TokenHash: tokenHash(token),
		UserID:    "u5",
		TenantID:  "t1",
		ExpiresAt: time.Now().Add(time.Hour),
	})

	ca, caKey := pivTestCA(t)
	roots := x509.NewCertPool()
	roots.AddCert(ca)
	_, certPEM := pivTestLeaf(t, ca, caKey, true)

	// The cert's issuer will be "CN=PIV Test Root" but we allow only "CN=Trusted Agency CA".
	act, err := NewPIVActivator(PIVActivatorDependencies{
		Roots:          roots,
		UserStorage:    userStorage,
		InviteStore:    inviteStore,
		TrustedIssuers: []string{"CN=Trusted Agency CA"},
	})
	if err != nil {
		t.Fatalf("NewPIVActivator: %v", err)
	}

	activateErr := act.Activate(ctx, token, certPEM)
	if activateErr == nil {
		t.Fatal("expected error but got nil")
	}
	var issuerErr *PIVCertIssuerRejectedError
	if !errors.As(activateErr, &issuerErr) {
		t.Fatalf("expected PIVCertIssuerRejectedError, got %T: %v", activateErr, activateErr)
	}
	if issuerErr.Issuer == "" {
		t.Error("expected non-empty Issuer in error")
	}
}

// TestPIVActivate_AcceptsTrustedIssuer verifies that a certificate whose issuer DN matches
// an entry in TrustedIssuers passes the second-gate check.
func TestPIVActivate_AcceptsTrustedIssuer(t *testing.T) {
	ctx := context.Background()

	userStorage := newMockUserStorage()
	inviteStore := NewMemoryInviteStore()

	user := &User{ID: "u6", TenantID: "t1", Email: "i@x.com", State: UserStatePending, Created: time.Now(), Updated: time.Now()}
	_ = userStorage.CreateUser(ctx, user)

	token, _ := GenerateInviteToken()
	_ = inviteStore.Create(ctx, &InviteRecord{
		TokenHash: tokenHash(token),
		UserID:    "u6",
		TenantID:  "t1",
		ExpiresAt: time.Now().Add(time.Hour),
	})

	ca, caKey := pivTestCA(t)
	roots := x509.NewCertPool()
	roots.AddCert(ca)
	_, certPEM := pivTestLeaf(t, ca, caKey, true)

	// Use the actual issuer DN of the test CA (CN=PIV Test Root).
	act, err := NewPIVActivator(PIVActivatorDependencies{
		Roots:          roots,
		UserStorage:    userStorage,
		InviteStore:    inviteStore,
		TrustedIssuers: []string{"CN=PIV Test Root"},
	})
	if err != nil {
		t.Fatalf("NewPIVActivator: %v", err)
	}

	if err := act.Activate(ctx, token, certPEM); err != nil {
		t.Fatalf("expected success but got: %v", err)
	}
}

// TestPIVActivate_EmptyTrustedIssuers verifies that an empty TrustedIssuers list
// is backward-compatible (any valid issuer is accepted).
func TestPIVActivate_EmptyTrustedIssuers(t *testing.T) {
	ctx := context.Background()

	userStorage := newMockUserStorage()
	inviteStore := NewMemoryInviteStore()

	user := &User{ID: "u7", TenantID: "t1", Email: "j@x.com", State: UserStatePending, Created: time.Now(), Updated: time.Now()}
	_ = userStorage.CreateUser(ctx, user)

	token, _ := GenerateInviteToken()
	_ = inviteStore.Create(ctx, &InviteRecord{
		TokenHash: tokenHash(token),
		UserID:    "u7",
		TenantID:  "t1",
		ExpiresAt: time.Now().Add(time.Hour),
	})

	ca, caKey := pivTestCA(t)
	roots := x509.NewCertPool()
	roots.AddCert(ca)
	_, certPEM := pivTestLeaf(t, ca, caKey, true)

	// Empty TrustedIssuers = permissive (any issuer that validates is accepted).
	act, err := NewPIVActivator(PIVActivatorDependencies{
		Roots:       roots,
		UserStorage: userStorage,
		InviteStore: inviteStore,
	})
	if err != nil {
		t.Fatalf("NewPIVActivator: %v", err)
	}

	if err := act.Activate(ctx, token, certPEM); err != nil {
		t.Fatalf("expected success but got: %v", err)
	}
}

// TestPIVActivator_UserNotPending verifies that activating an already-active user returns ErrUserNotPending.
func TestPIVActivator_UserNotPending(t *testing.T) {
	ctx := context.Background()

	userStorage := newMockUserStorage()
	inviteStore := NewMemoryInviteStore()

	user := &User{ID: "u4", TenantID: "t1", Email: "g@x.com", State: UserStateActive, Created: time.Now(), Updated: time.Now()}
	_ = userStorage.CreateUser(ctx, user)

	token, _ := GenerateInviteToken()
	_ = inviteStore.Create(ctx, &InviteRecord{
		TokenHash: tokenHash(token),
		UserID:    "u4",
		TenantID:  "t1",
		ExpiresAt: time.Now().Add(time.Hour),
	})

	ca, caKey := pivTestCA(t)
	roots := x509.NewCertPool()
	roots.AddCert(ca)

	_, certPEM := pivTestLeaf(t, ca, caKey, true)

	act, _ := NewPIVActivator(PIVActivatorDependencies{
		Roots:       roots,
		UserStorage: userStorage,
		InviteStore: inviteStore,
	})

	err := act.Activate(ctx, token, certPEM)
	if !errors.Is(err, ErrUserNotPending) {
		t.Fatalf("expected ErrUserNotPending, got %v", err)
	}
}
