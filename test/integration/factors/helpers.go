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
// via the descope/virtualwebauthn library for FIDO2 and, for PIV and
// mTLS, via X.509 client certificates ISSUED BY QuicBaaS's own
// certificate authority (test/integration/certfixture) rather than
// self-signed in this file. Distinct "vendors" are differentiated by
// AAGUID (FIDO2) and by metadata fields (PIV/mTLS) so the resolver's
// User <-> Factor mapping can be verified across heterogeneous
// credential sources.
package factors

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/x509"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/descope/virtualwebauthn"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/test/integration/certfixture"
)

// testAdminUserID is the synthetic admin user that owns every factor
// enrolled by the M13 tests. Matches the "system tenant admin user"
// convention used elsewhere in the IAM unification work.
const testAdminUserID uint64 = 1

// testAdminUsername is the human-readable login name returned by the
// userLookupStub for testAdminUserID.
const testAdminUsername = "admin"

// factorsTrustDomain and factorsTenantID scope the SPIFFE identity the fixture
// CA stamps on a credential it issues here. They are local to these tests: the
// package runs no cluster, so nothing pins them — they exist because a client
// certificate the product issues always carries a tenant, and issuing one
// without a tenant would model a credential the product does not produce.
const (
	factorsTrustDomain = "factors.integration"
	factorsTenantID    = "system"
)

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

// PutIfAbsent stores val at key only when the key is absent, reporting
// whether it stored. The write is performed under the same exclusive lock as
// the existence check so the operation is atomic with respect to concurrent
// callers — a compare-and-set that raced would defeat the very property the
// auth-factor service uses this for (single-winner enrolment).
func (m *memKV) PutIfAbsent(_ context.Context, key string, val []byte) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.store[key]; exists {
		return false, nil
	}
	// Defensive copy so the caller can mutate val without poisoning the
	// stored bytes.
	cp := make([]byte, len(val))
	copy(cp, val)
	m.store[key] = cp
	return true, nil
}

// CompareAndDelete mirrors PutIfAbsent's guard on the release side: it
// removes key only when the stored value still equals expected, reporting
// whether this call removed it. Implemented with real guard semantics rather
// than as a zero-value no-op because it is the release half of PutIfAbsent
// (which this fake also implements for real): an unguarded release would
// un-decide what the guarded create decided, defeating the single-winner
// property callers use the pair for.
func (m *memKV) CompareAndDelete(_ context.Context, key string, expected []byte) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	current, ok := m.store[key]
	if !ok {
		return false, nil
	}
	if !bytes.Equal(current, expected) {
		return false, nil
	}
	delete(m.store, key)
	return true, nil
}

// CompareAndSwap is the edit half of the guarded set PutIfAbsent and
// CompareAndDelete began: it replaces key's value only when the stored value
// still equals expected, reporting whether this call replaced it.
//
// A swap is not a create — an absent key stays absent — so the two honest
// answers are "I replaced it" and "the stored value was not what you expected".
// It is implemented with real guard semantics for the same reason its siblings
// are: a fake that always succeeds would let a test pass over a lost update.
func (m *memKV) CompareAndSwap(_ context.Context, key string, expected, next []byte) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	current, ok := m.store[key]
	if !ok {
		return false, nil
	}
	if !bytes.Equal(current, expected) {
		return false, nil
	}
	cp := make([]byte, len(next))
	copy(cp, next)
	m.store[key] = cp
	return true, nil
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
	cert, key := newClientCredential(t, cn)
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
	cert, key := newClientCredential(t, cn)
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

// newClientCredential issues a client-authentication certificate and its
// private key through QuicBaaS's own certificate authority, for use as a fake
// mTLS client cert or PIV slot cert. The returned cert.Raw is the DER encoding
// production code hashes to produce the canonical credential lookup key.
//
// Each credential gets its own CA under the test's temp directory, which models
// what it stands for — one authenticator from one vendor, trusted on its own
// terms — and keeps the helper hermetic with nothing left behind.
//
// It replaced a local ecdsa.GenerateKey + x509.CreateCertificate pair
// (go-quicbaas#943). That mattered here for the same reason it mattered for the
// cluster fixtures: a self-signed lookalike cannot show a divergence between
// what the resolver accepts and what the product's CA actually stamps, and
// these tests exist to assert exactly that resolution.
func newClientCredential(t *testing.T, cn string) (*x509.Certificate, *ecdsa.PrivateKey) {
	t.Helper()

	ctx := context.Background()
	authority, err := certfixture.Open(ctx, certfixture.Config{
		Dir:               t.TempDir(),
		CommonName:        "factors fixture CA for " + cn,
		SPIFFETrustDomain: factorsTrustDomain,
	})
	if err != nil {
		t.Fatalf("certfixture.Open(%q): %v", cn, err)
	}

	issued, err := authority.IssueClient(ctx, certfixture.ClientFixture{
		CommonName:   cn,
		TenantID:     factorsTenantID,
		ValidityDays: 1,
	})
	if err != nil {
		t.Fatalf("issue client credential %q: %v", cn, err)
	}

	key, ok := issued.Signer.(*ecdsa.PrivateKey)
	if !ok {
		t.Fatalf("issue client credential %q: signer is %T, want *ecdsa.PrivateKey", cn, issued.Signer)
	}
	return issued.Certificate, key
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
