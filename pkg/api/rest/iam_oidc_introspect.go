//go:build rest && iam

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
	"crypto/x509"
	"net/http"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// Introspect handles POST /oauth2/introspect - RFC 7662 OAuth 2.0 Token
// Introspection.
//
// The endpoint is privileged (RFC 7662 §2.1): the calling resource server MUST
// authenticate. Two caller-authentication methods are accepted:
//
//  1. mTLS — a verified client certificate on the TLS chain (RFC 8705 style).
//  2. Registered client credentials — client_id/client_secret via HTTP Basic
//     or form body, validated against the OIDC provider's app storage.
//
// An unauthenticated caller is rejected with 401 invalid_client before any
// token is inspected. The endpoint never leaks claims for an invalid, expired,
// or revoked token: it responds {"active": false} (fail-closed). The tenant is
// derived from the token's own tenant_id claim inside the provider, so no
// global token table is consulted.
func (h *IAMHandler) Introspect(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	if err := r.ParseForm(); err != nil {
		RespondOAuthError(w, r, http.StatusBadRequest,
			NewInvalidRequestError("failed to parse form data"))
		return
	}

	token := r.FormValue("token")
	if token == "" {
		RespondOAuthError(w, r, http.StatusBadRequest,
			NewInvalidRequestError("token is required"))
		return
	}

	provider, err := h.getOIDCProvider(ctx)
	if err != nil {
		RespondOAuthError(w, r, http.StatusInternalServerError,
			&OAuthError{Code: ErrServerError, ErrorDescription: err.Error()})
		return
	}

	// RFC 7662 §2.1: authenticate the calling resource server. mTLS first
	// (verified peer certificate), then registered client credentials. The
	// verified client certificate (when present) is also the SERVICE identity
	// recorded on the dual-identity audit entry below.
	peerCert := verifiedPeerCertFromRequest(r)
	callerClientID, authErr := h.authenticateIntrospectionCaller(ctx, r, provider, peerCert)
	if authErr != nil {
		RespondOAuthError(w, r, http.StatusUnauthorized,
			&OAuthError{Code: ErrInvalidClient, ErrorDescription: "introspection caller authentication failed"})
		return
	}

	// Introspect: the provider verifies the signature, derives the tenant from
	// the token's tenant_id claim, and checks the per-tenant revocation store.
	introReq := &iam.IntrospectRequest{
		Token:         token,
		TokenTypeHint: r.FormValue("token_type_hint"),
	}
	resp, err := provider.Introspect(ctx, introReq)
	if err != nil {
		if oauthErr, ok := err.(*iam.OAuthError); ok {
			RespondOAuthError(w, r, http.StatusBadRequest, &OAuthError{
				Code:             oauthErr.ErrorCode,
				ErrorDescription: oauthErr.ErrorDescription,
			})
			return
		}
		RespondOAuthError(w, r, http.StatusInternalServerError,
			&OAuthError{Code: ErrServerError, ErrorDescription: err.Error()})
		return
	}

	// Dual-identity audit: record BOTH the service (verified client cert) and
	// the user (token subject/tenant/scope) for an active token. An audit
	// failure must not fail the introspection itself; it is logged.
	if resp.Active {
		h.auditIntrospection(ctx, r, peerCert, callerClientID, resp)
	}

	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	RespondJSON(w, r, http.StatusOK, resp)
}

// authenticateIntrospectionCaller authenticates the resource server calling the
// introspection endpoint. It returns the authenticated client_id (or the
// certificate-derived service identity) and a non-nil error when caller
// authentication fails. A verified client certificate satisfies authentication
// on its own (mTLS); otherwise client_id/client_secret is validated against the
// OIDC provider (RFC 7662 §2.1).
func (h *IAMHandler) authenticateIntrospectionCaller(
	ctx context.Context,
	r *http.Request,
	provider *iam.OIDCProvider,
	peerCert *x509.Certificate,
) (string, error) {
	clientID, clientSecret, hasBasic := r.BasicAuth()
	if !hasBasic {
		clientID = r.FormValue("client_id")
		clientSecret = r.FormValue("client_secret")
	}

	// mTLS: a verified peer certificate authenticates the caller. When a
	// client_id is also supplied, bind it to the certificate via the registered
	// mTLS authenticator so a stolen client_id cannot be asserted over a
	// different cert.
	if peerCert != nil {
		if clientID != "" {
			if err := h.authenticateMTLSClient(ctx, peerCert, clientID); err != nil {
				return "", err
			}
			return clientID, nil
		}
		return iam.ServiceIdentityFromCert(peerCert), nil
	}

	// No client certificate: require registered client credentials.
	if clientID == "" {
		return "", iam.NewOAuthError(iam.ErrOAuthInvalidClient, "client authentication required")
	}
	if err := provider.AuthenticateClient(ctx, clientID, clientSecret); err != nil {
		return "", err
	}
	return clientID, nil
}

// auditIntrospection records a dual-identity audit entry for a successful
// introspection of an active token.
func (h *IAMHandler) auditIntrospection(
	ctx context.Context,
	r *http.Request,
	peerCert *x509.Certificate,
	callerClientID string,
	resp *iam.IntrospectionResponse,
) {
	logger := h.auditLogger(ctx)
	if logger == nil {
		return
	}

	id := iam.DualIdentity{
		Subject:         resp.Subject,
		Username:        resp.Username,
		TenantID:        resp.TenantID,
		ClientID:        resp.ClientID,
		JTI:             resp.JTI,
		Scopes:          resp.SCP,
		Roles:           resp.Roles,
		ServiceIdentity: iam.ServiceIdentityFromCert(peerCert),
		IPAddress:       getClientIP(r),
		UserAgent:       r.UserAgent(),
	}

	details := map[string]interface{}{
		"introspection_caller": callerClientID,
		"token_type":           resp.TokenType,
		"audience":             resp.Audience,
	}

	if err := iam.LogDualIdentity(ctx, logger, id,
		iam.AuditEventAuthorization, iam.AuditCategorySecurity,
		"oauth2.introspect", iam.AuditOutcomeSuccess, details); err != nil {
		h.server.logger.WithError(err).Warn("iam.oidc: dual-identity introspection audit failed")
	}
}

// auditLogger retrieves the IAM audit logger from the service, or nil when the
// service does not expose one.
func (h *IAMHandler) auditLogger(ctx context.Context) iam.AuditLogger {
	if h.server == nil || h.server.service == nil {
		return nil
	}
	getter, ok := h.server.service.(interface {
		GetAuditLogger(ctx context.Context) (iam.AuditLogger, error)
	})
	if !ok {
		return nil
	}
	logger, err := getter.GetAuditLogger(ctx)
	if err != nil {
		return nil
	}
	return logger
}

// auditAuthentication records a dual-identity authentication audit event. It is
// the shared helper for authenticated paths (login, token) so a single place
// builds the AU-2/AU-3 entry carrying both the service and the user identity.
func (h *IAMHandler) auditAuthentication(
	ctx context.Context,
	id iam.DualIdentity,
	action string,
	outcome iam.AuditOutcome,
	details map[string]interface{},
) {
	logger := h.auditLogger(ctx)
	if logger == nil {
		return
	}
	if err := iam.LogDualIdentity(ctx, logger, id,
		iam.AuditEventAuthentication, iam.AuditCategoryAuthentication,
		action, outcome, details); err != nil {
		h.server.logger.WithError(err).Warn("iam.oidc: dual-identity authentication audit failed")
	}
}
