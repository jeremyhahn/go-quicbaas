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
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"testing"
	"time"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/go-webauthn/webauthn/protocol"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"
	xkmswebauthn "github.com/jeremyhahn/go-xkms/pkg/webauthn"
)

// =============================================================================
// Test doubles
//
// These extend the write-through fixtures (webauthn_factor_writethrough_test.go)
// with the surface the deletion path exercises: a factor client that can answer
// List/Remove as well as Add.
// =============================================================================

// reapableFactorClient answers the List/Remove pair the credential deletion
// path needs. It embeds the add-recording fake so a test can drive a full
// register-then-delete round trip through one client.
type reapableFactorClient struct {
	fakeUserAuthFactorClient

	stored    []qrdbtransport.AuthFactorRecord
	removed   []string
	listErr   error
	removeErr error
}

func (f *reapableFactorClient) AddAuthFactor(ctx context.Context, req *qrdbtransport.AuthFactorAddRequest) (*qrdbtransport.AuthFactorAddResponse, error) {
	resp, err := f.fakeUserAuthFactorClient.AddAuthFactor(ctx, req)
	if err != nil {
		return nil, err
	}
	record := qrdbtransport.AuthFactorRecord{
		ID:         resp.Factor.ID,
		UserID:     req.UserID,
		Type:       req.Type,
		Label:      req.Label,
		Credential: req.Credential,
		Metadata:   req.Metadata,
	}
	f.stored = append(f.stored, record)
	return resp, nil
}

// AddAttested routes the recorder path through this fake's own AddAuthFactor,
// so a ceremony-proven enrollment lands in stored like any other.
func (f *reapableFactorClient) AddAttested(ctx context.Context, userID uint64, factor *qrdbsdk.AuthFactor, attestation qrdbsdk.VerifiedAttestation) error {
	if !attestation.Level.Valid() {
		return &qrdbsdk.InvalidAttestationError{Level: attestation.Level}
	}
	f.recordAttested(attestation)
	_, err := f.AddAuthFactor(ctx, attestedAddRequest(userID, factor, attestation))
	return err
}

func (f *reapableFactorClient) ListAuthFactors(_ context.Context, req *qrdbtransport.AuthFactorListRequest) (*qrdbtransport.AuthFactorListResponse, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	out := make([]qrdbtransport.AuthFactorRecord, 0, len(f.stored))
	for _, record := range f.stored {
		if record.UserID == req.UserID {
			out = append(out, record)
		}
	}
	return &qrdbtransport.AuthFactorListResponse{Factors: out, Total: len(out)}, nil
}

func (f *reapableFactorClient) RemoveAuthFactor(_ context.Context, req *qrdbtransport.AuthFactorRemoveRequest) (*qrdbtransport.AuthFactorRemoveResponse, error) {
	if f.removeErr != nil {
		return nil, f.removeErr
	}
	f.removed = append(f.removed, req.FactorID)
	out := f.stored[:0]
	for _, record := range f.stored {
		if record.ID != req.FactorID {
			out = append(out, record)
		}
	}
	f.stored = out
	return &qrdbtransport.AuthFactorRemoveResponse{Status: "revoked"}, nil
}

// newRevokingWriteThrough builds the decorator over xkms's real in-memory
// stores so the deletion paths run against genuine persistence semantics.
func newRevokingWriteThrough(
	t *testing.T,
	factors qrdbtransport.UserAuthFactorClient,
) (*webAuthnFactorWriteThrough, *xkmswebauthn.MemoryUserStore, xkmswebauthn.CredentialStore) {
	t.Helper()
	users := xkmswebauthn.NewMemoryUserStore()
	creds := xkmswebauthn.NewMemoryCredentialStore()
	wt, err := newWebAuthnFactorWriteThrough(creds, users, factors, recorderOf(factors), testWriteThroughTenant, permissiveTestGate(), nil)
	if err != nil {
		t.Fatalf("newWebAuthnFactorWriteThrough: %v", err)
	}
	return wt, users, creds
}

// =============================================================================
// Credential deletion revokes the login factor
// =============================================================================

// TestWriteThroughDelete_RevokesLoginFactor is the mirror of the enrollment
// guard: deleting a credential must take its fido2 factor with it, or the
// factor keeps resolving at login to a passkey that no longer exists.
func TestWriteThroughDelete_RevokesLoginFactor(t *testing.T) {
	factors := &reapableFactorClient{}
	wt, users, _ := newRevokingWriteThrough(t, factors)
	seedUser(t, users, 42, "alice@example.com", "alice's yubikey")

	credID := []byte{1, 2, 3, 4}
	ctx := context.Background()
	if err := wt.Save(ctx, testCredential(42, credID)); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if len(factors.stored) != 1 {
		t.Fatalf("registration must enroll one factor, got %d", len(factors.stored))
	}

	if err := wt.Delete(ctx, credID); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	if len(factors.stored) != 0 {
		t.Errorf("the fido2 factor must be revoked with its credential, %d left", len(factors.stored))
	}
	if _, err := wt.GetByCredentialID(ctx, credID); !errors.Is(err, xkmswebauthn.ErrCredentialNotFound) {
		t.Errorf("credential must be gone, GetByCredentialID err = %v", err)
	}
}

// TestWriteThroughDelete_FailsClosedOnRevokeFailure pins the ordering: a factor
// that cannot be revoked leaves the credential in place, so the pair never
// splits into "factor without credential" — the state that fails hardware
// verification at login and leaves an entry an operator has to chase.
func TestWriteThroughDelete_FailsClosedOnRevokeFailure(t *testing.T) {
	revokeErr := errors.New("qrdb unavailable")
	factors := &reapableFactorClient{removeErr: revokeErr}
	wt, users, _ := newRevokingWriteThrough(t, factors)
	seedUser(t, users, 42, "alice@example.com", "alice's yubikey")

	credID := []byte{9, 9, 9}
	ctx := context.Background()
	if err := wt.Save(ctx, testCredential(42, credID)); err != nil {
		t.Fatalf("Save: %v", err)
	}

	err := wt.Delete(ctx, credID)
	if err == nil {
		t.Fatal("a failed factor revocation must fail the credential deletion closed")
	}
	var typed *WebAuthnFactorRevokeError
	if !errors.As(err, &typed) {
		t.Fatalf("error = %T, want *WebAuthnFactorRevokeError", err)
	}
	if !errors.Is(err, revokeErr) {
		t.Error("the underlying revocation failure must be unwrappable")
	}
	if _, gErr := wt.GetByCredentialID(ctx, credID); gErr != nil {
		t.Errorf("the credential must survive a failed revocation, got %v", gErr)
	}
}

// TestWriteThroughDeleteByUserID_RevokesEveryFactor guards the bulk path: it
// must not shortcut to the inner store's bulk removal, which would strip every
// credential and leave every factor.
func TestWriteThroughDeleteByUserID_RevokesEveryFactor(t *testing.T) {
	factors := &reapableFactorClient{}
	wt, users, _ := newRevokingWriteThrough(t, factors)
	seedUser(t, users, 42, "alice@example.com", "alice's keys")

	ctx := context.Background()
	for i, credID := range [][]byte{{1}, {2}, {3}} {
		factors.addResp = &qrdbtransport.AuthFactorAddResponse{
			Factor: qrdbtransport.AuthFactorRecord{ID: string(rune('a' + i)), Type: string(qrdbtransport.FactorTypeFIDO2)},
		}
		if err := wt.Save(ctx, testCredential(42, credID)); err != nil {
			t.Fatalf("Save %d: %v", i, err)
		}
	}
	if len(factors.stored) != 3 {
		t.Fatalf("expected 3 enrolled factors, got %d", len(factors.stored))
	}

	if err := wt.DeleteByUserID(ctx, testUserHandle(42)); err != nil {
		t.Fatalf("DeleteByUserID: %v", err)
	}
	if len(factors.stored) != 0 {
		t.Errorf("every fido2 factor must be revoked, %d left", len(factors.stored))
	}
}

// =============================================================================
// IdentityStore cascade
// =============================================================================

// newCascadeIdentityStore builds an identity store over the package's in-memory
// test storage, returning it with the tenant id its users are created under.
func newCascadeIdentityStore(t *testing.T) (*IdentityStore, string) {
	t.Helper()
	return newTestIdentityStore(), "tenant1"
}

// recordingReaper captures the cascade's arguments and can fail on demand. It
// also answers the LISTING half of the seam, because the identity store
// resolves its credential lister from the reaper it is given
// (SetWebAuthnCredentialReaper) exactly as the production wiring does.
type recordingReaper struct {
	calls []string
	count int
	err   error

	// listed is what ListCredentials reports per address; listErr fails it.
	listed  map[string]int
	listErr error
}

func (r *recordingReaper) DeleteCredentials(_ context.Context, tenantID, email string) (int, error) {
	r.calls = append(r.calls, tenantID+"/"+email)
	if r.err != nil {
		return 0, r.err
	}
	return r.count, nil
}

func (r *recordingReaper) ListCredentials(_ context.Context, tenantID, email string) ([]WebAuthnCredentialSummary, error) {
	if r.listErr != nil {
		return nil, r.listErr
	}
	out := make([]WebAuthnCredentialSummary, 0, r.listed[tenantID+"/"+email])
	for i := 0; i < r.listed[tenantID+"/"+email]; i++ {
		out = append(out, WebAuthnCredentialSummary{ID: tenantID + "/" + email + "/" + strconv.Itoa(i)})
	}
	return out, nil
}

// TestIdentityStoreDeleteUser_CascadesWebAuthnCredentials is the #273 cascade
// guard: deleting a user must take its WebAuthn credentials with it, keyed by
// the user's EMAIL — the key the credential store and the registration ceremony
// both use.
func TestIdentityStoreDeleteUser_CascadesWebAuthnCredentials(t *testing.T) {
	store, tenantID := newCascadeIdentityStore(t)
	reaper := &recordingReaper{count: 1}
	store.SetWebAuthnCredentialReaper(reaper)

	user, err := store.CreateUser(context.Background(), tenantID, CreateUserOptions{
		Email:    "alice@example.com",
		Username: "alice",
	})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	if err := store.DeleteUser(context.Background(), tenantID, user.ID); err != nil {
		t.Fatalf("DeleteUser: %v", err)
	}

	if len(reaper.calls) != 1 {
		t.Fatalf("the cascade must run exactly once, got %d calls", len(reaper.calls))
	}
	if want := tenantID + "/alice@example.com"; reaper.calls[0] != want {
		t.Errorf("cascade called with %q, want %q", reaper.calls[0], want)
	}
}

// TestIdentityStoreDeleteUser_FailsClosedOnCascadeFailure pins the ordering
// choice. Deleting the user first would destroy the email the cascade needs to
// find its credentials, producing exactly the orphan #273 is about, so an
// unreachable credential store must abort the deletion instead.
func TestIdentityStoreDeleteUser_FailsClosedOnCascadeFailure(t *testing.T) {
	store, tenantID := newCascadeIdentityStore(t)
	store.SetWebAuthnCredentialReaper(&recordingReaper{err: errors.New("credential store unreachable")})

	ctx := context.Background()
	user, err := store.CreateUser(ctx, tenantID, CreateUserOptions{
		Email:    "alice@example.com",
		Username: "alice",
	})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	if dErr := store.DeleteUser(ctx, tenantID, user.ID); dErr == nil {
		t.Fatal("a failed credential cascade must fail the user deletion closed")
	}
	if _, gErr := store.GetUser(ctx, tenantID, user.ID); gErr != nil {
		t.Errorf("the user must survive a failed cascade, got %v", gErr)
	}
}

// TestIdentityStoreDeleteUser_NoReaperIsNotAnError keeps WebAuthn optional: a
// deployment without it deletes users exactly as it did before.
func TestIdentityStoreDeleteUser_NoReaperIsNotAnError(t *testing.T) {
	store, tenantID := newCascadeIdentityStore(t)

	ctx := context.Background()
	user, err := store.CreateUser(ctx, tenantID, CreateUserOptions{
		Email:    "alice@example.com",
		Username: "alice",
	})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if dErr := store.DeleteUser(ctx, tenantID, user.ID); dErr != nil {
		t.Fatalf("DeleteUser without a reaper: %v", dErr)
	}
}

// =============================================================================
// Credential id encoding
// =============================================================================

// TestDecodeWebAuthnCredentialID covers the operator-supplied handle: it is
// base64url as the browser reports it, and a value that is not must be rejected
// as a bad request rather than silently addressing some other credential.
func TestDecodeWebAuthnCredentialID(t *testing.T) {
	raw := []byte{0xfb, 0xff, 0x01, 0x02}
	unpadded := base64.RawURLEncoding.EncodeToString(raw)
	padded := base64.URLEncoding.EncodeToString(raw)

	for _, encoded := range []string{unpadded, padded} {
		got, err := decodeWebAuthnCredentialID(encoded)
		if err != nil {
			t.Fatalf("decode %q: %v", encoded, err)
		}
		if string(got) != string(raw) {
			t.Errorf("decode %q = %x, want %x", encoded, got, raw)
		}
	}

	for _, bad := range []string{"", "not base64!!"} {
		if _, err := decodeWebAuthnCredentialID(bad); err == nil {
			t.Errorf("decode %q must fail", bad)
		} else {
			var typed *WebAuthnCredentialIDError
			if !errors.As(err, &typed) {
				t.Errorf("decode %q error = %T, want *WebAuthnCredentialIDError", bad, err)
			}
		}
	}
}

// =============================================================================
// Credential summary projection
// =============================================================================

// TestSummarizeWebAuthnCredential pins what the admin surface reveals: enough
// to recognise a key, and no key material. A credential's public key leaking
// into an operator listing would put it in logs and terminal scrollback.
func TestSummarizeWebAuthnCredential(t *testing.T) {
	cred := testCredential(42, []byte{0x01, 0x02, 0x03})
	cred.Authenticator.SignCount = 17
	cred.Authenticator.CloneWarning = true
	cred.Flags.BackupEligible = true
	cred.Flags.BackupState = true
	cred.Transport = []protocol.AuthenticatorTransport{protocol.USB, protocol.NFC}

	got := summarizeWebAuthnCredential(cred)

	if want := base64.RawURLEncoding.EncodeToString(cred.ID); got.ID != want {
		t.Errorf("ID = %q, want the base64url credential id %q", got.ID, want)
	}
	if got.AAGUID != "aabbccdd" {
		t.Errorf("AAGUID = %q, want the hex-encoded authenticator id", got.AAGUID)
	}
	if got.SignCount != 17 || !got.CloneWarning || !got.BackupEligible || !got.BackupState {
		t.Errorf("authenticator context not projected: %+v", got)
	}
	if len(got.Transports) != 2 || got.Transports[0] != "usb" || got.Transports[1] != "nfc" {
		t.Errorf("Transports = %v, want [usb nfc]", got.Transports)
	}

	// The summary carries no field that could hold key material: the type is
	// the contract, so this asserts on the marshalled shape.
	blob, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, forbidden := range []string{"public_key", "publicKey", "cose"} {
		if strings.Contains(string(blob), forbidden) {
			t.Errorf("credential summary must not carry %q: %s", forbidden, blob)
		}
	}
}

// TestManagerCredentialVerbs_RejectEmptyEmail guards the lookup key. Every
// credential verb is keyed by email; an empty one would resolve nothing useful
// and must be refused before any store dispatch rather than silently operating
// on whatever an empty lookup returns.
func TestManagerCredentialVerbs_RejectEmptyEmail(t *testing.T) {
	factory, err := NewWebAuthnServiceFactory(WebAuthnFactoryParams{
		Config: WebAuthnFactoryConfig{
			RPID:          "example.com",
			RPDisplayName: "Example",
			RPOrigins:     []string{"https://example.com"},
		},
		QRDB:                dummyQRDBClient,
		AttestationRecorder: recorderOf(&fakeUserAuthFactorClient{}),
	})
	if err != nil {
		t.Fatalf("NewWebAuthnServiceFactory: %v", err)
	}
	m, err := NewXKMSWebAuthnManager(factory)
	if err != nil {
		t.Fatalf("NewXKMSWebAuthnManager: %v", err)
	}

	ctx := context.Background()
	if _, lErr := m.ListCredentials(ctx, "tenant-1", ""); lErr == nil {
		t.Error("ListCredentials must reject an empty email")
	}
	if dErr := m.DeleteCredential(ctx, "tenant-1", "", "AQID"); dErr == nil {
		t.Error("DeleteCredential must reject an empty email")
	}
	if _, dErr := m.DeleteCredentials(ctx, "tenant-1", ""); dErr == nil {
		t.Error("DeleteCredentials must reject an empty email")
	}
}

// TestWriteThroughDelete_FailsClosedOnUnreadableStore covers the other read
// failure: a store that errors for a reason other than not-found cannot say
// which factor to revoke, so deleting anyway would strand the factor. Only an
// honest not-found may fall through to a plain delete.
func TestWriteThroughDelete_FailsClosedOnUnreadableStore(t *testing.T) {
	readErr := errors.New("kv unavailable")
	factors := &reapableFactorClient{}
	users := xkmswebauthn.NewMemoryUserStore()
	inner := &unreadableCredentialStore{err: readErr}
	wt, err := newWebAuthnFactorWriteThrough(inner, users, factors, recorderOf(factors), testWriteThroughTenant, permissiveTestGate(), nil)
	if err != nil {
		t.Fatalf("newWebAuthnFactorWriteThrough: %v", err)
	}

	dErr := wt.Delete(context.Background(), []byte{1, 2, 3})
	if dErr == nil {
		t.Fatal("an unreadable credential store must fail the deletion closed")
	}
	if !errors.Is(dErr, readErr) {
		t.Errorf("error must wrap the read failure, got %v", dErr)
	}
	if inner.deleted {
		t.Error("the credential must not be deleted when its factor cannot be identified")
	}
}

// unreadableCredentialStore fails every read with a non-not-found error and
// records whether a delete was attempted anyway.
type unreadableCredentialStore struct {
	err     error
	deleted bool
}

func (s *unreadableCredentialStore) Save(context.Context, *xkmswebauthn.Credential) error {
	return s.err
}

func (s *unreadableCredentialStore) GetByUserID(context.Context, []byte) ([]*xkmswebauthn.Credential, error) {
	return nil, s.err
}

func (s *unreadableCredentialStore) GetByCredentialID(context.Context, []byte) (*xkmswebauthn.Credential, error) {
	return nil, s.err
}

func (s *unreadableCredentialStore) Update(context.Context, *xkmswebauthn.Credential) error {
	return s.err
}

func (s *unreadableCredentialStore) Delete(context.Context, []byte) error {
	s.deleted = true
	return nil
}

func (s *unreadableCredentialStore) DeleteByUserID(context.Context, []byte) error {
	return s.err
}

// =============================================================================
// Email change — the credentials come along
// =============================================================================

// TestIdentityStoreUpdateUser_KeepsCredentialsOnEmailChange is the
// go-quicbaas#582 regression guard, and it is the exact inverse of what this
// test asserted before: a rename must NOT reap anything.
//
// The revocation it replaces was introduced on the belief that the credential
// store is keyed by (tenant, email) and would strand a renamed user's passkeys.
// It is not — xkms's qrdb-backed adapter files them under the qrdb user id, and
// webauthn_rename_ownership_test.go measures that against the adapter itself —
// so the revocation destroyed working credentials to prevent an orphan that
// cannot occur. On the burn-in node it cost the tenant user their only passkey
// and returned 200.
func TestIdentityStoreUpdateUser_KeepsCredentialsOnEmailChange(t *testing.T) {
	store, tenantID := newCascadeIdentityStore(t)
	reaper := &recordingReaper{count: 1}
	store.SetWebAuthnCredentialReaper(reaper)
	ctx := context.Background()

	user, err := store.CreateUser(ctx, tenantID, CreateUserOptions{
		Email:    "old@example.com",
		Username: "alice",
	})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	newEmail := "new@example.com"
	updated, err := store.UpdateUser(ctx, tenantID, user.ID, UpdateUserOptions{Email: &newEmail})
	if err != nil {
		t.Fatalf("UpdateUser: %v", err)
	}
	if updated.Email != newEmail {
		t.Errorf("email = %q, want %q", updated.Email, newEmail)
	}

	if len(reaper.calls) != 0 {
		t.Errorf("a rename must revoke nothing, got %v", reaper.calls)
	}
}

// TestIdentityStoreUpdateUser_LogsTheCredentialsARenameCarried is the other
// half of #582: the rename was not merely destructive, it was SILENT. An update
// that changes the address a user answers to now says what became of that
// user's enrolled credentials, with the count on both sides of the write.
func TestIdentityStoreUpdateUser_LogsTheCredentialsARenameCarried(t *testing.T) {
	var buf bytes.Buffer
	store, tenantID := newLoggingCascadeIdentityStore(t, &buf)
	reaper := &recordingReaper{listed: map[string]int{
		tenantID + "/old@example.com": 2,
		tenantID + "/new@example.com": 2,
	}}
	store.SetWebAuthnCredentialReaper(reaper)
	ctx := context.Background()

	user, err := store.CreateUser(ctx, tenantID, CreateUserOptions{
		Email:    "old@example.com",
		Username: "alice",
	})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	newEmail := "new@example.com"
	if _, uErr := store.UpdateUser(ctx, tenantID, user.ID, UpdateUserOptions{Email: &newEmail}); uErr != nil {
		t.Fatalf("UpdateUser: %v", uErr)
	}

	record := findLogRecord(t, &buf, "carried to the new address")
	if record["level"] != "INFO" {
		t.Errorf("carry record level = %v, want INFO", record["level"])
	}
	if record["webauthn_credentials"] != float64(2) {
		t.Errorf("carry record reported %v credentials, want 2", record["webauthn_credentials"])
	}
	if record["previous_email"] != "old@example.com" || record["email"] != newEmail {
		t.Errorf("carry record named %v -> %v, want old@example.com -> %s",
			record["previous_email"], record["email"], newEmail)
	}
}

// TestIdentityStoreUpdateUser_LogsACredentialCountThatDidNotCarry pins the
// failure disposition. If the tally on the far side of the rename disagrees
// with the near side, that is the #582 symptom recurring, and it must be loud
// rather than inferred later from a user's complaint. The update itself still
// stands: the record is already committed by then, so reporting an error would
// misrepresent a completed write.
func TestIdentityStoreUpdateUser_LogsACredentialCountThatDidNotCarry(t *testing.T) {
	var buf bytes.Buffer
	store, tenantID := newLoggingCascadeIdentityStore(t, &buf)
	reaper := &recordingReaper{listed: map[string]int{
		tenantID + "/old@example.com": 2,
		tenantID + "/new@example.com": 0,
	}}
	store.SetWebAuthnCredentialReaper(reaper)
	ctx := context.Background()

	user, err := store.CreateUser(ctx, tenantID, CreateUserOptions{
		Email:    "old@example.com",
		Username: "alice",
	})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	newEmail := "new@example.com"
	if _, uErr := store.UpdateUser(ctx, tenantID, user.ID, UpdateUserOptions{Email: &newEmail}); uErr != nil {
		t.Fatalf("a logged discrepancy must not fail the committed update: %v", uErr)
	}

	record := findLogRecord(t, &buf, "credential count changed with it")
	if record["level"] != "ERROR" {
		t.Errorf("discrepancy record level = %v, want ERROR", record["level"])
	}
	if record["webauthn_credentials_before"] != float64(2) || record["webauthn_credentials_after"] != float64(0) {
		t.Errorf("discrepancy record reported %v -> %v, want 2 -> 0",
			record["webauthn_credentials_before"], record["webauthn_credentials_after"])
	}
}

// TestIdentityStoreUpdateUser_LogsAnUnknownCredentialCount covers the honest
// third state: a credential store that cannot be read yields no tally, and the
// rename says the ownership was not re-keyed rather than claiming zero. "Had
// none" and "nobody looked" are different facts.
func TestIdentityStoreUpdateUser_LogsAnUnknownCredentialCount(t *testing.T) {
	var buf bytes.Buffer
	store, tenantID := newLoggingCascadeIdentityStore(t, &buf)
	store.SetWebAuthnCredentialReaper(&recordingReaper{listErr: errors.New("credential store unreachable")})
	ctx := context.Background()

	user, err := store.CreateUser(ctx, tenantID, CreateUserOptions{
		Email:    "old@example.com",
		Username: "alice",
	})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	newEmail := "new@example.com"
	if _, uErr := store.UpdateUser(ctx, tenantID, user.ID, UpdateUserOptions{Email: &newEmail}); uErr != nil {
		t.Fatalf("an unreadable credential store must not fail the rename: %v", uErr)
	}

	record := findLogRecord(t, &buf, "were not re-keyed")
	if record["level"] != "INFO" {
		t.Errorf("unknown-count record level = %v, want INFO", record["level"])
	}
	if _, reported := record["webauthn_credentials"]; reported {
		t.Error("an unknown tally must not be reported as a number")
	}
	if _, warned := findOptionalLogRecord(&buf, "could not be counted"); !warned {
		t.Error("the unreadable credential store must be reported")
	}
}

// newLoggingCascadeIdentityStore builds the cascade identity store with its log
// captured as JSON, so a test can assert on the record rather than on a string.
func newLoggingCascadeIdentityStore(t *testing.T, buf *bytes.Buffer) (*IdentityStore, string) {
	t.Helper()
	store := NewIdentityStore(IdentityStoreOptions{
		UserStorage: newMockUserStorage(),
		RoleStorage: newMockRoleStorage(),
		Tenants:     &mockIdentityTenantManager{},
		Logger:      slog.New(slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})),
	})
	return store, "tenant1"
}

// findLogRecord returns the one captured record whose message contains needle,
// failing the test when none does.
func findLogRecord(t *testing.T, buf *bytes.Buffer, needle string) map[string]any {
	t.Helper()
	record, found := findOptionalLogRecord(buf, needle)
	if !found {
		t.Fatalf("no log record mentions %q; captured:\n%s", needle, buf.String())
	}
	return record
}

// findOptionalLogRecord is findLogRecord without the assertion, for the cases
// that test whether a record was emitted at all.
func findOptionalLogRecord(buf *bytes.Buffer, needle string) (map[string]any, bool) {
	for _, line := range strings.Split(buf.String(), "\n") {
		if line == "" {
			continue
		}
		record := map[string]any{}
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			continue
		}
		msg, _ := record["msg"].(string)
		if strings.Contains(msg, needle) {
			return record, true
		}
	}
	return nil, false
}

// TestIdentityStoreUpdateUser_NoRevocationWithoutEmailChange keeps every other
// update cheap and non-destructive: a name, role or state change must not cost
// the user a passkey.
func TestIdentityStoreUpdateUser_NoRevocationWithoutEmailChange(t *testing.T) {
	store, tenantID := newCascadeIdentityStore(t)
	reaper := &recordingReaper{}
	store.SetWebAuthnCredentialReaper(reaper)
	ctx := context.Background()

	user, err := store.CreateUser(ctx, tenantID, CreateUserOptions{
		Email:    "alice@example.com",
		Username: "alice",
	})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	name := "Alice Smith"
	if _, uErr := store.UpdateUser(ctx, tenantID, user.ID, UpdateUserOptions{Name: &name}); uErr != nil {
		t.Fatalf("UpdateUser: %v", uErr)
	}
	// Re-submitting the SAME email is not a change either.
	same := "alice@example.com"
	if _, uErr := store.UpdateUser(ctx, tenantID, user.ID, UpdateUserOptions{Email: &same}); uErr != nil {
		t.Fatalf("UpdateUser: %v", uErr)
	}

	if len(reaper.calls) != 0 {
		t.Errorf("no email change must revoke nothing, got %v", reaper.calls)
	}
}

// TestIdentityStoreUpdateUser_UnreadableCredentialStoreStillRenames pins the
// disposition the WebAuthn stack now has over an identity edit: none.
//
// The rename used to fail CLOSED on an unreachable credential store, because it
// was about to destroy that store's contents and had to know it could. It no
// longer touches them, so a store it cannot read is a gap in the LOG and
// nothing more — letting it veto the rename would be keeping the coupling
// go-quicbaas#582 removed while losing the behaviour that justified it.
func TestIdentityStoreUpdateUser_UnreadableCredentialStoreStillRenames(t *testing.T) {
	store, tenantID := newCascadeIdentityStore(t)
	store.SetWebAuthnCredentialReaper(&recordingReaper{listErr: errors.New("credential store unreachable")})
	ctx := context.Background()

	user, err := store.CreateUser(ctx, tenantID, CreateUserOptions{
		Email:    "old@example.com",
		Username: "alice",
	})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	newEmail := "new@example.com"
	if _, uErr := store.UpdateUser(ctx, tenantID, user.ID, UpdateUserOptions{Email: &newEmail}); uErr != nil {
		t.Fatalf("an unreadable credential store must not fail the rename: %v", uErr)
	}

	got, err := store.GetUser(ctx, tenantID, user.ID)
	if err != nil {
		t.Fatalf("GetUser: %v", err)
	}
	if got.Email != newEmail {
		t.Errorf("email = %q, want %q", got.Email, newEmail)
	}
}

// =============================================================================
// GDPR erasure
// =============================================================================

// newGDPRTestManagerWithReaper builds a GDPR manager over the in-memory stores
// with a credential reaper wired, mirroring createTestGDPRManager but supplying
// the dependency the erasure path needs.
func newGDPRTestManagerWithReaper(t *testing.T, reaper WebAuthnCredentialReaper) *GDPRManager {
	t.Helper()
	mem := NewMemoryStorage()
	mgr, err := NewGDPRManager(GDPRManagerDependencies{
		ConsentStorage:      NewMemoryConsentStorage(),
		DataExportStorage:   NewMemoryDataExportStorage(),
		DeletionStorage:     NewMemoryDeletionStorage(),
		UserStorage:         mem,
		SessionStorage:      NewMemorySessionStorage(),
		WebAuthnCredentials: reaper,
	})
	if err != nil {
		t.Fatalf("NewGDPRManager: %v", err)
	}
	return mgr
}

// newDueDeletionRequest files a deletion request that this test will process
// itself, with nothing racing it.
//
// RequestDeletion(gracePeriodDays=0) spawns a background processor, so a test
// that then calls ProcessDeletion has two goroutines contending for the same
// row and the outcome depends on which wins. Filing with a grace period takes
// that branch out entirely, and moving GracePeriodEndsAt into the past then
// satisfies the grace check for the manual call.
func newDueDeletionRequest(t *testing.T, mgr *GDPRManager, tenantID string, userID uint64,
	deleteCategories, preserveCategories []string) *DataDeletionRequest {
	t.Helper()
	ctx := context.Background()

	request, err := mgr.RequestDeletion(ctx, tenantID, userID, userID, "",
		deleteCategories, preserveCategories, 1)
	if err != nil {
		t.Fatalf("RequestDeletion: %v", err)
	}
	stored, err := mgr.deps.DeletionStorage.GetDeletionRequest(ctx, request.ID)
	if err != nil {
		t.Fatalf("GetDeletionRequest: %v", err)
	}
	past := time.Now().Add(-time.Hour)
	stored.GracePeriodEndsAt = &past
	if uErr := mgr.deps.DeletionStorage.UpdateDeletionRequest(ctx, stored); uErr != nil {
		t.Fatalf("UpdateDeletionRequest: %v", uErr)
	}
	return stored
}

// TestGDPRErasure_DeletesWebAuthnCredentials is the hook the erasure path's
// own note was waiting on. A stored credential is personal data, and an
// erasure that leaves it behind has not done what it reported doing — it also
// leaves the address able to block whoever is issued it next.
func TestGDPRErasure_DeletesWebAuthnCredentials(t *testing.T) {
	reaper := &recordingReaper{count: 2}
	mgr := newGDPRTestManagerWithReaper(t, reaper)
	ctx := context.Background()

	const tenantID, userID = "tenant1", 11
	createTestUser(t, mgr.deps.UserStorage, tenantID, userID)

	request := newDueDeletionRequest(t, mgr, tenantID, userID, nil, nil)
	summary, err := mgr.ProcessDeletion(ctx, request.ID)
	if err != nil {
		t.Fatalf("ProcessDeletion: %v", err)
	}

	if len(reaper.calls) != 1 {
		t.Fatalf("erasure must revoke credentials exactly once, got %d calls", len(reaper.calls))
	}
	// Keyed by the subject's email, resolved BEFORE the profile is deleted —
	// afterwards the address is gone and the credential cannot be named.
	if want := tenantID + "/11@example.com"; reaper.calls[0] != want {
		t.Errorf("revocation called with %q, want %q", reaper.calls[0], want)
	}
	if summary.CredentialsDeleted != 2 {
		t.Errorf("CredentialsDeleted = %d, want 2 reported to the subject", summary.CredentialsDeleted)
	}
	if !summary.ProfileDeleted {
		t.Error("the profile must still be deleted")
	}
}

// TestGDPRErasure_FailsClosedOnCredentialFailure keeps a partial erasure from
// reporting success. The profile must survive a failed credential revocation,
// because once it is gone the email is gone and nothing can name the
// credential that was left behind.
func TestGDPRErasure_FailsClosedOnCredentialFailure(t *testing.T) {
	mgr := newGDPRTestManagerWithReaper(t, &recordingReaper{err: errors.New("credential store unreachable")})
	ctx := context.Background()

	const tenantID, userID = "tenant1", 11
	createTestUser(t, mgr.deps.UserStorage, tenantID, userID)

	request := newDueDeletionRequest(t, mgr, tenantID, userID, nil, nil)
	if _, pErr := mgr.ProcessDeletion(ctx, request.ID); pErr == nil {
		t.Fatal("a failed credential revocation must fail the erasure closed")
	}

	if _, gErr := mgr.deps.UserStorage.GetUser(ctx, tenantID, userID); gErr != nil {
		t.Errorf("the profile must survive a failed erasure, got %v", gErr)
	}
}

// TestIdentityStoreUpdateUser_RevocationPrecedesPersistence documents the one
// window the ordering does NOT close, so nobody later reads the fail-closed
// wording as "nothing happened".
//
// If the revocation succeeds and the storage write then fails, the user keeps
// the old email and has lost the passkey. That is a real cost, and it is the
// deliberate trade: the alternative ordering swaps a re-registration for a
// credential nobody can reach and nobody can delete, which is the failure
// #273 was opened for. Closing the window needs an atomic rename-plus-revoke,
// which needs the single identity key of #289.
func TestIdentityStoreUpdateUser_RevocationPrecedesPersistence(t *testing.T) {
	store, tenantID := newCascadeIdentityStore(t)
	reaper := &recordingReaper{count: 1}
	store.SetWebAuthnCredentialReaper(reaper)
	ctx := context.Background()

	user, err := store.CreateUser(ctx, tenantID, CreateUserOptions{
		Email:    "old@example.com",
		Username: "alice",
	})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	// A second user already holds the target address, so the rename is
	// rejected — by the uniqueness check, which runs BEFORE the revocation.
	if _, cErr := store.CreateUser(ctx, tenantID, CreateUserOptions{
		Email:    "taken@example.com",
		Username: "bob",
	}); cErr != nil {
		t.Fatalf("CreateUser bob: %v", cErr)
	}

	taken := "taken@example.com"
	if _, uErr := store.UpdateUser(ctx, tenantID, user.ID, UpdateUserOptions{Email: &taken}); uErr == nil {
		t.Fatal("renaming onto a taken address must be rejected")
	}
	if len(reaper.calls) != 0 {
		t.Errorf("a rename rejected before the revocation must not burn a passkey, got %v", reaper.calls)
	}
}

// =============================================================================
// Tenant teardown
// =============================================================================

// TestDeleteTenant_RevokesWebAuthnCredentials guards the third path a
// credential can outlive its owner. Tenant teardown deprovisions users through
// the user store directly rather than through IdentityStore.DeleteUser, so the
// cascade wired there does not run: without this the tenant's credentials
// survive the tenant, filed under (tenant, email) keys whose users are gone.
func TestDeleteTenant_RevokesWebAuthnCredentials(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStorage()
	reaper := &recordingReaper{count: 1}

	mgr := NewDefaultTenantManager(store)
	mgr.SetUserStorage(store)
	mgr.SetWebAuthnCredentialReaper(reaper)

	const tenantID = "teardown-test"
	if _, err := mgr.CreateTenant(ctx, CreateTenantOptions{ID: tenantID, Name: "Teardown Test"}); err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	ids := seedTenantUsers(t, store, tenantID, 3)

	if err := mgr.DeleteTenant(ctx, tenantID); err != nil {
		t.Fatalf("DeleteTenant: %v", err)
	}

	if len(reaper.calls) != len(ids) {
		t.Fatalf("revocation ran %d times, want once per user (%d)", len(reaper.calls), len(ids))
	}
	// Each call must name that user's own address within the tenant.
	seen := map[string]bool{}
	for _, call := range reaper.calls {
		seen[call] = true
	}
	for _, id := range ids {
		want := fmt.Sprintf("%s/%d@%s.local", tenantID, id, tenantID)
		if !seen[want] {
			t.Errorf("no revocation for %q", want)
		}
	}
}

// TestDeleteTenant_FailsClosedOnCredentialRevocationFailure keeps tenant
// teardown from reporting success over a credential it could not remove. The
// tenant survives, so the operator can retry rather than hunt for records
// whose owning tenant no longer exists.
func TestDeleteTenant_FailsClosedOnCredentialRevocationFailure(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStorage()

	mgr := NewDefaultTenantManager(store)
	mgr.SetUserStorage(store)
	mgr.SetWebAuthnCredentialReaper(&recordingReaper{err: errors.New("credential store unreachable")})

	const tenantID = "teardown-fail"
	if _, err := mgr.CreateTenant(ctx, CreateTenantOptions{ID: tenantID, Name: "Teardown Fail"}); err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	seedTenantUsers(t, store, tenantID, 2)

	if err := mgr.DeleteTenant(ctx, tenantID); err == nil {
		t.Fatal("a failed credential revocation must abort the tenant delete")
	}
	if _, err := mgr.GetTenant(ctx, tenantID); err != nil {
		t.Errorf("the tenant must survive a failed teardown, got %v", err)
	}
}

// TestGDPRErasure_RejectsPreserveCredentialsWithProfileDeletion is the
// regression for the one way this hook could silently destroy data the caller
// explicitly asked to keep.
//
// PreserveCategories is a legal instruction. Deleting the profile while
// preserving credentials cannot be carried out as asked — the credential would
// survive under an address that no longer resolves, unreachable by every
// surface — so the request is refused at request time rather than resolved
// silently in either direction.
func TestGDPRErasure_RejectsPreserveCredentialsWithProfileDeletion(t *testing.T) {
	reaper := &recordingReaper{count: 1}
	mgr := newGDPRTestManagerWithReaper(t, reaper)
	ctx := context.Background()

	const tenantID, userID = "tenant1", 11
	createTestUser(t, mgr.deps.UserStorage, tenantID, userID)

	_, err := mgr.RequestDeletion(ctx, tenantID, userID, userID, "",
		[]string{"profile"}, []string{"credentials"}, 0)
	if err == nil {
		t.Fatal("deleting the profile while preserving credentials must be refused, not resolved silently")
	}
	if len(reaper.calls) != 0 {
		t.Errorf("a refused request must not touch credentials, got %v", reaper.calls)
	}
}

// TestGDPRErasure_PreservedCredentialsSurviveWhenProfileIsKept proves the
// preserve is authoritative wherever it CAN be honoured: retaining both the
// profile and the credentials is coherent, so the erasure runs and the
// credentials are left alone.
func TestGDPRErasure_PreservedCredentialsSurviveWhenProfileIsKept(t *testing.T) {
	reaper := &recordingReaper{count: 1}
	mgr := newGDPRTestManagerWithReaper(t, reaper)
	ctx := context.Background()

	const tenantID, userID = "tenant1", 11
	createTestUser(t, mgr.deps.UserStorage, tenantID, userID)

	request := newDueDeletionRequest(t, mgr, tenantID, userID,
		[]string{"sessions"}, []string{"credentials"})
	summary, err := mgr.ProcessDeletion(ctx, request.ID)
	if err != nil {
		t.Fatalf("ProcessDeletion: %v", err)
	}

	if len(reaper.calls) != 0 {
		t.Errorf("preserved credentials must be left alone, got %v", reaper.calls)
	}
	if summary.CredentialsDeleted != 0 {
		t.Errorf("CredentialsDeleted = %d, want 0", summary.CredentialsDeleted)
	}
	if summary.ProfileDeleted {
		t.Error("the profile was not selected for deletion and must survive")
	}
}

// TestGDPRErasure_OmittedCredentialsGoWithTheProfile pins the other half of the
// rule: a delete list that names the profile but never mentions credentials has
// not asked to retain them. Erasing an identity carries its authenticators
// with it, so they go too rather than being stranded.
func TestGDPRErasure_OmittedCredentialsGoWithTheProfile(t *testing.T) {
	reaper := &recordingReaper{count: 2}
	mgr := newGDPRTestManagerWithReaper(t, reaper)
	ctx := context.Background()

	const tenantID, userID = "tenant1", 11
	createTestUser(t, mgr.deps.UserStorage, tenantID, userID)

	request := newDueDeletionRequest(t, mgr, tenantID, userID,
		[]string{"profile", "sessions"}, nil)
	summary, err := mgr.ProcessDeletion(ctx, request.ID)
	if err != nil {
		t.Fatalf("ProcessDeletion: %v", err)
	}

	if len(reaper.calls) != 1 {
		t.Fatalf("credentials must go with the profile, revocations = %d", len(reaper.calls))
	}
	if summary.CredentialsDeleted != 2 {
		t.Errorf("CredentialsDeleted = %d, want 2", summary.CredentialsDeleted)
	}
}

// TestGDPRErasure_StoredConflictFailsClosed covers a request persisted before
// the conflict check existed. It must fail the erasure rather than orphan a
// credential: a stuck request an operator can amend is recoverable, and the
// orphan is not.
func TestGDPRErasure_StoredConflictFailsClosed(t *testing.T) {
	reaper := &recordingReaper{count: 1}
	mgr := newGDPRTestManagerWithReaper(t, reaper)
	ctx := context.Background()

	const tenantID, userID = "tenant1", 11
	createTestUser(t, mgr.deps.UserStorage, tenantID, userID)

	// File a coherent request, then mutate the stored record into the shape
	// the validation now refuses, as a pre-existing row would be.
	stored := newDueDeletionRequest(t, mgr, tenantID, userID, []string{"profile"}, nil)
	request := stored
	stored.PreserveCategories = []string{"credentials"}
	if uErr := mgr.deps.DeletionStorage.UpdateDeletionRequest(ctx, stored); uErr != nil {
		t.Fatalf("UpdateDeletionRequest: %v", uErr)
	}

	if _, pErr := mgr.ProcessDeletion(ctx, request.ID); pErr == nil {
		t.Fatal("a stored conflicting request must fail the erasure closed")
	}
	if len(reaper.calls) != 0 {
		t.Errorf("a refused erasure must not touch credentials, got %v", reaper.calls)
	}
	if _, gErr := mgr.deps.UserStorage.GetUser(ctx, tenantID, userID); gErr != nil {
		t.Errorf("the profile must survive a refused erasure, got %v", gErr)
	}
}

// TestGDPRErasure_ConflictOnlyWhereCredentialsCanExist keeps the refusal from
// inventing a failure on a node that has no WebAuthn at all.
//
// The conflict is real only where there is a credential store to strand
// something in. With no reaper wired, preserving "credentials" while deleting
// "profile" preserves nothing, and the request is coherent — refusing it would
// break a deployment that never had a passkey to lose.
func TestGDPRErasure_ConflictOnlyWhereCredentialsCanExist(t *testing.T) {
	mgr := newGDPRTestManagerWithReaper(t, nil)
	ctx := context.Background()

	const tenantID, userID = "tenant1", 11
	createTestUser(t, mgr.deps.UserStorage, tenantID, userID)

	if _, err := mgr.RequestDeletion(ctx, tenantID, userID, userID, "",
		[]string{"profile"}, []string{"credentials"}, 1); err != nil {
		t.Fatalf("without a credential store the request preserves nothing and must be accepted: %v", err)
	}
}

// =============================================================================
// Absence is a positive answer, never an inference from an error
// =============================================================================

// flakyReadCredentialStore holds real credentials but can be told to fail
// reads with a storage error — the state a momentarily unreadable store is in,
// as distinct from one that genuinely holds nothing.
type flakyReadCredentialStore struct {
	creds   map[string]*xkmswebauthn.Credential
	readErr error
	deletes int
	saves   int
}

func newFlakyReadCredentialStore() *flakyReadCredentialStore {
	return &flakyReadCredentialStore{creds: map[string]*xkmswebauthn.Credential{}}
}

func (s *flakyReadCredentialStore) key(credID []byte) string {
	return base64.RawURLEncoding.EncodeToString(credID)
}

func (s *flakyReadCredentialStore) Save(_ context.Context, cred *xkmswebauthn.Credential) error {
	s.saves++
	s.creds[s.key(cred.ID)] = cred
	return nil
}

func (s *flakyReadCredentialStore) GetByCredentialID(_ context.Context, credID []byte) (*xkmswebauthn.Credential, error) {
	if s.readErr != nil {
		return nil, s.readErr
	}
	cred, ok := s.creds[s.key(credID)]
	if !ok {
		return nil, xkmswebauthn.ErrCredentialNotFound
	}
	return cred, nil
}

func (s *flakyReadCredentialStore) GetByUserID(context.Context, []byte) ([]*xkmswebauthn.Credential, error) {
	return nil, errors.New("GetByUserID not expected")
}

func (s *flakyReadCredentialStore) Update(context.Context, *xkmswebauthn.Credential) error {
	return errors.New("Update not expected")
}

func (s *flakyReadCredentialStore) Delete(_ context.Context, credID []byte) error {
	s.deletes++
	delete(s.creds, s.key(credID))
	return nil
}

func (s *flakyReadCredentialStore) DeleteByUserID(context.Context, []byte) error {
	return errors.New("DeleteByUserID not expected")
}

// TestWriteThroughSave_UnreadableStoreDoesNotDestroyExistingCredential is the
// regression for a data-loss path: the pre-write snapshot used to treat EVERY
// lookup error as proof the credential did not exist, so a momentarily
// unreadable store made the compensation delete a real, working passkey during
// a failed re-registration.
//
// Absence must be the store's own not-found, never inferred from an error. Any
// other read failure aborts before the Save, so no compensation decision is
// ever taken on a snapshot that could be wrong.
func TestWriteThroughSave_UnreadableStoreDoesNotDestroyExistingCredential(t *testing.T) {
	ctx := context.Background()
	inner := newFlakyReadCredentialStore()
	users := xkmswebauthn.NewMemoryUserStore()

	// The factor enrollment fails, so a Save that got past the snapshot would
	// reach the compensation path.
	factors := &fakeUserAuthFactorClient{addErr: errors.New("qrdb unavailable")}
	wt, err := newWebAuthnFactorWriteThrough(inner, users, factors, recorderOf(factors), testWriteThroughTenant, permissiveTestGate(), nil)
	if err != nil {
		t.Fatalf("newWebAuthnFactorWriteThrough: %v", err)
	}
	seedUser(t, users, 42, "alice@example.com", "alice's yubikey")

	// A credential that genuinely exists and works. Its public key is the
	// marker that proves the stored record is still the ORIGINAL one: a retry
	// that reached Save would overwrite it with the replacement below.
	credID := []byte{1, 2, 3, 4}
	existing := testCredential(42, credID)
	existing.PublicKey = []byte("original-cose-key")
	if sErr := inner.Save(ctx, existing); sErr != nil {
		t.Fatalf("seed credential: %v", sErr)
	}
	seededSaves := inner.saves

	// The store goes unreadable, and the same authenticator re-registers with
	// an observably different record.
	readErr := errors.New("kv temporarily unavailable")
	inner.readErr = readErr
	replacement := testCredential(42, credID)
	replacement.PublicKey = []byte("replacement-cose-key")

	sErr := wt.Save(ctx, replacement)
	if sErr == nil {
		t.Fatal("an unreadable store must fail the registration rather than guess at the snapshot")
	}
	if !errors.Is(sErr, readErr) {
		t.Errorf("error = %v, want the read failure to be unwrappable", sErr)
	}
	// The failure must not masquerade as an enrollment failure: nothing was
	// enrolled, and whether the credential existed is exactly what is unknown.
	var snapshot *WebAuthnCredentialSnapshotError
	if !errors.As(sErr, &snapshot) {
		t.Errorf("error = %T, want *WebAuthnCredentialSnapshotError", sErr)
	}
	var enroll *WebAuthnFactorEnrollError
	if errors.As(sErr, &enroll) {
		t.Error("a pre-write read failure must not be reported as an enrollment failure")
	}

	// The load-bearing assertions. The write must not have happened at all —
	// checking only for a delete would pass an implementation that overwrote
	// the record and merely skipped the compensation.
	if inner.saves != seededSaves {
		t.Errorf("Save reached the store %d extra times; the registration must abort "+
			"BEFORE writing when the snapshot is unknown", inner.saves-seededSaves)
	}
	if inner.deletes != 0 {
		t.Errorf("compensation ran %d times; a credential that may exist must never be deleted "+
			"on an inferred absence", inner.deletes)
	}
	inner.readErr = nil
	got, gErr := inner.GetByCredentialID(ctx, credID)
	if gErr != nil {
		t.Fatalf("the pre-existing credential was destroyed: %v", gErr)
	}
	if string(got.PublicKey) != "original-cose-key" {
		t.Errorf("stored public key = %q, want the original untouched", got.PublicKey)
	}
}

// TestFlakyReadCredentialStore_DistinguishesAbsenceFromFailure checks the test
// DOUBLE, not the code under test.
//
// The fix turns on one distinction: a canonical not-found versus any other
// error. A double that returned the same error for both could not exercise
// that distinction, and every test above it would be theatre — the shape that
// let MemoryInviteStore return a spent invite where the real store errors, so
// the handler tests drilled a branch production never reaches. This asserts
// the double can tell the two apart before anything relies on it.
func TestFlakyReadCredentialStore_DistinguishesAbsenceFromFailure(t *testing.T) {
	ctx := context.Background()
	store := newFlakyReadCredentialStore()

	// Absent: the store's own canonical sentinel, which the fix treats as a
	// positive absence.
	_, err := store.GetByCredentialID(ctx, []byte{1})
	if !errors.Is(err, xkmswebauthn.ErrCredentialNotFound) {
		t.Errorf("missing credential: err = %v, want ErrCredentialNotFound", err)
	}

	// Unreadable: something else entirely, which the fix must NOT read as
	// absence.
	store.readErr = errors.New("kv temporarily unavailable")
	_, err = store.GetByCredentialID(ctx, []byte{1})
	if err == nil {
		t.Fatal("an unreadable store must return an error")
	}
	if errors.Is(err, xkmswebauthn.ErrCredentialNotFound) {
		t.Error("a storage failure must not be reported as a not-found, " +
			"or the double cannot exercise the distinction the fix is about")
	}
}

// TestWriteThroughSave_GenuineNotFoundStillCompensates keeps the fix from
// disabling the compensation it guards: a store that positively reports
// not-found is trustworthy, so a credential this ceremony created is still
// rolled back when the factor enrollment fails.
func TestWriteThroughSave_GenuineNotFoundStillCompensates(t *testing.T) {
	ctx := context.Background()
	inner := newFlakyReadCredentialStore()
	users := xkmswebauthn.NewMemoryUserStore()

	factors := &fakeUserAuthFactorClient{addErr: errors.New("qrdb unavailable")}
	wt, err := newWebAuthnFactorWriteThrough(inner, users, factors, recorderOf(factors), testWriteThroughTenant, permissiveTestGate(), nil)
	if err != nil {
		t.Fatalf("newWebAuthnFactorWriteThrough: %v", err)
	}
	seedUser(t, users, 42, "alice@example.com", "alice's yubikey")

	credID := []byte{9, 8, 7}
	sErr := wt.Save(ctx, testCredential(42, credID))
	if sErr == nil {
		t.Fatal("a failed factor enrollment must fail the registration closed")
	}
	var typed *WebAuthnFactorEnrollError
	if !errors.As(sErr, &typed) {
		t.Fatalf("error = %T, want *WebAuthnFactorEnrollError", sErr)
	}
	if !typed.Compensated {
		t.Error("a credential this ceremony created must still be rolled back")
	}
	if _, gErr := inner.GetByCredentialID(ctx, credID); !errors.Is(gErr, xkmswebauthn.ErrCredentialNotFound) {
		t.Errorf("the credential must be gone, got %v", gErr)
	}
}

// TestWriteThroughSave_SnapshotClassifiesBySentinelNotText varies the INPUT
// space rather than the implementation: mutation testing proves a test detects
// its fix being removed, and says nothing about inputs the test never supplies.
//
// The cases that matter here are the error KINDS a store can return. This layer
// must key on the canonical sentinel and nothing else — in particular an error
// whose text merely reads like an absence is still an error, and must abort the
// registration rather than license the compensation to delete.
//
// That last case also marks a boundary: go-xkms decides not-found by substring
// match on the message (isQRDBNotFound), so upstream a transport failure phrased
// "connection not found" would arrive here ALREADY converted into the sentinel
// and this guard could not tell. That is go-xkms#183, not something this layer
// can defend against; what it can do is never add a second, looser
// classification of its own, which is what this pins.
func TestWriteThroughSave_SnapshotClassifiesBySentinelNotText(t *testing.T) {
	cases := []struct {
		name      string
		readErr   error
		wantAbort bool
	}{
		{
			name:      "canonical not-found is a positive absence",
			readErr:   xkmswebauthn.ErrCredentialNotFound,
			wantAbort: false,
		},
		{
			name:      "wrapped not-found is still a positive absence",
			readErr:   fmt.Errorf("qrdb credential lookup: %w", xkmswebauthn.ErrCredentialNotFound),
			wantAbort: false,
		},
		{
			name:      "a transport failure is not an absence",
			readErr:   errors.New("kv temporarily unavailable"),
			wantAbort: true,
		},
		{
			name:      "an error that merely READS like an absence is not one",
			readErr:   errors.New("connection not found: dial tcp 10.0.0.1:7443"),
			wantAbort: true,
		},
		{
			name:      "a permission failure is not an absence",
			readErr:   errors.New("permission denied"),
			wantAbort: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			inner := newFlakyReadCredentialStore()
			users := xkmswebauthn.NewMemoryUserStore()
			// Enrollment fails, so a Save that got past the snapshot reaches
			// the compensation path.
			factors := &fakeUserAuthFactorClient{addErr: errors.New("qrdb unavailable")}
			wt, err := newWebAuthnFactorWriteThrough(inner, users, factors, recorderOf(factors), testWriteThroughTenant, permissiveTestGate(), nil)
			if err != nil {
				t.Fatalf("newWebAuthnFactorWriteThrough: %v", err)
			}
			seedUser(t, users, 42, "alice@example.com", "alice's yubikey")

			credID := []byte{5, 5, 5}
			inner.readErr = tc.readErr
			saveErr := wt.Save(ctx, testCredential(42, credID))
			if saveErr == nil {
				t.Fatal("the registration must fail: the factor enrollment was rigged to fail")
			}

			var snapshot *WebAuthnCredentialSnapshotError
			aborted := errors.As(saveErr, &snapshot)
			if aborted != tc.wantAbort {
				t.Fatalf("aborted before Save = %t, want %t (err = %v)", aborted, tc.wantAbort, saveErr)
			}
			if tc.wantAbort {
				if inner.saves != 0 {
					t.Errorf("Save reached the store %d times; an unknown snapshot must abort first", inner.saves)
				}
				if inner.deletes != 0 {
					t.Errorf("compensation ran %d times on an unknown snapshot", inner.deletes)
				}
				return
			}
			// A positive absence licenses the write and its rollback.
			if inner.saves == 0 {
				t.Error("a positive absence must let the registration proceed")
			}
			if inner.deletes == 0 {
				t.Error("a credential this ceremony created must still be rolled back")
			}
		})
	}
}
