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
	"errors"
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
		respondAuthorizeError(w, r, provider.Issuer(), req, err)
		return
	}

	// Redirect to redirect_uri with authorization code
	params := url.Values{}
	params.Set("code", resp.Code)
	if resp.State != "" {
		params.Set("state", resp.State)
	}
	redirectToClient(w, r, provider.Issuer(), resp.RedirectURI, params)
}

// respondAuthorizeError reports a failed authorization request
// (draft-ietf-oauth-v2-1-16 §4.1.2.1). While the client_id or redirect_uri is
// missing, unknown or unregistered (iam.AuthorizeDirectError) the error is
// returned to the user agent directly and never redirected. Once both are
// validated, the error goes back to the client as a 302 to its redirect URI
// carrying error, error_description and state.
func respondAuthorizeError(w http.ResponseWriter, r *http.Request, issuer string, req *iam.AuthorizeRequest, err error) {
	var directErr *iam.AuthorizeDirectError
	if errors.As(err, &directErr) {
		RespondOAuthError(w, r, http.StatusBadRequest, &OAuthError{
			Code:             directErr.ErrorCode,
			ErrorDescription: directErr.ErrorDescription,
			State:            req.State,
		})
		return
	}

	params := url.Values{}
	oauthErr := &iam.OAuthError{}
	if errors.As(err, &oauthErr) {
		params.Set("error", oauthErr.ErrorCode)
		if oauthErr.ErrorDescription != "" {
			params.Set("error_description", oauthErr.ErrorDescription)
		}
	} else {
		params.Set("error", ErrServerError)
	}
	if req.State != "" {
		params.Set("state", req.State)
	}
	redirectToClient(w, r, issuer, req.RedirectURI, params)
}

// redirectToClient sends the authorization response to the client with a 302
// to its validated redirect URI. The parameters are added to any query the
// registered URI already carries (draft-ietf-oauth-v2-1-16 §4.1.2), and iss
// identifies this authorization server (§4.1.2, RFC 9207).
func redirectToClient(w http.ResponseWriter, r *http.Request, issuer, redirectURI string, params url.Values) {
	redirectURL, err := url.Parse(redirectURI)
	if err != nil {
		RespondOAuthError(w, r, http.StatusInternalServerError,
			&OAuthError{Code: ErrServerError, ErrorDescription: "invalid redirect_uri", State: params.Get("state")})
		return
	}

	query := redirectURL.Query()
	for name, values := range params {
		query[name] = values
	}
	if issuer != "" {
		query.Set("iss", issuer)
	}
	redirectURL.RawQuery = query.Encode()

	http.Redirect(w, r, redirectURL.String(), http.StatusFound)
}

// Token handles POST /oauth2/token - OAuth2 token endpoint.
func (h *IAMHandler) Token(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Parse form data
	r.Body = http.MaxBytesReader(w, r.Body, MaxOAuth2FormBytes)
	if err := r.ParseForm(); err != nil {
		RespondOAuthError(w, r, http.StatusBadRequest,
			NewInvalidRequestError("failed to parse form data"))
		return
	}

	// Extract client credentials. RFC 6749 §2.3.1 specifies HTTP Basic
	// authentication as the recommended method for confidential clients;
	// OAuth2 libraries (including our own fleet node daemon) commonly send
	// Authorization: Basic base64(client_id:client_secret) on the
	// token endpoint. Body fields are an alternative fallback per the
	// same section. Every token-request parameter is read from the form
	// body only (r.PostForm): draft-ietf-oauth-v2-1-16 §3.2 forbids them in
	// the URL query, so a client_secret there is ignored. Basic-auth values take precedence when both are
	// supplied because the spec forbids clients from mixing the two.
	clientID, clientSecret, hasBasic := r.BasicAuth()
	if !hasBasic {
		clientID = r.PostForm.Get("client_id")
		clientSecret = r.PostForm.Get("client_secret")
	}

	// Scope the context to the tenant encoded in the clientID
	// (app_<tenant>_<suffix>) so the tenant-scoped app/token index lookups the
	// OIDC provider performs resolve correctly. The clientID is the
	// authoritative tenant binding for the client-credentials / code grants on
	// this endpoint.
	if cidTenant := tenantFromOAuthClientID(clientID); cidTenant != "" {
		ctx = loginContextWithTenant(ctx, cidTenant)
	}

	// Build token request
	req := &iam.TokenRequest{
		GrantType:    r.PostForm.Get("grant_type"),
		Code:         r.PostForm.Get("code"),
		RedirectURI:  r.PostForm.Get("redirect_uri"),
		ClientID:     clientID,
		ClientSecret: clientSecret,
		CodeVerifier: r.PostForm.Get("code_verifier"),
		RefreshToken: r.PostForm.Get("refresh_token"),
		Resource:     r.PostForm.Get("resource"), // RFC 8707 Resource Indicators
	}

	// Parse scope
	if scopeParam := r.PostForm.Get("scope"); scopeParam != "" {
		req.Scope = strings.Fields(scopeParam)
	}

	// RFC 9449 DPoP: when the client presents a proof in the DPoP request
	// header, sender-constrain the issued token to the proof key (cnf.jkt). The
	// issuer verifies the proof's htm/htu against this endpoint's method and
	// absolute URI, so capture the externally-visible token endpoint URL. No
	// header means a plain bearer; an invalid proof fails closed in the issuer.
	if proof := r.Header.Get("DPoP"); proof != "" {
		req.DPoPProof = proof
		req.HTTPMethod = r.Method
		req.HTTPURI = requestAbsoluteURI(r)
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
		oauthErr := &iam.OAuthError{}
		if errors.As(err, &oauthErr) {
			statusCode := http.StatusBadRequest
			if oauthErr.ErrorCode == ErrInvalidClient {
				statusCode = http.StatusUnauthorized
				challengeBasicClient(w, r)
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

	// Dual-identity audit (AU-2/AU-3): the token endpoint just minted a bearer
	// for a user. Record BOTH the service that presented the request (verified
	// client cert, when mTLS) and the user identity carried by the issued
	// access token. The issued token is validated locally to extract the
	// authoritative sub/tenant_id/scp/jti without trusting request inputs.
	if tokenResp != nil && tokenResp.AccessToken != "" {
		if claims, vErr := provider.ValidateAccessToken(ctx, tokenResp.AccessToken); vErr == nil && claims != nil {
			id := iam.DualIdentity{
				Subject:         claims.Subject,
				Username:        claims.Name,
				TenantID:        claims.TenantID,
				ClientID:        claims.ClientID,
				JTI:             claims.JTI,
				Scopes:          claims.SCP,
				Roles:           claims.Roles,
				ServiceIdentity: iam.ServiceIdentityFromCert(verifiedPeerCertFromRequest(r)),
				IPAddress:       getClientIP(r),
				UserAgent:       r.UserAgent(),
			}
			if id.ClientID == "" {
				id.ClientID = clientID
			}
			h.auditAuthentication(ctx, id, "oauth2.token", iam.AuditOutcomeSuccess, map[string]interface{}{
				"grant_type": req.GrantType,
			})
		}
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
	if len(parts) != 2 || !strings.EqualFold(parts[0], "bearer") {
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
		oauthErr := &iam.OAuthError{}
		if errors.As(err, &oauthErr) {
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
	r.Body = http.MaxBytesReader(w, r.Body, MaxOAuth2FormBytes)
	if err := r.ParseForm(); err != nil {
		RespondOAuthError(w, r, http.StatusBadRequest,
			NewInvalidRequestError("failed to parse form data"))
		return
	}

	token := r.PostForm.Get("token")
	if token == "" {
		RespondOAuthError(w, r, http.StatusBadRequest,
			NewInvalidRequestError("token is required"))
		return
	}

	tokenTypeHint := r.PostForm.Get("token_type_hint")

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
	clientID, clientSecret := revocationClientCredentials(r)
	revokeReq := &iam.RevokeRequest{
		Token:         token,
		TokenTypeHint: tokenTypeHint,
		ClientID:      clientID,
		ClientSecret:  clientSecret,
	}

	// Revoke token
	if err := provider.Revoke(ctx, revokeReq); err != nil {
		oauthErr := &iam.OAuthError{}
		if errors.As(err, &oauthErr) {
			statusCode := http.StatusBadRequest
			if oauthErr.ErrorCode == iam.ErrOAuthInvalidClient {
				statusCode = http.StatusUnauthorized
				challengeBasicClient(w, r)
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

	// RFC 7009 §2.2: the revocation endpoint answers 200 with an empty body,
	// including for a token that was unknown, expired or already revoked.
	w.WriteHeader(http.StatusOK)
}

// revocationClientCredentials returns the client credentials of a revocation
// request, read as the token endpoint reads them (RFC 7009 §2.1): HTTP Basic
// first, else client_id/client_secret from the form body. The query string is
// never consulted, so a secret placed there does not authenticate.
func revocationClientCredentials(r *http.Request) (clientID, clientSecret string) {
	if id, secret, ok := r.BasicAuth(); ok {
		return id, secret
	}
	return r.PostForm.Get("client_id"), r.PostForm.Get("client_secret")
}
