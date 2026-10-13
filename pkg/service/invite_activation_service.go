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
	"time"

	"github.com/google/uuid"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// InviteActivationService redeems the one-time invitation a pending user is
// created with: it reads the invite's public metadata and activates the
// account through a FIDO2 passkey ceremony or a PIV certificate.
//
// Every verb is pre-authentication — the invite token IS the credential — so
// the token is resolved inside the tenant's own invite store and a token
// issued for one tenant is simply absent from another. Absent, expired and
// spent invites all surface as errors iam.IsInviteUnusable recognises, so a
// transport can collapse them into one opaque answer and never become an
// existence oracle. Every transport (REST, gRPC and the Unix socket that
// delegates to it, embedded) resolves through this one implementation.
type InviteActivationService interface {
	// GetInvite returns the publicly visible metadata of a usable invite,
	// used to populate the activation UI.
	GetInvite(ctx context.Context, tenantID, token string) (*InviteMetadata, error)

	// ActivateFIDO2Begin starts the WebAuthn registration ceremony that
	// enrols the invited account's first passkey and returns the
	// CredentialCreationOptions for navigator.credentials.create().
	ActivateFIDO2Begin(ctx context.Context, tenantID, token string) (map[string]interface{}, error)

	// ActivateFIDO2Finish completes the ceremony, activates the account and
	// consumes the invite.
	ActivateFIDO2Finish(ctx context.Context, tenantID, token string, credential map[string]interface{}) (*InviteActivation, error)

	// ActivatePIV activates the account with a PIV certificate (PEM) whose
	// chain the server's PIV trust pool verifies, and consumes the invite.
	ActivatePIV(ctx context.Context, tenantID, token string, certPEM []byte) (*InviteActivation, error)
}

// InviteMetadata is the publicly visible part of a pending invite.
type InviteMetadata struct {
	UserID      string
	Email       string
	Name        string
	AuthMethods []string
	ExpiresAt   time.Time
}

// InviteActivation is the outcome of a successful activation.
type InviteActivation struct {
	// UserID is the activated user. It is empty when PIV activation
	// succeeded but the consumed invite could no longer be resolved to read
	// it back; the activation itself committed.
	UserID string

	// Warning records a non-fatal irregularity behind an otherwise
	// successful activation; see InviteActivationWarningReconciled.
	Warning string
}

// InviteActivationWarningReconciled is reported when the activation write
// returned an error but the re-read user is active: the activation happened,
// so the invite was consumed rather than left redeemable.
const InviteActivationWarningReconciled = "activation write reported an error but the account is active; the invite was consumed"

// defaultInviteAuthMethods is reported for an invited user whose record names
// no activation method: a passkey is the method every deployment offers.
var defaultInviteAuthMethods = []string{"webauthn"}

// Invite activation typed errors.
var (
	// ErrInviteActivationTenantRequired is returned when the tenant scope is
	// empty.
	ErrInviteActivationTenantRequired = &errors.QBError{
		Code: errors.ErrInvalidInput, Op: "InviteActivationService", Err: errInviteActivationTenantRequired}

	// ErrInviteActivationTokenRequired is returned when the invite token is
	// empty.
	ErrInviteActivationTokenRequired = &errors.QBError{
		Code: errors.ErrInvalidInput, Op: "InviteActivationService", Err: errInviteActivationTokenRequired}

	// ErrInviteActivationCredentialRequired is returned when a FIDO2 finish
	// carries no credential.
	ErrInviteActivationCredentialRequired = &errors.QBError{
		Code: errors.ErrInvalidInput, Op: "InviteActivationService", Err: errInviteActivationCredentialRequired}

	// ErrInviteActivationCertRequired is returned when a PIV activation
	// carries no certificate.
	ErrInviteActivationCertRequired = &errors.QBError{
		Code: errors.ErrInvalidInput, Op: "InviteActivationService", Err: errInviteActivationCertRequired}

	// ErrInviteActivationWebAuthnNotConfigured is returned when no WebAuthn
	// manager is wired, so no passkey ceremony can run.
	ErrInviteActivationWebAuthnNotConfigured = &errors.QBError{
		Code: errors.ErrInternal, Op: "InviteActivationService", Err: errInviteActivationWebAuthnNotConfigured}

	// ErrInviteActivationPIVNotConfigured is returned when no PIV activator
	// (trust pool) is configured on this server.
	ErrInviteActivationPIVNotConfigured = &errors.QBError{
		Code: errors.ErrNotImplemented, Op: "InviteActivationService", Err: errInviteActivationPIVNotConfigured}

	// ErrInviteActivationIdentityStoreUnavailable is returned when the
	// identity store the account lives in is not initialised.
	ErrInviteActivationIdentityStoreUnavailable = &errors.QBError{
		Code: errors.ErrInternal, Op: "InviteActivationService", Err: errInviteActivationIdentityStoreUnavailable}

	// ErrInviteActivationUserNoEmail is returned when the invited user has no
	// email: the WebAuthn ceremony is keyed by it.
	ErrInviteActivationUserNoEmail = &errors.QBError{
		Code: errors.ErrInternal, Op: "InviteActivationService", Err: errInviteActivationUserNoEmail}
)

var (
	errInviteActivationTenantRequired           = stderrors.New("tenant_id is required")
	errInviteActivationTokenRequired            = stderrors.New("token is required")
	errInviteActivationCredentialRequired       = stderrors.New("credential is required")
	errInviteActivationCertRequired             = stderrors.New("cert_pem is required")
	errInviteActivationWebAuthnNotConfigured    = stderrors.New("WebAuthn manager not available")
	errInviteActivationPIVNotConfigured         = stderrors.New("PIV activation is not configured on this server")
	errInviteActivationIdentityStoreUnavailable = stderrors.New("identity store not initialized")
	errInviteActivationUserNoEmail              = stderrors.New("invite user has no email")
)

// ErrInviteActivationInviteStoreNotWired is returned when no invite store is
// wired. It wraps iam.ErrInviteStoreUnavailable so callers that test for the
// iam condition recognise it.
var ErrInviteActivationInviteStoreNotWired = errors.Wrap(iam.ErrInviteStoreUnavailable,
	errors.ErrInternal, "InviteActivationService.inviteStore")

// InviteActivationSource supplies the IAM components invite activation
// composes. *quicBaaS satisfies it; each getter is resolved per call, so the
// service works whether or not IAM was up when it was constructed.
type InviteActivationSource interface {
	GetInviteStore(ctx context.Context) (iam.InviteStore, error)
	GetIdentityStore(ctx context.Context) (*iam.IdentityStore, error)
	GetWebAuthnManager(ctx context.Context) (WebAuthnManager, error)
	GetPIVActivator(ctx context.Context) (*iam.PIVActivator, error)
	GetAuditLogger(ctx context.Context) (iam.AuditLogger, error)
}

// inviteActivationService composes the invite store, identity store, WebAuthn
// manager and PIV activator into the activation verbs.
type inviteActivationService struct {
	source InviteActivationSource
}

// NewInviteActivationService returns the invite activation service over the
// supplied component source.
func NewInviteActivationService(source InviteActivationSource) InviteActivationService {
	return &inviteActivationService{source: source}
}

// GetInvite implements InviteActivationService.
func (s *inviteActivationService) GetInvite(ctx context.Context, tenantID, token string) (*InviteMetadata, error) {
	_, record, err := s.usableInvite(ctx, tenantID, token)
	if err != nil {
		return nil, err
	}

	meta := &InviteMetadata{UserID: record.UserID, ExpiresAt: record.ExpiresAt}
	// The display fields are best effort: a user that cannot be read still
	// has a usable invite, and the activation UI falls back to the token.
	if store, storeErr := s.source.GetIdentityStore(ctx); storeErr == nil && store != nil {
		if user, userErr := store.GetUser(ctx, record.TenantID, record.UserID); userErr == nil && user != nil {
			meta.Email = user.Email
			meta.Name = user.Name
			meta.AuthMethods = user.AuthMethods
		}
	}
	if len(meta.AuthMethods) == 0 {
		meta.AuthMethods = defaultInviteAuthMethods
	}
	return meta, nil
}

// ActivateFIDO2Begin implements InviteActivationService.
func (s *inviteActivationService) ActivateFIDO2Begin(ctx context.Context, tenantID, token string) (map[string]interface{}, error) {
	_, record, err := s.usableInvite(ctx, tenantID, token)
	if err != nil {
		return nil, err
	}
	mgr, err := s.webAuthnManager(ctx)
	if err != nil {
		return nil, err
	}
	email, err := s.inviteEmail(ctx, record)
	if err != nil {
		return nil, err
	}

	// The invite's account already exists and is pending: its first passkey
	// is enrolled through the invitation ceremony, the one unauthenticated
	// path go-xkms allows for a pre-existing account (go-xkms#1294).
	return mgr.BeginInviteRegistration(ctx, record.TenantID, email)
}

// ActivateFIDO2Finish implements InviteActivationService.
func (s *inviteActivationService) ActivateFIDO2Finish(ctx context.Context, tenantID, token string, credential map[string]interface{}) (*InviteActivation, error) {
	inviteStore, record, err := s.usableInvite(ctx, tenantID, token)
	if err != nil {
		return nil, err
	}
	// Checked after the invite so an unusable token gets the same opaque
	// answer whatever the body carried.
	if credential == nil {
		return nil, ErrInviteActivationCredentialRequired
	}
	mgr, err := s.webAuthnManager(ctx)
	if err != nil {
		return nil, err
	}
	// The ceremony session is keyed by the email ActivateFIDO2Begin used.
	email, err := s.inviteEmail(ctx, record)
	if err != nil {
		return nil, err
	}

	// The proven attestation is not used here: invite activation does not
	// mint a session, it transitions the account to active and the user logs
	// in afterwards. The gate has already refused the ceremony if this
	// account's policy required hardware.
	if _, err := mgr.FinishRegistration(ctx, record.TenantID, email, credential); err != nil {
		return nil, err
	}

	// Transition the user to active strictly before the invite is consumed,
	// so a one-time token is never spent on an account that stayed pending.
	// Failing here does NOT make the ceremony repeatable (xkms persisted the
	// credential and a later begin excludes it); what it preserves is the
	// invite, which keeps the account activatable by another authenticator
	// or an admin.
	store, err := s.identityStore(ctx)
	if err != nil {
		return nil, err
	}
	activeState := iam.LifecycleActive
	activation := &InviteActivation{UserID: record.UserID}
	if _, err := store.UpdateUser(ctx, record.TenantID, record.UserID, iam.UpdateUserOptions{
		Lifecycle: &activeState,
	}); err != nil {
		s.audit(ctx, record.TenantID, "invite.activate",
			&iam.AuditResource{Type: "user", ID: record.UserID}, err)

		// An error does not prove the write did not land: a commit whose
		// response was lost reports failure on a user that IS now active, and
		// refusing then would leave a live one-time token on an activated
		// account. Let the persisted state decide; anything but active
		// (including a failed re-read) keeps the fail-closed refusal.
		if !userIsActive(ctx, store, record.TenantID, record.UserID) {
			return nil, err
		}
		activation.Warning = InviteActivationWarningReconciled
	}

	// A one-time token that cannot be recorded as spent stays redeemable, so
	// a failed redeem is reported rather than treated as success.
	if _, err := inviteStore.Redeem(ctx, tenantID, token); err != nil {
		s.audit(ctx, record.TenantID, "invite.mark_used",
			&iam.AuditResource{Type: "invite", ID: record.UserID}, err)
		return nil, err
	}
	return activation, nil
}

// ActivatePIV implements InviteActivationService.
func (s *inviteActivationService) ActivatePIV(ctx context.Context, tenantID, token string, certPEM []byte) (*InviteActivation, error) {
	if tenantID == "" {
		return nil, ErrInviteActivationTenantRequired
	}
	if token == "" {
		return nil, ErrInviteActivationTokenRequired
	}
	if len(certPEM) == 0 {
		return nil, ErrInviteActivationCertRequired
	}
	activator, err := s.source.GetPIVActivator(ctx)
	if err != nil || activator == nil {
		return nil, ErrInviteActivationPIVNotConfigured
	}

	// PIVActivator resolves the invite before it parses the certificate and
	// consumes it last, so the lookup, validation and redeem are its own.
	if err := activator.Activate(ctx, tenantID, token, certPEM); err != nil {
		return nil, err
	}

	// Activation committed; reading the user id back from the consumed
	// invite is cosmetic, so a failure leaves it empty rather than failing
	// a request whose side effect already happened.
	activation := &InviteActivation{}
	if inviteStore, storeErr := s.inviteStore(ctx); storeErr == nil {
		if record, lookupErr := inviteStore.Lookup(ctx, tenantID, token); lookupErr == nil && record != nil {
			activation.UserID = record.UserID
		}
	}
	return activation, nil
}

// usableInvite validates the scope and resolves the token to an invite that
// is neither spent nor expired. A store that returns a spent or expired
// record instead of refusing gets the same unusable error as one that
// refuses, so the two shapes are indistinguishable from outside.
func (s *inviteActivationService) usableInvite(ctx context.Context, tenantID, token string) (iam.InviteStore, *iam.InviteRecord, error) {
	if tenantID == "" {
		return nil, nil, ErrInviteActivationTenantRequired
	}
	if token == "" {
		return nil, nil, ErrInviteActivationTokenRequired
	}
	inviteStore, err := s.inviteStore(ctx)
	if err != nil {
		return nil, nil, err
	}
	record, err := inviteStore.Lookup(ctx, tenantID, token)
	if err != nil {
		return nil, nil, err
	}
	if record.Used {
		return nil, nil, iam.ErrInviteUsed
	}
	if time.Now().After(record.ExpiresAt) {
		return nil, nil, iam.ErrInviteExpired
	}
	return inviteStore, record, nil
}

// inviteStore resolves the wired invite store.
func (s *inviteActivationService) inviteStore(ctx context.Context) (iam.InviteStore, error) {
	store, err := s.source.GetInviteStore(ctx)
	if err != nil {
		return nil, err
	}
	if store == nil {
		return nil, ErrInviteActivationInviteStoreNotWired
	}
	return store, nil
}

// identityStore resolves the initialised identity store.
func (s *inviteActivationService) identityStore(ctx context.Context) (*iam.IdentityStore, error) {
	store, err := s.source.GetIdentityStore(ctx)
	if err != nil {
		return nil, err
	}
	if store == nil {
		return nil, ErrInviteActivationIdentityStoreUnavailable
	}
	return store, nil
}

// webAuthnManager resolves the wired WebAuthn manager.
func (s *inviteActivationService) webAuthnManager(ctx context.Context) (WebAuthnManager, error) {
	mgr, err := s.source.GetWebAuthnManager(ctx)
	if err != nil || mgr == nil {
		return nil, ErrInviteActivationWebAuthnNotConfigured
	}
	return mgr, nil
}

// inviteEmail resolves the invited user's email, the key go-xkms's WebAuthn
// ceremony is addressed by.
func (s *inviteActivationService) inviteEmail(ctx context.Context, record *iam.InviteRecord) (string, error) {
	store, err := s.source.GetIdentityStore(ctx)
	if err != nil || store == nil {
		return "", ErrInviteActivationUserNoEmail
	}
	user, err := store.GetUser(ctx, record.TenantID, record.UserID)
	if err != nil || user == nil || user.Email == "" {
		return "", ErrInviteActivationUserNoEmail
	}
	return user.Email, nil
}

// audit records a failed activation step. It is best effort: activation never
// fails because the audit log is unavailable.
func (s *inviteActivationService) audit(ctx context.Context, tenantID, action string, resource *iam.AuditResource, cause error) {
	logger, err := s.source.GetAuditLogger(ctx)
	if err != nil || logger == nil {
		return
	}
	logger.LogAsync(&iam.AuditEvent{
		ID:        uuid.New().String(),
		TenantID:  tenantID,
		Timestamp: time.Now().UTC(),
		EventType: iam.AuditEventUserManagement,
		Category:  iam.AuditCategorySecurity,
		Action:    action,
		Outcome:   iam.AuditOutcomeFailure,
		Resource:  resource,
		Details:   map[string]interface{}{"error": cause.Error()},
	})
}

// userIsActive re-reads the user and reports whether it is active. A failed
// read is not active.
func userIsActive(ctx context.Context, store *iam.IdentityStore, tenantID, userID string) bool {
	user, err := store.GetUser(ctx, tenantID, userID)
	if err != nil || user == nil {
		return false
	}
	return user.Lifecycle == iam.LifecycleActive
}
