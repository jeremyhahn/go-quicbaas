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
	"encoding/json"
	"log/slog"
	"sync"

	"github.com/go-webauthn/webauthn/protocol"

	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"
)

// XKMSWebAuthnManager implements the multi-tenant WebAuthn surface that
// quicbaas presents to its REST and gRPC handlers. It is a thin domain
// adapter over per-tenant xkms WebAuthn services produced by
// WebAuthnServiceFactory.
//
// Method semantics (mirroring service.WebAuthnManager):
//
//   - BeginRegistration / FinishRegistration take a tenantID and a
//     userID. The userID here is the registrant's email address — the
//     same lookup key xkms's UserStore uses internally
//     (xkms.UserStore.GetByEmail).
//   - BeginLogin / FinishLogin run the assertion ceremony. FinishLogin
//     resolves the user via xkms and surfaces it as an iam.User so
//     downstream OIDC issuance and audit hooks consume the same domain
//     type they did before the migration.
//   - The registration ceremony session id travels through the manager
//     keyed by (tenantID, userID) because FinishRegistration receives the
//     userID back from the browser.
//   - The login ceremony session id is keyed by (tenantID, challenge).
//     The WebAuthn challenge is the per-ceremony random nonce that the
//     authenticator signs over; the browser echoes it verbatim in the
//     assertion's collected client data at FinishLogin time. Keying by
//     challenge resolves the EXACT begin→finish session even when many
//     users in the same tenant authenticate concurrently. The previous
//     tenant-prefix first-match scan was a concurrency and security bug:
//     two concurrent same-tenant logins could be cross-wired onto the
//     wrong session/credential.
//   - The login ceremony is USER-IDENTIFIED, not discoverable. The web
//     login is email-first: the caller supplies an email, the manager
//     resolves the user, and BeginLogin emits an assertion whose
//     allowCredentials lists exactly that user's registered credential
//     descriptors. This drives a deterministic, scriptable get() in the
//     browser instead of the native passkey account-picker that a
//     discoverable ceremony would trigger. FinishLogin re-resolves the
//     same user (its WebAuthn handle is stashed alongside the session at
//     begin time) and verifies the assertion against that user, so the
//     begin→finish session type is consistent end to end.
//
// The manager keeps a small in-memory map of (tenantID, challenge) to
// xkms session id. The map is concurrency-safe via sync.Map and lives
// for the lifetime of the daemon; ceremony sessions self-expire on
// xkms's side after the configured timeout.
type XKMSWebAuthnManager struct {
	factory *WebAuthnServiceFactory

	// userResolver resolves the canonical broker iam.User (carrying the
	// external string ID and the qrdb-resolved role set) by email after a
	// successful assertion. xkms's WebAuthnUser models ONLY the webauthn
	// handle/name/display name — it has no roles and its WebAuthnID() is the
	// raw 8-byte big-endian uint64 user handle, which is NOT a usable JWT
	// subject. FinishLogin therefore re-resolves the authenticated user
	// through this canonical store so the issued token carries the correct
	// string subject and role claims. Stored behind a small accessor so the
	// production wiring can late-bind it (the user store is constructed in the
	// same server-lifecycle phase as the manager).
	userResolver WebAuthnUserResolver

	// regSessions maps tenantID:userID -> xkms session id for the
	// registration ceremony.
	regSessions sync.Map // map[string]string

	// loginSessions maps tenantID:challenge -> loginHandoff for the login
	// ceremony. The challenge is the per-ceremony WebAuthn nonce, so the
	// key uniquely identifies a single begin→finish handoff even under
	// concurrent same-tenant logins. The handoff carries both the xkms
	// session id and the resolved user's WebAuthn handle so FinishLogin
	// can verify the assertion against the SAME user the ceremony began
	// for (the user-identified flow), not a discoverable nil user.
	loginSessions sync.Map // map[string]loginHandoff
}

// loginHandoff is the per-ceremony record stashed at BeginLogin time and
// recovered at FinishLogin time. It binds the xkms ceremony session id to
// the WebAuthn handle of the user the email-first ceremony resolved, so
// the finish step verifies the assertion against that exact user.
type loginHandoff struct {
	sessionID  string
	userHandle []byte
}

// WebAuthnUserResolver is the narrow seam FinishLogin uses to resolve the
// canonical broker iam.User after a successful assertion. It is satisfied by
// iam.UserStorage (GetUserByEmail), so the production wiring hands the same
// qrdb-backed user store the rest of the IAM stack resolves against. Keeping
// the interface to a single method avoids the manager taking a dependency on
// the full UserStorage surface.
type WebAuthnUserResolver interface {
	// GetUserByEmail returns the canonical user for the given email within a
	// tenant, carrying the qrdb Snowflake id and the qrdb-resolved role set.
	GetUserByEmail(ctx context.Context, tenantID, email string) (*User, error)
}

// SetUserResolver wires the canonical user resolver used by FinishLogin to map
// the asserted webauthn identity back to a full iam.User. It mirrors
// SetAAGUIDAllowlist's late-binding setter shape.
func (m *XKMSWebAuthnManager) SetUserResolver(resolver WebAuthnUserResolver) {
	m.userResolver = resolver
}

// NewXKMSWebAuthnManager constructs the manager. The factory must be
// non-nil; see NewWebAuthnServiceFactory for construction.
func NewXKMSWebAuthnManager(factory *WebAuthnServiceFactory) (*XKMSWebAuthnManager, error) {
	if factory == nil {
		return nil, &WebAuthnFactoryConfigError{Field: "factory"}
	}
	return &XKMSWebAuthnManager{factory: factory}, nil
}

// Factory returns the underlying factory so callers can mount the xkms
// webauthn HTTP handlers per tenant.
func (m *XKMSWebAuthnManager) Factory() *WebAuthnServiceFactory {
	return m.factory
}

// regKey scopes a registration handoff record by tenant and user.
func (m *XKMSWebAuthnManager) regKey(tenantID, userID string) string {
	return tenantID + ":" + userID
}

// loginKey scopes a login handoff record by tenant and the per-ceremony
// WebAuthn challenge. The challenge is a high-entropy random nonce, so the
// composite key uniquely identifies one begin→finish handoff and never
// collides across concurrent logins in the same tenant.
func (m *XKMSWebAuthnManager) loginKey(tenantID, challenge string) string {
	return tenantID + ":" + challenge
}

// BeginRegistration starts a WebAuthn registration ceremony for the
// (tenantID, userID) pair, where userID is the registrant's email. The
// xkms ceremony session id is stashed internally so FinishRegistration
// can correlate the begin/finish handoff via the same userID.
func (m *XKMSWebAuthnManager) BeginRegistration(ctx context.Context, tenantID, userID string) (map[string]interface{}, error) {
	svc, err := m.factory.ServiceFor(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	options, sessionID, err := svc.BeginRegistration(ctx, userID, userID)
	if err != nil {
		return nil, err
	}
	m.regSessions.Store(m.regKey(tenantID, userID), sessionID)
	return marshalToMap(options)
}

// BeginInviteRegistration starts the registration ceremony for an existing
// PENDING account, authorized by an invitation the caller already validated.
// The account is resolved by email; go-xkms refuses unless it is pending and
// holds no credential (go-xkms#1294 part B). The session is stashed under the
// same (tenant, email) key BeginRegistration uses, so FinishRegistration
// completes either ceremony.
func (m *XKMSWebAuthnManager) BeginInviteRegistration(ctx context.Context, tenantID, email string) (map[string]interface{}, error) {
	svc, err := m.factory.ServiceFor(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	options, sessionID, err := svc.BeginInviteRegistration(ctx, email)
	if err != nil {
		return nil, err
	}
	m.regSessions.Store(m.regKey(tenantID, email), sessionID)
	return marshalToMap(options)
}

// FinishRegistration completes the registration ceremony by resolving
// the stashed xkms session id and forwarding the parsed credential.
//
// It returns what the assurance gate PROVED about the authenticator, so a
// caller that mints a session for the credential it just enrolled can describe
// it accurately. The result is unforgeable — only verifiers in this package
// produce one — so handing it to a handler is safe in a way handing it a
// string never was.
//
// A nil result with a nil error means the ceremony completed but nothing about
// the authenticator's storage was established: the caller must then not claim
// hardware.
func (m *XKMSWebAuthnManager) FinishRegistration(ctx context.Context, tenantID, userID string, credential map[string]interface{}) (*AttestationResult, error) {
	svc, err := m.factory.ServiceFor(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	v, ok := m.regSessions.Load(m.regKey(tenantID, userID))
	if !ok {
		return nil, &WebAuthnFactoryConfigError{Field: "registration session not found"}
	}
	sessionID, ok := v.(string)
	if !ok {
		return nil, &WebAuthnFactoryConfigError{Field: "registration session not found"}
	}
	parsed, err := parseCredentialCreationMap(credential)
	if err != nil {
		return nil, err
	}
	// The attestation assurance gate — AAGUID allowlist included — runs inside
	// the credential store's write-through (webauthn_factor_writethrough.go),
	// not here.
	//
	// It used to run here, on parsed.Response.AttestationObject.AuthData, and
	// that placement was the defect rather than the fix: at this point the
	// attestation statement has NOT been verified, so the AAGUID is a value the
	// browser supplied and nothing more. A ceremony with no attestation reports
	// an empty AAGUID, which the allowlist could not test, so the gate passed
	// exactly the authenticators it existed to refuse. Moving it behind
	// go-webauthn's verification also puts it on the live registration route,
	// which dispatches into xkms directly and never reaches this method.
	//
	// What DOES belong here is the attestation trust path: the x5c chain lives
	// in the attestation object, which the credential store never sees. The
	// result rides the context to the gate. A failure is not fatal here —
	// enrollment policy is the gate's decision — so the reason is logged and
	// the context simply carries no verified chain.
	ctx = m.verifyAttestationChain(ctx, tenantID, userID, parsed)
	ctx, recorder := contextWithAttestationRecorder(ctx)
	if _, _, err := svc.FinishRegistration(ctx, sessionID, parsed); err != nil {
		return nil, err
	}
	m.regSessions.Delete(m.regKey(tenantID, userID))
	return recorder.get(), nil
}

// verifyAttestationChain verifies the registration's attestation trust path and
// returns a context carrying the result.
//
// A verification failure yields the original context — no verified chain — and
// a log line, rather than an error. The refusal belongs to the assurance gate,
// which knows whether THIS registrant must prove hardware; failing here would
// apply the administrator's rule to every tenant user.
func (m *XKMSWebAuthnManager) verifyAttestationChain(
	ctx context.Context,
	tenantID, userID string,
	parsed *protocol.ParsedCredentialCreationData,
) context.Context {
	verified, err := m.factory.VerifyAttestationChain(ctx, parsed)
	if err != nil {
		slog.Default().Debug("webauthn registration: attestation trust path not verified",
			"tenant_id", tenantID,
			"user_id", userID,
			"error", err)
	}
	// VerifyAttestationChain CLEARS any proof on the context when it fails, so
	// the returned context is the right one on both paths. Returning the
	// incoming ctx on failure would preserve a planted or stale chain.
	return verified
}

// BeginLogin starts a WebAuthn assertion ceremony for the user
// identified by email within the tenant. The xkms ceremony session id and
// the resolved user's WebAuthn handle are stashed keyed by
// (tenantID, challenge); FinishLogin recovers the exact session by matching
// the challenge the authenticator signs over, so the begin→finish handoff
// is correct under concurrent same-tenant logins.
//
// The ceremony is USER-IDENTIFIED: the manager resolves the user by email
// and forwards the user's WebAuthn handle to xkms, which emits an assertion
// whose allowCredentials enumerates exactly that user's registered
// credentials. The browser then performs a deterministic get() against those
// descriptors instead of the native passkey account-picker a discoverable
// ceremony would surface. xkms fails the begin closed with
// ErrWebAuthnNoCredentials when the user has no registered passkey, and the
// GetUserByEmail lookup fails closed for an unknown user.
func (m *XKMSWebAuthnManager) BeginLogin(ctx context.Context, tenantID, email string) (map[string]interface{}, error) {
	svc, err := m.factory.ServiceFor(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	user, err := svc.GetUserByEmail(ctx, email)
	if err != nil {
		return nil, err
	}
	userHandle := user.WebAuthnID()
	// User-identified ceremony: xkms populates allowCredentials from the
	// user's registered credentials and fails closed with
	// ErrWebAuthnNoCredentials when the user has none.
	options, sessionID, err := svc.BeginLogin(ctx, userHandle)
	if err != nil {
		return nil, err
	}
	challenge := options.Response.Challenge.String()
	if challenge == "" {
		return nil, &WebAuthnLoginSessionError{Reason: "xkms returned a login ceremony with an empty challenge"}
	}
	m.loginSessions.Store(m.loginKey(tenantID, challenge), loginHandoff{
		sessionID:  sessionID,
		userHandle: userHandle,
	})
	return marshalToMap(options)
}

// FinishLogin completes the assertion ceremony and surfaces the
// authenticated user as an iam.User.
//
// The exact begin→finish session is resolved by the WebAuthn challenge
// echoed in the assertion's collected client data — the same value the
// authenticator signed over. This is collision-free across concurrent
// same-tenant ceremonies. A missing or unknown challenge fails closed
// before any xkms dispatch, so a forged or expired/temp session handle
// cannot be coerced onto another user's in-flight ceremony.
//
// The ceremony is user-identified: the WebAuthn handle stashed at begin
// time is forwarded to xkms so the assertion is verified against the SAME
// user the ceremony began for, consistent with the allowCredentials list
// emitted by BeginLogin.
func (m *XKMSWebAuthnManager) FinishLogin(ctx context.Context, tenantID string, credential map[string]interface{}) (*User, error) {
	svc, err := m.factory.ServiceFor(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	parsed, err := parseCredentialAssertionMap(credential)
	if err != nil {
		return nil, err
	}

	handoff, key, err := m.resolveLoginSession(tenantID, parsed)
	if err != nil {
		return nil, err
	}

	_, xkmsUser, err := svc.FinishLogin(ctx, handoff.sessionID, handoff.userHandle, parsed)
	if err != nil {
		return nil, err
	}
	// Cleanup of the exact session consumed by this ceremony.
	m.loginSessions.Delete(key)

	// The asserted email (xkms's webauthn name == the qrdb username) is the
	// lookup key into the canonical broker store.
	return m.resolveAuthenticatedUser(ctx, tenantID, xkmsUser.WebAuthnName())
}

// resolveAuthenticatedUser re-resolves the authenticated user through the
// canonical broker store so the returned iam.User carries the external string
// ID and the qrdb-resolved role set that token issuance keys on. xkms's
// WebAuthnUser abstraction exposes ONLY the webauthn handle/name/display name:
// its WebAuthnID() is the raw 8-byte big-endian uint64 user handle (mojibake as
// a JWT subject) and it has no roles. Mapping it straight to iam.User produced a
// token with an unusable subject and zero permissions.
//
// The resolver is the same qrdb-backed UserStorage the rest of the IAM stack
// uses, so the user the broker created is the same record login and the PDP
// resolve against. It fails closed when no resolver is wired or the user cannot
// be resolved, rather than surfacing a half-populated user.
func (m *XKMSWebAuthnManager) resolveAuthenticatedUser(ctx context.Context, tenantID, email string) (*User, error) {
	if m.userResolver == nil {
		return nil, &WebAuthnUserResolverError{Reason: "canonical user resolver is not configured"}
	}
	user, err := m.userResolver.GetUserByEmail(ctx, tenantID, email)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, &WebAuthnUserResolverError{Reason: "canonical user resolver returned nil user"}
	}
	// The resolved record already belongs to tenantID: the lookup was scoped to
	// it. Stamping the tenant on afterwards was how a global index was made to
	// look tenant-safe, and it would have masked a cross-tenant hit rather than
	// refusing it.
	return user, nil
}

// resolveLoginSession maps a parsed assertion back to the exact login
// handoff stored at BeginLogin time, keyed by (tenantID, challenge). It
// returns the handoff (xkms session id + user WebAuthn handle) and the
// composite cache key (so the caller can delete the precise entry after a
// successful finish), or a typed WebAuthnLoginSessionError when the
// challenge is absent or unknown.
func (m *XKMSWebAuthnManager) resolveLoginSession(tenantID string, parsed *protocol.ParsedCredentialAssertionData) (loginHandoff, string, error) {
	if parsed == nil {
		return loginHandoff{}, "", &WebAuthnLoginSessionError{Reason: "nil assertion"}
	}
	challenge := parsed.Response.CollectedClientData.Challenge
	if challenge == "" {
		return loginHandoff{}, "", &WebAuthnLoginSessionError{Reason: "assertion has no challenge"}
	}
	key := m.loginKey(tenantID, challenge)
	v, ok := m.loginSessions.Load(key)
	if !ok {
		return loginHandoff{}, "", &WebAuthnLoginSessionError{Reason: "no login session matches the assertion challenge"}
	}
	handoff, ok := v.(loginHandoff)
	if !ok {
		return loginHandoff{}, "", &WebAuthnLoginSessionError{Reason: "no login session matches the assertion challenge"}
	}
	return handoff, key, nil
}

// marshalToMap converts a typed protocol structure to a JSON-decoded
// map[string]interface{}. The REST API serialises options as JSON, so
// the round-trip preserves all browser-visible fields.
func marshalToMap(v interface{}) (map[string]interface{}, error) {
	blob, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var out map[string]interface{}
	if err := json.Unmarshal(blob, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// parseCredentialCreationMap normalises the browser-supplied credential
// map into the protocol's typed representation. The quicbaas REST API
// has historically accepted credential bodies as map[string]interface{}
// to keep the JSON wire format stable; this helper bridges to xkms's
// strict type via a JSON round-trip and the go-webauthn library's
// own parser.
func parseCredentialCreationMap(m map[string]interface{}) (*protocol.ParsedCredentialCreationData, error) {
	blob, err := json.Marshal(m)
	if err != nil {
		return nil, err
	}
	return protocol.ParseCredentialCreationResponseBytes(blob)
}

// parseCredentialAssertionMap mirrors parseCredentialCreationMap for
// the login (assertion) ceremony.
func parseCredentialAssertionMap(m map[string]interface{}) (*protocol.ParsedCredentialAssertionData, error) {
	blob, err := json.Marshal(m)
	if err != nil {
		return nil, err
	}
	return protocol.ParseCredentialRequestResponseBytes(blob)
}

// Compile-time interface guard: xkms.User implements WebAuthnUser.
var _ xkmssdk.WebAuthnUser = (*xkmssdk.WebAuthnDefaultUser)(nil)

// Compile-time interface guard: the canonical UserStorage satisfies the narrow
// WebAuthnUserResolver seam FinishLogin uses, so the production wiring can hand
// the qrdb-backed user store straight to SetUserResolver.
var _ WebAuthnUserResolver = (UserStorage)(nil)
