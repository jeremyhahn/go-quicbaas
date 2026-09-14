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
	"encoding/hex"
	"strconv"
	"strings"

	// The credential type and the not-found predicates are declared in
	// pkg/webauthn and are not re-exported by go-xkms/sdk/go (see
	// webauthn_factor_writethrough.go for the same justified direct import).
	xkmswebauthn "github.com/jeremyhahn/go-xkms/pkg/webauthn"
	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"
)

// =============================================================================
// WebAuthn credential administration
//
// Every verb here is ADDRESSED by (tenant, email) and OWNED by the qrdb user
// id, and the difference between those two is the whole subject of
// go-quicbaas#582.
//
// The address is what a caller supplies: the ceremony resolves a registrant by
// email, so the operator verbs take the same pair rather than an id the
// ceremony never sees. The ownership is what the store records: xkms's
// qrdb-backed adapter files a credential under
// `__xkms:webauthn/users/<qrdb user id>/credentials`, its blob under the
// credential id, and its qrdb fido2 login factor under the same uint64. Email
// appears in none of those keys — resolveCredentialOwner reaches them by
// looking the email up in the qrdb user record and taking that record's id.
//
// So a rename re-addresses without re-keying: qrdb moves the email index with
// the record, the new address resolves to the same id, and the credentials are
// exactly where they were. Reading "keyed by (tenant, email)" off this
// addressing — as this file's own commentary once did — is what led UpdateUser
// to destroy a renamed user's passkeys to prevent an orphan that cannot occur.
//
// A credential CAN still outlive its user, because deleting the qrdb record
// leaves the id-keyed blobs behind with nothing naming them. That is what the
// DeleteUser cascade is for, and why these verbs exist at all: without them
// there was no operator surface to see or remove one (go-quicbaas#273).
// =============================================================================

// WebAuthnCredentialSummary is the operator-visible projection of one stored
// WebAuthn credential. It carries the credential's identity and the non-secret
// authenticator context an operator needs to recognise a key ("which YubiKey is
// this?"), and never the credential public key or any other material an
// attacker could use to impersonate the authenticator.
type WebAuthnCredentialSummary struct {
	// ID is the base64url (unpadded) WebAuthn credential id — the same
	// encoding the browser reports as rawId, the xkms credential store uses
	// for its KV key segment, and the qrdb fido2 auth factor stores as its
	// login lookup key. It is the handle the delete verb takes.
	ID string `json:"id"`

	// AAGUID is the hex-encoded authenticator model identifier, empty when
	// the authenticator reported none (self attestation).
	AAGUID string `json:"aaguid,omitempty"`

	// AttestationType is the attestation statement format ("none",
	// "packed", ...).
	AttestationType string `json:"attestation_type,omitempty"`

	// Transports are the authenticator-reported transports ("usb", "nfc",
	// "internal", ...).
	Transports []string `json:"transports,omitempty"`

	// SignCount is the authenticator's signature counter as of the last
	// successful assertion.
	SignCount uint32 `json:"sign_count"`

	// BackupEligible reports whether the credential may be backed up
	// (a multi-device passkey).
	BackupEligible bool `json:"backup_eligible"`

	// BackupState reports whether the credential is currently backed up.
	BackupState bool `json:"backup_state"`

	// CloneWarning reports that the authenticator's signature counter went
	// backwards — the FIDO cloned-authenticator signal.
	CloneWarning bool `json:"clone_warning"`
}

// WebAuthnCredentialNotFoundError reports that no WebAuthn credential with the
// given id is registered to the named account. It is returned rather than a
// bare not-found so the admin surface can distinguish "this user has no such
// credential" from "this user has no WebAuthn record at all".
type WebAuthnCredentialNotFoundError struct {
	// Email is the account the credential was looked up under.
	Email string

	// CredentialID is the base64url credential id that did not resolve.
	CredentialID string
}

// Error implements error.
func (e *WebAuthnCredentialNotFoundError) Error() string {
	if e == nil {
		return "iam.webauthn.credential: not found"
	}
	msg := "iam.webauthn.credential: no credential " + e.CredentialID + " is registered"
	if e.Email != "" {
		msg += " to " + e.Email
	}
	return msg
}

// WebAuthnCredentialTruncatedIDError reports a credential id that decodes as
// valid base64url but is a strict PREFIX of one the account actually holds —
// the operator supplied a fragment of a real id, not a different id.
//
// It exists because base64url cannot tell the two apart on its own. A
// credential id is an opaque byte string of no fixed length, so any prefix of
// an encoded id whose length is a multiple of four decodes cleanly into a
// shorter byte string. Truncating the 43-character id
// "Nj2AKQKYD4edHuPeUssHkKFDEgv3oS7LkIA9Q7vccF8" at 24 characters yields
// "Nj2AKQKYD4edHuPeUssHkKFD", which decodes without complaint to 18 bytes that
// match nothing. Reporting that as a plain not-found — the answer before
// go-quicbaas#574 — is indistinguishable from "that credential is already
// gone", and cost a full investigation into an encoding bug that did not
// exist. The server can tell, because it is holding the ids the caller is
// authorized to list, so it says so.
type WebAuthnCredentialTruncatedIDError struct {
	// CredentialID is the fragment the operator supplied.
	CredentialID string

	// Matches are the full base64url credential ids the fragment prefixes.
	// The caller is already authorized to read them: the list verb this
	// error tells them to re-run returns exactly these ids.
	Matches []string
}

// Error implements error. It names the full ids so the operator's next command
// is the correct one rather than another guess.
func (e *WebAuthnCredentialTruncatedIDError) Error() string {
	if e == nil {
		return "iam.webauthn.credential: truncated credential id"
	}
	msg := "iam.webauthn.credential: credential id " + e.CredentialID +
		" is a truncated prefix of a registered credential, not a credential id"
	switch len(e.Matches) {
	case 0:
		return msg
	case 1:
		return msg + "; the full id is " + e.Matches[0]
	default:
		return msg + "; it prefixes " + strconv.Itoa(len(e.Matches)) +
			" registered credentials: " + strings.Join(e.Matches, ", ")
	}
}

// WebAuthnCredentialIDError reports a malformed credential id supplied by an
// operator. Credential ids are base64url (unpadded) as reported by the browser.
type WebAuthnCredentialIDError struct {
	// CredentialID is the value that failed to decode.
	CredentialID string

	// Cause is the underlying decode failure.
	Cause error
}

// Error implements error.
func (e *WebAuthnCredentialIDError) Error() string {
	if e == nil {
		return "iam.webauthn.credential: invalid credential id"
	}
	msg := "iam.webauthn.credential: credential id is not valid base64url: " + e.CredentialID
	if e.Cause != nil {
		return msg + ": " + e.Cause.Error()
	}
	return msg
}

// Unwrap returns the underlying cause for errors.Is / errors.As.
func (e *WebAuthnCredentialIDError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

// WebAuthnCredentialReaper deletes every WebAuthn credential registered under a
// tenant+email, returning how many were removed. It is the narrow seam the
// identity store reaches the WebAuthn stack through, satisfied by
// *XKMSWebAuthnManager, so the store takes no dependency on that stack beyond
// the one operation it needs.
//
// Its callers are the ones that DESTROY the owning identity: DeleteUser, tenant
// deletion, and GDPR erasure. A rename is not one of them — the credentials are
// filed under the user id, which a rename does not change (go-quicbaas#582).
type WebAuthnCredentialReaper interface {
	// DeleteCredentials removes every WebAuthn credential registered to the
	// email within the tenant. An account with no WebAuthn record is not an
	// error: it reports (0, nil).
	DeleteCredentials(ctx context.Context, tenantID, email string) (int, error)
}

// WebAuthnCredentialLister reports the WebAuthn credentials registered under a
// tenant+email. It is the read half of the reaper seam, used by the identity
// store to say in the log how many credentials a rename carried with it.
type WebAuthnCredentialLister interface {
	// ListCredentials returns the credentials registered to the email within
	// the tenant. An account with no WebAuthn record yields an empty slice.
	ListCredentials(ctx context.Context, tenantID, email string) ([]WebAuthnCredentialSummary, error)
}

// Compile-time guards: the xkms-backed manager is the production reaper, and
// the same value supplies the listing the rename log reports.
var (
	_ WebAuthnCredentialReaper = (*XKMSWebAuthnManager)(nil)
	_ WebAuthnCredentialLister = (*XKMSWebAuthnManager)(nil)
)

// ListCredentials returns the WebAuthn credentials registered to the email
// within the tenant. An account with no WebAuthn record has no credentials and
// yields an empty slice rather than an error — the ceremony store is populated
// lazily by the first registration, so "never registered a passkey" and "has no
// credentials left" are the same operator-visible state.
//
// Listing never creates the account: it resolves through the user store's
// lookup, not through the ceremony's get-or-create path.
func (m *XKMSWebAuthnManager) ListCredentials(ctx context.Context, tenantID, email string) ([]WebAuthnCredentialSummary, error) {
	svc, handle, err := m.resolveCredentialOwner(ctx, tenantID, email)
	if err != nil {
		return nil, err
	}
	if handle == nil {
		return []WebAuthnCredentialSummary{}, nil
	}
	creds, err := svc.GetCredentials(ctx, handle)
	if err != nil {
		return nil, err
	}
	out := make([]WebAuthnCredentialSummary, 0, len(creds))
	for _, cred := range creds {
		if cred == nil {
			continue
		}
		out = append(out, summarizeWebAuthnCredential(cred))
	}
	return out, nil
}

// DeleteCredential removes ONE credential from the named account.
//
// The credential id is verified to belong to that account before anything is
// deleted. This check is load-bearing, not defensive: the qrdb credential
// blobs live in a single namespace keyed by credential id alone
// (__xkms:webauthn/credentials/<id>, no tenant or user segment), so a delete
// dispatched straight to the store would let a caller authorized over one user
// destroy any passkey in the deployment by naming its id. The ownership check
// is what binds the deletion to the subject the caller was authorized against.
func (m *XKMSWebAuthnManager) DeleteCredential(ctx context.Context, tenantID, email, credentialID string) error {
	rawID, err := decodeWebAuthnCredentialID(credentialID)
	if err != nil {
		return err
	}
	svc, handle, err := m.resolveCredentialOwner(ctx, tenantID, email)
	if err != nil {
		return err
	}
	if handle == nil {
		return &WebAuthnCredentialNotFoundError{Email: email, CredentialID: credentialID}
	}
	creds, err := svc.GetCredentials(ctx, handle)
	if err != nil {
		return err
	}
	// Compare on the DECODED bytes, not the operator's spelling of them: the
	// decode above accepts padded base64url as well as the canonical unpadded
	// form, so comparing the raw input against the canonical encoding would
	// report a credential the user genuinely owns as not found.
	owned := false
	for _, cred := range creds {
		if cred != nil && bytes.Equal(cred.ID, rawID) {
			owned = true
			break
		}
	}
	if !owned {
		if truncated := truncatedWebAuthnCredentialID(credentialID, rawID, creds); truncated != nil {
			return truncated
		}
		return &WebAuthnCredentialNotFoundError{Email: email, CredentialID: credentialID}
	}
	if err := svc.DeleteCredential(ctx, rawID); err != nil {
		if xkmswebauthn.IsCredentialNotFound(err) {
			return &WebAuthnCredentialNotFoundError{Email: email, CredentialID: credentialID}
		}
		return err
	}
	return nil
}

// DeleteCredentials removes every credential registered to the email within the
// tenant and reports how many were removed. It serves the operator recovery
// verb ("this email is stuck, clear its passkeys") and the identity cascades
// that destroy the owning account: DeleteUser, tenant deletion, GDPR erasure.
//
// Deletion goes one credential at a time through the credential store's Delete
// rather than its bulk DeleteByUserID so each removal passes through the
// registration write-through decorator, which also revokes the credential's
// qrdb fido2 login factor. A bulk delete would strip the credentials and leave
// the factors resolving at login against a passkey that no longer exists.
//
// The walk is not transactional: a failure part-way returns the count deleted
// so far alongside the error. That partial state is deliberately tolerable —
// every credential removed is one that can no longer block a re-registration,
// so a partial run leaves the account strictly closer to usable than it was,
// and re-running the verb finishes the job.
func (m *XKMSWebAuthnManager) DeleteCredentials(ctx context.Context, tenantID, email string) (int, error) {
	svc, handle, err := m.resolveCredentialOwner(ctx, tenantID, email)
	if err != nil {
		return 0, err
	}
	if handle == nil {
		return 0, nil
	}
	creds, err := svc.GetCredentials(ctx, handle)
	if err != nil {
		return 0, err
	}
	deleted := 0
	for _, cred := range creds {
		if cred == nil {
			continue
		}
		if dErr := svc.DeleteCredential(ctx, cred.ID); dErr != nil {
			if xkmswebauthn.IsCredentialNotFound(dErr) {
				// Already gone (concurrent delete): the postcondition the
				// caller asked for holds for this credential.
				continue
			}
			return deleted, dErr
		}
		deleted++
	}
	return deleted, nil
}

// resolveCredentialOwner resolves the per-tenant xkms service and the WebAuthn
// handle of the account registered under email. A tenant whose account has no
// WebAuthn record yields a nil handle and no error — the caller decides whether
// that is an empty listing or a not-found.
//
// The resolution is tenant-scoped end to end: ServiceFor builds the adapter
// pair with this tenant id, and its GetUserByEmail is a per-tenant username
// lookup. An email registered in two tenants therefore resolves to two
// different accounts with two different credential sets, and neither tenant's
// verbs can reach the other's.
func (m *XKMSWebAuthnManager) resolveCredentialOwner(ctx context.Context, tenantID, email string) (*xkmssdk.WebAuthnService, []byte, error) {
	if email == "" {
		return nil, nil, &WebAuthnFactoryConfigError{Field: "email"}
	}
	svc, err := m.factory.ServiceFor(ctx, tenantID)
	if err != nil {
		return nil, nil, err
	}
	user, err := svc.GetUserByEmail(ctx, email)
	if err != nil {
		if xkmswebauthn.IsUserNotFound(err) {
			return svc, nil, nil
		}
		return nil, nil, err
	}
	return svc, user.WebAuthnID(), nil
}

// summarizeWebAuthnCredential projects a stored credential onto the
// operator-visible summary. Key material stays in the credential store.
func summarizeWebAuthnCredential(cred *xkmswebauthn.Credential) WebAuthnCredentialSummary {
	summary := WebAuthnCredentialSummary{
		ID:              webAuthnCredentialLookupKey(cred.ID),
		AttestationType: cred.AttestationType,
		SignCount:       cred.Authenticator.SignCount,
		BackupEligible:  cred.Flags.BackupEligible,
		BackupState:     cred.Flags.BackupState,
		CloneWarning:    cred.Authenticator.CloneWarning,
	}
	if len(cred.Authenticator.AAGUID) > 0 {
		summary.AAGUID = hex.EncodeToString(cred.Authenticator.AAGUID)
	}
	if len(cred.Transport) > 0 {
		summary.Transports = make([]string, 0, len(cred.Transport))
		for _, t := range cred.Transport {
			summary.Transports = append(summary.Transports, string(t))
		}
	}
	return summary
}

// decodeWebAuthnCredentialID reverses webAuthnCredentialLookupKey. Padded
// base64url is accepted too so an id pasted from a tool that pads round-trips.
func decodeWebAuthnCredentialID(credentialID string) ([]byte, error) {
	if credentialID == "" {
		return nil, &WebAuthnCredentialIDError{CredentialID: credentialID}
	}
	if raw, err := base64.RawURLEncoding.DecodeString(credentialID); err == nil {
		return raw, nil
	}
	raw, err := base64.URLEncoding.DecodeString(credentialID)
	if err != nil {
		return nil, &WebAuthnCredentialIDError{CredentialID: credentialID, Cause: err}
	}
	return raw, nil
}

// truncatedWebAuthnCredentialID reports the supplied id as a truncated prefix
// of the account's registered credentials, or nil when it is simply unknown.
//
// The test is on the DECODED bytes, so it is spelling-independent: an operator
// who pasted a padded fragment gets the same diagnosis as one who pasted an
// unpadded one. A byte prefix must be strictly shorter than what it prefixes —
// an exact match is ownership and never reaches here.
func truncatedWebAuthnCredentialID(
	credentialID string,
	rawID []byte,
	creds []*xkmswebauthn.Credential,
) *WebAuthnCredentialTruncatedIDError {
	if len(rawID) == 0 {
		return nil
	}
	var matches []string
	for _, cred := range creds {
		if cred == nil || len(cred.ID) <= len(rawID) {
			continue
		}
		if bytes.Equal(cred.ID[:len(rawID)], rawID) {
			matches = append(matches, webAuthnCredentialLookupKey(cred.ID))
		}
	}
	if len(matches) == 0 {
		return nil
	}
	return &WebAuthnCredentialTruncatedIDError{CredentialID: credentialID, Matches: matches}
}
