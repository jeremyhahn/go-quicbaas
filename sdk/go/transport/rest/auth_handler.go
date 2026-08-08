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
	"strings"
	"time"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/service"
	sdktransport "github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

// Auth service prefix for routing.
const (
	authServicePrefix = "quicbaas.Auth"
)

// isAuthService checks if the service name is an Auth service.
func isAuthService(svc string) bool {
	return strings.HasPrefix(svc, authServicePrefix)
}

// authRESTHandler handles Auth service requests over REST.
type authRESTHandler struct {
	transport *Transport
}

// newAuthRESTHandler creates a new Auth REST handler.
func newAuthRESTHandler(t *Transport) *authRESTHandler {
	return &authRESTHandler{transport: t}
}

// loginResponse represents the REST API login response.
type authLoginResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int64  `json:"expires_in"`
	ExpiresAt    string `json:"expires_at"`
}

// refreshTokenRequest is the internal request type for token refresh.
type authRefreshTokenRequest struct {
	RefreshToken string `json:"refresh_token"`
}

// validateTokenResponse represents the REST API validate token response.
type authValidateTokenResponse struct {
	Valid     bool     `json:"valid"`
	UserID    string   `json:"user_id"`
	Username  string   `json:"username"`
	TenantID  string   `json:"tenant_id"`
	Roles     []string `json:"roles"`
	ExpiresAt string   `json:"expires_at"`
	IsRevoked bool     `json:"is_revoked"`
}

// logoutRequest is the internal request type for logout.
type authLogoutRequest struct {
	AccessToken string `json:"access_token"`
}

// handleRequest routes Auth requests to the appropriate handler based on method name.
func (h *authRESTHandler) handleRequest(ctx context.Context, svc, method string, req interface{}) (interface{}, error) {
	switch method {
	case "Login":
		return h.handleLogin(ctx, req)
	case "Logout":
		return h.handleLogout(ctx, req)
	case "RefreshToken":
		return h.handleRefreshToken(ctx, req)
	case "ValidateToken":
		return h.handleValidateToken(ctx, req)
	case "BeginLogin":
		return h.handleBeginLogin(ctx, req)
	case "FinishLogin":
		return h.handleFinishLogin(ctx, req)
	default:
		return nil, errors.NewUnimplemented("authRESTHandler.handleRequest",
			"unsupported method: "+method)
	}
}

// handleLogin is the retired legacy username/password login verb. The locked
// architecture has no passwords; authentication is cert/factor-based. This
// path previously submitted an empty password to the server, which is unsafe.
// It now fails closed with a typed PasswordLoginUnsupportedError directing the
// caller to the unified login ceremony (BeginLogin/FinishLogin) with an mtls,
// fido2, or piv factor. The method is retained so the router and downstream
// callers keep compiling; it never reaches the network.
func (h *authRESTHandler) handleLogin(_ context.Context, _ interface{}) (interface{}, error) {
	return nil, errors.NewPasswordLoginUnsupported("authRESTHandler.handleLogin")
}

// handleLogout handles Logout requests.
func (h *authRESTHandler) handleLogout(ctx context.Context, req interface{}) (interface{}, error) {
	var accessToken string

	switch r := req.(type) {
	case map[string]interface{}:
		accessToken = getString(r, "access_token")
	default:
		reqMap, err := toMap(req)
		if err != nil {
			return nil, errors.NewInvalidInput("authRESTHandler.handleLogout",
				"invalid request type")
		}
		accessToken = getString(reqMap, "access_token")
	}

	logoutReq := authLogoutRequest{
		AccessToken: accessToken,
	}

	// Make the HTTP request
	var resp map[string]interface{}
	err := h.transport.doJSONRequest(ctx, http.MethodPost, "/api/v1/auth/logout", logoutReq, &resp)
	if err != nil {
		return nil, err
	}

	// Return nil to indicate success (matches gRPC handler behavior)
	return nil, nil
}

// handleRefreshToken handles RefreshToken requests.
func (h *authRESTHandler) handleRefreshToken(ctx context.Context, req interface{}) (interface{}, error) {
	var refreshToken string

	switch r := req.(type) {
	case map[string]interface{}:
		refreshToken = refreshTokenFromMap(r)
	default:
		reqMap, err := toMap(req)
		if err != nil {
			return nil, errors.NewInvalidInput("authRESTHandler.handleRefreshToken",
				"invalid request type")
		}
		refreshToken = refreshTokenFromMap(reqMap)
	}
	if refreshToken == "" {
		return nil, errors.NewInvalidInput("authRESTHandler.handleRefreshToken",
			"refresh token is required")
	}

	refreshReq := authRefreshTokenRequest{
		RefreshToken: refreshToken,
	}

	// Make the HTTP request
	var resp authLoginResponse
	err := h.transport.doJSONRequest(ctx, http.MethodPost, "/api/v1/auth/refresh", refreshReq, &resp)
	if err != nil {
		return nil, err
	}

	// Parse expires_at timestamp
	expiresAt, err := time.Parse(time.RFC3339, resp.ExpiresAt)
	if err != nil {
		// Fallback: calculate from expires_in
		expiresAt = time.Now().Add(time.Duration(resp.ExpiresIn) * time.Second)
	}

	// Convert to SDK response type. The rotated refresh token MUST be carried
	// back: the server revokes the presented one, so a client that keeps the
	// old value is locked out at the next renewal.
	return &service.ExtendedRefreshTokenResponse{
		AccessToken:  resp.AccessToken,
		TokenType:    resp.TokenType,
		ExpiresAt:    expiresAt,
		RefreshToken: resp.RefreshToken,
	}, nil
}

// refreshTokenFromMap reads the refresh token from a decoded request map. The
// SDK marshals service.RefreshTokenRequest (Go field names, no json tags) while
// callers that hand-build a map use the wire name, so both spellings resolve.
func refreshTokenFromMap(m map[string]interface{}) string {
	if v := getString(m, "refresh_token"); v != "" {
		return v
	}
	return getString(m, "RefreshToken")
}

// handleValidateToken handles ValidateToken requests.
func (h *authRESTHandler) handleValidateToken(ctx context.Context, req interface{}) (interface{}, error) {
	var accessToken string

	switch r := req.(type) {
	case map[string]interface{}:
		accessToken = getString(r, "access_token")
	default:
		reqMap, err := toMap(req)
		if err != nil {
			return nil, errors.NewInvalidInput("authRESTHandler.handleValidateToken",
				"invalid request type")
		}
		accessToken = getString(reqMap, "access_token")
	}

	// Set the Authorization header for validate requests
	h.transport.SetHeader("Authorization", "Bearer "+accessToken)
	defer h.transport.SetHeader("Authorization", "")

	// Make the HTTP request
	var resp authValidateTokenResponse
	err := h.transport.doJSONRequest(ctx, http.MethodPost, "/api/v1/auth/validate", nil, &resp)
	if err != nil {
		return nil, err
	}

	// Parse expires_at timestamp
	expiresAt, err := time.Parse(time.RFC3339, resp.ExpiresAt)
	if err != nil {
		expiresAt = time.Time{}
	}

	// Convert to SDK response type
	return &service.TokenClaims{
		Valid:     resp.Valid,
		UserID:    resp.UserID,
		Username:  resp.Username,
		Roles:     resp.Roles,
		ExpiresAt: expiresAt,
	}, nil
}

// =============================================================================
// Unified Login Ceremony (REST client)
// =============================================================================

// restBeginLoginRequest is the JSON body for POST /api/v1/auth/login/begin.
type restBeginLoginRequest struct {
	Type     string `json:"type"`
	Hint     string `json:"hint,omitempty"`
	TenantID string `json:"tenant_id,omitempty"`
}

// restBeginLoginResponse mirrors transport.BeginLoginResponse over JSON.
type restBeginLoginResponse struct {
	SessionID         string            `json:"session_id"`
	Nonce             []byte            `json:"nonce"`
	ChallengeMetadata map[string]string `json:"challenge_metadata,omitempty"`
	ExpiresAt         string            `json:"expires_at"`
}

// restFinishLoginRequest is the JSON body for POST /api/v1/auth/login/finish.
type restFinishLoginRequest struct {
	SessionID      string            `json:"session_id"`
	Type           string            `json:"type"`
	LookupKey      []byte            `json:"lookup_key,omitempty"`
	AltLookupKeys  [][]byte          `json:"alt_lookup_keys,omitempty"`
	Signature      []byte            `json:"signature,omitempty"`
	MethodMetadata map[string]string `json:"method_metadata,omitempty"`
}

// restLoginIdentity mirrors transport.Identity over JSON.
type restLoginIdentity struct {
	Subject  string   `json:"subject"`
	TenantID string   `json:"tenant_id"`
	Roles    []string `json:"roles,omitempty"`
}

// restFinishLoginResponse mirrors the REST login-finish JSON body.
type restFinishLoginResponse struct {
	Bearer    string             `json:"bearer"`
	Identity  *restLoginIdentity `json:"identity,omitempty"`
	ExpiresAt string             `json:"expires_at"`
	FactorID  string             `json:"factor_id,omitempty"`
	// RefreshToken / RefreshExpiresAt carry the session's renewal credential.
	RefreshToken     string `json:"refresh_token,omitempty"`
	RefreshExpiresAt string `json:"refresh_expires_at,omitempty"`
}

// handleBeginLogin posts a BeginLogin request to the REST login-begin endpoint.
func (h *authRESTHandler) handleBeginLogin(ctx context.Context, req interface{}) (interface{}, error) {
	r, ok := req.(*qrdbtransport.BeginLoginRequest)
	if !ok {
		return nil, errors.NewInvalidInput("authRESTHandler.handleBeginLogin",
			"invalid request type, expected *transport.BeginLoginRequest")
	}
	body := restBeginLoginRequest{
		Type:     string(r.Type),
		Hint:     r.Hint,
		TenantID: r.TenantID,
	}
	var resp restBeginLoginResponse
	if err := h.transport.doJSONRequest(ctx, http.MethodPost, "/api/v1/auth/login/begin", body, &resp); err != nil {
		return nil, err
	}
	out := &qrdbtransport.BeginLoginResponse{
		SessionID:         resp.SessionID,
		Nonce:             resp.Nonce,
		ChallengeMetadata: resp.ChallengeMetadata,
	}
	if resp.ExpiresAt != "" {
		if parsed, perr := time.Parse(time.RFC3339Nano, resp.ExpiresAt); perr == nil {
			out.ExpiresAt = parsed
		}
	}
	return out, nil
}

// handleFinishLogin posts a FinishLogin request to the REST login-finish
// endpoint. For an mtls factor the server derives the lookup key from the
// verified TLS peer certificate; the lookup_key sent here is ignored for mtls.
func (h *authRESTHandler) handleFinishLogin(ctx context.Context, req interface{}) (interface{}, error) {
	r, ok := req.(*qrdbtransport.FinishLoginRequest)
	if !ok {
		return nil, errors.NewInvalidInput("authRESTHandler.handleFinishLogin",
			"invalid request type, expected *transport.FinishLoginRequest")
	}
	body := restFinishLoginRequest{
		SessionID:      r.SessionID,
		Type:           string(r.Type),
		LookupKey:      r.LookupKey,
		AltLookupKeys:  r.AltLookupKeys,
		Signature:      r.Signature,
		MethodMetadata: r.MethodMetadata,
	}
	var resp restFinishLoginResponse
	if err := h.transport.doJSONRequest(ctx, http.MethodPost, "/api/v1/auth/login/finish", body, &resp); err != nil {
		return nil, err
	}
	inner := &qrdbtransport.FinishLoginResponse{
		Bearer:   resp.Bearer,
		FactorID: resp.FactorID,
	}
	if resp.ExpiresAt != "" {
		if parsed, perr := time.Parse(time.RFC3339Nano, resp.ExpiresAt); perr == nil {
			inner.ExpiresAt = parsed
		}
	}
	if resp.Identity != nil {
		inner.Identity = &qrdbtransport.Identity{
			Subject:  resp.Identity.Subject,
			TenantID: resp.Identity.TenantID,
			Roles:    resp.Identity.Roles,
		}
	}
	out := &sdktransport.FinishLoginResponse{
		FinishLoginResponse: inner,
		RefreshToken:        resp.RefreshToken,
	}
	if resp.RefreshExpiresAt != "" {
		if parsed, perr := time.Parse(time.RFC3339Nano, resp.RefreshExpiresAt); perr == nil {
			out.RefreshExpiresAt = parsed
		}
	}
	return out, nil
}
