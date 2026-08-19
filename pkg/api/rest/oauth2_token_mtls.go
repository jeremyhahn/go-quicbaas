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
	"crypto/x509"
	"errors"
	"net/http"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// SetMTLSAuthenticators wires the PKI and self-signed client authenticators on
// the IAM handler. Call this from the server lifecycle after the IAM subsystem
// has been initialized. Both arguments are optional; nil means that auth method
// is unavailable.
func (h *IAMHandler) SetMTLSAuthenticators(pkiAuth, selfSigned iam.MTLSClientAuthenticator) {
	h.pkiAuth = pkiAuth
	h.selfSignedAuth = selfSigned
}

// SetTPMAttestationResolver wires the TPM/xkey attestation resolver. Call this
// from the server lifecycle when operator-provided EK root pools are available.
func (h *IAMHandler) SetTPMAttestationResolver(r *iam.TPMAttestationResolver) {
	h.tpmResolver = r
}

// TokenMTLS handles POST /oauth2/token-mtls - mTLS token endpoint (RFC 8705 §5).
//
// The TLS layer must present at least tls.RequestClientCert so that peer
// certificates are available. When TLSCAFile is set without TLSClientAuth the
// server already configures tls.VerifyClientCertIfGiven which satisfies this.
//
// Flow:
//  1. Read r.TLS.PeerCertificates[0]; return 401 if absent.
//  2. Authenticate client via registered MTLSClientAuthenticator.
//  3. Resolve attestation method (tpm / xkey / manual).
//  4. Delegate to OIDCProvider.TokenMTLS to issue a cnf-bound access token.
func (h *IAMHandler) TokenMTLS(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// A client certificate is mandatory on this endpoint.
	if r.TLS == nil || len(r.TLS.PeerCertificates) == 0 {
		RespondOAuthError(w, r, http.StatusUnauthorized,
			&OAuthError{Code: ErrInvalidClient, ErrorDescription: "client certificate required"})
		return
	}
	peerCert := r.TLS.PeerCertificates[0]

	r.Body = http.MaxBytesReader(w, r.Body, MaxOAuth2FormBytes)
	if err := r.ParseForm(); err != nil {
		RespondOAuthError(w, r, http.StatusBadRequest,
			NewInvalidRequestError("failed to parse form data"))
		return
	}

	// client_id is required; client_secret is NOT used on this endpoint.
	clientID := r.FormValue("client_id")
	if clientID == "" {
		// Also accept Basic Auth username-only (no password) for tooling compatibility.
		if id, _, ok := r.BasicAuth(); ok && id != "" {
			clientID = id
		}
	}
	if clientID == "" {
		RespondOAuthError(w, r, http.StatusBadRequest,
			NewInvalidRequestError("client_id is required"))
		return
	}

	grantType := r.FormValue("grant_type")
	if grantType == "" {
		RespondOAuthError(w, r, http.StatusBadRequest,
			NewInvalidRequestError("grant_type is required"))
		return
	}

	// Derive the tenant for this token request and scope the context so the
	// tenant-scoped app/token index lookups resolve correctly. The clientID
	// encodes the tenant (app_<tenant>_<suffix>); cross-check it against the
	// tenant derived from the client certificate's SPIFFE SAN (when present).
	// A non-bootstrap request is never defaulted to the system tenant.
	tenantID, derr := h.server.deriveLoginTenant(ctx, r, peerCert, "")
	if derr != nil {
		RespondOAuthError(w, r, http.StatusUnauthorized,
			&OAuthError{Code: ErrInvalidClient, ErrorDescription: "tenant could not be resolved"})
		return
	}
	if cidTenant := tenantFromOAuthClientID(clientID); cidTenant != "" && cidTenant != tenantID {
		RespondOAuthError(w, r, http.StatusUnauthorized,
			&OAuthError{Code: ErrInvalidClient, ErrorDescription: "client_id tenant does not match certificate tenant"})
		return
	}
	ctx = loginContextWithTenant(ctx, tenantID)

	// Authenticate the client cert against the registered authenticator.
	// This is done before reaching the OIDC provider so a bad cert gets a
	// deterministic 401 regardless of provider availability.
	if authErr := h.authenticateMTLSClient(ctx, peerCert, clientID); authErr != nil {
		RespondOAuthError(w, r, http.StatusUnauthorized,
			&OAuthError{Code: ErrInvalidClient, ErrorDescription: "client authentication failed"})
		return
	}

	// Retrieve OIDC provider.
	provider, err := h.getOIDCProvider(ctx)
	if err != nil {
		RespondOAuthError(w, r, http.StatusInternalServerError,
			&OAuthError{Code: ErrServerError, ErrorDescription: err.Error()})
		return
	}

	// Compute the RFC 8705 §3.1 cert thumbprint.
	thumbprint := iam.CertThumbprintS256(peerCert)

	// Resolve attestation method for amr / attest_method claims.
	attestMethod := h.resolveAttest(peerCert)

	mtlsReq := &iam.MTLSTokenRequest{
		GrantType:          grantType,
		ClientID:           clientID,
		Scope:              r.FormValue("scope"),
		CertThumbprintS256: thumbprint,
		AttestMethod:       attestMethod,
		Resource:           r.FormValue("resource"),
	}

	tokenResp, err := provider.TokenMTLS(ctx, mtlsReq)
	if err != nil {
		oauthErr := &iam.OAuthError{}
		if errors.As(err, &oauthErr) {
			statusCode := http.StatusBadRequest
			if oauthErr.ErrorCode == iam.ErrOAuthInvalidClient {
				statusCode = http.StatusUnauthorized
			}
			RespondOAuthError(w, r, statusCode, &OAuthError{
				Code:             oauthErr.ErrorCode,
				ErrorDescription: oauthErr.ErrorDescription,
			})
			return
		}
		RespondOAuthError(w, r, http.StatusInternalServerError,
			&OAuthError{Code: ErrServerError, ErrorDescription: err.Error()})
		return
	}

	RespondJSON(w, r, http.StatusOK, tokenResp)
}

// getOIDCProvider is a small helper that retrieves the OIDCProvider from the
// underlying service, shared with other OIDC handlers.
func (h *IAMHandler) getOIDCProvider(ctx context.Context) (*iam.OIDCProvider, error) {
	type oidcGetter interface {
		GetOIDCProvider(ctx context.Context) (*iam.OIDCProvider, error)
	}
	getter, ok := h.server.service.(oidcGetter)
	if !ok {
		return nil, &iam.IAMMessageError{Message: "OIDC provider not available"}
	}
	provider, err := getter.GetOIDCProvider(ctx)
	if err != nil {
		return nil, err
	}
	if provider == nil {
		return nil, &iam.IAMMessageError{Message: "OIDC provider not initialized"}
	}
	return provider, nil
}

// authenticateMTLSClient tries the PKI authenticator first, then falls back to
// self-signed thumbprint matching. Returns nil on success.
func (h *IAMHandler) authenticateMTLSClient(ctx context.Context, cert *x509.Certificate, clientID string) error {
	// Prefer PKI auth (tls_client_auth) when available.
	if h.pkiAuth != nil {
		if _, err := h.pkiAuth.AuthenticateClient(ctx, cert, clientID); err == nil {
			return nil
		}
	}
	// Fall back to self-signed thumbprint (self_signed_tls_client_auth).
	if h.selfSignedAuth != nil {
		if _, err := h.selfSignedAuth.AuthenticateClient(ctx, cert, clientID); err != nil {
			return err
		}
		return nil
	}
	// Neither authenticator configured — attempt to wire a temporary one via
	// the service's app storage if the service exposes it.
	type appStorageGetter interface {
		GetAppStorage(ctx context.Context) iam.AppStorage
	}
	if sg, ok := h.server.service.(appStorageGetter); ok {
		auth := iam.NewSelfSignedClientAuth(sg.GetAppStorage(ctx))
		_, err := auth.AuthenticateClient(ctx, cert, clientID)
		return err
	}
	return iam.ErrMTLSNoCertificate
}

// resolveAttest classifies the peer certificate using the optional TPM resolver.
func (h *IAMHandler) resolveAttest(cert *x509.Certificate) string {
	if h.tpmResolver != nil {
		return h.tpmResolver.Resolve(cert)
	}
	return iam.AttestMethodManual
}
