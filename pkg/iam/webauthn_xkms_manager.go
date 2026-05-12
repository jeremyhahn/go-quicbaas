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
//   - The ceremony session id returned by xkms travels through the
//     manager keyed by (tenantID, registrationID) so quicbaas's REST
//     handlers continue to use a "user_id" key in their request bodies
//     for the begin → finish handoff.
//
// The manager keeps a small in-memory map of (tenantID, lookup key) to
// xkms session id. The map is concurrency-safe via sync.Map and lives
// for the lifetime of the daemon; ceremony sessions self-expire on
// xkms's side after the configured timeout.
type XKMSWebAuthnManager struct {
	factory *WebAuthnServiceFactory

	// regSessions maps tenantID:userID -> xkms session id for the
	// registration ceremony.
	regSessions sync.Map // map[string]string

	// loginSessions maps tenantID:email -> xkms session id for the
	// login ceremony.
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

// BeginRegistration starts a WebAuthn registration ceremony for the
// (tenantID, userID) pair, where userID is the registrant's email. The
// xkms ceremony session id is stashed internally so FinishRegistration
// can correlate the begin/finish handoff via the same userID.
func (m *XKMSWebAuthnManager) BeginRegistration(ctx context.Context, tenantID, userID string) (map[string]interface{}, error) {
	svc, err := m.factory.ServiceFor(tenantID)
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
	svc, err := m.factory.ServiceFor(tenantID)
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
// identified by email within the tenant.
func (m *XKMSWebAuthnManager) BeginLogin(ctx context.Context, tenantID, email string) (map[string]interface{}, error) {
	svc, err := m.factory.ServiceFor(tenantID)
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
	m.loginSessions.Store(m.regKey(tenantID, email), sessionID)
	return marshalToMap(options)
}

// FinishLogin completes the assertion ceremony and surfaces the
// authenticated user as an iam.User. The email key is mandatory at
// FinishLogin time because quicbaas's REST handler does not receive
// it back from the browser; we recover it from the credential's user
// handle (xkms returns the matching User) and look up the email from
// xkms.
func (m *XKMSWebAuthnManager) FinishLogin(ctx context.Context, tenantID string, credential map[string]interface{}) (*User, error) {
	svc, err := m.factory.ServiceFor(tenantID)
	if err != nil {
		return nil, err
	}
	parsed, err := parseCredentialAssertionMap(credential)
	if err != nil {
		return nil, err
	}

	// Locate the corresponding xkms ceremony session id. We probe by
	// iterating active login sessions for this tenant — the login UI
	// may not echo the email back on FinishLogin (discoverable
	// credentials flow), so the safest correlation is by user handle
	// resolution after FinishLogin runs.
	var sessionID string
	prefix := tenantID + ":"
	m.loginSessions.Range(func(k, v any) bool {
		ks := k.(string)
		if len(ks) >= len(prefix) && ks[:len(prefix)] == prefix {
			sessionID = v.(string)
			return false
		}
		return true
	})
	if sessionID == "" {
		return nil, &WebAuthnFactoryConfigError{Field: "login session not found"}
	}

	_, xkmsUser, err := svc.FinishLogin(ctx, sessionID, nil, parsed)
	if err != nil {
		return nil, err
	}
	// Best-effort cleanup of the matched session.
	m.loginSessions.Range(func(k, v any) bool {
		if v.(string) == sessionID {
			m.loginSessions.Delete(k)
			return false
		}
		return true
	})

	// Map xkms's User abstraction back to iam.User. xkms's email is the
	// qrdb username; everything else is per-tenant context.
	return &User{
		ID:       string(xkmsUser.WebAuthnID()),
		Email:    xkmsUser.WebAuthnName(),
		Name:     xkmsUser.WebAuthnDisplayName(),
		TenantID: tenantID,
	}, nil
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
