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
	"net/http"
	"net/url"
	"strings"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// GetOIDCConfig handles GET /.well-known/openid-configuration - OIDC discovery endpoint.
func (h *IAMHandler) GetOIDCConfig(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Get OIDC provider
	oidcProvider, ok := h.server.service.(interface {
		GetOIDCProvider(ctx context.Context) (*iam.OIDCProvider, error)
	})
	if !ok {
		RespondIAMError(w, r, http.StatusInternalServerError,
			NewIAMAPIError(ErrInternalError, "OIDC provider not available"))
		return
	}

	provider, err := oidcProvider.GetOIDCProvider(ctx)
	if err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}
	if provider == nil {
		RespondIAMError(w, r, http.StatusInternalServerError,
			NewIAMAPIError(ErrInternalError, "OIDC provider not initialized"))
		return
	}

	// Get discovery document (OpenID Configuration)
	doc := provider.GetConfiguration()

	RespondJSON(w, r, http.StatusOK, doc)
}

// GetJWKS handles GET /.well-known/jwks.json - JWKS endpoint.
func (h *IAMHandler) GetJWKS(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Get OIDC provider
	oidcProvider, ok := h.server.service.(interface {
		GetOIDCProvider(ctx context.Context) (*iam.OIDCProvider, error)
	})
	if !ok {
		RespondIAMError(w, r, http.StatusInternalServerError,
			NewIAMAPIError(ErrInternalError, "OIDC provider not available"))
		return
	}

	provider, err := oidcProvider.GetOIDCProvider(ctx)
	if err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}
	if provider == nil {
		RespondIAMError(w, r, http.StatusInternalServerError,
			NewIAMAPIError(ErrInternalError, "OIDC provider not initialized"))
		return
	}

	// Get JWKS
	jwks := provider.GetJWKS()

	RespondJSON(w, r, http.StatusOK, jwks)
}

// Authorize handles GET /oauth2/authorize - OAuth2 authorization endpoint.
func (h *IAMHandler) Authorize(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Parse authorization request from query parameters
	req := &iam.AuthorizeRequest{
		ClientID:            r.URL.Query().Get("client_id"),
		RedirectURI:         r.URL.Query().Get("redirect_uri"),
		ResponseType:        r.URL.Query().Get("response_type"),
		State:               r.URL.Query().Get("state"),
		CodeChallenge:       r.URL.Query().Get("code_challenge"),
		CodeChallengeMethod: r.URL.Query().Get("code_challenge_method"),
		Nonce:               r.URL.Query().Get("nonce"),
		Resource:            r.URL.Query().Get("resource"), // RFC 8707 Resource Indicators
	}

	// Parse scope (can be space-separated or multiple parameters)
	if scopeParam := r.URL.Query().Get("scope"); scopeParam != "" {
		req.Scope = strings.Fields(scopeParam)
	}

	// Validate required parameters
	if req.ClientID == "" {
		RespondOAuthError(w, r, http.StatusBadRequest,
			NewInvalidRequestError("client_id is required"))
		return
	}

	if req.RedirectURI == "" {
		RespondOAuthError(w, r, http.StatusBadRequest,
			NewInvalidRequestError("redirect_uri is required"))
		return
	}

	if req.ResponseType == "" {
		RespondOAuthError(w, r, http.StatusBadRequest,
			NewInvalidRequestError("response_type is required"))
		return
	}

	// Get OIDC provider
	oidcProvider, ok := h.server.service.(interface {
		GetOIDCProvider(ctx context.Context) (*iam.OIDCProvider, error)
	})
	if !ok {
		RespondOAuthError(w, r, http.StatusInternalServerError,
			&OAuthError{Code: ErrServerError, ErrorDescription: "OIDC provider not available"})
		return
	}

	provider, err := oidcProvider.GetOIDCProvider(ctx)
	if err != nil {
		RespondOAuthError(w, r, http.StatusInternalServerError,
			&OAuthError{Code: ErrServerError, ErrorDescription: err.Error()})
		return
	}
	if provider == nil {
		RespondOAuthError(w, r, http.StatusInternalServerError,
			&OAuthError{Code: ErrServerError, ErrorDescription: "OIDC provider not initialized"})
		return
	}

	// Perform authorization
	resp, err := provider.Authorize(ctx, req)
	if err != nil {
		if oauthErr, ok := err.(*iam.OAuthError); ok {
			RespondOAuthError(w, r, http.StatusBadRequest, &OAuthError{
				Code:             oauthErr.ErrorCode,
				ErrorDescription: oauthErr.ErrorDescription,
				State:            req.State,
			})
			return
		}
		RespondOAuthError(w, r, http.StatusInternalServerError,
			&OAuthError{Code: ErrServerError, ErrorDescription: err.Error(), State: req.State})
		return
	}

	// Redirect to redirect_uri with authorization code
	redirectURL, err := url.Parse(resp.RedirectURI)
	if err != nil {
		RespondOAuthError(w, r, http.StatusInternalServerError,
			&OAuthError{Code: ErrServerError, ErrorDescription: "invalid redirect_uri", State: req.State})
		return
	}

	query := redirectURL.Query()
	query.Set("code", resp.Code)
	if resp.State != "" {
		query.Set("state", resp.State)
	}
	redirectURL.RawQuery = query.Encode()

	http.Redirect(w, r, redirectURL.String(), http.StatusFound)
}

// Token handles POST /oauth2/token - OAuth2 token endpoint.
func (h *IAMHandler) Token(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Parse form data
	if err := r.ParseForm(); err != nil {
		RespondOAuthError(w, r, http.StatusBadRequest,
			NewInvalidRequestError("failed to parse form data"))
		return
	}

	// Build token request
	req := &iam.TokenRequest{
		GrantType:    r.FormValue("grant_type"),
		Code:         r.FormValue("code"),
		RedirectURI:  r.FormValue("redirect_uri"),
		ClientID:     r.FormValue("client_id"),
		ClientSecret: r.FormValue("client_secret"),
		CodeVerifier: r.FormValue("code_verifier"),
		RefreshToken: r.FormValue("refresh_token"),
		Resource:     r.FormValue("resource"), // RFC 8707 Resource Indicators
	}

	// Parse scope
	if scopeParam := r.FormValue("scope"); scopeParam != "" {
		req.Scope = strings.Fields(scopeParam)
	}

	// Validate required parameters
	if req.GrantType == "" {
		RespondOAuthError(w, r, http.StatusBadRequest,
			NewInvalidRequestError("grant_type is required"))
		return
	}

	// Get OIDC provider
	oidcProvider, ok := h.server.service.(interface {
		GetOIDCProvider(ctx context.Context) (*iam.OIDCProvider, error)
	})
	if !ok {
		RespondOAuthError(w, r, http.StatusInternalServerError,
			&OAuthError{Code: ErrServerError, ErrorDescription: "OIDC provider not available"})
		return
	}

	provider, err := oidcProvider.GetOIDCProvider(ctx)
	if err != nil {
		RespondOAuthError(w, r, http.StatusInternalServerError,
			&OAuthError{Code: ErrServerError, ErrorDescription: err.Error()})
		return
	}
	if provider == nil {
		RespondOAuthError(w, r, http.StatusInternalServerError,
			&OAuthError{Code: ErrServerError, ErrorDescription: "OIDC provider not initialized"})
		return
	}

	// Exchange for tokens
	tokenResp, err := provider.Token(ctx, req)
	if err != nil {
		if oauthErr, ok := err.(*iam.OAuthError); ok {
			statusCode := http.StatusBadRequest
			if oauthErr.ErrorCode == ErrInvalidClient {
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

// UserInfo handles GET /oauth2/userinfo - OIDC UserInfo endpoint.
func (h *IAMHandler) UserInfo(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Extract bearer token from Authorization header
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		RespondOAuthError(w, r, http.StatusUnauthorized,
			NewInvalidRequestError("authorization header is required"))
		return
	}

	parts := strings.SplitN(authHeader, " ", 2)
	if len(parts) != 2 || strings.ToLower(parts[0]) != "bearer" {
		RespondOAuthError(w, r, http.StatusUnauthorized,
			NewInvalidRequestError("invalid authorization header format"))
		return
	}

	accessToken := parts[1]

	// Get OIDC provider
	oidcProvider, ok := h.server.service.(interface {
		GetOIDCProvider(ctx context.Context) (*iam.OIDCProvider, error)
	})
	if !ok {
		RespondOAuthError(w, r, http.StatusInternalServerError,
			&OAuthError{Code: ErrServerError, ErrorDescription: "OIDC provider not available"})
		return
	}

	provider, err := oidcProvider.GetOIDCProvider(ctx)
	if err != nil {
		RespondOAuthError(w, r, http.StatusInternalServerError,
			&OAuthError{Code: ErrServerError, ErrorDescription: err.Error()})
		return
	}
	if provider == nil {
		RespondOAuthError(w, r, http.StatusInternalServerError,
			&OAuthError{Code: ErrServerError, ErrorDescription: "OIDC provider not initialized"})
		return
	}

	// Get user info
	userInfo, err := provider.UserInfo(ctx, accessToken)
	if err != nil {
		if oauthErr, ok := err.(*iam.OAuthError); ok {
			RespondOAuthError(w, r, http.StatusUnauthorized, &OAuthError{
				Code:             oauthErr.ErrorCode,
				ErrorDescription: oauthErr.ErrorDescription,
			})
			return
		}
		RespondOAuthError(w, r, http.StatusInternalServerError,
			&OAuthError{Code: ErrServerError, ErrorDescription: err.Error()})
		return
	}

	RespondJSON(w, r, http.StatusOK, userInfo)
}

// Revoke handles POST /oauth2/revoke - OAuth2 token revocation endpoint.
func (h *IAMHandler) Revoke(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Parse form data
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

	tokenTypeHint := r.FormValue("token_type_hint")

	// Get OIDC provider
	oidcProvider, ok := h.server.service.(interface {
		GetOIDCProvider(ctx context.Context) (*iam.OIDCProvider, error)
	})
	if !ok {
		RespondOAuthError(w, r, http.StatusInternalServerError,
			&OAuthError{Code: ErrServerError, ErrorDescription: "OIDC provider not available"})
		return
	}

	provider, err := oidcProvider.GetOIDCProvider(ctx)
	if err != nil {
		RespondOAuthError(w, r, http.StatusInternalServerError,
			&OAuthError{Code: ErrServerError, ErrorDescription: err.Error()})
		return
	}
	if provider == nil {
		RespondOAuthError(w, r, http.StatusInternalServerError,
			&OAuthError{Code: ErrServerError, ErrorDescription: "OIDC provider not initialized"})
		return
	}

	// Build revoke request
	revokeReq := &iam.RevokeRequest{
		Token:         token,
		TokenTypeHint: tokenTypeHint,
	}

	// Revoke token
	if err := provider.Revoke(ctx, revokeReq); err != nil {
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

	// RFC 7009: The revocation endpoint responds with HTTP status code 200
	RespondNoContent(w, r)
}
