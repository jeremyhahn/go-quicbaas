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

package quicbaas

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"
	"sync"
	"testing"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
)

// =============================================================================
// Enroll -> Login round-trip: the whole point of the split-store fix.
//
// These tests prove that a factor enrolled through the quicbaas SDK
// enrollment path (Security().Auth().Factors()) is written into the EXACT
// same qrdb UserAuthFactor store the unified-login ceremony resolves against,
// so LookupByCredentialKey finds the enrolled factor for the resolved uint64
// user. Before the fix, enrollment wrote to the string-subject ceremony
// registry while login read the uint64 user store — so login could never
// find an enrolled factor.
//
// The store and lookup are exercised with REAL production code:
//
//   - qrdbsdk.NewUserAuthFactorService over a real in-memory KV is the SAME
//     service the node's login resolver uses (service.NewUserAuthFactorService
//     with the same KV).
//   - The enrollment path is the production authFactorsImpl routed through the
//     real qrdb embedded transport.
//
// The user directory is a focused in-test UserServicer that maps a
// tenant-scoped username to a uint64 id; this mirrors the node's user store
// for the subject->uint64 resolution the enrollment path performs.
// =============================================================================

// memKV is a real in-memory KVServicer. It reuses stubKVService for the
// KVServicer methods the user-auth-factor service never touches and provides
// a live map-backed Put/Get/Delete so factor records actually persist and are
// resolvable. This is the store the login lookup reads from.
type memKV struct {
	stubKVService
	mu sync.Mutex
	m  map[string][]byte
}

func newMemKV() *memKV { return &memKV{m: make(map[string][]byte)} }

// List returns the keys under prefix in sorted order, as the real servicer
// does. The auth-factor service lists a user's factor index on every Add
// (go-qrdb v0.25 projects the user's factor view through it), so the fake
// must answer from the map rather than refuse.
func (k *memKV) List(_ context.Context, prefix string) ([]string, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	var keys []string
	for key := range k.m {
		if strings.HasPrefix(key, prefix) {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	return keys, nil
}

func (k *memKV) Put(_ context.Context, key string, val []byte) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	cp := append([]byte(nil), val...)
	k.m[key] = cp
	return nil
}

// PutIfAbsent stores only when the key is absent, which is the contract
// KVServicer defines: stored=false with a nil error means the key was already
// there and the store is unchanged.
func (k *memKV) PutIfAbsent(_ context.Context, key string, val []byte) (bool, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if _, ok := k.m[key]; ok {
		return false, nil
	}
	k.m[key] = append([]byte(nil), val...)
	return true, nil
}

// CompareAndDelete removes the key only when its stored value is byte-identical
// to expected, which is the contract KVServicer defines: deleted=false with a
// nil error covers BOTH a missing key and a key holding something else, and the
// store is unchanged in either case. The fake distinguishes nothing the real
// store does not — a fake that deleted on mismatch, or reported success without
// looking, would let a guarded release pass here and lose a live claim in
// production.
func (k *memKV) CompareAndDelete(_ context.Context, key string, expected []byte) (bool, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	v, ok := k.m[key]
	if !ok || !bytes.Equal(v, expected) {
		return false, nil
	}
	delete(k.m, key)
	return true, nil
}

// CompareAndSwap replaces the value only when the stored one is byte-identical
// to expected — the edit half of the guarded set the two verbs above began. A
// swap is not a create, so an absent key leaves the store unchanged and reports
// false, exactly as the real servicer does. Implemented for real for the same
// reason its siblings are: a fake that always succeeded would let a lost update
// pass here and be found in production.
func (k *memKV) CompareAndSwap(_ context.Context, key string, expected, next []byte) (bool, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	v, ok := k.m[key]
	if !ok || !bytes.Equal(v, expected) {
		return false, nil
	}
	cp := make([]byte, len(next))
	copy(cp, next)
	k.m[key] = cp
	return true, nil
}

func (k *memKV) Get(_ context.Context, key string) ([]byte, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	v, ok := k.m[key]
	if !ok {
		return nil, nil
	}
	return append([]byte(nil), v...), nil
}

func (k *memKV) Delete(_ context.Context, key string) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	delete(k.m, key)
	return nil
}

// testUser is one directory entry keyed by (tenant, username) -> id. The
// email is optional; only a user that has one is resolvable by email.
type testUser struct {
	id       uint64
	tenantID string
	username string
	email    string
}

// directoryUserServicer is a focused UserServicer test double that resolves a
// tenant-scoped username to a uint64 id. Only GetUserByUsername / GetUser are
// exercised by the enrollment path and the login resolver; the remaining
// methods fail loudly so an accidental dependency surfaces in tests.
type directoryUserServicer struct {
	byName map[string]testUser // key: tenantID + "\x00" + username
	byID   map[uint64]testUser
}

func newDirectory(users ...testUser) *directoryUserServicer {
	d := &directoryUserServicer{
		byName: make(map[string]testUser),
		byID:   make(map[uint64]testUser),
	}
	for _, u := range users {
		d.byName[u.tenantID+"\x00"+u.username] = u
		d.byID[u.id] = u
	}
	return d
}

func (d *directoryUserServicer) toSDK(u testUser) *qrdbsdk.User {
	return &qrdbsdk.User{
		ID:        u.id,
		Username:  u.username,
		Email:     u.email,
		TenantID:  u.tenantID,
		Lifecycle: qrdbsdk.LifecycleActive,
	}
}

func (d *directoryUserServicer) CreateUser(context.Context, *qrdbsdk.CreateUserRequest) (*qrdbsdk.User, error) {
	return nil, stubErr
}

func (d *directoryUserServicer) GetUser(_ context.Context, id uint64) (*qrdbsdk.User, error) {
	u, ok := d.byID[id]
	if !ok {
		return nil, stubErr
	}
	return d.toSDK(u), nil
}

func (d *directoryUserServicer) GetUserByUsername(_ context.Context, tenantID, username string) (*qrdbsdk.User, error) {
	u, ok := d.byName[tenantID+"\x00"+username]
	if !ok {
		return nil, stubErr
	}
	return d.toSDK(u), nil
}

// GetUserByEmail resolves only users the fixture gave an email, within the
// tenant; every other lookup fails loudly. The WebAuthn relying party keys
// accounts by email, so a passkey test sets one; a double that answered for a
// user without one would be inventing an index the test never set up.
func (d *directoryUserServicer) GetUserByEmail(_ context.Context, tenantID, email string) (*qrdbsdk.User, error) {
	for _, u := range d.byID {
		if email != "" && u.email == email && u.tenantID == tenantID {
			return d.toSDK(u), nil
		}
	}
	return nil, stubErr
}

func (d *directoryUserServicer) UserByCertThumbprint(context.Context, string) (*qrdbsdk.User, error) {
	return nil, stubErr
}

func (d *directoryUserServicer) AddCertBinding(context.Context, uint64, string, int64) (*qrdbsdk.User, error) {
	return nil, stubErr
}

func (d *directoryUserServicer) RemoveCertBinding(context.Context, uint64, string) (*qrdbsdk.User, error) {
	return nil, stubErr
}

func (d *directoryUserServicer) UpdateUser(context.Context, *qrdbsdk.UpdateUserRequest) (*qrdbsdk.User, error) {
	return nil, stubErr
}
func (d *directoryUserServicer) DeleteUser(context.Context, uint64) error { return stubErr }
func (d *directoryUserServicer) ListUsers(context.Context, *qrdbsdk.ListUsersRequest) (*qrdbsdk.ListUsersResponse, error) {
	return nil, stubErr
}
func (d *directoryUserServicer) EnableUser(context.Context, uint64) error  { return stubErr }
func (d *directoryUserServicer) DisableUser(context.Context, uint64) error { return stubErr }

// buildLoginFactorRoundtrip wires a quicbaas SDK client over a real
// in-memory qrdb UserAuthFactor store (the login store) plus the user
// directory. It returns the client and the SAME factor service instance the
// login resolver would read, so the test can call LookupByCredentialKey
// directly on the store enrollment wrote to.
func buildLoginFactorRoundtrip(t *testing.T, dir *directoryUserServicer) (*client, qrdbsdk.UserAuthFactorServicer) {
	t.Helper()
	kv := newMemKV()
	factorSvc, err := qrdbsdk.NewUserAuthFactorService(qrdbsdk.UserAuthFactorServiceConfig{KV: kv})
	if err != nil {
		t.Fatalf("NewUserAuthFactorService: %v", err)
	}
	c := buildEmbeddedQRDBClient(t, QRDBEmbeddedServicers{
		KV:             kv,
		Admin:          stubAdminService{},
		User:           dir,
		UserAuthFactor: factorSvc,
	})
	return c, factorSvc
}

// TestFactorEnrollment_WritesToLoginStore proves enrollment via the quicbaas
// SDK lands in the store login reads, and LookupByCredentialKey resolves the
// enrolled factor to the SAME uint64 user.
func TestFactorEnrollment_WritesToLoginStore(t *testing.T) {
	const (
		tenant   = "acme"
		username = "alice"
		userID   = uint64(42)
	)
	dir := newDirectory(testUser{id: userID, tenantID: tenant, username: username})
	c, loginStore := buildLoginFactorRoundtrip(t, dir)

	// Synthetic PIV credential: the canonical lookup key login submits is
	// SHA-256(certDER). The enrollment request carries it hex-encoded under
	// credential_sha256, exactly as the CLI builds it.
	certDER := []byte("synthetic-piv-slot-certificate-DER-bytes")
	sum := sha256.Sum256(certDER)
	credKeyHex := hex.EncodeToString(sum[:])

	factors := c.Security().Auth().Factors()
	if factors == nil {
		t.Fatal("expected non-nil Factors() client")
	}

	resp, err := factors.Add(context.Background(), &AuthFactorEnrollRequest{
		FactorName: "piv",
		Subject:    username,
		TenantID:   tenant,
		MethodParams: map[string]any{
			"label":             "alice-yubikey",
			"credential_sha256": credKeyHex,
			"reader":            "Yubico YubiKey",
			"slot":              "9a",
		},
	})
	if err != nil {
		t.Fatalf("Factors().Add: %v", err)
	}
	if resp.EnrollmentID == "" {
		t.Fatal("expected a store-assigned enrollment ID")
	}
	if resp.UserID != username {
		t.Errorf("response UserID = %q, want %q", resp.UserID, username)
	}

	// LOGIN PATH: resolve by the canonical lookup key. The login resolver
	// derives this same key from the PIV proof; here we feed the raw 32-byte
	// SHA-256 the proof reduces to.
	got, err := loginStore.LookupByCredentialKey(context.Background(), qrdbsdk.AuthFactorPIV, sum[:])
	if err != nil {
		t.Fatalf("LookupByCredentialKey (login path): %v — enrollment did NOT write to the login store", err)
	}
	if got == nil {
		t.Fatal("login lookup resolved no factor — split-store regression")
	}
	if got.UserID != userID {
		t.Errorf("resolved UserID = %d, want %d", got.UserID, userID)
	}
	if got.ID != resp.EnrollmentID {
		t.Errorf("resolved factor ID = %q, want enrolled ID %q", got.ID, resp.EnrollmentID)
	}
	if string(got.Credential) != string(sum[:]) {
		t.Errorf("stored Credential != login lookup key: enroll-key and login-key diverged")
	}
	if got.Type != qrdbsdk.AuthFactorPIV {
		t.Errorf("resolved factor type = %q, want piv", got.Type)
	}
}

// TestFactorEnrollment_NotResolvableForDifferentUser proves a factor enrolled
// for user A is NOT resolvable as user B: login resolution returns the OWNING
// user, never a different one, and a different credential key resolves nothing.
func TestFactorEnrollment_NotResolvableForDifferentUser(t *testing.T) {
	const tenant = "acme"
	dir := newDirectory(
		testUser{id: 1, tenantID: tenant, username: "alice"},
		testUser{id: 2, tenantID: tenant, username: "bob"},
	)
	c, loginStore := buildLoginFactorRoundtrip(t, dir)

	aliceCert := []byte("alice-piv-cert-DER")
	aliceSum := sha256.Sum256(aliceCert)

	if _, err := c.Security().Auth().Factors().Add(context.Background(), &AuthFactorEnrollRequest{
		FactorName: "piv",
		Subject:    "alice",
		TenantID:   tenant,
		MethodParams: map[string]any{
			"label":             "alice-card",
			"credential_sha256": hex.EncodeToString(aliceSum[:]),
		},
	}); err != nil {
		t.Fatalf("enroll alice: %v", err)
	}

	// Alice's factor resolves to alice (id 1), never bob (id 2).
	got, err := loginStore.LookupByCredentialKey(context.Background(), qrdbsdk.AuthFactorPIV, aliceSum[:])
	if err != nil {
		t.Fatalf("lookup alice factor: %v", err)
	}
	if got.UserID != 1 {
		t.Fatalf("alice's factor resolved to user %d, want 1", got.UserID)
	}

	// Bob's (never-enrolled) credential key resolves nothing — login fails
	// closed for an unenrolled credential.
	bobCert := []byte("bob-piv-cert-DER")
	bobSum := sha256.Sum256(bobCert)
	if _, err := loginStore.LookupByCredentialKey(context.Background(), qrdbsdk.AuthFactorPIV, bobSum[:]); err == nil {
		t.Fatal("expected lookup of bob's unenrolled credential to fail, but it resolved a factor")
	}
}

// TestFactorEnrollment_FIDO2CredentialIDIsLookupKey proves the fido2 path
// stores the WebAuthn credentialID as the credential bytes so login (whose
// lookup key is []byte(credentialID)) resolves the same factor.
func TestFactorEnrollment_FIDO2CredentialIDIsLookupKey(t *testing.T) {
	const tenant = "acme"
	dir := newDirectory(testUser{id: 7, tenantID: tenant, username: "carol"})
	c, loginStore := buildLoginFactorRoundtrip(t, dir)

	credentialID := "AQIDBAUGBwgJCgsMDQ4PEA" // base64url-ish WebAuthn credential id

	if _, err := c.Security().Auth().Factors().Add(context.Background(), &AuthFactorEnrollRequest{
		FactorName: "fido2",
		Subject:    "carol",
		TenantID:   tenant,
		MethodParams: map[string]any{
			"label":         "carol-passkey",
			"credential_id": credentialID,
			"aaguid":        "00000000-0000-0000-0000-000000000000",
		},
	}); err != nil {
		t.Fatalf("enroll fido2: %v", err)
	}

	got, err := loginStore.LookupByCredentialKey(context.Background(), qrdbsdk.AuthFactorFIDO2, []byte(credentialID))
	if err != nil {
		t.Fatalf("fido2 login lookup: %v", err)
	}
	if got.UserID != 7 {
		t.Errorf("fido2 factor resolved to user %d, want 7", got.UserID)
	}
	if string(got.Credential) != credentialID {
		t.Errorf("stored fido2 Credential = %q, want credentialID %q", string(got.Credential), credentialID)
	}
}

// TestFactorEnrollment_MissingCredentialFailsClosed proves enrollment is
// rejected (never written) when the credential lookup key cannot be derived —
// the auth path fails closed rather than writing a factor login can't read.
func TestFactorEnrollment_MissingCredentialFailsClosed(t *testing.T) {
	const tenant = "acme"
	dir := newDirectory(testUser{id: 1, tenantID: tenant, username: "alice"})
	c, loginStore := buildLoginFactorRoundtrip(t, dir)

	_, err := c.Security().Auth().Factors().Add(context.Background(), &AuthFactorEnrollRequest{
		FactorName:   "piv",
		Subject:      "alice",
		TenantID:     tenant,
		MethodParams: map[string]any{"label": "no-credential"},
	})
	if err == nil {
		t.Fatal("expected Add to fail when credential_sha256 is absent")
	}

	// Nothing was written: a subsequent lookup of any key finds no factor.
	if _, lookErr := loginStore.LookupByCredentialKey(context.Background(), qrdbsdk.AuthFactorPIV, []byte("anything")); lookErr == nil {
		t.Fatal("expected no factor to be resolvable after a rejected enrollment")
	}
}
