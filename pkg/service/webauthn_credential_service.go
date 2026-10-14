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

package service

import (
	"context"
	stderrors "errors"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// WebAuthnCredentialService is the administrative surface over a user's
// registered WebAuthn authenticators: see what is stored, and remove it.
//
// It exists as a composition service rather than as per-transport handler code
// because the operation has two halves that must not drift apart. The verbs
// are addressed by IAM user id, the way every other user-nested admin verb is,
// but the credential store is keyed by (tenant, email) — the key the
// registration ceremony uses. Translating between the two is the whole job, and
// a transport that got the translation subtly wrong would silently operate on
// the wrong account. Every transport (REST, gRPC and the Unix socket that
// delegates to it, embedded, MCP) resolves through this one implementation.
type WebAuthnCredentialService interface {
	// ListCredentials returns the authenticators registered to the user. A
	// user who has never registered a passkey has none, which is an empty
	// slice rather than an error.
	ListCredentials(ctx context.Context, tenantID string, userID uint64) ([]iam.WebAuthnCredentialSummary, error)

	// DeleteCredential removes one of the user's authenticators by its
	// base64url credential id. The credential is verified to belong to that
	// user before anything is deleted.
	DeleteCredential(ctx context.Context, tenantID string, userID uint64, credentialID string) error

	// DeleteCredentials removes every authenticator registered to the user
	// and reports how many were removed.
	DeleteCredentials(ctx context.Context, tenantID string, userID uint64) (int, error)
}

// WebAuthn credential service typed errors.
var (
	// ErrWebAuthnCredentialTenantRequired is returned when the tenant scope
	// is empty.
	ErrWebAuthnCredentialTenantRequired = &errors.QBError{
		Code: errors.ErrInvalidInput, Op: "WebAuthnCredentialService", Err: errWebAuthnCredentialTenantRequired}

	// ErrWebAuthnCredentialUserRequired is returned when the user scope is
	// empty.
	ErrWebAuthnCredentialUserRequired = &errors.QBError{
		Code: errors.ErrInvalidInput, Op: "WebAuthnCredentialService", Err: errWebAuthnCredentialUserRequired}

	// ErrWebAuthnCredentialNotConfigured is returned when the xkms-backed
	// multi-tenant WebAuthn manager is not wired (legacy manager or IAM
	// disabled), so there is no credential store to administer.
	ErrWebAuthnCredentialNotConfigured = &errors.QBError{
		Code: errors.ErrUnavailable, Op: "WebAuthnCredentialService", Err: errWebAuthnCredentialNotConfigured}

	// ErrWebAuthnCredentialNoEmail is returned when the user exists but
	// carries no email address. The credential store is keyed by email, so
	// such a user has no addressable credentials and the verb fails rather
	// than operating on an empty key.
	ErrWebAuthnCredentialNoEmail = &errors.QBError{
		Code: errors.ErrNotFound, Op: "WebAuthnCredentialService", Err: errWebAuthnCredentialNoEmail}
)

var (
	errWebAuthnCredentialTenantRequired = stderrors.New("tenant is required")
	errWebAuthnCredentialUserRequired   = stderrors.New("user is required")
	errWebAuthnCredentialNotConfigured  = stderrors.New("xkms-backed WebAuthn manager is not configured")
	errWebAuthnCredentialNoEmail        = stderrors.New("user has no email address to resolve WebAuthn credentials by")
)

// webAuthnCredentialIdentityGetter resolves the canonical identity store, used
// to translate the IAM user id the caller was authorized against into the email
// the credential store is keyed by. Satisfied by *quicBaaS.
type webAuthnCredentialIdentityGetter interface {
	GetIdentityStore(ctx context.Context) (*iam.IdentityStore, error)
}

// webAuthnCredentialService composes the identity store and the WebAuthn
// manager into the user-addressed credential verbs.
type webAuthnCredentialService struct {
	identity webAuthnCredentialIdentityGetter
	manager  webAuthnManagerGetter
}

// NewWebAuthnCredentialService returns the user-scoped WebAuthn credential
// administration service. Both getters are structural: *quicBaaS satisfies
// them, and a test can supply either half on its own.
func NewWebAuthnCredentialService(identity webAuthnCredentialIdentityGetter, manager webAuthnManagerGetter) WebAuthnCredentialService {
	return &webAuthnCredentialService{identity: identity, manager: manager}
}

// NewWebAuthnCredentialServiceFor builds the same service from already-resolved
// components. Callers that hold the identity store and WebAuthn manager
// directly — the gRPC IAM adapter, which is constructed from concrete IAM
// components rather than from the service graph — use this so they run the same
// implementation as the transports that resolve lazily, instead of re-deriving
// the user-id-to-email translation themselves.
//
// Either component may be nil; the verbs then report
// ErrWebAuthnCredentialNotConfigured, which is the same answer a deployment
// without WebAuthn gives on every other transport.
func NewWebAuthnCredentialServiceFor(store *iam.IdentityStore, manager WebAuthnManager) WebAuthnCredentialService {
	svc := &webAuthnCredentialService{}
	if store != nil {
		svc.identity = staticIdentityStore{store: store}
	}
	if manager != nil {
		svc.manager = staticWebAuthnManager{manager: manager}
	}
	return svc
}

// staticIdentityStore adapts an already-resolved identity store to the getter
// the service consumes.
type staticIdentityStore struct{ store *iam.IdentityStore }

// GetIdentityStore implements webAuthnCredentialIdentityGetter.
func (s staticIdentityStore) GetIdentityStore(context.Context) (*iam.IdentityStore, error) {
	return s.store, nil
}

// staticWebAuthnManager adapts an already-resolved WebAuthn manager to the
// getter the service consumes.
type staticWebAuthnManager struct{ manager WebAuthnManager }

// GetWebAuthnManager implements webAuthnManagerGetter.
func (s staticWebAuthnManager) GetWebAuthnManager(context.Context) (WebAuthnManager, error) {
	return s.manager, nil
}

// ListCredentials implements WebAuthnCredentialService.
func (s *webAuthnCredentialService) ListCredentials(ctx context.Context, tenantID string, userID uint64) ([]iam.WebAuthnCredentialSummary, error) {
	mgr, email, err := s.resolve(ctx, tenantID, userID)
	if err != nil {
		return nil, err
	}
	return mgr.ListCredentials(ctx, tenantID, email)
}

// DeleteCredential implements WebAuthnCredentialService.
func (s *webAuthnCredentialService) DeleteCredential(ctx context.Context, tenantID string, userID uint64, credentialID string) error {
	mgr, email, err := s.resolve(ctx, tenantID, userID)
	if err != nil {
		return err
	}
	return mgr.DeleteCredential(ctx, tenantID, email, credentialID)
}

// DeleteCredentials implements WebAuthnCredentialService.
func (s *webAuthnCredentialService) DeleteCredentials(ctx context.Context, tenantID string, userID uint64) (int, error) {
	mgr, email, err := s.resolve(ctx, tenantID, userID)
	if err != nil {
		return 0, err
	}
	return mgr.DeleteCredentials(ctx, tenantID, email)
}

// resolve validates the scope, translates the IAM user id into the email the
// credential store is keyed by, and returns the xkms-backed manager.
//
// The user lookup is tenant-scoped and happens before any credential access, so
// a caller can only reach the credentials of a user that exists in the tenant
// it named — the same subject the transport's authorization gate ran against.
func (s *webAuthnCredentialService) resolve(ctx context.Context, tenantID string, userID uint64) (*iam.XKMSWebAuthnManager, string, error) {
	if tenantID == "" {
		return nil, "", ErrWebAuthnCredentialTenantRequired
	}
	if userID == 0 {
		return nil, "", ErrWebAuthnCredentialUserRequired
	}
	if s.identity == nil || s.manager == nil {
		return nil, "", ErrWebAuthnCredentialNotConfigured
	}

	store, err := s.identity.GetIdentityStore(ctx)
	if err != nil {
		return nil, "", err
	}
	if store == nil {
		return nil, "", ErrWebAuthnCredentialNotConfigured
	}
	user, err := store.GetUser(ctx, tenantID, userID)
	if err != nil {
		return nil, "", err
	}
	if user == nil || user.Email == "" {
		return nil, "", ErrWebAuthnCredentialNoEmail
	}

	mgr, err := s.manager.GetWebAuthnManager(ctx)
	if err != nil {
		return nil, "", err
	}
	xkmsMgr, ok := mgr.(*iam.XKMSWebAuthnManager)
	if !ok || xkmsMgr == nil {
		return nil, "", ErrWebAuthnCredentialNotConfigured
	}
	return xkmsMgr, user.Email, nil
}
