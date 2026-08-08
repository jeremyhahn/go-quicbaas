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
	"strings"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"

	// go-xkms's WebAuthn store interfaces (CredentialStore, UserStore,
	// Credential) and the canonical handle<->qrdb-user-id codec
	// (UserIDFromHandle) are NOT re-exported by go-xkms/sdk/go — see
	// go-xkms/sdk/go/webauthn.go, which aliases only the service, config,
	// user and HTTP handler types. The write-through decorator must
	// implement the exact CredentialStore interface xkms's
	// WebAuthnServiceParams accepts, so this is a justified direct package
	// import (type/interface dependency only, no behavior is copied).
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

// WebAuthnFactorStoreConfigError reports a missing dependency for the
// registration write-through. It is returned at service-construction time so
// a misconfiguration surfaces at startup rather than mid-ceremony.
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

// webAuthnFactorWriteThrough decorates an xkms CredentialStore so a completed
// registration ceremony also enrolls the authenticator in qrdb's UserAuthFactor
// store — the store the unified login ceremony resolves against.
//
// The inner store is EMBEDDED, so every read/mutate method
// (GetByUserID, GetByCredentialID, Update, Delete, DeleteByUserID) is promoted
// unchanged; only Save is overridden.
type webAuthnFactorWriteThrough struct {
	xkmswebauthn.CredentialStore

	// users resolves the registrant so the enrolled factor carries a human
	// label (the WebAuthn display name, which the CLI sets from --label).
	users xkmswebauthn.UserStore

	// factors is the qrdb user-side auth-factor client — the SAME store
	// go-qrdb's LoginResolver reads through LookupByCredentialKey.
	factors qrdbtransport.UserAuthFactorClient

	// tenantID scopes the relying party this store belongs to. It is
	// recorded in the factor metadata; the factor itself binds to the qrdb
	// uint64 user id, which is already tenant-unique.
	tenantID string

	logger *slog.Logger
}

// Compile-time guard: the decorator satisfies the interface xkms's
// WebAuthnServiceParams.CredentialStore field expects.
var _ xkmswebauthn.CredentialStore = (*webAuthnFactorWriteThrough)(nil)

// newWebAuthnFactorWriteThrough wraps inner so registrations write through to
// the qrdb auth-factor store. Every dependency is required: a nil factor client
// would silently reproduce the split-store bug this decorator exists to close.
func newWebAuthnFactorWriteThrough(
	inner xkmswebauthn.CredentialStore,
	users xkmswebauthn.UserStore,
	factors qrdbtransport.UserAuthFactorClient,
	tenantID string,
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
		tenantID:        tenantID,
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

	// Snapshot before writing: only a credential WE created may be rolled
	// back if the factor enrollment fails.
	_, getErr := s.CredentialStore.GetByCredentialID(ctx, cred.ID)
	credentialExisted := getErr == nil

	if saveErr := s.CredentialStore.Save(ctx, cred); saveErr != nil {
		return saveErr
	}

	enrollErr := s.enrollFactor(ctx, userID, lookupKey, cred)
	if enrollErr == nil {
		return nil
	}
	if isFactorAlreadyEnrolled(enrollErr) {
		// Same authenticator re-running the ceremony: the login-resolvable
		// factor already exists, so the registration is complete.
		return nil
	}

	compensated := false
	if !credentialExisted {
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

// enrollFactor writes the fido2 auth factor the login resolver reads. The
// Credential bytes are the base64url credential id — byte-identical to the
// lookup key the login proof reduces to (cmd/quicbaas proveCanonicalFIDO2 and
// go-xkms's WebAuthn factor verifier both key on that exact encoding).
func (s *webAuthnFactorWriteThrough) enrollFactor(
	ctx context.Context,
	userID uint64,
	lookupKey string,
	cred *xkmswebauthn.Credential,
) error {
	_, err := s.factors.AddAuthFactor(ctx, &qrdbtransport.AuthFactorAddRequest{
		UserID:     userID,
		Type:       string(qrdbtransport.FactorTypeFIDO2),
		Label:      s.factorLabel(ctx, cred.UserID),
		Credential: []byte(lookupKey),
		Metadata:   s.factorMetadata(cred),
		CreatedBy:  webAuthnFactorCreatedBy,
	})
	return err
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
// attestation format, and the reported transports. It never carries key
// material — the credential public key stays in the xkms credential store.
func (s *webAuthnFactorWriteThrough) factorMetadata(cred *xkmswebauthn.Credential) map[string]string {
	meta := map[string]string{"tenant_id": s.tenantID}
	if len(cred.Authenticator.AAGUID) > 0 {
		meta["aaguid"] = hex.EncodeToString(cred.Authenticator.AAGUID)
	}
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
