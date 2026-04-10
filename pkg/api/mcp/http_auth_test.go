//go:build mcp

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
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExtractBearerToken(t *testing.T) {
	tests := []struct {
		name     string
		authHdr  string
		expected string
	}{
		{
			name:     "valid bearer token",
			authHdr:  "Bearer test-token-123",
			expected: "test-token-123",
		},
		{
			name:     "bearer lowercase",
			authHdr:  "bearer lowercase-token",
			expected: "lowercase-token",
		},
		{
			name:     "BEARER uppercase",
			authHdr:  "BEARER UPPERCASE-TOKEN",
			expected: "UPPERCASE-TOKEN",
		},
		{
			name:     "empty header",
			authHdr:  "",
			expected: "",
		},
		{
			name:     "no bearer prefix",
			authHdr:  "Basic dXNlcjpwYXNz",
			expected: "",
		},
		{
			name:     "just bearer",
			authHdr:  "Bearer",
			expected: "",
		},
		{
			name:     "token with spaces",
			authHdr:  "Bearer token with spaces",
			expected: "token with spaces",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			if tc.authHdr != "" {
				req.Header.Set("Authorization", tc.authHdr)
			}
			token := ExtractBearerToken(req)
			assert.Equal(t, tc.expected, token)
		})
	}
}

func TestWriteWWWAuthenticate(t *testing.T) {
	tests := []struct {
		name        string
		realm       string
		authError   WWWAuthenticateError
		description string
		scope       string
		expected    string
	}{
		{
			name:        "default realm only",
			realm:       "",
			authError:   "",
			description: "",
			scope:       "",
			expected:    `realm="quicbaas-mcp"`,
		},
		{
			name:        "custom realm",
			realm:       "my-app",
			authError:   "",
			description: "",
			scope:       "",
			expected:    `realm="my-app"`,
		},
		{
			name:        "with error",
			realm:       "test",
			authError:   WWWAuthenticateErrorInvalidToken,
			description: "",
			scope:       "",
			expected:    `error="invalid_token"`,
		},
		{
			name:        "with description",
			realm:       "test",
			authError:   WWWAuthenticateErrorInvalidToken,
			description: "token expired",
			scope:       "",
			expected:    `error_description="token expired"`,
		},
		{
			name:        "with scope",
			realm:       "test",
			authError:   WWWAuthenticateErrorInsufficientScope,
			description: "need admin",
			scope:       "admin",
			expected:    `scope="admin"`,
		},
		{
			name:        "full header",
			realm:       "app",
			authError:   WWWAuthenticateErrorInsufficientScope,
			description: "access denied",
			scope:       "read write",
			expected:    `realm="app", error="insufficient_scope", error_description="access denied", scope="read write"`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			WriteWWWAuthenticate(w, tc.realm, tc.authError, tc.description, tc.scope)
			header := w.Header().Get("WWW-Authenticate")
			assert.True(t, strings.HasPrefix(header, "Bearer "), "Should start with Bearer")
			assert.Contains(t, header, tc.expected)
		})
	}
}

func TestRespondAuthError(t *testing.T) {
	tests := []struct {
		name             string
		realm            string
		authErr          *HTTPAuthError
		expectedStatus   int
		expectedJSONCode int
	}{
		{
			name:  "unauthorized - missing token",
			realm: "test",
			authErr: &HTTPAuthError{
				StatusCode: http.StatusUnauthorized,
				Error:      WWWAuthenticateErrorInvalidRequest,
				Message:    "authentication required",
			},
			expectedStatus:   http.StatusUnauthorized,
			expectedJSONCode: AuthenticationRequired,
		},
		{
			name:  "unauthorized - invalid token",
			realm: "test",
			authErr: &HTTPAuthError{
				StatusCode: http.StatusUnauthorized,
				Error:      WWWAuthenticateErrorInvalidToken,
				Message:    "token expired",
			},
			expectedStatus:   http.StatusUnauthorized,
			expectedJSONCode: InvalidToken,
		},
		{
			name:  "forbidden - insufficient scope",
			realm: "test",
			authErr: &HTTPAuthError{
				StatusCode: http.StatusForbidden,
				Error:      WWWAuthenticateErrorInsufficientScope,
				Message:    "need admin",
				Scope:      "admin",
			},
			expectedStatus:   http.StatusForbidden,
			expectedJSONCode: PermissionDenied,
		},
		{
			name:  "unknown status - internal error",
			realm: "test",
			authErr: &HTTPAuthError{
				StatusCode: http.StatusBadGateway,
				Error:      "",
				Message:    "upstream error",
			},
			expectedStatus:   http.StatusBadGateway,
			expectedJSONCode: InternalError,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/", nil)
			w := httptest.NewRecorder()

			RespondAuthError(w, req, tc.realm, tc.authErr)

			assert.Equal(t, tc.expectedStatus, w.Code)
			assert.Equal(t, "application/json", w.Header().Get("Content-Type"))
			assert.NotEmpty(t, w.Header().Get("WWW-Authenticate"))

			var resp JSONRPCResponse
			err := json.Unmarshal(w.Body.Bytes(), &resp)
			require.NoError(t, err)
			assert.Equal(t, "2.0", resp.JSONRPC)
			require.NotNil(t, resp.Error)
			assert.Equal(t, tc.expectedJSONCode, resp.Error.Code)
			assert.Equal(t, tc.authErr.Message, resp.Error.Message)
		})
	}
}

func TestErrorMissingToken(t *testing.T) {
	err := ErrorMissingToken()
	require.NotNil(t, err)
	assert.Equal(t, http.StatusUnauthorized, err.StatusCode)
	assert.Equal(t, WWWAuthenticateErrorInvalidRequest, err.Error)
	assert.Equal(t, "authentication required", err.Message)
}

func TestErrorInvalidToken(t *testing.T) {
	tests := []struct {
		name        string
		description string
		expected    string
	}{
		{
			name:        "empty description",
			description: "",
			expected:    "token is invalid or expired",
		},
		{
			name:        "custom description",
			description: "token signature mismatch",
			expected:    "token signature mismatch",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := ErrorInvalidToken(tc.description)
			require.NotNil(t, err)
			assert.Equal(t, http.StatusUnauthorized, err.StatusCode)
			assert.Equal(t, WWWAuthenticateErrorInvalidToken, err.Error)
			assert.Equal(t, tc.expected, err.Message)
		})
	}
}

func TestErrorInsufficientScope(t *testing.T) {
	err := ErrorInsufficientScope("admin write")
	require.NotNil(t, err)
	assert.Equal(t, http.StatusForbidden, err.StatusCode)
	assert.Equal(t, WWWAuthenticateErrorInsufficientScope, err.Error)
	assert.Equal(t, "insufficient scope", err.Message)
	assert.Equal(t, "admin write", err.Scope)
}

func TestErrorPermissionDenied(t *testing.T) {
	tests := []struct {
		name     string
		message  string
		expected string
	}{
		{
			name:     "empty message",
			message:  "",
			expected: "permission denied",
		},
		{
			name:     "custom message",
			message:  "user not authorized for this resource",
			expected: "user not authorized for this resource",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := ErrorPermissionDenied(tc.message)
			require.NotNil(t, err)
			assert.Equal(t, http.StatusForbidden, err.StatusCode)
			assert.Equal(t, WWWAuthenticateErrorInsufficientScope, err.Error)
			assert.Equal(t, tc.expected, err.Message)
		})
	}
}
