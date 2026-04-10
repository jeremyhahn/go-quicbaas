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

package mcp

import (
	"strings"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// authHandler handles Auth-specific request/response transformations for MCP transport.
type authHandler struct {
	transport *Transport
}

// newAuthHandler creates a new Auth handler.
func newAuthHandler(t *Transport) *authHandler {
	return &authHandler{transport: t}
}

// transformResponse transforms MCP tool response to SDK auth types.
func (h *authHandler) transformResponse(method string, resp interface{}) (interface{}, error) {
	switch method {
	case "Login":
		return h.transformLoginResponse(resp)
	case "RefreshToken":
		return h.transformRefreshTokenResponse(resp)
	case "ValidateToken":
		return h.transformValidateTokenResponse(resp)
	default:
		// For other methods, return as-is
		return resp, nil
	}
}

// extractStringField extracts a string field from a map, checking both PascalCase and snake_case keys.
// Returns the value and true if found and successfully asserted as a string, or empty string and false otherwise.
func extractStringField(m map[string]interface{}, pascalKey, snakeKey string) (string, bool) {
	if v, ok := m[pascalKey]; ok {
		s, ok := v.(string)
		return s, ok
	}
	if v, ok := m[snakeKey]; ok {
		s, ok := v.(string)
		return s, ok
	}
	return "", false
}

// extractTimeField extracts a time field from a map, checking both PascalCase and snake_case keys.
// Returns the time value and true if found and successfully parsed, or zero time and false otherwise.
func extractTimeField(m map[string]interface{}, pascalKey, snakeKey string) (time.Time, bool) {
	if v, ok := m[pascalKey]; ok {
		if f, ok := v.(float64); ok {
			return time.Unix(0, int64(f)), true
		}
	}
	if v, ok := m[snakeKey]; ok {
		if f, ok := v.(float64); ok {
			return time.Unix(0, int64(f)), true
		}
	}
	return time.Time{}, false
}

// transformLoginResponse converts MCP login response to service.ExtendedLoginResponse.
func (h *authHandler) transformLoginResponse(resp interface{}) (*service.ExtendedLoginResponse, error) {
	m, ok := resp.(map[string]interface{})
	if !ok {
		return nil, errors.NewInternal("mcp.authHandler", "invalid response type for login")
	}

	result := &service.ExtendedLoginResponse{}

	// Handle both PascalCase (from server JSON) and snake_case keys
	accessToken, found := extractStringField(m, "AccessToken", "access_token")
	if !found {
		return nil, errors.NewInternal("mcp.authHandler", "missing or invalid access_token in login response")
	}
	result.AccessToken = accessToken

	refreshToken, found := extractStringField(m, "RefreshToken", "refresh_token")
	if found {
		result.RefreshToken = refreshToken
	}

	expiresAt, found := extractTimeField(m, "ExpiresAt", "expires_at")
	if found {
		result.ExpiresAt = expiresAt
	}

	tokenType, found := extractStringField(m, "TokenType", "token_type")
	if found {
		result.TokenType = tokenType
	}

	// Default token type if not set
	if result.TokenType == "" {
		result.TokenType = "Bearer"
	}

	return result, nil
}

// transformRefreshTokenResponse converts MCP refresh token response to service.ExtendedRefreshTokenResponse.
func (h *authHandler) transformRefreshTokenResponse(resp interface{}) (*service.ExtendedRefreshTokenResponse, error) {
	m, ok := resp.(map[string]interface{})
	if !ok {
		return nil, errors.NewInternal("mcp.authHandler", "invalid response type for refresh token")
	}

	result := &service.ExtendedRefreshTokenResponse{}

	// Handle both PascalCase and snake_case keys
	accessToken, found := extractStringField(m, "AccessToken", "access_token")
	if !found {
		return nil, errors.NewInternal("mcp.authHandler", "missing or invalid access_token in refresh token response")
	}
	result.AccessToken = accessToken

	refreshToken, found := extractStringField(m, "RefreshToken", "refresh_token")
	if found {
		result.RefreshToken = refreshToken
	}

	expiresAt, found := extractTimeField(m, "ExpiresAt", "expires_at")
	if found {
		result.ExpiresAt = expiresAt
	}

	tokenType, found := extractStringField(m, "TokenType", "token_type")
	if found {
		result.TokenType = tokenType
	}

	if result.TokenType == "" {
		result.TokenType = "Bearer"
	}

	return result, nil
}

// transformValidateTokenResponse converts MCP validate token response to service.TokenClaims.
func (h *authHandler) transformValidateTokenResponse(resp interface{}) (*service.TokenClaims, error) {
	m, ok := resp.(map[string]interface{})
	if !ok {
		return nil, errors.NewInternal("mcp.authHandler", "invalid response type for validate token")
	}

	result := &service.TokenClaims{}

	userID, found := extractStringField(m, "UserID", "user_id")
	if found {
		result.UserID = userID
	}

	username, found := extractStringField(m, "Username", "username")
	if found {
		result.Username = username
	}

	if v, ok := m["Roles"]; ok {
		if roles, ok := v.([]interface{}); ok {
			for _, r := range roles {
				if s, ok := r.(string); ok {
					result.Roles = append(result.Roles, s)
				}
			}
		}
	} else if v, ok := m["roles"]; ok {
		if roles, ok := v.([]interface{}); ok {
			for _, r := range roles {
				if s, ok := r.(string); ok {
					result.Roles = append(result.Roles, s)
				}
			}
		}
	}

	tenantID, found := extractStringField(m, "TenantID", "tenant_id")
	if found {
		result.TenantID = tenantID
	}

	expiresAt, found := extractTimeField(m, "ExpiresAt", "expires_at")
	if found {
		result.ExpiresAt = expiresAt
	}

	issuer, found := extractStringField(m, "Issuer", "issuer")
	if found {
		result.Issuer = issuer
	}

	return result, nil
}

// isMCPAuthService returns true if the service is an Auth service.
func isMCPAuthService(service string) bool {
	serviceName := service
	if idx := strings.LastIndex(service, "."); idx >= 0 {
		serviceName = service[idx+1:]
	}
	return serviceName == "Auth"
}
