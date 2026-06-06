//go:build iam

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
	"sync"

	"github.com/go-webauthn/webauthn/protocol"

	xkmswebauthn "github.com/jeremyhahn/go-xkms/pkg/webauthn"
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
//     users in the same tenant authenticate concurrently, and it works
//     for the discoverable-credential flow where the browser never echoes
//     an email back. The previous tenant-prefix first-match scan was a
//     concurrency and security bug: two concurrent same-tenant logins
//     could be cross-wired onto the wrong session/credential.
//
// The manager keeps a small in-memory map of (tenantID, challenge) to
// xkms session id. The map is concurrency-safe via sync.Map and lives
// for the lifetime of the daemon; ceremony sessions self-expire on
// xkms's side after the configured timeout.
type XKMSWebAuthnManager struct {
	factory *WebAuthnServiceFactory

	// regSessions maps tenantID:userID -> xkms session id for the
	// registration ceremony.
	regSessions sync.Map // map[string]string

	// loginSessions maps tenantID:challenge -> xkms session id for the
	// login ceremony. The challenge is the per-ceremony WebAuthn nonce, so
	// the key uniquely identifies a single begin→finish handoff even under
	// concurrent same-tenant logins.
	loginSessions sync.Map // map[string]string

	// aaguidAllowlist is the optional FIDO2 AAGUID enforcement gate.
	// Stored as an atomic-loaded value so SetAAGUIDAllowlist races
	// safely with concurrent ceremonies. Nil disables enforcement
	// entirely (xkms accepts any authenticator).
	aaguidAllowlist atomicAllowlist
}

// atomicAllowlist holds an *AAGUIDAllowlist behind a sync.RWMutex.
// We avoid sync/atomic on a typed pointer because it makes the code
// portable across pre-generic releases without resorting to unsafe.
type atomicAllowlist struct {
	mu        sync.RWMutex
	allowlist *AAGUIDAllowlist
}

// SetAAGUIDAllowlist swaps in a new allowlist. Pass nil to disable
// AAGUID enforcement.
func (m *XKMSWebAuthnManager) SetAAGUIDAllowlist(allowlist *AAGUIDAllowlist) {
	m.aaguidAllowlist.mu.Lock()
	m.aaguidAllowlist.allowlist = allowlist
	m.aaguidAllowlist.mu.Unlock()
}

// allowlist returns the current AAGUID allowlist, or nil when none is
// configured.
func (m *XKMSWebAuthnManager) allowlist() *AAGUIDAllowlist {
	m.aaguidAllowlist.mu.RLock()
	defer m.aaguidAllowlist.mu.RUnlock()
	return m.aaguidAllowlist.allowlist
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

// FinishRegistration completes the registration ceremony by resolving
// the stashed xkms session id and forwarding the parsed credential.
func (m *XKMSWebAuthnManager) FinishRegistration(ctx context.Context, tenantID, userID string, credential map[string]interface{}) error {
	svc, err := m.factory.ServiceFor(ctx, tenantID)
	if err != nil {
		return err
	}
	v, ok := m.regSessions.Load(m.regKey(tenantID, userID))
	if !ok {
		return &WebAuthnFactoryConfigError{Field: "registration session not found"}
	}
	sessionID := v.(string)
	parsed, err := parseCredentialCreationMap(credential)
	if err != nil {
		return err
	}
	if allowlist := m.allowlist(); allowlist != nil {
		aaguid := parsed.Response.AttestationObject.AuthData.AttData.AAGUID
		if aErr := allowlist.Check(aaguid); aErr != nil {
			return aErr
		}
	}
	if _, _, err := svc.FinishRegistration(ctx, sessionID, parsed); err != nil {
		return err
	}
	m.regSessions.Delete(m.regKey(tenantID, userID))
	return nil
}

// BeginLogin starts a WebAuthn assertion ceremony for the user
// identified by email within the tenant. The xkms ceremony session id is
// stashed keyed by (tenantID, challenge); FinishLogin recovers the exact
// session by matching the challenge the authenticator signs over, so the
// begin→finish handoff is correct under concurrent same-tenant logins.
func (m *XKMSWebAuthnManager) BeginLogin(ctx context.Context, tenantID, email string) (map[string]interface{}, error) {
	svc, err := m.factory.ServiceFor(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	user, err := svc.GetUserByEmail(ctx, email)
	if err != nil {
		return nil, err
	}
	options, sessionID, err := svc.BeginLogin(ctx, user.WebAuthnID())
	if err != nil {
		return nil, err
	}
	challenge := options.Response.Challenge.String()
	if challenge == "" {
		return nil, &WebAuthnLoginSessionError{Reason: "xkms returned a login ceremony with an empty challenge"}
	}
	m.loginSessions.Store(m.loginKey(tenantID, challenge), sessionID)
	return marshalToMap(options)
}

// FinishLogin completes the assertion ceremony and surfaces the
// authenticated user as an iam.User.
//
// The exact begin→finish session is resolved by the WebAuthn challenge
// echoed in the assertion's collected client data — the same value the
// authenticator signed over. This is collision-free across concurrent
// same-tenant ceremonies and works for the discoverable-credential flow
// where the browser never sends an email back. A missing or unknown
// challenge fails closed before any xkms dispatch, so a forged or
// expired/temp session handle cannot be coerced onto another user's
// in-flight ceremony.
func (m *XKMSWebAuthnManager) FinishLogin(ctx context.Context, tenantID string, credential map[string]interface{}) (*User, error) {
	svc, err := m.factory.ServiceFor(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	parsed, err := parseCredentialAssertionMap(credential)
	if err != nil {
		return nil, err
	}

	sessionID, key, err := m.resolveLoginSession(tenantID, parsed)
	if err != nil {
		return nil, err
	}

	_, xkmsUser, err := svc.FinishLogin(ctx, sessionID, nil, parsed)
	if err != nil {
		return nil, err
	}
	// Cleanup of the exact session consumed by this ceremony.
	m.loginSessions.Delete(key)

	// Map xkms's User abstraction back to iam.User. xkms's email is the
	// qrdb username; everything else is per-tenant context.
	return &User{
		ID:       string(xkmsUser.WebAuthnID()),
		Email:    xkmsUser.WebAuthnName(),
		Name:     xkmsUser.WebAuthnDisplayName(),
		TenantID: tenantID,
	}, nil
}

// resolveLoginSession maps a parsed assertion back to the exact xkms
// session id stored at BeginLogin time, keyed by (tenantID, challenge).
// It returns the session id and the composite cache key (so the caller can
// delete the precise entry after a successful finish), or a typed
// WebAuthnLoginSessionError when the challenge is absent or unknown.
func (m *XKMSWebAuthnManager) resolveLoginSession(tenantID string, parsed *protocol.ParsedCredentialAssertionData) (string, string, error) {
	if parsed == nil {
		return "", "", &WebAuthnLoginSessionError{Reason: "nil assertion"}
	}
	challenge := parsed.Response.CollectedClientData.Challenge
	if challenge == "" {
		return "", "", &WebAuthnLoginSessionError{Reason: "assertion has no challenge"}
	}
	key := m.loginKey(tenantID, challenge)
	v, ok := m.loginSessions.Load(key)
	if !ok {
		return "", "", &WebAuthnLoginSessionError{Reason: "no login session matches the assertion challenge"}
	}
	return v.(string), key, nil
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
var _ xkmswebauthn.User = (*xkmswebauthn.DefaultUser)(nil)
