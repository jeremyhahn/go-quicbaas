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

package iam

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"testing"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"
	xkmswebauthn "github.com/jeremyhahn/go-xkms/pkg/webauthn"
)

// =============================================================================
// Test doubles
// =============================================================================

// fakeUserAuthFactorClient records AddAuthFactor calls and lets a test inject a
// failure. Every other method of the qrdb UserAuthFactorClient surface is
// unreachable from the write-through and fails loudly if that changes.
type fakeUserAuthFactorClient struct {
	added   []*qrdbtransport.AuthFactorAddRequest
	addErr  error
	addResp *qrdbtransport.AuthFactorAddResponse
}

func (f *fakeUserAuthFactorClient) AddAuthFactor(_ context.Context, req *qrdbtransport.AuthFactorAddRequest) (*qrdbtransport.AuthFactorAddResponse, error) {
	f.added = append(f.added, req)
	if f.addErr != nil {
		return nil, f.addErr
	}
	if f.addResp != nil {
		return f.addResp, nil
	}
	return &qrdbtransport.AuthFactorAddResponse{
		Factor: qrdbtransport.AuthFactorRecord{ID: "factor-1", Type: req.Type},
	}, nil
}

func (f *fakeUserAuthFactorClient) ListAuthFactors(context.Context, *qrdbtransport.AuthFactorListRequest) (*qrdbtransport.AuthFactorListResponse, error) {
	return nil, errors.New("ListAuthFactors not expected")
}

func (f *fakeUserAuthFactorClient) GetAuthFactor(context.Context, *qrdbtransport.AuthFactorGetRequest) (*qrdbtransport.AuthFactorGetResponse, error) {
	return nil, errors.New("GetAuthFactor not expected")
}

func (f *fakeUserAuthFactorClient) RemoveAuthFactor(context.Context, *qrdbtransport.AuthFactorRemoveRequest) (*qrdbtransport.AuthFactorRemoveResponse, error) {
	return nil, errors.New("RemoveAuthFactor not expected")
}

func (f *fakeUserAuthFactorClient) RotateAuthFactor(context.Context, *qrdbtransport.AuthFactorRotateRequest) (*qrdbtransport.AuthFactorRotateResponse, error) {
	return nil, errors.New("RotateAuthFactor not expected")
}

func (f *fakeUserAuthFactorClient) RelabelAuthFactor(context.Context, *qrdbtransport.AuthFactorRelabelRequest) (*qrdbtransport.AuthFactorRelabelResponse, error) {
	return nil, errors.New("RelabelAuthFactor not expected")
}

func (f *fakeUserAuthFactorClient) LoginAuthFactor(context.Context, *qrdbtransport.AuthFactorLoginRequest) (*qrdbtransport.AuthFactorLoginResponse, error) {
	return nil, errors.New("LoginAuthFactor not expected")
}

// stubCredentialStore mirrors the PRODUCTION qrdb-backed credential store the
// write-through actually wraps: Save is an UPSERT (a KV Put), so
// re-registering the same authenticator refreshes the record instead of
// erroring. xkms's MemoryCredentialStore rejects duplicates, which is the
// wrong semantics for the compensation tests. Save and Delete failures are
// injectable so the transactional paths can be driven deterministically.
type stubCredentialStore struct {
	creds     map[string]*xkmswebauthn.Credential
	saveErr   error
	deleteErr error
	deletes   [][]byte
}

func newStubCredentialStore() *stubCredentialStore {
	return &stubCredentialStore{creds: make(map[string]*xkmswebauthn.Credential)}
}

func (s *stubCredentialStore) key(credID []byte) string {
	return base64.RawURLEncoding.EncodeToString(credID)
}

func (s *stubCredentialStore) Save(_ context.Context, cred *xkmswebauthn.Credential) error {
	if s.saveErr != nil {
		return s.saveErr
	}
	s.creds[s.key(cred.ID)] = cred
	return nil
}

func (s *stubCredentialStore) GetByCredentialID(_ context.Context, credID []byte) (*xkmswebauthn.Credential, error) {
	cred, ok := s.creds[s.key(credID)]
	if !ok {
		return nil, xkmswebauthn.ErrCredentialNotFound
	}
	return cred, nil
}

func (s *stubCredentialStore) Delete(_ context.Context, credID []byte) error {
	s.deletes = append(s.deletes, credID)
	if s.deleteErr != nil {
		return s.deleteErr
	}
	delete(s.creds, s.key(credID))
	return nil
}

func (s *stubCredentialStore) GetByUserID(context.Context, []byte) ([]*xkmswebauthn.Credential, error) {
	return nil, errors.New("GetByUserID not expected")
}

func (s *stubCredentialStore) Update(context.Context, *xkmswebauthn.Credential) error {
	return errors.New("Update not expected")
}

func (s *stubCredentialStore) DeleteByUserID(context.Context, []byte) error {
	return errors.New("DeleteByUserID not expected")
}

// =============================================================================
// Fixtures
// =============================================================================

const testWriteThroughTenant = "acme"

// testUserHandle is the qrdb uint64 user id in its canonical 8-byte big-endian
// WebAuthn handle form — the exact encoding xkms's qrdb adapter uses.
func testUserHandle(id uint64) []byte {
	h := make([]byte, 8)
	binary.BigEndian.PutUint64(h, id)
	return h
}

// testCredential builds a WebAuthn credential owned by the given qrdb user id.
func testCredential(userID uint64, credID []byte) *xkmswebauthn.Credential {
	cred := &xkmswebauthn.Credential{
		ID:              credID,
		UserID:          testUserHandle(userID),
		PublicKey:       []byte("cose-public-key"),
		AttestationType: "none",
	}
	cred.Authenticator.AAGUID = []byte{0xaa, 0xbb, 0xcc, 0xdd}
	return cred
}

// newTestWriteThrough builds the decorator over xkms's real in-memory stores so
// the tests exercise genuine persistence semantics rather than a mock.
func newTestWriteThrough(t *testing.T, factors qrdbtransport.UserAuthFactorClient) (*webAuthnFactorWriteThrough, *xkmswebauthn.MemoryUserStore, xkmswebauthn.CredentialStore) {
	t.Helper()
	users := xkmswebauthn.NewMemoryUserStore()
	creds := xkmswebauthn.NewMemoryCredentialStore()
	wt, err := newWebAuthnFactorWriteThrough(creds, users, factors, testWriteThroughTenant, nil)
	if err != nil {
		t.Fatalf("newWebAuthnFactorWriteThrough: %v", err)
	}
	return wt, users, creds
}

// seedUser stores a user under the given qrdb user id so the label lookup
// resolves. MemoryUserStore.Save keys by the user's WebAuthn handle, so the
// handle must be the big-endian qrdb id the credential carries.
func seedUser(t *testing.T, users *xkmswebauthn.MemoryUserStore, id uint64, email, displayName string) {
	t.Helper()
	user := xkmswebauthn.NewDefaultUser(testUserHandle(id), email, displayName)
	if err := users.Save(context.Background(), user); err != nil {
		t.Fatalf("seed user: %v", err)
	}
}

// =============================================================================
// Success path
// =============================================================================

// TestWriteThroughSave_EnrollsLoginResolvableFactor is the headline regression
// guard: a completed registration must write a qrdb fido2 factor whose
// Credential is byte-identical to the base64url credential id the login proof
// reduces to. Without it a browser-registered passkey can never complete a
// CLI/SDK login.
func TestWriteThroughSave_EnrollsLoginResolvableFactor(t *testing.T) {
	factors := &fakeUserAuthFactorClient{}
	wt, users, creds := newTestWriteThrough(t, factors)
	seedUser(t, users, 42, "alice@example.com", "alice's yubikey")

	credID := []byte{1, 2, 3, 4, 5, 6, 7, 8}
	if err := wt.Save(context.Background(), testCredential(42, credID)); err != nil {
		t.Fatalf("Save: %v", err)
	}

	if len(factors.added) != 1 {
		t.Fatalf("expected exactly one factor enrollment, got %d", len(factors.added))
	}
	req := factors.added[0]

	if req.UserID != 42 {
		t.Errorf("factor must bind to the qrdb uint64 user id decoded from the WebAuthn handle: got %d", req.UserID)
	}
	if req.Type != string(qrdbtransport.FactorTypeFIDO2) {
		t.Errorf("factor type = %q, want %q", req.Type, qrdbtransport.FactorTypeFIDO2)
	}
	wantKey := base64.RawURLEncoding.EncodeToString(credID)
	if string(req.Credential) != wantKey {
		t.Errorf("factor credential = %q, want the base64url credential id %q", req.Credential, wantKey)
	}
	if req.Label != "alice's yubikey" {
		t.Errorf("factor label = %q, want the WebAuthn display name", req.Label)
	}
	if req.Metadata["tenant_id"] != testWriteThroughTenant {
		t.Errorf("factor metadata tenant_id = %q, want %q", req.Metadata["tenant_id"], testWriteThroughTenant)
	}
	if req.Metadata["aaguid"] != "aabbccdd" {
		t.Errorf("factor metadata aaguid = %q, want the hex AAGUID", req.Metadata["aaguid"])
	}
	if req.Metadata["attestation_type"] != "none" {
		t.Errorf("factor metadata attestation_type = %q, want %q", req.Metadata["attestation_type"], "none")
	}
	if req.CreatedBy != webAuthnFactorCreatedBy {
		t.Errorf("factor created_by = %q, want %q", req.CreatedBy, webAuthnFactorCreatedBy)
	}

	// The credential itself must still be persisted in the xkms store.
	if _, err := creds.GetByCredentialID(context.Background(), credID); err != nil {
		t.Errorf("credential must remain persisted after a successful enrollment: %v", err)
	}
}

// TestWriteThroughSave_LabelFallsBackToEmail proves a registrant with no
// display name still yields a labelled factor, so `auth factor list` is never
// blank.
func TestWriteThroughSave_LabelFallsBackToEmail(t *testing.T) {
	factors := &fakeUserAuthFactorClient{}
	wt, users, _ := newTestWriteThrough(t, factors)
	seedUser(t, users, 7, "bob@example.com", "")

	if err := wt.Save(context.Background(), testCredential(7, []byte{9, 9, 9})); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if got := factors.added[0].Label; got != "bob@example.com" {
		t.Errorf("label = %q, want the registrant email fallback", got)
	}
}

// TestWriteThroughSave_UnknownUserStillEnrolls proves the label lookup is
// best-effort: a user the WebAuthn store cannot resolve must NOT block the
// enrollment, because the factor binding comes from the credential's handle,
// not from the profile read.
func TestWriteThroughSave_UnknownUserStillEnrolls(t *testing.T) {
	factors := &fakeUserAuthFactorClient{}
	wt, _, _ := newTestWriteThrough(t, factors)

	if err := wt.Save(context.Background(), testCredential(99, []byte{4, 4})); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if len(factors.added) != 1 {
		t.Fatalf("expected the factor to be enrolled without a resolvable profile")
	}
	if factors.added[0].Label != "" {
		t.Errorf("label = %q, want empty when the profile cannot be resolved", factors.added[0].Label)
	}
}

// =============================================================================
// Idempotency
// =============================================================================

// TestWriteThroughSave_IdempotentReRegistration proves re-registering the same
// authenticator does not error the ceremony. qrdb answers the duplicate with
// FactorAlreadyEnrolledError; the write-through treats that as success because
// the login-resolvable factor already exists.
func TestWriteThroughSave_IdempotentReRegistration(t *testing.T) {
	factors := &fakeUserAuthFactorClient{
		addErr: &qrdbtransport.FactorAlreadyEnrolledError{
			Type:     qrdbtransport.FactorTypeFIDO2,
			FactorID: "factor-1",
		},
	}
	wt, users, creds := newTestWriteThrough(t, factors)
	seedUser(t, users, 42, "alice@example.com", "alice's yubikey")

	credID := []byte{1, 2, 3}
	if err := wt.Save(context.Background(), testCredential(42, credID)); err != nil {
		t.Fatalf("a duplicate enrollment must not fail the ceremony: %v", err)
	}
	// The credential must NOT be compensated away — the factor is present.
	if _, err := creds.GetByCredentialID(context.Background(), credID); err != nil {
		t.Errorf("credential must survive an idempotent re-registration: %v", err)
	}
}

// TestIsFactorAlreadyEnrolled covers both duplicate shapes plus the negative
// cases, so idempotency never silently depends on which qrdb transport the
// server was built with.
func TestIsFactorAlreadyEnrolled(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"transport shape", &qrdbtransport.FactorAlreadyEnrolledError{FactorID: "f1"}, true},
		{"wrapped transport shape", &WebAuthnFactorEnrollError{
			Cause: &qrdbtransport.FactorAlreadyEnrolledError{FactorID: "f1"},
		}, true},
		{"unrelated", errors.New("kv unavailable"), false},
		{"not-found", &qrdbtransport.FactorNotFoundError{FactorID: "f1"}, false},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			if got := isFactorAlreadyEnrolled(tt.err); got != tt.want {
				t.Errorf("isFactorAlreadyEnrolled(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

// =============================================================================
// Failure / compensation
// =============================================================================

// TestWriteThroughSave_CompensatesOnEnrollFailure is the transactional guard:
// when the factor enrollment fails, the credential written moments earlier must
// be rolled back and the ceremony must fail closed. Otherwise the registration
// leaves exactly the orphaned credential this fix exists to prevent.
func TestWriteThroughSave_CompensatesOnEnrollFailure(t *testing.T) {
	enrollErr := errors.New("qrdb unavailable")
	factors := &fakeUserAuthFactorClient{addErr: enrollErr}

	users := xkmswebauthn.NewMemoryUserStore()
	inner := newStubCredentialStore()
	wt, err := newWebAuthnFactorWriteThrough(inner, users, factors, testWriteThroughTenant, nil)
	if err != nil {
		t.Fatalf("newWebAuthnFactorWriteThrough: %v", err)
	}
	seedUser(t, users, 42, "alice@example.com", "alice's yubikey")

	credID := []byte{1, 2, 3}
	saveErr := wt.Save(context.Background(), testCredential(42, credID))
	if saveErr == nil {
		t.Fatal("a failed factor enrollment must fail the registration ceremony closed")
	}

	var typed *WebAuthnFactorEnrollError
	if !errors.As(saveErr, &typed) {
		t.Fatalf("error = %T, want *WebAuthnFactorEnrollError", saveErr)
	}
	if !typed.Compensated {
		t.Error("the credential written by this ceremony must be reported as rolled back")
	}
	if typed.CredentialID != base64.RawURLEncoding.EncodeToString(credID) {
		t.Errorf("CredentialID = %q, want the base64url credential id", typed.CredentialID)
	}
	if !errors.Is(saveErr, enrollErr) {
		t.Error("the enrollment cause must be unwrappable")
	}

	// The orphan must be gone.
	if _, gerr := inner.GetByCredentialID(context.Background(), credID); gerr == nil {
		t.Error("the credential must be deleted when its factor could not be enrolled")
	}
}

// TestWriteThroughSave_DoesNotCompensateAPreExistingCredential proves a retry
// can never destroy a good credential: when the credential already existed
// before this Save, a subsequent enrollment failure leaves it in place.
func TestWriteThroughSave_DoesNotCompensateAPreExistingCredential(t *testing.T) {
	factors := &fakeUserAuthFactorClient{}
	users := xkmswebauthn.NewMemoryUserStore()
	inner := newStubCredentialStore()
	wt, err := newWebAuthnFactorWriteThrough(inner, users, factors, testWriteThroughTenant, nil)
	if err != nil {
		t.Fatalf("newWebAuthnFactorWriteThrough: %v", err)
	}
	seedUser(t, users, 42, "alice@example.com", "alice's yubikey")

	credID := []byte{1, 2, 3}
	cred := testCredential(42, credID)
	if serr := wt.Save(context.Background(), cred); serr != nil {
		t.Fatalf("first Save: %v", serr)
	}

	// Second ceremony over the SAME credential, now with a failing enroll.
	factors.addErr = errors.New("qrdb unavailable")
	saveErr := wt.Save(context.Background(), cred)
	if saveErr == nil {
		t.Fatal("the failed enrollment must still fail closed")
	}
	var typed *WebAuthnFactorEnrollError
	if !errors.As(saveErr, &typed) {
		t.Fatalf("error = %T, want *WebAuthnFactorEnrollError", saveErr)
	}
	if typed.Compensated {
		t.Error("a credential that pre-existed this ceremony must not be rolled back")
	}
	if len(inner.deletes) != 0 {
		t.Errorf("no delete may be issued for a pre-existing credential, got %d", len(inner.deletes))
	}
	if _, gerr := inner.GetByCredentialID(context.Background(), credID); gerr != nil {
		t.Errorf("the pre-existing credential must survive: %v", gerr)
	}
}

// TestWriteThroughSave_ReportsFailedCompensation proves a rollback that itself
// fails is reported honestly (Compensated=false) instead of claiming a clean
// state the store is not in.
func TestWriteThroughSave_ReportsFailedCompensation(t *testing.T) {
	factors := &fakeUserAuthFactorClient{addErr: errors.New("qrdb unavailable")}
	users := xkmswebauthn.NewMemoryUserStore()
	inner := newStubCredentialStore()
	inner.deleteErr = errors.New("kv delete failed")
	wt, err := newWebAuthnFactorWriteThrough(inner, users, factors, testWriteThroughTenant, nil)
	if err != nil {
		t.Fatalf("newWebAuthnFactorWriteThrough: %v", err)
	}

	saveErr := wt.Save(context.Background(), testCredential(42, []byte{1, 2, 3}))
	var typed *WebAuthnFactorEnrollError
	if !errors.As(saveErr, &typed) {
		t.Fatalf("error = %T, want *WebAuthnFactorEnrollError", saveErr)
	}
	if typed.Compensated {
		t.Error("Compensated must be false when the rollback itself failed")
	}
	if typed.Error() == "" {
		t.Error("the typed error must render a message")
	}
}

// TestWriteThroughSave_InnerSaveFailureSkipsEnrollment proves the enrollment
// never runs when the credential could not be persisted — an enroll-first
// ordering would leave a factor pointing at a credential that does not exist.
func TestWriteThroughSave_InnerSaveFailureSkipsEnrollment(t *testing.T) {
	factors := &fakeUserAuthFactorClient{}
	users := xkmswebauthn.NewMemoryUserStore()
	storeErr := errors.New("kv put failed")
	inner := newStubCredentialStore()
	inner.saveErr = storeErr
	wt, err := newWebAuthnFactorWriteThrough(inner, users, factors, testWriteThroughTenant, nil)
	if err != nil {
		t.Fatalf("newWebAuthnFactorWriteThrough: %v", err)
	}

	if serr := wt.Save(context.Background(), testCredential(42, []byte{1, 2, 3})); !errors.Is(serr, storeErr) {
		t.Fatalf("Save error = %v, want the inner store failure", serr)
	}
	if len(factors.added) != 0 {
		t.Error("no factor may be enrolled when the credential was not persisted")
	}
}

// TestWriteThroughSave_RejectsMalformedUserHandle proves a credential whose
// user handle is not the canonical 8-byte qrdb id fails closed BEFORE anything
// is written, rather than enrolling a factor against a bogus user id.
func TestWriteThroughSave_RejectsMalformedUserHandle(t *testing.T) {
	factors := &fakeUserAuthFactorClient{}
	wt, _, creds := newTestWriteThrough(t, factors)

	credID := []byte{1, 2, 3}
	err := wt.Save(context.Background(), &xkmswebauthn.Credential{
		ID:     credID,
		UserID: []byte{1, 2, 3}, // not an 8-byte big-endian handle
	})
	if err == nil {
		t.Fatal("a malformed user handle must fail the ceremony closed")
	}
	var typed *WebAuthnFactorEnrollError
	if !errors.As(err, &typed) {
		t.Fatalf("error = %T, want *WebAuthnFactorEnrollError", err)
	}
	if len(factors.added) != 0 {
		t.Error("no factor may be enrolled for a malformed handle")
	}
	if _, gerr := creds.GetByCredentialID(context.Background(), credID); gerr == nil {
		t.Error("no credential may be persisted for a malformed handle")
	}
}

// TestWriteThroughSave_RejectsNilCredential proves a nil credential is refused
// with a typed error instead of being handed to the inner store, which is not
// required to nil-check before dereferencing.
func TestWriteThroughSave_RejectsNilCredential(t *testing.T) {
	factors := &fakeUserAuthFactorClient{}
	wt, _, _ := newTestWriteThrough(t, factors)

	err := wt.Save(context.Background(), nil)
	var typed *WebAuthnFactorStoreConfigError
	if !errors.As(err, &typed) {
		t.Fatalf("error = %v, want *WebAuthnFactorStoreConfigError", err)
	}
	if typed.Field != "credential" {
		t.Errorf("Field = %q, want %q", typed.Field, "credential")
	}
	if len(factors.added) != 0 {
		t.Error("no factor may be enrolled for a nil credential")
	}
}

// TestWriteThroughSave_EmptyCredentialIDDelegates proves a credential with no
// id is handed straight to the inner store — its validation owns that case —
// and that no factor is enrolled for an unkeyable credential.
func TestWriteThroughSave_EmptyCredentialIDDelegates(t *testing.T) {
	factors := &fakeUserAuthFactorClient{}
	inner := newStubCredentialStore()
	wt, err := newWebAuthnFactorWriteThrough(
		inner, xkmswebauthn.NewMemoryUserStore(), factors, testWriteThroughTenant, nil)
	if err != nil {
		t.Fatalf("newWebAuthnFactorWriteThrough: %v", err)
	}

	if serr := wt.Save(context.Background(), &xkmswebauthn.Credential{UserID: testUserHandle(42)}); serr != nil {
		t.Fatalf("Save must delegate, got %v", serr)
	}
	if len(factors.added) != 0 {
		t.Error("no factor may be enrolled for a credential with no id")
	}
}

// =============================================================================
// Construction
// =============================================================================

// TestNewWebAuthnFactorWriteThrough_RequiresEveryDependency proves the
// decorator fails closed at construction. A nil factor client would silently
// reproduce the split-store bug (registration succeeds, login can never
// resolve), so it must be a startup error, not a runtime no-op.
func TestNewWebAuthnFactorWriteThrough_RequiresEveryDependency(t *testing.T) {
	users := xkmswebauthn.NewMemoryUserStore()
	creds := xkmswebauthn.NewMemoryCredentialStore()
	factors := &fakeUserAuthFactorClient{}

	tests := []struct {
		name      string
		inner     xkmswebauthn.CredentialStore
		users     xkmswebauthn.UserStore
		factors   qrdbtransport.UserAuthFactorClient
		tenantID  string
		wantField string
	}{
		{"nil credential store", nil, users, factors, testWriteThroughTenant, "CredentialStore"},
		{"nil user store", creds, nil, factors, testWriteThroughTenant, "UserStore"},
		{"nil factor client", creds, users, nil, testWriteThroughTenant, "UserAuthFactorClient"},
		{"empty tenant", creds, users, factors, "", "tenantID"},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			_, err := newWebAuthnFactorWriteThrough(tt.inner, tt.users, tt.factors, tt.tenantID, nil)
			var typed *WebAuthnFactorStoreConfigError
			if !errors.As(err, &typed) {
				t.Fatalf("error = %v, want *WebAuthnFactorStoreConfigError", err)
			}
			if typed.Field != tt.wantField {
				t.Errorf("Field = %q, want %q", typed.Field, tt.wantField)
			}
		})
	}
}

// TestNewWebAuthnFactorWriteThrough_DefaultsLogger proves a nil logger is
// tolerated (production always supplies one; tests should not have to).
func TestNewWebAuthnFactorWriteThrough_DefaultsLogger(t *testing.T) {
	wt, err := newWebAuthnFactorWriteThrough(
		xkmswebauthn.NewMemoryCredentialStore(),
		xkmswebauthn.NewMemoryUserStore(),
		&fakeUserAuthFactorClient{},
		testWriteThroughTenant,
		nil)
	if err != nil {
		t.Fatalf("newWebAuthnFactorWriteThrough: %v", err)
	}
	if wt.logger == nil {
		t.Error("a nil logger must default to slog.Default()")
	}
}

// TestWebAuthnFactorErrors_RenderAndUnwrap covers the typed errors' zero-value
// and unwrap behaviour so callers can rely on errors.Is / errors.As.
func TestWebAuthnFactorErrors_RenderAndUnwrap(t *testing.T) {
	var nilEnroll *WebAuthnFactorEnrollError
	if nilEnroll.Error() == "" {
		t.Error("a nil WebAuthnFactorEnrollError must still render")
	}
	if nilEnroll.Unwrap() != nil {
		t.Error("a nil WebAuthnFactorEnrollError must unwrap to nil")
	}

	cause := errors.New("boom")
	enroll := &WebAuthnFactorEnrollError{CredentialID: "abc", Cause: cause}
	if !errors.Is(enroll, cause) {
		t.Error("the cause must be unwrappable")
	}

	var nilCfg *WebAuthnFactorStoreConfigError
	if nilCfg.Error() == "" {
		t.Error("a nil WebAuthnFactorStoreConfigError must still render")
	}
	if (&WebAuthnFactorStoreConfigError{}).Error() == "" {
		t.Error("an empty-field config error must still render")
	}
}

// TestWebAuthnCredentialLookupKey pins the encoding contract shared with the
// login path: the factor lookup key is the unpadded base64url credential id.
func TestWebAuthnCredentialLookupKey(t *testing.T) {
	got := webAuthnCredentialLookupKey([]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16})
	if want := "AQIDBAUGBwgJCgsMDQ4PEA"; got != want {
		t.Errorf("lookup key = %q, want %q", got, want)
	}
	if got := webAuthnCredentialLookupKey(nil); got != "" {
		t.Errorf("empty credential id must encode to an empty key, got %q", got)
	}
}
