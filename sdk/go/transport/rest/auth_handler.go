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

	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/service"
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

// loginRequest is the internal request type used by the auth client.
type authLoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
	MFACode  string `json:"mfa_code,omitempty"`
	TenantID string `json:"tenant_id,omitempty"`
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
	default:
		return nil, errors.NewUnimplemented("authRESTHandler.handleRequest",
			"unsupported method: "+method)
	}
}

// handleLogin handles Login requests.
func (h *authRESTHandler) handleLogin(ctx context.Context, req interface{}) (interface{}, error) {
	// Build the request body
	var loginReq authLoginRequest

	switch r := req.(type) {
	case *service.ExtendedLoginRequest:
		// TODO: Password-based login was removed from ExtendedLoginRequest; auth must now
		// flow through WebAuthn or PIV smartcard. Pass an empty password until the REST
		// payload schema carries the new credential evidence.
		loginReq = authLoginRequest{
			Username: r.Username,
			Password: "",
			MFACode:  r.MFACode,
			TenantID: r.TenantID,
		}
	case map[string]interface{}:
		// Handle map[string]interface{} from SDK
		loginReq = authLoginRequest{
			Username: getString(r, "username"),
			Password: getString(r, "password"),
			MFACode:  getString(r, "mfa_code"),
			TenantID: getString(r, "tenant_id"),
		}
	default:
		// Try to extract from the internal loginRequest type
		reqMap, err := toMap(req)
		if err != nil {
			return nil, errors.NewInvalidInput("authRESTHandler.handleLogin",
				"invalid request type")
		}
		loginReq = authLoginRequest{
			Username: getString(reqMap, "username"),
			Password: getString(reqMap, "password"),
			MFACode:  getString(reqMap, "mfa_code"),
			TenantID: getString(reqMap, "tenant_id"),
		}
	}

	// Make the HTTP request
	var resp authLoginResponse
	err := h.transport.doJSONRequest(ctx, http.MethodPost, "/api/v1/auth/login", loginReq, &resp)
	if err != nil {
		return nil, err
	}

	// Parse expires_at timestamp
	expiresAt, err := time.Parse(time.RFC3339, resp.ExpiresAt)
	if err != nil {
		// Fallback: calculate from expires_in
		expiresAt = time.Now().Add(time.Duration(resp.ExpiresIn) * time.Second)
	}

	// Convert to SDK response type
	return &service.ExtendedLoginResponse{
		AccessToken:  resp.AccessToken,
		RefreshToken: resp.RefreshToken,
		TokenType:    resp.TokenType,
		ExpiresAt:    expiresAt,
		MFARequired:  false,
		MFAChallenge: nil,
	}, nil
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
		refreshToken = getString(r, "refresh_token")
	default:
		reqMap, err := toMap(req)
		if err != nil {
			return nil, errors.NewInvalidInput("authRESTHandler.handleRefreshToken",
				"invalid request type")
		}
		refreshToken = getString(reqMap, "refresh_token")
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

	// Convert to SDK response type
	return &service.ExtendedRefreshTokenResponse{
		AccessToken: resp.AccessToken,
		TokenType:   resp.TokenType,
		ExpiresAt:   expiresAt,
	}, nil
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
