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

// Package iam provides Identity and Access Management functionality for QuicBaaS.
package iam

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"strconv"
	"strings"
	"time"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"
)

// PIVActivatorDependencies contains required dependencies for PIVActivator.
type PIVActivatorDependencies struct {
	// Roots is the trusted CA pool used to verify the PIV client certificate chain.
	Roots *x509.CertPool

	// Intermediates is an optional intermediate CA pool.
	Intermediates *x509.CertPool

	// UserStorage is used to fetch and update users.
	UserStorage UserStorage

	// InviteStore is used to look up and consume invite tokens.
	InviteStore InviteStore

	// LoginFactors enrolls the activated certificate as the user's mtls login
	// factor, the record the unified login ceremony resolves. Required.
	LoginFactors *CertLoginFactorEnroller

	// ActivationClaims arbitrates activations of the same user that present
	// different invite tokens. Required.
	ActivationClaims *UserActivationClaims

	// AuditLogger records activation events (may be nil; activations are logged best-effort).
	AuditLogger AuditLogger

	// TrustedIssuers is an optional list of Subject DN strings that the certificate
	// issuer must match (case-insensitive per RFC 4514). When empty, any issuer
	// that validates against the CA pool is accepted.
	TrustedIssuers []string
}

// PIVActivator handles the PIV certificate activation flow.
// It verifies the certificate chain, binds the thumbprint to the user, and
// transitions the user from pending to active.
type PIVActivator struct {
	roots          *x509.CertPool
	intermediates  *x509.CertPool
	userStorage    UserStorage
	inviteStore    InviteStore
	loginFactors   *CertLoginFactorEnroller
	claims         *UserActivationClaims
	auditLogger    AuditLogger
	trustedIssuers []string
}

// NewPIVActivator creates a new PIVActivator with the provided dependencies.
func NewPIVActivator(deps PIVActivatorDependencies) (*PIVActivator, error) {
	if deps.Roots == nil {
		return nil, &PIVActivatorConfigError{Reason: "trusted root pool is required"}
	}
	if deps.UserStorage == nil {
		return nil, &PIVActivatorConfigError{Reason: "user storage is required"}
	}
	if deps.InviteStore == nil {
		return nil, &PIVActivatorConfigError{Reason: "invite store is required"}
	}
	if deps.LoginFactors == nil {
		return nil, &PIVActivatorConfigError{Reason: "login factor enroller is required"}
	}
	if deps.ActivationClaims == nil {
		return nil, &PIVActivatorConfigError{Reason: "activation claims are required"}
	}
	return &PIVActivator{
		roots:          deps.Roots,
		intermediates:  deps.Intermediates,
		userStorage:    deps.UserStorage,
		inviteStore:    deps.InviteStore,
		loginFactors:   deps.LoginFactors,
		claims:         deps.ActivationClaims,
		auditLogger:    deps.AuditLogger,
		trustedIssuers: deps.TrustedIssuers,
	}, nil
}

// Activate processes a PIV activation request.
//
// It validates the invite token, verifies the certificate chain (ClientAuth EKU required),
// fetches the pending user, transitions the user to active, stores the certificate thumbprint,
// and consumes the invite.
//
// tenantID is required because invitations live in that tenant's own store: a
// token issued for one tenant is simply absent from another, so isolation is
// structural rather than a check that could be forgotten. A token replayed
// against the wrong tenant is indistinguishable from one that never existed.
func (a *PIVActivator) Activate(ctx context.Context, tenantID, token string, certPEM []byte) error {
	if tenantID == "" {
		return ErrInviteNotFound
	}

	// 1. Look up the invite WITHOUT consuming it. Validation runs first
	//    because the expensive, failure-prone work (chain verification, user
	//    lookup) must not burn a single-use token when it fails: an operator
	//    presenting the wrong certificate should be able to retry with the
	//    right one rather than having to be re-invited.
	invite, err := a.inviteStore.Lookup(ctx, tenantID, token)
	if err != nil {
		return err // typed: ErrInviteNotFound
	}
	if invite.Used {
		return ErrInviteUsed
	}
	if time.Now().After(invite.ExpiresAt) {
		return ErrInviteExpired
	}

	// 2. Decode and parse the PEM certificate.
	block, _ := pem.Decode(certPEM)
	if block == nil {
		return &PIVCertDecodeError{Reason: "no PEM block found"}
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return &PIVCertDecodeError{Reason: "failed to parse certificate: " + err.Error()}
	}

	// 3. Verify the certificate chain (ClientAuth EKU enforced inside VerifyChain).
	if err := xkmssdk.PIVVerifyChain(cert, a.roots, a.intermediates); err != nil {
		return ErrPIVCertRejected
	}

	// 3a. Defense-in-depth: check the issuing CA's Subject DN against the
	// operator-configured TrustedIssuers allowlist. An empty list means "any
	// issuer that validates against the CA pool is accepted" (backward-compatible).
	if err := a.checkTrustedIssuer(cert); err != nil {
		return err
	}

	// 4. Compute both PIV thumbprints. The primary is the canonical SPKI
	// thumbprint (SHA-256 over SubjectPublicKeyInfo, base64url) that qrdb's
	// UserByCertThumbprint resolves; the fallback is the legacy hex SHA-256 of
	// the certificate DER, which qrdb's same cert-thumbprint index resolves.
	// Binding BOTH lets a renewal (same key, new cert) resolve via the stable
	// SPKI thumbprint while persisted legacy-only deployments keep resolving
	// via the fallback.
	spkiThumbprint := qrdbsdk.ComputeSPKIThumbprint(cert)
	legacyThumbprint := xkmssdk.PIVThumbprintSHA256(cert)

	// 5. Fetch the pending user.
	user, err := a.userStorage.GetUser(ctx, invite.TenantID, invite.UserID)
	if err != nil {
		return err
	}
	if user.Lifecycle != LifecyclePending {
		return ErrUserNotPending
	}

	// Steps 5a-8 run in the order check -> claim -> redeem -> enroll ->
	// update, chosen so a factor exists only for the one activation that won
	// both the user and the invite:
	//
	//   - The user claim arbitrates activations of the same user. A pending
	//     user can hold several live invites (a retried provisioning mints
	//     another), and each token is single-use only on its own, so two
	//     activations with different tokens would both win their redemption.
	//     The claim is a store-decided create-if-absent, taken before anything
	//     is written; a loser returns ErrUserNotPending having written nothing.
	//   - Redeem is the commit point for the invite. It is the store's
	//     single-winner primitive for the token. A loser returns a typed error
	//     and releases its claim.
	//   - Only the winner enrolls. Two activations presenting the same
	//     certificate share one factor (enrollment is idempotent), so if
	//     losers enrolled, a loser that created the factor would revoke the
	//     one the winner adopted. Enrolling after the commit point means no
	//     loser ever holds a factor to revoke, whichever certificate it had.
	//   - The ownership check before the redeem keeps the common user error,
	//     a certificate already enrolled for another user, retryable: the
	//     invite stays unspent. A factor store failure after the redeem spends
	//     the invite, and the user must be re-invited.
	//   - Only the winner writes the user. If that write fails, the factor
	//     this call created is revoked; the invite is already spent. A
	//     failure whose write landed anyway (a lost response) is reconciled
	//     from the persisted user instead, and the activation succeeds.
	//   - A successful activation completes the claim, so no other invite of
	//     the user can activate it unless an administrator returns the user to
	//     pending. A failed one releases the claim, so the user can be
	//     re-invited.
	//
	// A factor that existed before this call (the same certificate already
	// enrolled for this user) is never revoked here: it is not this call's.

	// 5a. Reject a certificate enrolled as another user's login factor while
	// the invite is still unspent. Enrollment re-checks it after the redeem.
	if err := a.checkLoginFactorOwner(ctx, invite.TenantID, invite.UserID, cert); err != nil {
		return &PIVActivationLoginFactorError{UserID: invite.UserID, Cause: err}
	}

	// 5b. Claim the user before anything is written.
	claim, err := a.claims.Claim(ctx, invite.TenantID, invite.UserID, user.Updated)
	if err != nil {
		return err
	}

	// The claim's lease outlives this bound, so the claim cannot expire and be
	// taken over while this activation is still writing.
	workCtx, cancel := context.WithTimeout(ctx, userActivationWorkTimeout)
	defer cancel()
	if err := a.commitActivation(workCtx, tenantID, token, invite, user, cert); err != nil {
		return a.releaseClaim(ctx, invite, claim, err)
	}
	a.completeClaim(ctx, invite, claim, user.Updated)

	a.logAudit(ctx, invite.TenantID, invite.UserID, "piv_activate", "success", map[string]interface{}{
		"piv_spki_thumbprint": spkiThumbprint,
		"piv_cert_thumbprint": legacyThumbprint,
	})
	return nil
}

// commitActivation runs the writes of an activation that holds the user's
// claim: it redeems the invite, enrolls the certificate as the user's login
// factor, and binds the certificate to the user as it marks the user active.
func (a *PIVActivator) commitActivation(ctx context.Context, tenantID, token string, invite *InviteRecord, user *User, cert *x509.Certificate) error {
	// 6. Consume the invite. The earlier Used check is an early-out for the
	//    common case, not the enforcement point — that lives here.
	if _, err := a.inviteStore.Redeem(ctx, tenantID, token); err != nil {
		return &PIVActivationRedeemError{UserID: invite.UserID, Cause: err}
	}

	// 7. Enroll the certificate as the user's mtls login factor: the
	// thumbprint binding alone does not let the unified login ceremony
	// resolve it.
	factor, err := a.enrollLoginFactor(ctx, invite.TenantID, invite.UserID, cert)
	if err != nil {
		return &PIVActivationLoginFactorError{UserID: invite.UserID, Cause: err}
	}

	// 8. Transition user to active. The invite was consumed from the invite
	// store above, which is where it lives; there is no copy on the user record
	// to clear.
	user.Lifecycle = LifecycleActive
	user.SetPIVThumbprints(qrdbsdk.ComputeSPKIThumbprint(cert), xkmssdk.PIVThumbprintSHA256(cert))
	// go-xkms's certificate login admits the bound certificate only when the
	// policy permits mtls; piv stays (CA Decision 103).
	user.permitCertificateLogin()
	user.Updated = time.Now().UTC()
	if err := a.userStorage.UpdateUser(ctx, user); err != nil {
		if a.activationPersisted(ctx, invite, cert) {
			a.logAudit(ctx, invite.TenantID, invite.UserID, "piv_activate", "failure", map[string]interface{}{
				"error":     "user update reported failure but the activation persisted: " + err.Error(),
				"factor_id": factor.FactorID,
			})
			return nil
		}
		return a.revokeCreatedFactor(ctx, invite, factor, &PIVActivationUpdateError{Cause: err})
	}
	return nil
}

// activationPersisted reports whether a user update that reported failure
// landed anyway: the user is active and bound to this certificate. Revoking
// the factor of such a user would leave it active with a spent invite and no
// usable mTLS login, and no further activation could repair it because the
// user is no longer pending.
//
// A failed re-read reports false, so the factor is revoked. The outcome is
// then unknown either way, and revoking is the side that grants nothing: it
// can strand an activated user, which an administrator repairs by returning
// the user to pending, but it never leaves a login factor on an account that
// stayed pending.
func (a *PIVActivator) activationPersisted(ctx context.Context, invite *InviteRecord, cert *x509.Certificate) bool {
	persisted, err := RereadUserAfterFailedWrite(ctx, a.userStorage, invite.TenantID, invite.UserID)
	if err != nil || persisted == nil {
		return false
	}
	return persisted.Lifecycle == LifecycleActive && persisted.HasPIVThumbprint(qrdbsdk.ComputeSPKIThumbprint(cert))
}

// releaseClaim gives up the claim of a failed activation so the user can be
// re-invited, and returns the activation's error. The release is detached
// from the request, as the factor revocation is: a client that disconnects
// must not leave the user claimed until the lease expires. A release that
// fails is audited; the lease still frees the user.
func (a *PIVActivator) releaseClaim(ctx context.Context, invite *InviteRecord, claim *UserActivationClaim, activationErr error) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), inviteRollbackTimeout)
	defer cancel()
	if err := claim.Release(ctx); err != nil {
		a.logAudit(ctx, invite.TenantID, invite.UserID, "piv_activate", "failure", map[string]interface{}{
			"error": "failed to release the activation claim of a failed activation: " + err.Error(),
		})
	}
	return activationErr
}

// completeClaim records the successful activation on its claim. activatedAt is
// the Updated time written to the active user. The user is already active, so
// a failure is audited rather than returned: the lease-bound claim still
// blocks every activation until it expires, and the user is no longer pending
// afterwards.
func (a *PIVActivator) completeClaim(ctx context.Context, invite *InviteRecord, claim *UserActivationClaim, activatedAt time.Time) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), inviteRollbackTimeout)
	defer cancel()
	if err := claim.Complete(ctx, activatedAt); err != nil {
		a.logAudit(ctx, invite.TenantID, invite.UserID, "piv_activate", "failure", map[string]interface{}{
			"error": "failed to complete the activation claim: " + err.Error(),
		})
	}
}

// revokeCreatedFactor undoes a failed activation's login factor and returns
// the error the caller sees. A factor the activation did not create is left in
// place. When the revocation itself fails, the returned error says a residual
// factor may exist, and the failure is audit-logged with the factor id so an
// operator can revoke it.
//
// The revocation runs detached from the request's cancellation, bounded by
// inviteRollbackTimeout: the failure being undone may itself be the request's
// cancellation, and a canceled revocation would leave the factor usable.
func (a *PIVActivator) revokeCreatedFactor(ctx context.Context, invite *InviteRecord, factor CertLoginFactorEnrollResult, activationErr error) error {
	if !factor.Created {
		return activationErr
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), inviteRollbackTimeout)
	defer cancel()
	err := a.loginFactors.Remove(qrdbsdk.ContextWithTenant(ctx, invite.TenantID), factor.FactorID)
	if err == nil {
		return activationErr
	}
	a.logAudit(ctx, invite.TenantID, invite.UserID, "piv_activate", "failure", map[string]interface{}{
		"error":     "failed to revoke the login factor of a failed activation: " + err.Error(),
		"factor_id": factor.FactorID,
	})
	return &PIVActivationFactorCleanupError{
		UserID:     invite.UserID,
		FactorID:   factor.FactorID,
		Cause:      err,
		Activation: activationErr,
	}
}

// checkLoginFactorOwner rejects cert when it is already enrolled as another
// user's login factor. Like enrollment, it binds the invited tenant into the
// context the factor store routes by.
func (a *PIVActivator) checkLoginFactorOwner(ctx context.Context, tenantID string, userID uint64, cert *x509.Certificate) error {
	return a.loginFactors.CheckOwner(qrdbsdk.ContextWithTenant(ctx, tenantID), userID, cert)
}

// enrollLoginFactor enrolls cert as userID's mtls login factor, recording the
// tenant and the invite activation as its provenance. The factor store routes
// by the context tenant, and the activation runs on a pre-authentication
// context that need not carry one, so the invited tenant is bound into it.
func (a *PIVActivator) enrollLoginFactor(ctx context.Context, tenantID string, userID uint64, cert *x509.Certificate) (CertLoginFactorEnrollResult, error) {
	label := "PIV certificate"
	if cn := cert.Subject.CommonName; cn != "" {
		label += " (" + cn + ")"
	}
	return a.loginFactors.Enroll(qrdbsdk.ContextWithTenant(ctx, tenantID), CertLoginFactorEnrollment{
		UserID:      userID,
		Certificate: cert,
		Label:       label,
		Metadata: map[string]string{
			"binding_source": pivInviteBindingSource,
			"tenant_id":      tenantID,
			"broker_user_id": strconv.FormatUint(userID, 10),
			"cert_subject":   cert.Subject.String(),
			"cert_serial":    cert.SerialNumber.String(),
			"cert_not_after": cert.NotAfter.UTC().Format(time.RFC3339),
		},
	})
}

// checkTrustedIssuer verifies that the certificate's issuer Subject DN is in the
// operator-configured TrustedIssuers list. Returns nil when TrustedIssuers is
// empty (permissive default) or the issuer DN matches one of the allowed values
// using case-insensitive comparison per RFC 4514.
func (a *PIVActivator) checkTrustedIssuer(cert *x509.Certificate) error {
	if len(a.trustedIssuers) == 0 {
		return nil
	}
	issuerDN := cert.Issuer.String()
	issuerLower := strings.ToLower(issuerDN)
	for _, allowed := range a.trustedIssuers {
		if strings.EqualFold(allowed, issuerLower) {
			return nil
		}
	}
	return NewPIVCertIssuerRejectedError(issuerDN, a.trustedIssuers)
}

// logAudit emits a best-effort audit event if an AuditLogger is configured.
func (a *PIVActivator) logAudit(ctx context.Context, tenantID string, userID uint64, action, outcome string, details map[string]interface{}) {
	if a.auditLogger == nil {
		return
	}
	event := &AuditEvent{
		TenantID:  tenantID,
		EventType: AuditEventUserManagement,
		Category:  AuditCategoryAuthentication,
		Action:    action,
		Resource:  &AuditResource{Type: "user", ID: strconv.FormatUint(userID, 10)},
		Details:   details,
	}
	switch outcome {
	case "success":
		event.Outcome = AuditOutcomeSuccess
	default:
		event.Outcome = AuditOutcomeFailure
	}
	a.auditLogger.LogAsync(event)
}

// PIVActivatorConfigError is returned when PIVActivator is misconfigured.
type PIVActivatorConfigError struct {
	Reason string
}

// Error implements the error interface.
func (e *PIVActivatorConfigError) Error() string {
	return "piv activator configuration error: " + e.Reason
}

// PIVCertDecodeError is returned when the provided PEM cannot be decoded or parsed.
type PIVCertDecodeError struct {
	Reason string
}

// Error implements the error interface.
func (e *PIVCertDecodeError) Error() string {
	return "piv certificate decode error: " + e.Reason
}

// PIVActivationUpdateError is returned when the user update fails after the
// activation won its invite. The invite is spent and the user stays pending,
// so the user must be re-invited; the login factor the activation created has
// been revoked.
type PIVActivationUpdateError struct {
	Cause error
}

// Error implements the error interface.
func (e *PIVActivationUpdateError) Error() string {
	return "failed to update user during piv activation (invite consumed, re-invite required): " + e.Cause.Error()
}

// Unwrap returns the underlying cause.
func (e *PIVActivationUpdateError) Unwrap() error {
	return e.Cause
}

// pivInviteBindingSource marks a login factor enrolled by PIV invite
// activation.
const pivInviteBindingSource = "piv_invite_activation"

// PIVActivationLoginFactorError is returned when the activated certificate
// cannot be enrolled as the user's mtls login factor. The user stays pending
// and the invite unspent, so the activation can be retried.
type PIVActivationLoginFactorError struct {
	UserID uint64
	Cause  error
}

// Error implements the error interface.
func (e *PIVActivationLoginFactorError) Error() string {
	return "failed to enroll the piv certificate as a login factor for user " + strconv.FormatUint(e.UserID, 10) + ": " + e.Cause.Error()
}

// Unwrap returns the underlying cause.
func (e *PIVActivationLoginFactorError) Unwrap() error {
	return e.Cause
}

// PIVActivationRedeemError is returned when an activation does not win its
// invite: a concurrent activation consumed it first (Cause wraps
// ErrInviteUsed), or the invite store failed. Nothing was committed: the user
// is unchanged and the login factor the activation created has been revoked.
type PIVActivationRedeemError struct {
	UserID uint64
	Cause  error
}

// Error implements the error interface.
func (e *PIVActivationRedeemError) Error() string {
	return "piv activation for user " + strconv.FormatUint(e.UserID, 10) + " did not consume its invite: " + e.Cause.Error()
}

// Unwrap returns the underlying cause.
func (e *PIVActivationRedeemError) Unwrap() error {
	return e.Cause
}

// PIVActivationFactorCleanupError is returned when an activation failed and
// the login factor it created could not be revoked. A residual factor may
// exist and still authenticate the certificate: FactorID names it so an
// operator can revoke it.
type PIVActivationFactorCleanupError struct {
	UserID   uint64
	FactorID string

	// Cause is the revocation failure.
	Cause error

	// Activation is the failure that made the revocation necessary.
	Activation error
}

// Error implements the error interface.
func (e *PIVActivationFactorCleanupError) Error() string {
	return "piv activation for user " + strconv.FormatUint(e.UserID, 10) + " failed (" + e.Activation.Error() +
		") and its login factor " + e.FactorID + " could not be revoked, so a residual factor may exist: " + e.Cause.Error()
}

// Unwrap returns both the revocation failure and the activation failure.
func (e *PIVActivationFactorCleanupError) Unwrap() []error {
	return []error{e.Cause, e.Activation}
}
