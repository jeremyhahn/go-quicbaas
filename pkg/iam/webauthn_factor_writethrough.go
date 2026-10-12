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
	"encoding/hex"
	"errors"
	"log/slog"
	"strconv"
	"strings"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"

	// go-xkms/sdk/go aliases CredentialStore and Credential
	// (WebAuthnCredentialStore / WebAuthnCredential), but NOT UserStore, the
	// ErrCredentialNotFound sentinel, or the canonical
	// handle<->qrdb-user-id codec (UserIDFromHandle) — see
	// go-xkms/sdk/go/webauthn.go. Those three are what keep this direct
	// package import: it is a type/interface dependency only, and no
	// behavior is copied.
	xkmswebauthn "github.com/jeremyhahn/go-xkms/pkg/webauthn"
)

// =============================================================================
// WebAuthn registration -> qrdb auth-factor write-through
//
// PROBLEM THIS SOLVES
//
// A WebAuthn registration ceremony persists its result through xkms's
// CredentialStore, which the qrdb adapter writes to a KV side-channel
// (__xkms:webauthn/...). The UNIFIED login ceremony
// (go-qrdb pkg/auth.LoginResolver -> UserAuthFactorServicer.LookupByCredentialKey)
// resolves against a DIFFERENT store: qrdb's uint64-keyed UserAuthFactor
// store. A passkey registered through any browser/SDK ceremony therefore had
// no resolvable factor and could never complete a CLI/SDK login — finishing
// with a real credential id was byte-identical to finishing with a bogus one
// ("factor not found").
//
// WHY THIS LAYER
//
// Every registration entry point in the stack converges on ONE call:
// xkms Service.FinishRegistration -> CredentialStore.Save. That includes the
// paths that never touch XKMSWebAuthnManager — notably the xkms-native mount
// at /api/v1/iam/{tenant}/webauthn (pkg/api/rest/iam_xkms_webauthn_mount.go),
// which dispatches straight to xkms's HTTP handler and is the surface the SDK
// WebAuthn client and the `quicbaas auth factor add --type fido2` CLI use.
// Decorating the credential store is therefore the ONLY single choke point
// that covers self-registration, invite activation, both REST mounts, the SDK
// and the CLI without duplicating enrollment logic per entry point.
//
// TRANSACTIONAL BEHAVIOUR (deliberate choice)
//
// The invariant we hold is: "a persisted WebAuthn credential implies a
// login-resolvable qrdb factor". The ordering is persist-then-enroll with
// COMPENSATION:
//
//  1. Snapshot whether the credential already existed in the xkms store.
//  2. Persist the credential (the inner store's Save).
//  3. Enroll the fido2 factor in qrdb.
//  4. If (3) fails, delete the credential written by (2) — but only when the
//     snapshot proved we created it, so a retry can never destroy a
//     previously-good credential — and fail the ceremony closed.
//
// Enroll-first was rejected: a factor whose credential does not exist resolves
// at login and then fails hardware verification, leaving a dangling entry in
// `auth factor list`. Compensating the credential write keeps the two stores
// consistent in both directions and makes a failed registration a no-op the
// user can simply retry.
//
// IDEMPOTENCY
//
// Re-registering the same credential id must not error the ceremony: qrdb
// answers the duplicate enroll with FactorAlreadyEnrolledError, which is
// treated as success. Credential ids are authenticator-generated and
// unforgeable without the private key, so a duplicate is always the same
// authenticator re-running the ceremony.
//
// ACCOUNT DELETION (go-quicbaas#290)
//
// An account marked deleting (LifecycleDeleting) gains no credential. Save
// refuses it before writing anything, and re-reads the account after the write:
// deletion marks the account before it sweeps its credentials, so a write that
// races past the first read and lands after the sweep is caught by the re-read
// and rolled back. Either refusal is a WebAuthnRegistrantDeletingError, and an
// account that cannot be read fails the ceremony closed with
// WebAuthnRegistrantLookupError.
//
// REMOVAL IS THE SAME INVARIANT, RUN BACKWARDS
//
// "A persisted WebAuthn credential implies a login-resolvable qrdb factor" is
// violated just as badly from the other end: a credential deleted through the
// inner store alone leaves a fido2 factor that resolves at login and then fails
// hardware verification, and shows up in `auth factor list` pointing at a
// passkey that no longer exists. Delete and DeleteByUserID therefore revoke the
// matching factor as well, so the pair stays consistent whichever direction the
// mutation runs.

// =============================================================================

// webAuthnFactorCreatedBy is the qrdb factor provenance string recorded for
// factors enrolled by a WebAuthn registration ceremony. It matches the value
// the SDK's Security().Auth().Factors().Add surface writes, so both paths
// produce indistinguishable records.
const webAuthnFactorCreatedBy = "self"

// WebAuthnFactorEnrollError reports that a WebAuthn registration ceremony
// completed the cryptographic exchange but the resulting authenticator could
// not be enrolled as a qrdb auth factor — meaning the passkey would not be
// resolvable at login. Registration fails closed with this error rather than
// silently persisting an unusable credential.
type WebAuthnFactorEnrollError struct {
	// CredentialID is the base64url WebAuthn credential id (the login
	// lookup key) the enrollment was attempted for.
	CredentialID string

	// Compensated reports whether the credential written earlier in the
	// ceremony was rolled back. False means the credential pre-existed and
	// was deliberately left in place.
	Compensated bool

	// Cause is the underlying enrollment failure.
	Cause error
}

// Error implements error.
func (e *WebAuthnFactorEnrollError) Error() string {
	if e == nil {
		return "iam.webauthn.factor: enrollment failed"
	}
	msg := "iam.webauthn.factor: failed to enroll the registered authenticator as a login factor"
	if e.CredentialID != "" {
		msg += " (credential " + e.CredentialID + ")"
	}
	if e.Compensated {
		msg += "; the credential was rolled back"
	}
	if e.Cause != nil {
		return msg + ": " + e.Cause.Error()
	}
	return msg
}

// Unwrap returns the underlying cause for errors.Is / errors.As.
func (e *WebAuthnFactorEnrollError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

// WebAuthnCredentialSnapshotError reports that a registration could not
// establish whether the credential already existed, because the pre-write
// lookup failed for a reason other than a genuine not-found.
//
// It is deliberately NOT a WebAuthnFactorEnrollError: that type states an
// enrollment was attempted and its Compensated=false means a pre-existing
// credential was knowingly left in place. On this path no enrollment was
// attempted and existence is precisely what is unknown — reporting it as an
// enrollment failure would tell the caller two things that are both false.
type WebAuthnCredentialSnapshotError struct {
	// CredentialID is the base64url credential id whose prior existence
	// could not be established.
	CredentialID string

	// Cause is the underlying lookup failure.
	Cause error
}

// Error implements error.
func (e *WebAuthnCredentialSnapshotError) Error() string {
	if e == nil {
		return "iam.webauthn.credential: could not determine whether the credential already existed"
	}
	msg := "iam.webauthn.credential: could not determine whether the credential already existed, " +
		"so the registration was not attempted"
	if e.CredentialID != "" {
		msg += " (credential " + e.CredentialID + ")"
	}
	if e.Cause != nil {
		return msg + ": " + e.Cause.Error()
	}
	return msg
}

// Unwrap returns the underlying cause for errors.Is / errors.As.
func (e *WebAuthnCredentialSnapshotError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

// WebAuthnFactorStoreConfigError reports a missing dependency for the
// registration write-through. It is returned at service-construction time so
// a misconfiguration surfaces at startup rather than mid-ceremony.
// AttestationRecorderResolver yields qrdb's trusted-server AttestationRecorder
// for the node this write-through runs on, or nil while the node is not up.
// The server obtains it by type assertion on the node's auth-factor servicer;
// no transport or client can produce one, by qrdb's design (go-qrdb#342).
type AttestationRecorderResolver func() qrdbsdk.AttestationRecorder

// WebAuthnAttestationRecorderUnavailableError reports that a registration
// ceremony completed but the node's AttestationRecorder could not be resolved,
// so what the ceremony proved could not be recorded. The enrollment fails
// closed: a factor written without its proof would be indistinguishable from
// one that proved nothing.
type WebAuthnAttestationRecorderUnavailableError struct {
	TenantID string
}

// Error implements error.
func (e *WebAuthnAttestationRecorderUnavailableError) Error() string {
	return "iam.webauthn: attestation recorder unavailable for tenant " + e.TenantID +
		": the node's auth-factor servicer is not up or does not record attestation"
}

type WebAuthnFactorStoreConfigError struct {
	Field string
}

// Error implements error.
func (e *WebAuthnFactorStoreConfigError) Error() string {
	if e == nil || e.Field == "" {
		return "iam.webauthn.factor: invalid write-through configuration"
	}
	return "iam.webauthn.factor: missing or invalid field: " + e.Field
}

// WebAuthnFactorRevokeError reports that a WebAuthn credential could not be
// deleted because its matching qrdb login factor could not be revoked. The
// deletion fails closed with this error rather than stripping the credential
// and leaving a factor that resolves at login to a passkey that no longer
// exists.
type WebAuthnFactorRevokeError struct {
	// CredentialID is the base64url WebAuthn credential id whose factor
	// could not be revoked.
	CredentialID string

	// Cause is the underlying revocation failure.
	Cause error
}

// Error implements error.
func (e *WebAuthnFactorRevokeError) Error() string {
	if e == nil {
		return "iam.webauthn.factor: revocation failed"
	}
	msg := "iam.webauthn.factor: failed to revoke the login factor for the credential being deleted"
	if e.CredentialID != "" {
		msg += " (credential " + e.CredentialID + ")"
	}
	if e.Cause != nil {
		return msg + ": " + e.Cause.Error()
	}
	return msg
}

// Unwrap returns the underlying cause for errors.Is / errors.As.
func (e *WebAuthnFactorRevokeError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

// WebAuthnRegistrantDeletingError reports that a WebAuthn registration was
// refused because the registrant's account is being deleted
// (LifecycleDeleting). Deletion sweeps the account's credentials after marking
// it, so a credential written now would outlive the account it belongs to. The
// ceremony fails closed with this error rather than persisting that orphan.
type WebAuthnRegistrantDeletingError struct {
	// CredentialID is the base64url WebAuthn credential id the registration
	// was for.
	CredentialID string

	// UserID is the qrdb user id decoded from the credential's user handle.
	UserID uint64

	// Compensated reports whether a credential this registration had already
	// written was removed again. It is false when the refusal came before
	// anything was written, and when the removal itself failed.
	Compensated bool
}

// Error implements error.
func (e *WebAuthnRegistrantDeletingError) Error() string {
	if e == nil {
		return "iam.webauthn.registration: the account is being deleted"
	}
	msg := "iam.webauthn.registration: refused, the account is being deleted (user " +
		strconv.FormatUint(e.UserID, 10) + ")"
	if e.CredentialID != "" {
		msg += " (credential " + e.CredentialID + ")"
	}
	if e.Compensated {
		msg += "; the credential was rolled back"
	}
	return msg
}

// WebAuthnRegistrantLookupError reports that a WebAuthn registration could not
// read the registrant's account, so whether the account is being deleted is
// unknown. The ceremony fails closed rather than risk enrolling a credential
// for an account that is being torn down.
type WebAuthnRegistrantLookupError struct {
	// CredentialID is the base64url WebAuthn credential id the registration
	// was for.
	CredentialID string

	// Compensated reports whether a credential this registration had already
	// written was removed again.
	Compensated bool

	// Cause is the underlying lookup failure.
	Cause error
}

// Error implements error.
func (e *WebAuthnRegistrantLookupError) Error() string {
	if e == nil {
		return "iam.webauthn.registration: could not read the registrant's account"
	}
	msg := "iam.webauthn.registration: could not read the registrant's account, so the registration was refused"
	if e.CredentialID != "" {
		msg += " (credential " + e.CredentialID + ")"
	}
	if e.Compensated {
		msg += "; the credential was rolled back"
	}
	if e.Cause != nil {
		return msg + ": " + e.Cause.Error()
	}
	return msg
}

// Unwrap returns the underlying cause for errors.Is / errors.As.
func (e *WebAuthnRegistrantLookupError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

// webAuthnFactorWriteThrough decorates an xkms CredentialStore so a completed
// registration ceremony also enrolls the authenticator in qrdb's UserAuthFactor
// store — the store the unified login ceremony resolves against.
//
// The inner store is EMBEDDED, so the methods that need no bookkeeping
// (GetByUserID, GetByCredentialID, Update) are promoted unchanged; Save,
// Delete and DeleteByUserID are overridden.
type webAuthnFactorWriteThrough struct {
	xkmswebauthn.CredentialStore

	// users resolves the registrant so the enrolled factor carries a human
	// label (the WebAuthn display name, which the CLI sets from --label).
	users xkmswebauthn.UserStore

	// factors is the qrdb user-side auth-factor client — the SAME store
	// go-qrdb's LoginResolver reads through LookupByCredentialKey.
	factors qrdbtransport.UserAuthFactorClient

	// attestations resolves qrdb's trusted-server AttestationRecorder — the
	// ONLY path that records what a ceremony proved. It is resolved lazily
	// because the node servicer it is asserted from exists only once the
	// embedded node is up; a resolver that yields nil at enrollment time
	// fails the enrollment closed rather than persisting an unattested factor
	// for a ceremony that proved hardware.
	attestations AttestationRecorderResolver

	// tenantID scopes the relying party this store belongs to. It is
	// recorded in the factor metadata; the factor itself binds to the qrdb
	// uint64 user id, which is already tenant-unique.
	tenantID string

	// gate decides whether a completed ceremony proved enough about the
	// authenticator to be persisted at all, and reports what it proved so the
	// enrolled factor can record it. A nil gate is maximally strict rather
	// than absent — see WebAuthnAttestationGate.
	gate *WebAuthnAttestationGate

	logger *slog.Logger
}

// Compile-time guard: the decorator satisfies the interface xkms's
// WebAuthnServiceParams.CredentialStore field expects.
var _ xkmswebauthn.CredentialStore = (*webAuthnFactorWriteThrough)(nil)

// newWebAuthnFactorWriteThrough wraps inner so registrations write through to
// the qrdb auth-factor store. Every dependency is required: a nil factor client
// would silently reproduce the split-store bug this decorator exists to close.
//
// gate may be nil: the zero-value gate semantics (hardware required, no
// allowlist, no admin resolver) are the safe ones, so a caller that forgets to
// pass a policy gets the strict one rather than none.
func newWebAuthnFactorWriteThrough(
	inner xkmswebauthn.CredentialStore,
	users xkmswebauthn.UserStore,
	factors qrdbtransport.UserAuthFactorClient,
	attestations AttestationRecorderResolver,
	tenantID string,
	gate *WebAuthnAttestationGate,
	logger *slog.Logger,
) (*webAuthnFactorWriteThrough, error) {
	if inner == nil {
		return nil, &WebAuthnFactorStoreConfigError{Field: "CredentialStore"}
	}
	if users == nil {
		return nil, &WebAuthnFactorStoreConfigError{Field: "UserStore"}
	}
	if factors == nil {
		return nil, &WebAuthnFactorStoreConfigError{Field: "UserAuthFactorClient"}
	}
	if attestations == nil {
		return nil, &WebAuthnFactorStoreConfigError{Field: "AttestationRecorder"}
	}
	if tenantID == "" {
		return nil, &WebAuthnFactorStoreConfigError{Field: "tenantID"}
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &webAuthnFactorWriteThrough{
		CredentialStore: inner,
		users:           users,
		factors:         factors,
		attestations:    attestations,
		tenantID:        tenantID,
		gate:            gate,
		logger:          logger,
	}, nil
}

// Save persists the credential and enrolls the matching qrdb fido2 factor.
// See the package-level commentary above for the ordering and compensation
// rationale.
func (s *webAuthnFactorWriteThrough) Save(ctx context.Context, cred *xkmswebauthn.Credential) error {
	if cred == nil {
		// Guarded here rather than delegated: a nil credential is a caller
		// bug, and not every CredentialStore implementation nil-checks
		// before dereferencing.
		return &WebAuthnFactorStoreConfigError{Field: "credential"}
	}
	if len(cred.ID) == 0 {
		// Delegate so the inner store's own typed validation error is the
		// one the ceremony reports.
		return s.CredentialStore.Save(ctx, cred)
	}

	lookupKey := webAuthnCredentialLookupKey(cred.ID)

	userID, err := xkmswebauthn.UserIDFromHandle(cred.UserID)
	if err != nil {
		return &WebAuthnFactorEnrollError{CredentialID: lookupKey, Cause: err}
	}

	// An account being deleted must not gain a credential: the deletion sweeps
	// the account's credentials after marking it, so one written now could land
	// after the sweep and outlive the account. Refusing here leaves no residue;
	// confirmRegistrantStanding below covers a deletion that starts after this
	// read.
	registrantExisted, deleting, err := s.registrantStanding(ctx, cred.UserID)
	if err != nil {
		return &WebAuthnRegistrantLookupError{CredentialID: lookupKey, Cause: err}
	}
	if deleting {
		return &WebAuthnRegistrantDeletingError{CredentialID: lookupKey, UserID: userID}
	}

	// The assurance gate runs BEFORE the credential is written, so a refused
	// authenticator leaves no residue at all: no credential in the xkms store,
	// no factor in qrdb, nothing for an operator to clean up. Refusing after
	// the Save would need the same compensation dance the factor-enrollment
	// failure below performs, and would briefly persist exactly the credential
	// the policy exists to keep out.
	attestation, gateErr := s.evaluateAttestation(ctx, cred)
	if gateErr != nil {
		s.logger.Warn("webauthn registration: refused by the attestation assurance gate",
			"tenant_id", s.tenantID,
			"credential_id", lookupKey,
			"attestation_type", cred.AttestationType,
			"aaguid", hex.EncodeToString(cred.Authenticator.AAGUID),
			"error", gateErr)
		return gateErr
	}
	// Hand the proven result back to the ceremony entry point, so a caller that
	// mints a session for the credential it just enrolled (self-service
	// registration) can describe it accurately instead of falling back to
	// "unattested" for a credential that just cleared the hardware bar.
	recordAttestation(ctx, attestation)

	// Snapshot before writing: only a credential WE created may be rolled
	// back if the factor enrollment fails.
	//
	// A LOOKUP THAT FAILS IS NOT A LOOKUP THAT FOUND NOTHING. Absence has to be
	// a positive answer — the store's own not-found — never an inference from
	// an error. Reading "any error means it wasn't there" would let a momentary
	// unreadable store turn the compensation below into a delete of a
	// pre-existing, working credential, destroying a passkey during nothing
	// worse than a failed re-registration. So any other read failure returns
	// here, BEFORE the Save: refusing to enter a state whose compensation
	// cannot be reasoned about costs the caller a retry, and getting it wrong
	// costs them their authenticator.
	_, getErr := s.GetByCredentialID(ctx, cred.ID)
	credentialExisted := getErr == nil
	if getErr != nil && !errors.Is(getErr, xkmswebauthn.ErrCredentialNotFound) {
		return &WebAuthnCredentialSnapshotError{CredentialID: lookupKey, Cause: getErr}
	}

	if saveErr := s.CredentialStore.Save(ctx, cred); saveErr != nil {
		return saveErr
	}

	enrollErr := s.enrollFactor(ctx, userID, lookupKey, cred, attestation)
	// A factor that is already enrolled is the same authenticator re-running
	// the ceremony: the login-resolvable factor exists, so the registration is
	// complete.
	if enrollErr == nil || isFactorAlreadyEnrolled(enrollErr) {
		return s.confirmRegistrantStanding(ctx, cred, lookupKey, userID, registrantExisted, credentialExisted)
	}

	compensated := false
	if !credentialExisted {
		// The INNER delete: the enrollment we are compensating for is the
		// thing that failed, so there is no factor to revoke, and routing
		// through the decorator's Delete would only go looking for one.
		if delErr := s.CredentialStore.Delete(ctx, cred.ID); delErr != nil {
			s.logger.Error("webauthn registration: failed to roll back the credential after a factor-enrollment failure",
				"tenant_id", s.tenantID,
				"credential_id", lookupKey,
				"error", delErr)
		} else {
			compensated = true
		}
	}
	return &WebAuthnFactorEnrollError{
		CredentialID: lookupKey,
		Compensated:  compensated,
		Cause:        enrollErr,
	}
}

// registrantStanding reads the registrant's account and reports whether it
// exists and whether it is being deleted. A store with no record for the handle
// is a positive answer (exists is false), not a failure; any other read error
// is returned, because an account whose state cannot be read cannot be shown
// not to be deleting.
func (s *webAuthnFactorWriteThrough) registrantStanding(ctx context.Context, handle []byte) (exists, deleting bool, err error) {
	user, err := s.users.GetByID(ctx, handle)
	if errors.Is(err, xkmswebauthn.ErrUserNotFound) {
		return false, false, nil
	}
	if err != nil {
		return false, false, err
	}
	if user == nil {
		return false, false, nil
	}
	return true, user.Lifecycle() == LifecycleDeleting, nil
}

// confirmRegistrantStanding re-reads the registrant once the credential and its
// factor are written, closing the window between Save's first read and the
// write. Account deletion marks the account deleting BEFORE it sweeps the
// account's credentials (IdentityStore.DeleteUser), so a write that lands after
// the sweep is always followed by a read here that sees the mark, or no account
// at all, and the write is rolled back instead of surviving as an orphan.
func (s *webAuthnFactorWriteThrough) confirmRegistrantStanding(
	ctx context.Context,
	cred *xkmswebauthn.Credential,
	lookupKey string,
	userID uint64,
	registrantExisted bool,
	credentialExisted bool,
) error {
	exists, deleting, err := s.registrantStanding(ctx, cred.UserID)
	if err != nil {
		// Unknown standing fails the ceremony closed. Only a credential this
		// call created is rolled back, for the same reason the enrollment
		// compensation spares a pre-existing one: a read failure is no
		// evidence against a working passkey.
		compensated := !credentialExisted && s.rollBackRegistration(ctx, cred.ID, lookupKey)
		return &WebAuthnRegistrantLookupError{CredentialID: lookupKey, Compensated: compensated, Cause: err}
	}
	if deleting || (registrantExisted && !exists) {
		// The account is being torn down, so whatever this call wrote goes
		// with it, pre-existing or not: the sweep may already have passed.
		return &WebAuthnRegistrantDeletingError{
			CredentialID: lookupKey,
			UserID:       userID,
			Compensated:  s.rollBackRegistration(ctx, cred.ID, lookupKey),
		}
	}
	return nil
}

// rollBackRegistration removes a credential written by a registration that is
// being refused after the fact, revoking its factor with it. It reports whether
// the removal succeeded; a failure is logged, because the caller's typed error
// is the only place left to say the credential is still there.
func (s *webAuthnFactorWriteThrough) rollBackRegistration(ctx context.Context, credID []byte, lookupKey string) bool {
	if err := s.Delete(ctx, credID); err != nil {
		s.logger.Error("webauthn registration: failed to roll back the credential of a refused registration",
			"tenant_id", s.tenantID,
			"credential_id", lookupKey,
			"error", err)
		return false
	}
	return true
}

// Delete removes the credential and revokes the fido2 login factor enrolled
// alongside it, so the pair the ceremony created is torn down together.
//
// The credential is read BEFORE the delete because the factor is keyed by the
// credential's owning user, which only the stored record knows. The ordering is
// revoke-then-delete: a factor without its credential fails hardware
// verification at login (a dangling entry an operator must chase), while a
// credential whose factor is already gone simply cannot log in — the strictly
// safer of the two intermediate states if the process dies between the steps.
func (s *webAuthnFactorWriteThrough) Delete(ctx context.Context, credID []byte) error {
	cred, err := s.GetByCredentialID(ctx, credID)
	if err != nil {
		if errors.Is(err, xkmswebauthn.ErrCredentialNotFound) {
			// Nothing stored, so nothing to revoke. Delegate so the inner
			// store's own typed not-found is what the caller sees.
			return s.CredentialStore.Delete(ctx, credID)
		}
		// A store that cannot be read cannot tell us which factor to revoke.
		// Deleting anyway would strip the credential and strand the factor —
		// exactly the split this method exists to prevent — so fail closed.
		return &WebAuthnFactorRevokeError{
			CredentialID: webAuthnCredentialLookupKey(credID),
			Cause:        err,
		}
	}
	if rErr := s.revokeFactor(ctx, cred); rErr != nil {
		return rErr
	}
	return s.CredentialStore.Delete(ctx, credID)
}

// DeleteByUserID removes every credential for a user, revoking each one's
// fido2 login factor. It walks the set through Delete rather than delegating to
// the inner store's bulk removal so no credential is stripped without its
// factor.
//
// The inner bulk removal runs only when the walk found nothing to delete. Each
// Delete already detaches its credential from the user's index, so after a full
// walk the index is gone and a trailing bulk delete would be operating on keys
// that no longer exist — on a store that reports a missing key as an error,
// that would turn a completely successful teardown into a failure. Running it
// on the empty case still clears a stale index left pointing at credentials
// some earlier partial delete already removed.
func (s *webAuthnFactorWriteThrough) DeleteByUserID(ctx context.Context, userID []byte) error {
	creds, err := s.GetByUserID(ctx, userID)
	if err != nil {
		return err
	}
	deleted := 0
	for _, cred := range creds {
		if cred == nil {
			continue
		}
		if dErr := s.Delete(ctx, cred.ID); dErr != nil {
			if errors.Is(dErr, xkmswebauthn.ErrCredentialNotFound) {
				continue
			}
			return dErr
		}
		deleted++
	}
	if deleted > 0 {
		return nil
	}
	return s.CredentialStore.DeleteByUserID(ctx, userID)
}

// revokeFactor soft-deletes the qrdb fido2 factor enrolled for cred. A factor
// that was never enrolled (a credential written before the write-through, or by
// a store that bypassed it) is not an error: the postcondition "no factor
// resolves to this credential" already holds.
//
// Force is set because the guard it bypasses — refusing to remove a user's last
// active factor — protects a user from locking themselves out, and here the
// credential that factor points at is being deleted regardless. Leaving the
// factor behind would not keep the user logged in; it would only leave a factor
// resolving at login to a passkey that no longer exists.
func (s *webAuthnFactorWriteThrough) revokeFactor(ctx context.Context, cred *xkmswebauthn.Credential) error {
	lookupKey := webAuthnCredentialLookupKey(cred.ID)
	userID, err := xkmswebauthn.UserIDFromHandle(cred.UserID)
	if err != nil {
		return &WebAuthnFactorRevokeError{CredentialID: lookupKey, Cause: err}
	}
	list, err := s.factors.ListAuthFactors(ctx, &qrdbtransport.AuthFactorListRequest{UserID: userID})
	if err != nil {
		return &WebAuthnFactorRevokeError{CredentialID: lookupKey, Cause: err}
	}
	if list == nil {
		// No factor listing means no factor to revoke; a transport that
		// answers (nil, nil) must not panic the deletion.
		return nil
	}
	for _, factor := range list.Factors {
		if factor.Type != string(qrdbtransport.FactorTypeFIDO2) {
			continue
		}
		if string(factor.Credential) != lookupKey {
			continue
		}
		if factor.RevokedAt != nil {
			continue
		}
		if _, rErr := s.factors.RemoveAuthFactor(ctx, &qrdbtransport.AuthFactorRemoveRequest{
			FactorID: factor.ID,
			Force:    true,
		}); rErr != nil {
			return &WebAuthnFactorRevokeError{CredentialID: lookupKey, Cause: rErr}
		}
	}
	return nil
}

// enrollFactor writes the fido2 auth factor the login resolver reads. The
// Credential bytes are the base64url credential id — byte-identical to the
// lookup key the login proof reduces to (cmd/quicbaas proveCanonicalFIDO2 and
// go-xkms's WebAuthn factor verifier both key on that exact encoding).
func (s *webAuthnFactorWriteThrough) enrollFactor(
	ctx context.Context,
	userID uint64,
	lookupKey string,
	cred *xkmswebauthn.Credential,
	attestation *AttestationResult,
) error {
	recorder := s.attestations()
	if recorder == nil {
		return &WebAuthnAttestationRecorderUnavailableError{TenantID: s.tenantID}
	}
	var aaguidHex string
	if len(cred.Authenticator.AAGUID) > 0 {
		aaguidHex = hex.EncodeToString(cred.Authenticator.AAGUID)
	}
	// The proven level and model travel as a VerifiedAttestation, which only
	// this in-process path can produce; the server writes them under its
	// reserved attest_* keys. They are deliberately NOT in Metadata: a caller
	// that put them there would be refused, and this path is not a caller.
	return recorder.AddAttested(ctx, userID, &qrdbsdk.AuthFactor{
		UserID:     userID,
		Type:       qrdbsdk.AuthFactorFIDO2,
		Label:      s.factorLabel(ctx, cred.UserID),
		Credential: []byte(lookupKey),
		Metadata:   s.factorMetadata(cred),
		CreatedBy:  webAuthnFactorCreatedBy,
	}, VerifiedAttestationFor(attestation, aaguidHex))
}

// evaluateAttestation runs the assurance gate for the registrant this
// credential belongs to.
//
// The registrant's email is resolved through the xkms user store the ceremony
// itself keys on, so the gate asks about the SAME account the credential is
// about to be bound to. A failed resolution is passed to the gate as an empty
// email, which the admin resolver cannot resolve, which the gate reads as
// administrative — the fail-safe direction.
func (s *webAuthnFactorWriteThrough) evaluateAttestation(
	ctx context.Context,
	cred *xkmswebauthn.Credential,
) (*AttestationResult, error) {
	return s.gate.Evaluate(
		ctx,
		s.tenantID,
		s.registrantEmail(ctx, cred.UserID),
		cred.AttestationType,
		cred.Authenticator.AAGUID,
	)
}

// registrantEmail resolves the registrant's email from the WebAuthn user
// handle. It returns the empty string when the account cannot be read; see
// evaluateAttestation for why that is safe.
func (s *webAuthnFactorWriteThrough) registrantEmail(ctx context.Context, handle []byte) string {
	user, err := s.users.GetByID(ctx, handle)
	if err != nil || user == nil {
		return ""
	}
	return user.Email()
}

// factorLabel resolves a human-readable label for the enrolled factor from the
// registrant's WebAuthn profile. The CLI passes --label as the ceremony's
// display name, so this preserves the operator-chosen label; the browser flows
// yield the user's display name. Label is descriptive metadata, so a lookup
// failure degrades to an empty label rather than failing the ceremony.
func (s *webAuthnFactorWriteThrough) factorLabel(ctx context.Context, handle []byte) string {
	user, err := s.users.GetByID(ctx, handle)
	if err != nil || user == nil {
		return ""
	}
	if name := user.DisplayName(); name != "" {
		return name
	}
	return user.Email()
}

// factorMetadata projects the non-secret authenticator context onto the factor
// record: the relying-party tenant, the AAGUID (authenticator model), the
// attestation format, the reported transports, and the PROVEN attestation
// level. It never carries key material — the credential public key stays in the
// xkms credential store.
//
// attest_level is the field that makes a stored credential self-describing.
// Before it, a factor row said "fido2" and nothing more, so login could not
// tell a YubiKey from a virtual authenticator and had to assume. It is written
// on every enrollment, including permissive ones, precisely so the permissive
// case is visible rather than indistinguishable.
func (s *webAuthnFactorWriteThrough) factorMetadata(
	cred *xkmswebauthn.Credential,
) map[string]string {
	meta := map[string]string{"tenant_id": s.tenantID}
	// The proven attestation level and the attested model are NOT here: they
	// live under qrdb's reserved attest_* keys, which only the server writes
	// (AttestationRecorder, fed from enrollFactor). The model used to be
	// written as the generic "aaguid", which no guard reserved — so a caller
	// could Add or Rotate a factor carrying an "aaguid" of their choosing and
	// produce a field indistinguishable from one a ceremony actually proved.
	// Attested facts belong in one namespace under one rule, and that rule is
	// now qrdb's.
	if cred.AttestationType != "" {
		meta["attestation_type"] = cred.AttestationType
	}
	if len(cred.Transport) > 0 {
		transports := make([]string, 0, len(cred.Transport))
		for _, t := range cred.Transport {
			transports = append(transports, string(t))
		}
		meta["transports"] = strings.Join(transports, ",")
	}
	return meta
}

// webAuthnCredentialLookupKey encodes a raw WebAuthn credential id into the
// canonical base64url form used as the qrdb fido2 factor lookup key. It is the
// SAME encoding the browser reports as rawId and the xkms credential store
// uses for its KV key segment, so enrollment and login agree byte for byte.
func webAuthnCredentialLookupKey(credentialID []byte) string {
	return base64.RawURLEncoding.EncodeToString(credentialID)
}

// isFactorAlreadyEnrolled reports whether err is qrdb's duplicate-credential
// signal. Both the transport-mapped shape (every network transport plus the
// embedded one) and the raw domain sentinel are recognised so idempotency does
// not depend on which qrdb transport the server was built with.
func isFactorAlreadyEnrolled(err error) bool {
	if err == nil {
		return false
	}
	var enrolled *qrdbtransport.FactorAlreadyEnrolledError
	if errors.As(err, &enrolled) {
		return true
	}
	return errors.Is(err, qrdbsdk.ErrFactorAlreadyExists)
}
