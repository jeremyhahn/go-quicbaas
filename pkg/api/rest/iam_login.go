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
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// LoginResponse represents the response from a successful WebAuthn or PIV login.
type LoginResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int64  `json:"expires_in"`
	ExpiresAt    string `json:"expires_at"`
}

// RefreshTokenRequest represents the request body for token refresh.
type RefreshTokenRequest struct {
	RefreshToken string `json:"refresh_token"`
}

// LogoutRequest represents the request body for logout.
type LogoutRequest struct {
	AccessToken string `json:"access_token,omitempty"`
}

// RefreshToken handles POST /api/v1/auth/refresh - Refresh access token.
func (h *IAMHandler) RefreshToken(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Read request body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "failed to read request body"))
		return
	}
	defer r.Body.Close()

	var req RefreshTokenRequest
	if err := json.Unmarshal(body, &req); err != nil {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "invalid request body"))
		return
	}

	// Validate required fields
	if req.RefreshToken == "" {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "refresh_token is required"))
		return
	}

	// Use service layer for refresh (like gRPC does)
	serviceReq := &service.RefreshTokenRequest{
		RefreshToken: req.RefreshToken,
	}

	resp, err := h.server.service.RefreshToken(ctx, serviceReq)
	if err != nil {
		// Check for configuration error
		if qbErr, ok := err.(*errors.QBError); ok && qbErr.Code == errors.ErrConfiguration {
			RespondIAMError(w, r, http.StatusServiceUnavailable,
				NewIAMAPIError(ErrInternalError, "authentication service not configured"))
			return
		}
		RespondIAMError(w, r, http.StatusUnauthorized,
			NewIAMAPIError(ErrUnauthorized, "invalid or expired refresh token"))
		return
	}

	// Build response
	expiresIn := resp.ExpiresAt - time.Now().Unix()
	if expiresIn < 0 {
		expiresIn = 0
	}

	response := &LoginResponse{
		AccessToken:  resp.AccessToken,
		RefreshToken: "", // RefreshTokenResponse doesn't include a new refresh token
		TokenType:    "Bearer",
		ExpiresIn:    expiresIn,
		ExpiresAt:    time.Unix(resp.ExpiresAt, 0).Format(time.RFC3339),
	}
	RespondJSON(w, r, http.StatusOK, response)
}

// Logout handles POST /api/v1/auth/logout - Revoke access token.
func (h *IAMHandler) Logout(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Try to get token from Authorization header first
	token := getBearerTokenString(r)

	// If not in header, try request body
	if token == "" {
		body, err := io.ReadAll(r.Body)
		if err == nil && len(body) > 0 {
			var req LogoutRequest
			if json.Unmarshal(body, &req) == nil && req.AccessToken != "" {
				token = req.AccessToken
			}
		}
		defer r.Body.Close()
	}

	if token == "" {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "access token is required (provide in Authorization header or request body)"))
		return
	}

	// Use service layer for logout (like gRPC does)
	serviceReq := &service.LogoutRequest{
		AccessToken: token,
	}

	_, err := h.server.service.Logout(ctx, serviceReq)
	if err != nil {
		// Check for configuration error
		if qbErr, ok := err.(*errors.QBError); ok && qbErr.Code == errors.ErrConfiguration {
			RespondIAMError(w, r, http.StatusServiceUnavailable,
				NewIAMAPIError(ErrInternalError, "authentication service not configured"))
			return
		}
		// Log but don't expose internal errors
		RespondIAMError(w, r, http.StatusInternalServerError,
			NewIAMAPIError(ErrInternalError, "failed to revoke token"))
		return
	}

	RespondJSON(w, r, http.StatusOK, map[string]interface{}{
		"message": "logged out successfully",
	})
}

// ValidateToken handles POST /api/v1/auth/validate - Validate access token.
func (h *IAMHandler) ValidateToken(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Get token from Authorization header
	token := getBearerTokenString(r)
	if token == "" {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "access token is required in Authorization header"))
		return
	}

	// Use service layer for validation (like gRPC does)
	serviceReq := &service.ValidateTokenRequest{
		AccessToken: token,
	}

	resp, err := h.server.service.ValidateToken(ctx, serviceReq)
	if err != nil {
		RespondJSON(w, r, http.StatusOK, map[string]interface{}{
			"valid":   false,
			"message": "token validation failed",
		})
		return
	}

	RespondJSON(w, r, http.StatusOK, map[string]interface{}{
		"valid":      resp.Valid,
		"user_id":    resp.UserID,
		"username":   resp.Username,
		"roles":      resp.Roles,
		"expires_at": time.Unix(resp.ExpiresAt, 0).Format(time.RFC3339),
	})
}

// getBearerTokenString extracts the bearer token from the Authorization header.
// Returns empty string if token is not found or invalid.
func getBearerTokenString(r *http.Request) string {
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		return ""
	}

	parts := strings.SplitN(authHeader, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return ""
	}

	return strings.TrimSpace(parts[1])
}
