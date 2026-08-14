//go:build rest

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

package rest

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"
	"time"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// PIVLogin handles POST /api/v1/auth/login/piv
//
// This endpoint performs mTLS-based PIV smartcard authentication. The client
// must present a valid TLS client certificate. The certificate's SHA-256
// thumbprint (hex of the cert DER) is matched against a registered user's
// PIV-attested cert bindings (user.CertBindings) via the PIV thumbprint index.
//
// Gate: the endpoint returns 501 when PIV login is not enabled in configuration.
// To enable, set IAM.PIVClientCert.Enabled = true and configure the TLS listener
// with ClientAuth: tls.RequestClientCert.
func (h *IAMHandler) PIVLogin(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Guard: reject when PIV login is not enabled in operator config.
	if !h.server.config.PIVClientCert.Enabled {
		RespondIAMError(w, r, http.StatusUnauthorized,
			NewIAMAPIError(ErrPIVLoginDisabled, "PIV login is disabled; set iam.piv_login.enabled: true to enable"))
		return
	}

	// Require a TLS client certificate (set by VerifyClientCertIfGiven on the TLS listener).
	if r.TLS == nil || len(r.TLS.PeerCertificates) == 0 {
		RespondIAMError(w, r, http.StatusUnauthorized,
			NewIAMAPIError(ErrClientCertRequired, "client certificate required for PIV login"))
		return
	}

	cert := r.TLS.PeerCertificates[0]

	// Compute BOTH PIV lookup thumbprints. The primary is the canonical SPKI
	// thumbprint (SHA-256 over SubjectPublicKeyInfo, base64url) qrdb's
	// UserByCertThumbprint resolves natively; the fallback is the legacy hex
	// SHA-256 of the certificate DER, resolved via the broker-side legacy-PIV
	// index. Resolution tries SPKI first, then legacy — mirroring the
	// login-service mTLS primary + alt-lookup order — so a renewed cert (same
	// key, new DER) resolves via the stable SPKI thumbprint while persisted
	// legacy-only bindings keep resolving via the fallback.
	spkiThumbprint := qrdbsdk.ComputeSPKIThumbprint(cert)
	legacyDigest := sha256.Sum256(cert.Raw)
	legacyThumbprint := hex.EncodeToString(legacyDigest[:])
	// thumbprint is the value carried into audit records; the SPKI thumbprint is
	// the canonical identifier.
	thumbprint := spkiThumbprint

	// Defense-in-depth: verify the issuing CA's Subject DN against the
	// operator-configured TrustedIssuers list. Empty list = permissive.
	if trustedIssuers := h.server.config.PIVClientCert.TrustedIssuers; len(trustedIssuers) > 0 {
		issuerDN := cert.Issuer.String()
		issuerLower := strings.ToLower(issuerDN)
		allowed := false
		for _, dn := range trustedIssuers {
			if strings.EqualFold(dn, issuerLower) {
				allowed = true
				break
			}
		}
		if !allowed {
			h.logPIVLoginFailureAudit(ctx, "", thumbprint, ErrPIVCertIssuerRejected,
				"issuer DN \""+issuerDN+"\" not in trusted_issuers", r)
			RespondIAMError(w, r, http.StatusUnauthorized,
				NewIAMAPIError(ErrPIVCertIssuerRejected, "PIV certificate issuer is not trusted"))
			return
		}
	}

	// Resolve the identity store to look up the user by thumbprint.
	identityGetter, ok := h.server.service.(interface {
		GetIdentityStore(ctx context.Context) (*iam.IdentityStore, error)
	})
	if !ok {
		RespondIAMError(w, r, http.StatusInternalServerError,
			NewIAMAPIError(ErrInternalError, "identity store not available"))
		return
	}

	store, err := identityGetter.GetIdentityStore(ctx)
	if err != nil || store == nil {
		RespondIAMError(w, r, http.StatusInternalServerError,
			NewIAMAPIError(ErrInternalError, "identity store not configured"))
		return
	}

	// Derive the login tenant before the index lookup: SPIFFE SAN (pinned
	// trust domain) -> home-realm discovery -> fail closed. Injecting the
	// tenant into the context scopes the tenant-scoped PIV index lookup that
	// follows. A non-bootstrap login is NEVER defaulted to the system tenant.
	tenantID, err := h.server.deriveLoginTenant(ctx, r, cert, "")
	if err != nil {
		h.logPIVLoginFailureAudit(ctx, "", thumbprint, ErrUnauthorized,
			"login tenant could not be resolved: "+err.Error(), r)
		RespondIAMError(w, r, http.StatusUnauthorized,
			NewIAMAPIError(ErrUnauthorized, "login tenant could not be resolved"))
		return
	}
	ctx = loginContextWithTenant(ctx, tenantID)

	// SPKI first, then legacy hex digest. Either resolves the same user; an
	// unknown thumbprint fails closed.
	user, err := store.GetUserByPIVThumbprint(ctx, spkiThumbprint)
	if err != nil || user == nil {
		user, err = store.GetUserByPIVThumbprint(ctx, legacyThumbprint)
	}
	if err != nil || user == nil {
		h.logPIVLoginFailureAudit(ctx, "", thumbprint, ErrUnauthorized,
			"no user found for the presented certificate", r)
		RespondIAMError(w, r, http.StatusUnauthorized,
			NewIAMAPIError(ErrUnauthorized, "no user found for the presented certificate"))
		return
	}
	// Defense-in-depth: the resolved user must belong to the derived tenant.
	if user.TenantID != tenantID {
		h.logPIVLoginFailureAudit(ctx, user.ID, thumbprint, ErrUnauthorized,
			"resolved user tenant does not match derived login tenant", r)
		RespondIAMError(w, r, http.StatusUnauthorized,
			NewIAMAPIError(ErrUnauthorized, "no user found for the presented certificate"))
		return
	}

	// Resolve OIDC provider for token issuance.
	oidcGetter, ok := h.server.service.(interface {
		GetOIDCProvider(ctx context.Context) (*iam.OIDCProvider, error)
	})
	if !ok {
		RespondIAMError(w, r, http.StatusInternalServerError,
			NewIAMAPIError(ErrInternalError, "OIDC provider not available"))
		return
	}

	oidcProvider, err := oidcGetter.GetOIDCProvider(ctx)
	if err != nil || oidcProvider == nil {
		RespondIAMError(w, r, http.StatusInternalServerError,
			NewIAMAPIError(ErrInternalError, "OIDC provider not configured"))
		return
	}

	tokens, err := oidcProvider.IssueHardwareTokens(ctx, iam.HardwareTokenOptions{
		User:        user,
		AMR:         []string{"smartcard", "mfa"},
		ACR:         iam.ACRHardwareAttested,
		AttestLevel: "hardware",
	})
	if err != nil {
		h.logPIVLoginFailureAudit(ctx, user.TenantID, thumbprint, ErrInternalError,
			"token issuance failed: "+err.Error(), r)
		h.server.handleServiceError(w, r, err)
		return
	}

	// Emit success audit event.
	h.logPIVLoginAudit(ctx, user.TenantID, user.ID, user.Email, thumbprint,
		cert.Subject.String(), cert.Issuer.String(), "smartcard", r)

	RespondJSON(w, r, http.StatusOK, map[string]interface{}{
		"access_token":  tokens.AccessToken,
		"id_token":      tokens.IDToken,
		"refresh_token": tokens.RefreshToken,
		"token_type":    tokens.TokenType,
		"expires_in":    tokens.ExpiresIn,
	})
}

// logPIVLoginAudit emits a success audit event for a PIV smartcard login.
// Emits event_type "auth.login.piv.success".
func (h *IAMHandler) logPIVLoginAudit(ctx context.Context, tenantID, userID, email, certThumbprint, certSubject, certIssuer, attestMethod string, r *http.Request) {
	auditGetter, ok := h.server.service.(interface {
		GetAuditLogger(ctx context.Context) (iam.AuditLogger, error)
	})
	if !ok {
		return
	}
	logger, err := auditGetter.GetAuditLogger(ctx)
	if err != nil || logger == nil {
		return
	}
	requestID := r.Header.Get("X-Request-ID")
	logger.LogAsync(&iam.AuditEvent{
		TenantID:  tenantID,
		Timestamp: time.Now().UTC(),
		EventType: iam.AuditEventAuthentication,
		Category:  iam.AuditCategoryAuthentication,
		Actor: &iam.AuditActor{
			UserID: userID,
			Email:  email,
		},
		Action:  "auth.login.piv.success",
		Outcome: iam.AuditOutcomeSuccess,
		Details: map[string]interface{}{
			"tenant_id":       tenantID,
			"user_id":         userID,
			"cert_thumbprint": certThumbprint,
			"cert_subject":    certSubject,
			"cert_issuer":     certIssuer,
			"attest_method":   attestMethod,
			"request_id":      requestID,
		},
		IPAddress: r.RemoteAddr,
		UserAgent: r.Header.Get("User-Agent"),
	})
}

// logPIVLoginFailureAudit emits a failure audit event for a PIV smartcard login attempt.
// Emits event_type "auth.login.piv.failure".
func (h *IAMHandler) logPIVLoginFailureAudit(ctx context.Context, tenantID, certThumbprint, errorCode, errorMessage string, r *http.Request) {
	auditGetter, ok := h.server.service.(interface {
		GetAuditLogger(ctx context.Context) (iam.AuditLogger, error)
	})
	if !ok {
		return
	}
	logger, err := auditGetter.GetAuditLogger(ctx)
	if err != nil || logger == nil {
		return
	}
	requestID := r.Header.Get("X-Request-ID")
	logger.LogAsync(&iam.AuditEvent{
		TenantID:  tenantID,
		Timestamp: time.Now().UTC(),
		EventType: iam.AuditEventAuthentication,
		Category:  iam.AuditCategoryAuthentication,
		Action:    "auth.login.piv.failure",
		Outcome:   iam.AuditOutcomeFailure,
		Details: map[string]interface{}{
			"cert_thumbprint": certThumbprint,
			"error_code":      errorCode,
			"error_message":   errorMessage,
			"request_id":      requestID,
		},
		IPAddress: r.RemoteAddr,
		UserAgent: r.Header.Get("User-Agent"),
	})
}
