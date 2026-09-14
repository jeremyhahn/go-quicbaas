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
	"errors"
	"strings"
	"testing"

	xkmswebauthn "github.com/jeremyhahn/go-xkms/pkg/webauthn"
	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"
)

// =============================================================================
// Credential administration round trip (go-quicbaas#574)
//
// The verbs under test are the operator's only way to see and remove a stored
// authenticator. The listing hands out a base64url credential id and the
// per-id delete takes it back; if those two spellings ever diverge, removing
// one stale passkey becomes impossible and the only recovery left is the bulk
// delete, which takes every working passkey with it. These tests pin the
// round trip in both directions.
// =============================================================================

const credentialTestEmail = "alice@example.com"

// newCredentialAdminManager builds the manager over xkms's real in-memory
// stores wrapped in the production write-through decorator, so the ids these
// tests round-trip are produced and consumed by the same code the server runs.
func newCredentialAdminManager(t *testing.T) (*XKMSWebAuthnManager, *webAuthnFactorWriteThrough) {
	t.Helper()

	factors := &reapableFactorClient{}
	users := xkmswebauthn.NewMemoryUserStore()
	creds := xkmswebauthn.NewMemoryCredentialStore()
	wt, err := newWebAuthnFactorWriteThrough(
		creds, users, factors, recorderOf(factors), testWriteThroughTenant, permissiveTestGate(), nil)
	if err != nil {
		t.Fatalf("newWebAuthnFactorWriteThrough: %v", err)
	}
	seedUser(t, users, 42, credentialTestEmail, "alice")

	svc, err := xkmssdk.NewWebAuthnService(xkmssdk.WebAuthnServiceParams{
		Config: &xkmssdk.WebAuthnConfig{
			RPID:          "example.com",
			RPDisplayName: "Example",
			RPOrigins:     []string{"https://example.com"},
		},
		UserStore:       users,
		SessionStore:    xkmssdk.NewWebAuthnMemorySessionStore(),
		CredentialStore: wt,
	})
	if err != nil {
		t.Fatalf("NewWebAuthnService: %v", err)
	}

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
	factory.services.Store(testWriteThroughTenant, svc)

	mgr, err := NewXKMSWebAuthnManager(factory)
	if err != nil {
		t.Fatalf("NewXKMSWebAuthnManager: %v", err)
	}
	return mgr, wt
}

// saveCredential registers a credential of the given raw id under the seeded
// account, through the same write-through the ceremony uses.
func saveCredential(t *testing.T, wt *webAuthnFactorWriteThrough, rawID []byte) {
	t.Helper()
	if err := wt.Save(context.Background(), testCredential(42, rawID)); err != nil {
		t.Fatalf("save credential %x: %v", rawID, err)
	}
}

// listedIDs returns the credential ids the list verb reports, in order.
func listedIDs(t *testing.T, mgr *XKMSWebAuthnManager) []string {
	t.Helper()
	creds, err := mgr.ListCredentials(context.Background(), testWriteThroughTenant, credentialTestEmail)
	if err != nil {
		t.Fatalf("ListCredentials: %v", err)
	}
	ids := make([]string, 0, len(creds))
	for _, cred := range creds {
		ids = append(ids, cred.ID)
	}
	return ids
}

// rawCredentialID is a 32-byte credential id, the length every authenticator
// in the burn-in deployment issues.
func rawCredentialID(seed byte) []byte {
	id := make([]byte, 32)
	for i := range id {
		id[i] = seed + byte(i)
	}
	return id
}

// TestDeleteCredential_RoundTripsEveryListedID is the go-quicbaas#574 guard:
// every id the listing emits must be accepted verbatim by the per-id delete,
// and deleting them all must leave the account empty. A divergence between the
// two spellings is exactly the failure the issue reported.
func TestDeleteCredential_RoundTripsEveryListedID(t *testing.T) {
	mgr, wt := newCredentialAdminManager(t)
	ctx := context.Background()

	for _, seed := range []byte{0x10, 0x40, 0x70} {
		saveCredential(t, wt, rawCredentialID(seed))
	}

	ids := listedIDs(t, mgr)
	if len(ids) != 3 {
		t.Fatalf("listed %d credentials, want 3", len(ids))
	}
	for _, id := range ids {
		if err := mgr.DeleteCredential(ctx, testWriteThroughTenant, credentialTestEmail, id); err != nil {
			t.Fatalf("DeleteCredential(%q) returned %T: %v", id, err, err)
		}
	}
	if remaining := listedIDs(t, mgr); len(remaining) != 0 {
		t.Fatalf("after deleting every listed id, %d remain: %v", len(remaining), remaining)
	}
}

// TestDeleteCredential_AcceptsPaddedSpellingOfAListedID pins the tolerance the
// decode was written for: an id pasted from a tool that pads base64url names
// the same credential as the canonical unpadded listing does.
func TestDeleteCredential_AcceptsPaddedSpellingOfAListedID(t *testing.T) {
	mgr, wt := newCredentialAdminManager(t)

	// 31 bytes so the canonical encoding needs padding to round out.
	rawID := rawCredentialID(0x20)[:31]
	saveCredential(t, wt, rawID)

	padded := base64.URLEncoding.EncodeToString(rawID)
	if !strings.HasSuffix(padded, "=") {
		t.Fatalf("fixture is not padded: %q", padded)
	}
	if err := mgr.DeleteCredential(
		context.Background(), testWriteThroughTenant, credentialTestEmail, padded); err != nil {
		t.Fatalf("DeleteCredential(padded) returned %T: %v", err, err)
	}
	if remaining := listedIDs(t, mgr); len(remaining) != 0 {
		t.Fatalf("padded delete left %d credentials: %v", len(remaining), remaining)
	}
}

// TestDeleteCredential_UnknownIDIsTypedNotFound is the error-handling half: an
// id that is well-formed and owned by nobody must be a
// WebAuthnCredentialNotFoundError naming the account, not a store failure.
func TestDeleteCredential_UnknownIDIsTypedNotFound(t *testing.T) {
	mgr, wt := newCredentialAdminManager(t)
	saveCredential(t, wt, rawCredentialID(0x10))

	unknown := base64.RawURLEncoding.EncodeToString(rawCredentialID(0x90))
	err := mgr.DeleteCredential(
		context.Background(), testWriteThroughTenant, credentialTestEmail, unknown)

	var notFound *WebAuthnCredentialNotFoundError
	if !errors.As(err, &notFound) {
		t.Fatalf("DeleteCredential(unknown) = %T %v, want *WebAuthnCredentialNotFoundError", err, err)
	}
	if notFound.Email != credentialTestEmail || notFound.CredentialID != unknown {
		t.Fatalf("not-found error = %+v, want email %q and id %q",
			notFound, credentialTestEmail, unknown)
	}
	if remaining := listedIDs(t, mgr); len(remaining) != 1 {
		t.Fatalf("a failed delete removed something: %v", remaining)
	}
}

// TestDeleteCredential_MalformedIDIsTypedIDError covers the other bad input:
// a value that is not base64url at all is a 400-class id error, distinct from
// not-found, so an operator can tell a typo from a missing credential.
func TestDeleteCredential_MalformedIDIsTypedIDError(t *testing.T) {
	mgr, wt := newCredentialAdminManager(t)
	saveCredential(t, wt, rawCredentialID(0x10))

	err := mgr.DeleteCredential(
		context.Background(), testWriteThroughTenant, credentialTestEmail, "!!! not base64 !!!")

	var idErr *WebAuthnCredentialIDError
	if !errors.As(err, &idErr) {
		t.Fatalf("DeleteCredential(malformed) = %T %v, want *WebAuthnCredentialIDError", err, err)
	}
	if idErr.Unwrap() == nil {
		t.Error("id error carries no decode cause")
	}
}

// TestDeleteCredential_TruncatedIDNamesTheFullID is the #574 diagnosis. A
// prefix of a real id whose length is a multiple of four decodes cleanly into
// bytes that own nothing; before this guard the answer was a bare not-found,
// indistinguishable from "that credential is already gone". The 24-character
// fragment below is the exact shape the issue reported.
func TestDeleteCredential_TruncatedIDNamesTheFullID(t *testing.T) {
	mgr, wt := newCredentialAdminManager(t)
	rawID := rawCredentialID(0x10)
	saveCredential(t, wt, rawID)

	full := base64.RawURLEncoding.EncodeToString(rawID)
	fragment := full[:24]
	if _, decErr := base64.RawURLEncoding.DecodeString(fragment); decErr != nil {
		t.Fatalf("fixture fragment does not decode, so it is not the reported failure: %v", decErr)
	}

	err := mgr.DeleteCredential(
		context.Background(), testWriteThroughTenant, credentialTestEmail, fragment)

	var truncated *WebAuthnCredentialTruncatedIDError
	if !errors.As(err, &truncated) {
		t.Fatalf("DeleteCredential(fragment) = %T %v, want *WebAuthnCredentialTruncatedIDError", err, err)
	}
	if len(truncated.Matches) != 1 || truncated.Matches[0] != full {
		t.Fatalf("truncation error matches = %v, want [%s]", truncated.Matches, full)
	}
	if !strings.Contains(truncated.Error(), full) {
		t.Errorf("truncation message does not name the full id: %s", truncated.Error())
	}
	var notFound *WebAuthnCredentialNotFoundError
	if errors.As(err, &notFound) {
		t.Error("a truncated id must not also read as a plain not-found")
	}
	if remaining := listedIDs(t, mgr); len(remaining) != 1 {
		t.Fatalf("a rejected delete removed something: %v", remaining)
	}
}

// TestDeleteCredential_TruncatedIDPrefixingSeveralCredentialsReportsThemAll
// covers the ambiguous fragment: a prefix short enough to match more than one
// registered credential names every candidate, because naming one would send
// the operator to an arbitrary credential.
func TestDeleteCredential_TruncatedIDPrefixingSeveralCredentialsReportsThemAll(t *testing.T) {
	mgr, wt := newCredentialAdminManager(t)

	// Two credentials sharing their first four bytes; the fragment below
	// decodes to exactly those four bytes.
	shared := []byte{0xaa, 0xbb, 0xcc, 0xdd}
	first := append(append([]byte{}, shared...), rawCredentialID(0x10)...)
	second := append(append([]byte{}, shared...), rawCredentialID(0x50)...)
	saveCredential(t, wt, first)
	saveCredential(t, wt, second)

	fragment := base64.RawURLEncoding.EncodeToString(shared)
	err := mgr.DeleteCredential(
		context.Background(), testWriteThroughTenant, credentialTestEmail, fragment)

	var truncated *WebAuthnCredentialTruncatedIDError
	if !errors.As(err, &truncated) {
		t.Fatalf("DeleteCredential(shared prefix) = %T %v, want *WebAuthnCredentialTruncatedIDError", err, err)
	}
	if len(truncated.Matches) != 2 {
		t.Fatalf("matches = %v, want both credentials", truncated.Matches)
	}
	for _, want := range []string{
		base64.RawURLEncoding.EncodeToString(first),
		base64.RawURLEncoding.EncodeToString(second),
	} {
		if !strings.Contains(truncated.Error(), want) {
			t.Errorf("message does not name %s: %s", want, truncated.Error())
		}
	}
	if remaining := listedIDs(t, mgr); len(remaining) != 2 {
		t.Fatalf("a rejected delete removed something: %v", remaining)
	}
}

// TestDeleteCredentials_BulkRemovesEverything pins the bulk verb the operator
// falls back on, so the per-id fix cannot quietly change it.
func TestDeleteCredentials_BulkRemovesEverything(t *testing.T) {
	mgr, wt := newCredentialAdminManager(t)
	for _, seed := range []byte{0x10, 0x40} {
		saveCredential(t, wt, rawCredentialID(seed))
	}

	deleted, err := mgr.DeleteCredentials(
		context.Background(), testWriteThroughTenant, credentialTestEmail)
	if err != nil {
		t.Fatalf("DeleteCredentials: %v", err)
	}
	if deleted != 2 {
		t.Fatalf("DeleteCredentials removed %d, want 2", deleted)
	}
	if remaining := listedIDs(t, mgr); len(remaining) != 0 {
		t.Fatalf("bulk delete left %v", remaining)
	}
}

// TestDeleteCredentials_UnregisteredAccountIsNotAnError pins the operator
// recovery contract: an account that never registered a passkey reports zero
// removals rather than failing, so the cascade callers need no special case.
func TestDeleteCredentials_UnregisteredAccountIsNotAnError(t *testing.T) {
	mgr, _ := newCredentialAdminManager(t)

	deleted, err := mgr.DeleteCredentials(
		context.Background(), testWriteThroughTenant, "nobody@example.com")
	if err != nil {
		t.Fatalf("DeleteCredentials(unregistered): %v", err)
	}
	if deleted != 0 {
		t.Fatalf("DeleteCredentials(unregistered) removed %d, want 0", deleted)
	}
}

// TestListCredentials_ProjectsAuthenticatorContextWithoutKeyMaterial pins what
// the operator surface is allowed to disclose: enough to recognise a key, and
// never the credential public key.
func TestListCredentials_ProjectsAuthenticatorContextWithoutKeyMaterial(t *testing.T) {
	mgr, wt := newCredentialAdminManager(t)
	rawID := rawCredentialID(0x10)
	saveCredential(t, wt, rawID)

	creds, err := mgr.ListCredentials(
		context.Background(), testWriteThroughTenant, credentialTestEmail)
	if err != nil {
		t.Fatalf("ListCredentials: %v", err)
	}
	if len(creds) != 1 {
		t.Fatalf("listed %d credentials, want 1", len(creds))
	}
	got := creds[0]
	if want := base64.RawURLEncoding.EncodeToString(rawID); got.ID != want {
		t.Errorf("id = %q, want %q", got.ID, want)
	}
	if got.AAGUID != "aabbccdd" {
		t.Errorf("aaguid = %q, want hex of the authenticator model", got.AAGUID)
	}
	if got.AttestationType != "none" {
		t.Errorf("attestation type = %q, want %q", got.AttestationType, "none")
	}
}

// TestListCredentials_UnregisteredAccountIsEmptyNotAnError pins the listing's
// half of the same contract: never registered and nothing left are the same
// operator-visible state.
func TestListCredentials_UnregisteredAccountIsEmptyNotAnError(t *testing.T) {
	mgr, _ := newCredentialAdminManager(t)

	creds, err := mgr.ListCredentials(
		context.Background(), testWriteThroughTenant, "nobody@example.com")
	if err != nil {
		t.Fatalf("ListCredentials(unregistered): %v", err)
	}
	if len(creds) != 0 {
		t.Fatalf("ListCredentials(unregistered) = %v, want empty", creds)
	}
}

// TestCredentialVerbs_RejectEmptyEmail covers the bad-input side of the shared
// owner resolution: the credential store is keyed by email, so an empty one
// must fail rather than operate on an empty key.
func TestCredentialVerbs_RejectEmptyEmail(t *testing.T) {
	mgr, _ := newCredentialAdminManager(t)
	ctx := context.Background()

	if _, err := mgr.ListCredentials(ctx, testWriteThroughTenant, ""); err == nil {
		t.Error("ListCredentials with no email succeeded")
	}
	if err := mgr.DeleteCredential(
		ctx, testWriteThroughTenant, "", base64.RawURLEncoding.EncodeToString(rawCredentialID(0x10)),
	); err == nil {
		t.Error("DeleteCredential with no email succeeded")
	}
	if _, err := mgr.DeleteCredentials(ctx, testWriteThroughTenant, ""); err == nil {
		t.Error("DeleteCredentials with no email succeeded")
	}
}

// TestWebAuthnCredentialTruncatedIDError_Message covers the typed error's own
// rendering, including the nil receiver every typed error in this package
// tolerates.
func TestWebAuthnCredentialTruncatedIDError_Message(t *testing.T) {
	var nilErr *WebAuthnCredentialTruncatedIDError
	if msg := nilErr.Error(); msg == "" {
		t.Error("nil receiver produced an empty message")
	}

	bare := &WebAuthnCredentialTruncatedIDError{CredentialID: "abcd"}
	if strings.Contains(bare.Error(), "full id") {
		t.Errorf("message claims a full id it does not have: %s", bare.Error())
	}

	one := &WebAuthnCredentialTruncatedIDError{CredentialID: "abcd", Matches: []string{"abcdefgh"}}
	if !strings.Contains(one.Error(), "the full id is abcdefgh") {
		t.Errorf("single-match message = %s", one.Error())
	}

	many := &WebAuthnCredentialTruncatedIDError{
		CredentialID: "abcd",
		Matches:      []string{"abcdefgh", "abcdijkl"},
	}
	if !strings.Contains(many.Error(), "prefixes 2 registered credentials") {
		t.Errorf("multi-match message = %s", many.Error())
	}
}
