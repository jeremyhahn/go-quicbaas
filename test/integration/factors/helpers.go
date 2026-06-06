// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
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

//go:build integration

// Package factors provides hermetic in-process cross-vendor hardware
// factor integration tests per spec §M13 of
// docs/08-bootstrap-auth-architecture.md.
//
// The tests exercise the production qrdb pkg/auth + pkg/service types
// directly (no Docker, no cluster, no real hardware). Hardware is faked
// via the descope/virtualwebauthn library for FIDO2 and via ECDSA
// self-signed X.509 certificates for PIV and mTLS. Distinct "vendors"
// are differentiated by AAGUID (FIDO2) and by metadata fields
// (PIV/mTLS) so the resolver's User <-> Factor mapping can be verified
// across heterogeneous credential sources.
package factors

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"math/big"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/descope/virtualwebauthn"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
)

// testAdminUserID is the synthetic admin user that owns every factor
// enrolled by the M13 tests. Matches the "system tenant admin user"
// convention used elsewhere in the IAM unification work.
const testAdminUserID uint64 = 1

// testAdminUsername is the human-readable login name returned by the
// userLookupStub for testAdminUserID.
const testAdminUsername = "admin"

// ---------------------------------------------------------------------------
// In-memory KVServicer
// ---------------------------------------------------------------------------

// memKV is a hermetic, in-process implementation of
// qrdbsdk.KVServicer backed by a sync.Map-protected map. Only the
// Put/Get/Delete/List/Scan surface is exercised by the user auth-factor
// service; the remaining methods are no-ops that satisfy the interface
// without panicking so that any accidental call surfaces as a
// recognisable test failure (zero values + nil error).
type memKV struct {
	mu    sync.RWMutex
	store map[string][]byte
}

// Compile-time assertion that memKV satisfies the KVServicer interface
// the user auth-factor service depends on.
var _ qrdbsdk.KVServicer = (*memKV)(nil)

// newMemKV constructs an empty in-memory KV servicer.
func newMemKV() *memKV {
	return &memKV{store: make(map[string][]byte)}
}

// Put stores key -> val (last-write-wins).
func (m *memKV) Put(_ context.Context, key string, val []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	// Defensive copy so the caller can mutate val without poisoning the
	// stored bytes.
	cp := make([]byte, len(val))
	copy(cp, val)
	m.store[key] = cp
	return nil
}

// Get returns the value at key or a KeyNotFoundError when absent.
func (m *memKV) Get(_ context.Context, key string) ([]byte, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	v, ok := m.store[key]
	if !ok {
		return nil, &qrdbsdk.KeyNotFoundError{Key: key}
	}
	// Defensive copy so callers can mutate the returned slice without
	// poisoning the stored bytes.
	out := make([]byte, len(v))
	copy(out, v)
	return out, nil
}

// Delete removes key (no-op if absent).
func (m *memKV) Delete(_ context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.store, key)
	return nil
}

// List returns every key matching the prefix in unspecified order.
func (m *memKV) List(_ context.Context, prefix string) ([]string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	keys := make([]string, 0, len(m.store))
	for k := range m.store {
		if strings.HasPrefix(k, prefix) {
			keys = append(keys, k)
		}
	}
	return keys, nil
}

// Scan returns every (key, value) pair matching the prefix.
func (m *memKV) Scan(_ context.Context, prefix string) (map[string][]byte, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make(map[string][]byte)
	for k, v := range m.store {
		if strings.HasPrefix(k, prefix) {
			cp := make([]byte, len(v))
			copy(cp, v)
			out[k] = cp
		}
	}
	return out, nil
}

// GetStale is a no-op for the auth-factor surface.
func (m *memKV) GetStale(_ context.Context, _ string) ([]byte, error) { return nil, nil }

// ListStale is a no-op for the auth-factor surface.
func (m *memKV) ListStale(_ context.Context, _ string) ([]string, error) { return nil, nil }

// ScanStale is a no-op for the auth-factor surface.
func (m *memKV) ScanStale(_ context.Context, _ string) (map[string][]byte, error) {
	return nil, nil
}

// PutWithMode is a no-op for the auth-factor surface.
func (m *memKV) PutWithMode(_ context.Context, _ string, _ []byte, _ qrdbsdk.WriteMode) error {
	return nil
}

// DeleteWithMode is a no-op for the auth-factor surface.
func (m *memKV) DeleteWithMode(_ context.Context, _ string, _ qrdbsdk.WriteMode) error {
	return nil
}

// GetWithConsistency is a no-op for the auth-factor surface.
func (m *memKV) GetWithConsistency(_ context.Context, _ string, _ qrdbsdk.ReadOptions) ([]byte, error) {
	return nil, nil
}

// ListWithConsistency is a no-op for the auth-factor surface.
func (m *memKV) ListWithConsistency(_ context.Context, _ string, _ qrdbsdk.ReadOptions) ([]string, error) {
	return nil, nil
}

// ScanWithConsistency is a no-op for the auth-factor surface.
func (m *memKV) ScanWithConsistency(_ context.Context, _ string, _ qrdbsdk.ReadOptions) (map[string][]byte, error) {
	return nil, nil
}

// ExistsWithConsistency is a no-op for the auth-factor surface.
func (m *memKV) ExistsWithConsistency(_ context.Context, _ string, _ qrdbsdk.ReadOptions) (bool, error) {
	return false, nil
}

// PutEntity is a no-op for the auth-factor surface.
func (m *memKV) PutEntity(_ context.Context, _ *qrdbsdk.PutEntityRequest) (*qrdbsdk.PutEntityResponse, error) {
	return &qrdbsdk.PutEntityResponse{}, nil
}

// GetEntity is a no-op for the auth-factor surface.
func (m *memKV) GetEntity(_ context.Context, _ *qrdbsdk.GetEntityRequest) (*qrdbsdk.GetEntityResponse, error) {
	return &qrdbsdk.GetEntityResponse{}, nil
}

// DeleteEntity is a no-op for the auth-factor surface.
func (m *memKV) DeleteEntity(_ context.Context, _ *qrdbsdk.DeleteEntityRequest) (*qrdbsdk.DeleteEntityResponse, error) {
	return &qrdbsdk.DeleteEntityResponse{}, nil
}

// ListEntities is a no-op for the auth-factor surface.
func (m *memKV) ListEntities(_ context.Context, _ *qrdbsdk.ListEntitiesRequest) (*qrdbsdk.ListEntitiesResponse, error) {
	return &qrdbsdk.ListEntitiesResponse{}, nil
}

// ---------------------------------------------------------------------------
// UserStateUpdater stub
// ---------------------------------------------------------------------------

// userStateStub captures invocations of SetUserActive so the M13
// last-factor-protection test can assert whether the force=true Remove
// call disabled the user (and conversely that the force=false guard
// path did NOT touch the user).
type userStateStub struct {
	mu    sync.Mutex
	calls []userStateCall
}

// userStateCall records a single SetUserActive invocation.
type userStateCall struct {
	UserID uint64
	Active bool
}

// newUserStateStub constructs an empty stub.
func newUserStateStub() *userStateStub {
	return &userStateStub{calls: make([]userStateCall, 0, 4)}
}

// SetUserActive records the (userID, active) tuple and returns nil.
func (s *userStateStub) SetUserActive(_ context.Context, userID uint64, active bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, userStateCall{UserID: userID, Active: active})
	return nil
}

// Calls returns a snapshot of every SetUserActive invocation in order.
func (s *userStateStub) Calls() []userStateCall {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]userStateCall, len(s.calls))
	copy(out, s.calls)
	return out
}

// ---------------------------------------------------------------------------
// UserLookup stub
// ---------------------------------------------------------------------------

// userLookupStub returns a constant admin Principal for testAdminUserID
// and an error for any other ID. The auth.LoginResolver only needs
// LookupUser to translate factor.UserID back into a Principal.
type userLookupStub struct{}

// Compile-time assertion that the stub satisfies auth.UserLookup.
var _ qrdbsdk.UserLookup = (*userLookupStub)(nil)

// LookupUser returns the admin Principal for testAdminUserID.
func (userLookupStub) LookupUser(_ context.Context, userID uint64) (*qrdbsdk.Principal, error) {
	if userID != testAdminUserID {
		return nil, &qrdbsdk.UserHasNoFactorsError{UserID: userID}
	}
	return &qrdbsdk.Principal{
		ID:       testAdminUserID,
		Username: testAdminUsername,
		Email:    "admin@example.test",
		TenantID: "system",
		Roles:    []string{"admin"},
		Active:   true,
	}, nil
}

// ---------------------------------------------------------------------------
// Harness
// ---------------------------------------------------------------------------

// harness bundles the production service, the login resolver, and the
// stubs into a single value so individual tests can keep their wiring
// concise.
type harness struct {
	// svc is the production UserAuthFactorServicer under test.
	svc qrdbsdk.UserAuthFactorServicer

	// resolver is the production LoginResolver bound to svc.
	resolver *qrdbsdk.LoginResolver

	// kv is the in-memory KV store backing svc.
	kv *memKV

	// users captures SetUserActive calls triggered by force=true Remove.
	users *userStateStub
}

// newHarness wires a fresh in-memory KV, a fresh userStateStub, the
// production UserAuthFactorService, and a LoginResolver. The returned
// harness is independent across tests.
func newHarness(t *testing.T) *harness {
	t.Helper()
	kv := newMemKV()
	users := newUserStateStub()
	svc, err := qrdbsdk.NewUserAuthFactorService(qrdbsdk.UserAuthFactorServiceConfig{
		KV:               kv,
		UserStateUpdater: users,
	})
	if err != nil {
		t.Fatalf("NewUserAuthFactorService: %v", err)
	}
	// The service implements auth.FactorStore directly (see compile-time
	// assertion in user_auth_factor_service.go), but the Go type system
	// only sees the UserAuthFactorServicer interface here. Cast to the
	// concrete store interface for the resolver.
	store, ok := svc.(qrdbsdk.FactorStore)
	if !ok {
		t.Fatalf("UserAuthFactorServicer does not implement auth.FactorStore")
	}
	resolver := qrdbsdk.NewLoginResolver(store, userLookupStub{})
	return &harness{
		svc:      svc,
		resolver: resolver,
		kv:       kv,
		users:    users,
	}
}

// ---------------------------------------------------------------------------
// Fake hardware authenticators
// ---------------------------------------------------------------------------

// fido2Vendor bundles a virtual FIDO2 authenticator with the credential
// it holds and the AuthFactor record that should be enrolled with the
// service. The credentialID (cred.ID) is the canonical lookup key.
type fido2Vendor struct {
	Authenticator virtualwebauthn.Authenticator
	Credential    virtualwebauthn.Credential
	Factor        *qrdbsdk.AuthFactor
}

// newFIDO2VendorAuthenticator constructs a fake FIDO2 authenticator
// with the supplied AAGUID and label. The AAGUID differentiates one
// "vendor" from another inside the AuthFactor.Metadata map so the
// upper layers can attribute a credential to a particular hardware
// model (YubiKey, SoloKey, Feitian, etc.).
func newFIDO2VendorAuthenticator(t *testing.T, aaguid [16]byte, label string) *fido2Vendor {
	t.Helper()
	auth := virtualwebauthn.NewAuthenticator()
	auth.Aaguid = aaguid
	cred := virtualwebauthn.NewCredential(virtualwebauthn.KeyTypeEC2)
	auth.AddCredential(cred)
	aaguidHex := hexEncode(aaguid[:])
	factor := &qrdbsdk.AuthFactor{
		UserID:     testAdminUserID,
		Type:       qrdbsdk.AuthFactorFIDO2,
		Label:      label,
		Credential: append([]byte(nil), cred.ID...),
		Metadata: map[string]string{
			"aaguid":   aaguidHex,
			"key_type": string(virtualwebauthn.KeyTypeEC2),
			"vendor":   label,
		},
		CreatedBy: qrdbsdk.FactorCreatedBySelf,
	}
	return &fido2Vendor{
		Authenticator: auth,
		Credential:    cred,
		Factor:        factor,
	}
}

// pivVendor bundles a fake PIV slot certificate, the private key, and
// the AuthFactor record that should be enrolled with the service. The
// canonical lookup key is auth.CredentialKey(cert.Raw).
type pivVendor struct {
	Cert       *x509.Certificate
	PrivateKey *ecdsa.PrivateKey
	Factor     *qrdbsdk.AuthFactor
}

// newPIVVendorCard constructs a fake PIV smart-card credential. The
// vendor and slot strings populate the AuthFactor.Metadata so multiple
// PIV cards from different vendors (Yubico, Thales, Feitian, ...) can
// be distinguished even though they all reduce to the same
// SHA-256(cert) lookup key at the wire layer.
func newPIVVendorCard(t *testing.T, vendor, slot string) *pivVendor {
	t.Helper()
	cn := vendor + "-piv-" + slot
	cert, key := newSelfSignedECDSACert(t, cn)
	cardSerial := hexEncode(cert.SerialNumber.Bytes())
	factor := &qrdbsdk.AuthFactor{
		UserID:     testAdminUserID,
		Type:       qrdbsdk.AuthFactorPIV,
		Label:      cn,
		Credential: qrdbsdk.CredentialKey(cert.Raw),
		Metadata: map[string]string{
			"vendor":      vendor,
			"slot":        slot,
			"card_serial": cardSerial,
			"cert_cn":     cn,
		},
		CreatedBy: qrdbsdk.FactorCreatedByAdmin,
	}
	return &pivVendor{Cert: cert, PrivateKey: key, Factor: factor}
}

// mtlsClient bundles a fake mTLS client certificate, its private key,
// and the AuthFactor record that should be enrolled with the service.
type mtlsClient struct {
	Cert       *x509.Certificate
	PrivateKey *ecdsa.PrivateKey
	Factor     *qrdbsdk.AuthFactor
}

// newMTLSClientCert constructs a fake mTLS client credential. The
// canonical lookup key is auth.CredentialKey(cert.Raw).
func newMTLSClientCert(t *testing.T, cn string) *mtlsClient {
	t.Helper()
	cert, key := newSelfSignedECDSACert(t, cn)
	factor := &qrdbsdk.AuthFactor{
		UserID:     testAdminUserID,
		Type:       qrdbsdk.AuthFactorMTLS,
		Label:      cn,
		Credential: qrdbsdk.CredentialKey(cert.Raw),
		Metadata: map[string]string{
			"cert_cn":     cn,
			"cert_serial": hexEncode(cert.SerialNumber.Bytes()),
		},
		CreatedBy: qrdbsdk.FactorCreatedBySelf,
	}
	return &mtlsClient{Cert: cert, PrivateKey: key, Factor: factor}
}

// ---------------------------------------------------------------------------
// Low-level certificate / encoding helpers
// ---------------------------------------------------------------------------

// newSelfSignedECDSACert mints a fresh P-256 ECDSA self-signed
// certificate suitable for use as a fake mTLS client cert or PIV slot
// cert. The returned cert.Raw is the DER encoding that production code
// hashes to produce the canonical credential lookup key.
func newSelfSignedECDSACert(t *testing.T, cn string) (*x509.Certificate, *ecdsa.PrivateKey) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("ecdsa.GenerateKey: %v", err)
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 127))
	if err != nil {
		t.Fatalf("rand serial: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: cn},
		NotBefore:    time.Now().Add(-time.Hour).UTC(),
		NotAfter:     time.Now().Add(24 * time.Hour).UTC(),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("x509.CreateCertificate: %v", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("x509.ParseCertificate: %v", err)
	}
	return cert, key
}

// hexEncode renders a byte slice as lowercase hex. Used for the
// human-readable AAGUID / card-serial metadata fields. Kept local so
// the helpers file has no encoding/hex import cluttering its surface.
func hexEncode(b []byte) string {
	const digits = "0123456789abcdef"
	out := make([]byte, 0, len(b)*2)
	for _, c := range b {
		out = append(out, digits[c>>4], digits[c&0x0f])
	}
	return string(out)
}

// ---------------------------------------------------------------------------
// Convenience helpers used across multiple test cases
// ---------------------------------------------------------------------------

// addFactor invokes svc.Add for testAdminUserID and returns the
// stored factor's ID. Fails the test on any error.
func (h *harness) addFactor(t *testing.T, factor *qrdbsdk.AuthFactor) string {
	t.Helper()
	if err := h.svc.Add(context.Background(), testAdminUserID, factor); err != nil {
		t.Fatalf("svc.Add(%s, %s): %v", factor.Type, factor.Label, err)
	}
	return factor.ID
}

// resolveOrFatal calls resolver.Resolve and fails the test on error.
func (h *harness) resolveOrFatal(t *testing.T, ft qrdbsdk.AuthFactorType, key []byte) (*qrdbsdk.Principal, *qrdbsdk.AuthFactor) {
	t.Helper()
	p, f, err := h.resolver.Resolve(context.Background(), ft, key)
	if err != nil {
		t.Fatalf("resolver.Resolve(%s): %v", ft, err)
	}
	return p, f
}

// resolveExpectErr calls resolver.Resolve and asserts that the returned
// error matches the given sentinel via errors.Is.
func (h *harness) resolveExpectErr(t *testing.T, ft qrdbsdk.AuthFactorType, key []byte, want error) {
	t.Helper()
	_, _, err := h.resolver.Resolve(context.Background(), ft, key)
	if err == nil {
		t.Fatalf("resolver.Resolve(%s): expected error %v, got nil", ft, want)
	}
	if !errors.Is(err, want) {
		t.Fatalf("resolver.Resolve(%s): expected errors.Is %v, got %v (%T)", ft, want, err, err)
	}
}
