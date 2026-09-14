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
	return &PIVActivator{
		roots:          deps.Roots,
		intermediates:  deps.Intermediates,
		userStorage:    deps.UserStorage,
		inviteStore:    deps.InviteStore,
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
	// UserByCertThumbprint resolves natively; the fallback is the legacy hex
	// SHA-256 of the certificate DER, resolved via the broker-side legacy-PIV
	// index for the migration window. Binding BOTH lets a renewal (same key,
	// new cert) resolve via the stable SPKI thumbprint while persisted
	// legacy-only deployments keep resolving via the fallback.
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

	// 6. Transition user to active. The invite is consumed from the invite
	// store below, which is where it lives; there is no copy on the user record
	// to clear.
	user.Lifecycle = LifecycleActive
	user.SetPIVThumbprints(spkiThumbprint, legacyThumbprint)
	now := time.Now().UTC()
	user.Updated = now

	if err := a.userStorage.UpdateUser(ctx, user); err != nil {
		return &PIVActivationUpdateError{Cause: err}
	}

	// 7. Consume the invite. Redeem is the single-winner primitive: concurrent
	//    redemptions resolve to exactly one, so two racing activations cannot
	//    both succeed. The earlier Used check is an early-out for the common
	//    case, not the enforcement point — that lives here.
	if _, err := a.inviteStore.Redeem(ctx, tenantID, token); err != nil {
		// The user is already activated; log but don't fail the caller.
		a.logAudit(ctx, invite.TenantID, invite.UserID, "piv_activate", "warning", map[string]interface{}{
			"warning": "failed to consume invite after activation: " + err.Error(),
		})
	} else {
		a.logAudit(ctx, invite.TenantID, invite.UserID, "piv_activate", "success", map[string]interface{}{
			"piv_spki_thumbprint": spkiThumbprint,
			"piv_cert_thumbprint": legacyThumbprint,
		})
	}

	return nil
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
func (a *PIVActivator) logAudit(ctx context.Context, tenantID, userID, action, outcome string, details map[string]interface{}) {
	if a.auditLogger == nil {
		return
	}
	event := &AuditEvent{
		TenantID:  tenantID,
		EventType: AuditEventUserManagement,
		Category:  AuditCategoryAuthentication,
		Action:    action,
		Resource:  &AuditResource{Type: "user", ID: userID},
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

// PIVActivationUpdateError is returned when the user update fails during activation.
type PIVActivationUpdateError struct {
	Cause error
}

// Error implements the error interface.
func (e *PIVActivationUpdateError) Error() string {
	return "failed to update user during piv activation: " + e.Cause.Error()
}

// Unwrap returns the underlying cause.
func (e *PIVActivationUpdateError) Unwrap() error {
	return e.Cause
}
