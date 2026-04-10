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
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/auth"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGetBearerTokenString tests the getBearerTokenString helper function.
func TestGetBearerTokenString(t *testing.T) {
	t.Run("valid bearer token", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.Header.Set("Authorization", "Bearer mytoken123")

		token := getBearerTokenString(req)
		assert.Equal(t, "mytoken123", token)
	})

	t.Run("bearer token case insensitive", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.Header.Set("Authorization", "BEARER mytoken123")

		token := getBearerTokenString(req)
		assert.Equal(t, "mytoken123", token)
	})

	t.Run("no authorization header", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)

		token := getBearerTokenString(req)
		assert.Equal(t, "", token)
	})

	t.Run("basic auth instead of bearer", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.Header.Set("Authorization", "Basic dXNlcjpwYXNz")

		token := getBearerTokenString(req)
		assert.Equal(t, "", token)
	})

	t.Run("malformed header - single word", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.Header.Set("Authorization", "BearerNoSpace")

		token := getBearerTokenString(req)
		assert.Equal(t, "", token)
	})

	t.Run("token with leading/trailing spaces", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.Header.Set("Authorization", "Bearer   mytoken123   ")

		token := getBearerTokenString(req)
		assert.Equal(t, "mytoken123", token)
	})
}

type mockLoginAuthManager struct {
	authenticateFunc func(ctx context.Context, username, password string) (*auth.Token, error)
	refreshFunc      func(ctx context.Context, refreshToken string) (*auth.Token, error)
	revokeFunc       func(ctx context.Context, token string) error
	validateFunc     func(ctx context.Context, token string) (*auth.TokenValidationResult, error)
	createUserFunc   func(username, password string, roles []string) (*auth.User, error)
}

func (m *mockLoginAuthManager) Authenticate(ctx context.Context, username, password string) (*auth.Token, error) {
	if m.authenticateFunc != nil {
		return m.authenticateFunc(ctx, username, password)
	}
	return nil, errors.New("not implemented")
}

func (m *mockLoginAuthManager) RefreshToken(ctx context.Context, refreshToken string) (*auth.Token, error) {
	if m.refreshFunc != nil {
		return m.refreshFunc(ctx, refreshToken)
	}
	return nil, errors.New("not implemented")
}

func (m *mockLoginAuthManager) RevokeToken(ctx context.Context, token string) error {
	if m.revokeFunc != nil {
		return m.revokeFunc(ctx, token)
	}
	return errors.New("not implemented")
}

func (m *mockLoginAuthManager) ValidateToken(ctx context.Context, token string) (*auth.TokenValidationResult, error) {
	if m.validateFunc != nil {
		return m.validateFunc(ctx, token)
	}
	return nil, errors.New("not implemented")
}

func (m *mockLoginAuthManager) CreateUser(username, password string, roles []string) (*auth.User, error) {
	if m.createUserFunc != nil {
		return m.createUserFunc(username, password, roles)
	}
	return &auth.User{
		ID:       "user-123",
		Username: username,
		Created:  time.Now(),
	}, nil
}

func (m *mockLoginAuthManager) IsPasswordAuthEnabled() bool {
	return true
}

type mockQuicBaaSWithLoginAuth struct {
	*testutil.MockQuicBaaS
	authMgr service.AuthManagerInterface
}

func (m *mockQuicBaaSWithLoginAuth) Login(ctx context.Context, req *service.LoginRequest) (*service.LoginResponse, error) {
	if m.authMgr == nil {
		return nil, errors.New("authentication not configured")
	}
	token, err := m.authMgr.Authenticate(ctx, req.Username, req.Password)
	if err != nil {
		return nil, err
	}
	return &service.LoginResponse{
		AccessToken:  token.AccessToken,
		RefreshToken: token.RefreshToken,
		ExpiresAt:    token.ExpiresAt.Unix(),
		UserID:       "user-123", // auth.Token doesn't include UserID
	}, nil
}

func (m *mockQuicBaaSWithLoginAuth) RefreshToken(ctx context.Context, req *service.RefreshTokenRequest) (*service.RefreshTokenResponse, error) {
	if m.authMgr == nil {
		return nil, errors.New("authentication not configured")
	}
	token, err := m.authMgr.RefreshToken(ctx, req.RefreshToken)
	if err != nil {
		return nil, err
	}
	return &service.RefreshTokenResponse{
		AccessToken: token.AccessToken,
		ExpiresAt:   token.ExpiresAt.Unix(),
	}, nil
}

func (m *mockQuicBaaSWithLoginAuth) Logout(ctx context.Context, req *service.LogoutRequest) (*service.LogoutResponse, error) {
	if m.authMgr == nil {
		return nil, errors.New("authentication not configured")
	}
	err := m.authMgr.RevokeToken(ctx, req.AccessToken)
	if err != nil {
		return nil, err
	}
	return &service.LogoutResponse{Success: true}, nil
}

func (m *mockQuicBaaSWithLoginAuth) ValidateToken(ctx context.Context, req *service.ValidateTokenRequest) (*service.ValidateTokenResponse, error) {
	if m.authMgr == nil {
		return nil, errors.New("authentication not configured")
	}
	result, err := m.authMgr.ValidateToken(ctx, req.AccessToken)
	if err != nil {
		return nil, err
	}
	return &service.ValidateTokenResponse{
		Valid:     result.Valid,
		UserID:    result.UserID,
		Username:  result.Username,
		Roles:     result.Roles,
		ExpiresAt: result.ExpiresAt.Unix(),
	}, nil
}

func setupIAMHandlerWithLoginAuth(t *testing.T, authMgr service.AuthManagerInterface) (*IAMHandler, *Server, func()) {
	t.Helper()

	log, err := logger.New(logger.LogConfig{
		Level:  "error",
		Format: "json",
		Output: "stdout",
	})
	require.NoError(t, err)

	mockService := &mockQuicBaaSWithLoginAuth{
		MockQuicBaaS: testutil.NewMockQuicBaaS(),
		authMgr:      authMgr,
	}

	serverConfig := DefaultConfig()
	serverConfig.Address = ":0"

	server, err := NewServer(mockService.MockQuicBaaS, serverConfig, log)
	require.NoError(t, err)

	// Replace the service with our auth-enabled mock
	server.service = mockService

	handler := &IAMHandler{
		server: server,
	}

	cleanup := func() {
		server.Close()
	}

	return handler, server, cleanup
}

func TestLoginWithMockLoginAuthManager(t *testing.T) {
	t.Run("successful login", func(t *testing.T) {
		authMgr := &mockLoginAuthManager{
			authenticateFunc: func(ctx context.Context, username, password string) (*auth.Token, error) {
				return &auth.Token{
					AccessToken:  "access-token-123",
					RefreshToken: "refresh-token-456",
					TokenType:    "Bearer",
					ExpiresAt:    time.Now().Add(1 * time.Hour),
				}, nil
			},
		}

		handler, _, cleanup := setupIAMHandlerWithLoginAuth(t, authMgr)
		defer cleanup()

		body := `{"username":"admin","password":"secret123"}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handler.Login(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), "access_token")
		assert.Contains(t, w.Body.String(), "refresh_token")
	})

	t.Run("authentication failure", func(t *testing.T) {
		authMgr := &mockLoginAuthManager{
			authenticateFunc: func(ctx context.Context, username, password string) (*auth.Token, error) {
				return nil, errors.New("invalid credentials")
			},
		}

		handler, _, cleanup := setupIAMHandlerWithLoginAuth(t, authMgr)
		defer cleanup()

		body := `{"username":"admin","password":"wrongpassword"}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handler.Login(w, req)

		assert.Equal(t, http.StatusUnauthorized, w.Code)
		assert.Contains(t, w.Body.String(), "invalid credentials")
	})

	t.Run("empty request body", func(t *testing.T) {
		authMgr := &mockLoginAuthManager{}

		handler, _, cleanup := setupIAMHandlerWithLoginAuth(t, authMgr)
		defer cleanup()

		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(""))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handler.Login(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("missing username", func(t *testing.T) {
		authMgr := &mockLoginAuthManager{}

		handler, _, cleanup := setupIAMHandlerWithLoginAuth(t, authMgr)
		defer cleanup()

		body := `{"password":"secret123"}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handler.Login(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "username is required")
	})

	t.Run("missing password", func(t *testing.T) {
		authMgr := &mockLoginAuthManager{}

		handler, _, cleanup := setupIAMHandlerWithLoginAuth(t, authMgr)
		defer cleanup()

		body := `{"username":"admin"}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handler.Login(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "password is required")
	})

	t.Run("read body error", func(t *testing.T) {
		authMgr := &mockLoginAuthManager{}

		handler, _, cleanup := setupIAMHandlerWithLoginAuth(t, authMgr)
		defer cleanup()

		// Create a request with an error-producing body
		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", &loginErrorReader{})
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handler.Login(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "failed to read request body")
	})
}

func TestRefreshTokenWithMockLoginAuthManager(t *testing.T) {
	t.Run("successful refresh", func(t *testing.T) {
		authMgr := &mockLoginAuthManager{
			refreshFunc: func(ctx context.Context, refreshToken string) (*auth.Token, error) {
				return &auth.Token{
					AccessToken:  "new-access-token",
					RefreshToken: "new-refresh-token",
					TokenType:    "Bearer",
					ExpiresAt:    time.Now().Add(1 * time.Hour),
				}, nil
			},
		}

		handler, _, cleanup := setupIAMHandlerWithLoginAuth(t, authMgr)
		defer cleanup()

		body := `{"refresh_token":"old-refresh-token"}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/refresh", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handler.RefreshToken(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), "new-access-token")
	})

	t.Run("refresh failure - expired token", func(t *testing.T) {
		authMgr := &mockLoginAuthManager{
			refreshFunc: func(ctx context.Context, refreshToken string) (*auth.Token, error) {
				return nil, errors.New("token expired")
			},
		}

		handler, _, cleanup := setupIAMHandlerWithLoginAuth(t, authMgr)
		defer cleanup()

		body := `{"refresh_token":"expired-token"}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/refresh", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handler.RefreshToken(w, req)

		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("missing refresh token", func(t *testing.T) {
		authMgr := &mockLoginAuthManager{}

		handler, _, cleanup := setupIAMHandlerWithLoginAuth(t, authMgr)
		defer cleanup()

		body := `{}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/refresh", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handler.RefreshToken(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "refresh_token is required")
	})

	t.Run("invalid JSON", func(t *testing.T) {
		authMgr := &mockLoginAuthManager{}

		handler, _, cleanup := setupIAMHandlerWithLoginAuth(t, authMgr)
		defer cleanup()

		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/refresh", strings.NewReader("{invalid"))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handler.RefreshToken(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("read body error", func(t *testing.T) {
		authMgr := &mockLoginAuthManager{}

		handler, _, cleanup := setupIAMHandlerWithLoginAuth(t, authMgr)
		defer cleanup()

		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/refresh", &loginErrorReader{})
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handler.RefreshToken(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})
}

// =============================================================================
// Logout Handler Tests with Mock Auth Manager
// =============================================================================

func TestLogoutWithMockLoginAuthManager(t *testing.T) {
	t.Run("successful logout with header token", func(t *testing.T) {
		authMgr := &mockLoginAuthManager{
			revokeFunc: func(ctx context.Context, token string) error {
				return nil
			},
		}

		handler, _, cleanup := setupIAMHandlerWithLoginAuth(t, authMgr)
		defer cleanup()

		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
		req.Header.Set("Authorization", "Bearer my-access-token")
		w := httptest.NewRecorder()

		handler.Logout(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), "logged out successfully")
	})

	t.Run("successful logout with body token", func(t *testing.T) {
		authMgr := &mockLoginAuthManager{
			revokeFunc: func(ctx context.Context, token string) error {
				return nil
			},
		}

		handler, _, cleanup := setupIAMHandlerWithLoginAuth(t, authMgr)
		defer cleanup()

		body := `{"access_token":"body-access-token"}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handler.Logout(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("logout failure - revoke error", func(t *testing.T) {
		authMgr := &mockLoginAuthManager{
			revokeFunc: func(ctx context.Context, token string) error {
				return errors.New("failed to revoke")
			},
		}

		handler, _, cleanup := setupIAMHandlerWithLoginAuth(t, authMgr)
		defer cleanup()

		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
		req.Header.Set("Authorization", "Bearer my-access-token")
		w := httptest.NewRecorder()

		handler.Logout(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})

	t.Run("missing token - no header no body", func(t *testing.T) {
		authMgr := &mockLoginAuthManager{}

		handler, _, cleanup := setupIAMHandlerWithLoginAuth(t, authMgr)
		defer cleanup()

		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
		w := httptest.NewRecorder()

		handler.Logout(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "access token is required")
	})

	t.Run("empty body token falls back to missing", func(t *testing.T) {
		authMgr := &mockLoginAuthManager{}

		handler, _, cleanup := setupIAMHandlerWithLoginAuth(t, authMgr)
		defer cleanup()

		body := `{"access_token":""}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handler.Logout(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("invalid JSON body still checks header", func(t *testing.T) {
		authMgr := &mockLoginAuthManager{
			revokeFunc: func(ctx context.Context, token string) error {
				return nil
			},
		}

		handler, _, cleanup := setupIAMHandlerWithLoginAuth(t, authMgr)
		defer cleanup()

		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", strings.NewReader("{invalid"))
		req.Header.Set("Authorization", "Bearer header-token")
		w := httptest.NewRecorder()

		handler.Logout(w, req)

		// Should use header token
		assert.Equal(t, http.StatusOK, w.Code)
	})
}

// =============================================================================
// ValidateToken Handler Tests with Mock Auth Manager
// =============================================================================

func TestValidateTokenWithMockLoginAuthManager(t *testing.T) {
	t.Run("successful validation - valid token", func(t *testing.T) {
		authMgr := &mockLoginAuthManager{
			validateFunc: func(ctx context.Context, token string) (*auth.TokenValidationResult, error) {
				return &auth.TokenValidationResult{
					Valid:     true,
					UserID:    "user-123",
					Username:  "admin",
					TenantID:  "tenant-1",
					Roles:     []string{"admin", "user"},
					ExpiresAt: time.Now().Add(1 * time.Hour),
					IsRevoked: false,
				}, nil
			},
		}

		handler, _, cleanup := setupIAMHandlerWithLoginAuth(t, authMgr)
		defer cleanup()

		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/validate", nil)
		req.Header.Set("Authorization", "Bearer valid-token")
		w := httptest.NewRecorder()

		handler.ValidateToken(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), "\"valid\":true")
		assert.Contains(t, w.Body.String(), "user-123")
	})

	t.Run("validation failure - invalid token", func(t *testing.T) {
		authMgr := &mockLoginAuthManager{
			validateFunc: func(ctx context.Context, token string) (*auth.TokenValidationResult, error) {
				return nil, errors.New("invalid token")
			},
		}

		handler, _, cleanup := setupIAMHandlerWithLoginAuth(t, authMgr)
		defer cleanup()

		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/validate", nil)
		req.Header.Set("Authorization", "Bearer invalid-token")
		w := httptest.NewRecorder()

		handler.ValidateToken(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), "\"valid\":false")
	})

	t.Run("missing authorization header", func(t *testing.T) {
		authMgr := &mockLoginAuthManager{}

		handler, _, cleanup := setupIAMHandlerWithLoginAuth(t, authMgr)
		defer cleanup()

		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/validate", nil)
		w := httptest.NewRecorder()

		handler.ValidateToken(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "access token is required")
	})

	t.Run("malformed authorization header", func(t *testing.T) {
		authMgr := &mockLoginAuthManager{}

		handler, _, cleanup := setupIAMHandlerWithLoginAuth(t, authMgr)
		defer cleanup()

		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/validate", nil)
		req.Header.Set("Authorization", "InvalidFormat")
		w := httptest.NewRecorder()

		handler.ValidateToken(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})
}

// =============================================================================
// Auth Not Configured Tests
// =============================================================================

func TestLoginAuthNotConfiguredPaths(t *testing.T) {
	t.Run("auth not configured (nil auth manager)", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandlerWithLoginAuth(t, nil)
		defer cleanup()

		body := `{"username":"test","password":"test"}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handler.Login(w, req)

		// Should return 401 Unauthorized with credentials error since auth is not configured
		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})
}

// =============================================================================
// Helper Types
// =============================================================================

// loginErrorReader is an io.Reader that always returns an error.
type loginErrorReader struct{}

func (e *loginErrorReader) Read(p []byte) (n int, err error) {
	return 0, errors.New("read error")
}

// =============================================================================
// getBearerTokenString Edge Cases
// =============================================================================

func TestGetBearerTokenStringEdgeCasesMore(t *testing.T) {
	t.Run("empty authorization header", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.Header.Set("Authorization", "")

		token := getBearerTokenString(req)
		assert.Equal(t, "", token)
	})

	t.Run("bearer with multiple spaces", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.Header.Set("Authorization", "Bearer    token   with   spaces")

		token := getBearerTokenString(req)
		// Should get trimmed token
		assert.NotEmpty(t, token)
	})

	t.Run("lowercase bearer", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.Header.Set("Authorization", "bearer mytoken")

		token := getBearerTokenString(req)
		assert.Equal(t, "mytoken", token)
	})

	t.Run("mixed case bearer", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.Header.Set("Authorization", "BeArEr mytoken")

		token := getBearerTokenString(req)
		assert.Equal(t, "mytoken", token)
	})
}

// =============================================================================
// Request Body Handling Tests
// =============================================================================

func TestLoginRequestBodyHandling(t *testing.T) {
	t.Run("login with large body", func(t *testing.T) {
		authMgr := &mockLoginAuthManager{}

		handler, _, cleanup := setupIAMHandlerWithLoginAuth(t, authMgr)
		defer cleanup()

		// Create a large body
		largeBody := bytes.Repeat([]byte("x"), 10000)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader(largeBody))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handler.Login(w, req)

		// Should fail to parse JSON
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("refresh with unicode body", func(t *testing.T) {
		authMgr := &mockLoginAuthManager{}

		handler, _, cleanup := setupIAMHandlerWithLoginAuth(t, authMgr)
		defer cleanup()

		body := `{"refresh_token":"token-with-unicode-\u0000"}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/refresh", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handler.RefreshToken(w, req)

		// Should parse JSON successfully
		assert.NotEqual(t, http.StatusServiceUnavailable, w.Code)
	})
}

// =============================================================================
// Concurrent Request Tests
// =============================================================================

func TestConcurrentLoginRequestsMore(t *testing.T) {
	authMgr := &mockLoginAuthManager{
		authenticateFunc: func(ctx context.Context, username, password string) (*auth.Token, error) {
			return &auth.Token{
				AccessToken:  "token-" + username,
				RefreshToken: "refresh-" + username,
				TokenType:    "Bearer",
				ExpiresAt:    time.Now().Add(1 * time.Hour),
			}, nil
		},
	}

	handler, _, cleanup := setupIAMHandlerWithLoginAuth(t, authMgr)
	defer cleanup()

	// Run multiple concurrent login requests
	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func(i int) {
			body := `{"username":"user` + string(rune('0'+i)) + `","password":"secret"}`
			req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			handler.Login(w, req)

			assert.Equal(t, http.StatusOK, w.Code)
			done <- true
		}(i)
	}

	// Wait for all goroutines to complete
	for i := 0; i < 10; i++ {
		<-done
	}
}

// =============================================================================
// Error Response Tests
// =============================================================================

func TestLoginErrorResponseFormat(t *testing.T) {
	authMgr := &mockLoginAuthManager{}

	handler, _, cleanup := setupIAMHandlerWithLoginAuth(t, authMgr)
	defer cleanup()

	t.Run("bad request error format", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(`{}`))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handler.Login(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		// Should return JSON error
		assert.Contains(t, w.Header().Get("Content-Type"), "application/json")
	})
}
