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

// Package main provides tests for authentication CLI commands.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/cli"
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/auth"
)

// mockSDKAuthClient is a mock implementation of auth.Client for testing.
type mockSDKAuthClient struct {
	LoginFunc         func(ctx context.Context, req *service.ExtendedLoginRequest) (*service.ExtendedLoginResponse, error)
	LogoutFunc        func(ctx context.Context, accessToken string) error
	RefreshTokenFunc  func(ctx context.Context, refreshToken string) (*service.ExtendedRefreshTokenResponse, error)
	ValidateTokenFunc func(ctx context.Context, accessToken string) (*service.TokenClaims, error)
}

func (m *mockSDKAuthClient) Login(ctx context.Context, req *service.ExtendedLoginRequest) (*service.ExtendedLoginResponse, error) {
	if m.LoginFunc != nil {
		return m.LoginFunc(ctx, req)
	}
	return &service.ExtendedLoginResponse{
		AccessToken:  "mock-access-token",
		RefreshToken: "mock-refresh-token",
		ExpiresAt:    time.Now().Add(1 * time.Hour),
		TokenType:    "Bearer",
	}, nil
}

func (m *mockSDKAuthClient) Logout(ctx context.Context, accessToken string) error {
	if m.LogoutFunc != nil {
		return m.LogoutFunc(ctx, accessToken)
	}
	return nil
}

func (m *mockSDKAuthClient) RefreshToken(ctx context.Context, refreshToken string) (*service.ExtendedRefreshTokenResponse, error) {
	if m.RefreshTokenFunc != nil {
		return m.RefreshTokenFunc(ctx, refreshToken)
	}
	return &service.ExtendedRefreshTokenResponse{
		AccessToken:  "refreshed-access-token",
		ExpiresAt:    time.Now().Add(1 * time.Hour),
		TokenType:    "Bearer",
		RefreshToken: "new-refresh-token",
	}, nil
}

func (m *mockSDKAuthClient) ValidateToken(ctx context.Context, accessToken string) (*service.TokenClaims, error) {
	if m.ValidateTokenFunc != nil {
		return m.ValidateTokenFunc(ctx, accessToken)
	}
	return &service.TokenClaims{
		Valid:     true,
		UserID:    "user-123",
		Username:  "testuser",
		ExpiresAt: time.Now().Add(1 * time.Hour),
	}, nil
}

// Implement remaining auth.Client interface methods with no-ops for testing.
func (m *mockSDKAuthClient) BeginFederatedLogin(ctx context.Context, req *service.BeginFederatedLoginRequest) (*service.BeginFederatedLoginResponse, error) {
	return nil, nil
}

func (m *mockSDKAuthClient) HandleFederatedCallback(ctx context.Context, req *service.HandleFederatedCallbackRequest) (*service.HandleFederatedCallbackResponse, error) {
	return nil, nil
}

func (m *mockSDKAuthClient) ListProviders(ctx context.Context, tenantID string) (*service.AuthListProvidersResponse, error) {
	return nil, nil
}

func (m *mockSDKAuthClient) GetProvider(ctx context.Context, tenantID, providerID string) (*service.AuthProvider, error) {
	return nil, nil
}

func (m *mockSDKAuthClient) GetSession(ctx context.Context, sessionID string) (*service.AuthSession, error) {
	return nil, nil
}

func (m *mockSDKAuthClient) ListSessions(ctx context.Context) (*service.AuthListSessionsResponse, error) {
	return nil, nil
}

func (m *mockSDKAuthClient) RevokeSession(ctx context.Context, sessionID string) error {
	return nil
}

func (m *mockSDKAuthClient) RevokeAllSessions(ctx context.Context) error {
	return nil
}

func (m *mockSDKAuthClient) IntrospectToken(ctx context.Context, token string) (*service.TokenIntrospection, error) {
	return nil, nil
}

func (m *mockSDKAuthClient) GetJWKS(ctx context.Context) (*service.JWKS, error) {
	return nil, nil
}

func (m *mockSDKAuthClient) GetOpenIDConfiguration(ctx context.Context) (*service.OpenIDConfiguration, error) {
	return nil, nil
}


// TestAuthLogoutCommand_Success tests successful logout scenarios.
func TestAuthLogoutCommand_Success(t *testing.T) {
	tmpDir := t.TempDir()

	tests := []struct {
		name         string
		setupToken   bool
		tokenFile    string
		factoryErr   error
		logoutErr    error
		checkNoToken bool
	}{
		{
			name:         "logout with existing token",
			setupToken:   true,
			tokenFile:    filepath.Join(tmpDir, "token1"),
			checkNoToken: true,
		},
		{
			name:         "logout with no token",
			setupToken:   false,
			tokenFile:    filepath.Join(tmpDir, "nonexistent"),
			checkNoToken: false,
		},
		{
			name:         "logout with server unreachable",
			setupToken:   true,
			tokenFile:    filepath.Join(tmpDir, "token2"),
			factoryErr:   errors.New(errors.ErrUnavailable, "auth.factory", "server unreachable"),
			checkNoToken: true,
		},
		{
			name:         "logout with revoke error",
			setupToken:   true,
			tokenFile:    filepath.Join(tmpDir, "token3"),
			logoutErr:    errors.New(errors.ErrInternal, "auth.logout", "failed to revoke"),
			checkNoToken: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Save original factory
			origFactory := authClientFactory
			defer func() { authClientFactory = origFactory }()

			// Setup token if needed
			if tt.setupToken {
				token := &cli.TokenData{
					AccessToken:  "test-access-token",
					RefreshToken: "test-refresh-token",
					ExpiresAt:    time.Now().Add(1 * time.Hour),
					TokenType:    "Bearer",
				}
				err := cli.SaveToken(tt.tokenFile, token)
				require.NoError(t, err)
			}

			// Setup factory
			if tt.factoryErr != nil {
				authClientFactory = func() (auth.Client, error) {
					return nil, tt.factoryErr
				}
			} else if tt.logoutErr != nil {
				mockClient := &mockSDKAuthClient{
					LogoutFunc: func(ctx context.Context, accessToken string) error {
						return tt.logoutErr
					},
				}
				authClientFactory = func() (auth.Client, error) {
					return mockClient, nil
				}
			} else {
				mockClient := &mockSDKAuthClient{
					LogoutFunc: func(ctx context.Context, accessToken string) error {
						return nil
					},
				}
				authClientFactory = func() (auth.Client, error) {
					return mockClient, nil
				}
			}

			// Create and execute command
			cmd := newAuthLogoutCommand()
			cmd.SetArgs([]string{"--token-file", tt.tokenFile})

			err := cmd.Execute()
			require.NoError(t, err)

			// Check token file removed
			if tt.checkNoToken {
				_, err := os.Stat(tt.tokenFile)
				assert.True(t, os.IsNotExist(err), "Token file should be deleted")
			}
		})
	}
}

// TestAuthLogoutCommand_Errors tests error scenarios for logout command.
func TestAuthLogoutCommand_Errors(t *testing.T) {
	tmpDir := t.TempDir()

	tests := []struct {
		name          string
		setupToken    bool
		tokenContent  string
		tokenFile     string
		expectedError string
	}{
		{
			name:          "invalid token file content",
			setupToken:    true,
			tokenFile:     filepath.Join(tmpDir, "invalid"),
			tokenContent:  "invalid json",
			expectedError: "failed to parse token file",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Save original factory
			origFactory := authClientFactory
			defer func() { authClientFactory = origFactory }()

			// Setup invalid token file
			if tt.setupToken && tt.tokenContent != "" {
				err := os.MkdirAll(filepath.Dir(tt.tokenFile), 0o700)
				require.NoError(t, err)
				err = os.WriteFile(tt.tokenFile, []byte(tt.tokenContent), 0o600)
				require.NoError(t, err)
			}

			// Setup factory (won't be called if token parsing fails)
			mockClient := &mockSDKAuthClient{}
			authClientFactory = func() (auth.Client, error) {
				return mockClient, nil
			}

			// Create and execute command
			cmd := newAuthLogoutCommand()
			cmd.SetArgs([]string{"--token-file", tt.tokenFile})

			err := cmd.Execute()
			require.Error(t, err)
			if tt.expectedError != "" {
				assert.Contains(t, err.Error(), tt.expectedError)
			}

			// Cleanup
			os.Remove(tt.tokenFile)
		})
	}
}

// TestAuthStatusCommand_Success tests successful status scenarios.
func TestAuthStatusCommand_Success(t *testing.T) {
	tmpDir := t.TempDir()

	tests := []struct {
		name         string
		setupToken   bool
		tokenExpired bool
		tokenFile    string
		outputFormat string
		checkOutput  func(t *testing.T, output string)
	}{
		{
			name:       "status with valid token - table format",
			setupToken: true,
			tokenFile:  filepath.Join(tmpDir, "token1"),
			checkOutput: func(t *testing.T, output string) {
				assert.Contains(t, output, "Authentication Status")
				assert.Contains(t, output, "Authenticated: true")
				assert.Contains(t, output, "Status: valid")
				assert.Contains(t, output, "Token Type: Bearer")
				assert.Contains(t, output, "Expires in:")
			},
		},
		{
			name:         "status with expired token - table format",
			setupToken:   true,
			tokenExpired: true,
			tokenFile:    filepath.Join(tmpDir, "token2"),
			checkOutput: func(t *testing.T, output string) {
				assert.Contains(t, output, "Authentication Status")
				assert.Contains(t, output, "Authenticated: false")
				assert.Contains(t, output, "Status: expired")
				assert.Contains(t, output, "Token expired")
			},
		},
		{
			name:       "status with no token - table format",
			setupToken: false,
			tokenFile:  filepath.Join(tmpDir, "nonexistent"),
			checkOutput: func(t *testing.T, output string) {
				assert.Contains(t, output, "Not authenticated")
			},
		},
		{
			name:         "status with valid token - json format",
			setupToken:   true,
			tokenFile:    filepath.Join(tmpDir, "token3"),
			outputFormat: "json",
			checkOutput: func(t *testing.T, output string) {
				var result map[string]interface{}
				err := json.Unmarshal([]byte(output), &result)
				require.NoError(t, err)
				assert.Equal(t, true, result["authenticated"])
				assert.Equal(t, "valid", result["status"])
				assert.Contains(t, result, "expires_at")
				assert.Contains(t, result, "token_type")
			},
		},
		{
			name:         "status with expired token - json format",
			setupToken:   true,
			tokenExpired: true,
			tokenFile:    filepath.Join(tmpDir, "token4"),
			outputFormat: "json",
			checkOutput: func(t *testing.T, output string) {
				var result map[string]interface{}
				err := json.Unmarshal([]byte(output), &result)
				require.NoError(t, err)
				assert.Equal(t, false, result["authenticated"])
				assert.Equal(t, "expired", result["status"])
			},
		},
		{
			name:         "status with no token - json format",
			setupToken:   false,
			tokenFile:    filepath.Join(tmpDir, "nonexistent2"),
			outputFormat: "json",
			checkOutput: func(t *testing.T, output string) {
				var result map[string]interface{}
				err := json.Unmarshal([]byte(output), &result)
				require.NoError(t, err)
				assert.Equal(t, false, result["authenticated"])
				assert.Equal(t, "not authenticated", result["status"])
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup token if needed
			if tt.setupToken {
				expiresAt := time.Now().Add(1 * time.Hour)
				if tt.tokenExpired {
					expiresAt = time.Now().Add(-1 * time.Hour)
				}

				token := &cli.TokenData{
					AccessToken:  "test-access-token",
					RefreshToken: "test-refresh-token",
					ExpiresAt:    expiresAt,
					TokenType:    "Bearer",
				}
				err := cli.SaveToken(tt.tokenFile, token)
				require.NoError(t, err)
				defer os.Remove(tt.tokenFile)
			}

			// Build command args
			args := []string{"--token-file", tt.tokenFile}
			if tt.outputFormat != "" {
				args = append(args, "--output", tt.outputFormat)
			}

			// Create and execute command
			cmd := newAuthStatusCommand()
			cmd.SetArgs(args)

			var stdout, stderr bytes.Buffer
			cmd.SetOut(&stdout)
			cmd.SetErr(&stderr)

			err := cmd.Execute()
			require.NoError(t, err)

			// Check output
			if tt.checkOutput != nil {
				tt.checkOutput(t, stdout.String())
			}
		})
	}
}

// TestAuthStatusCommand_Errors tests error scenarios for status command.
func TestAuthStatusCommand_Errors(t *testing.T) {
	tmpDir := t.TempDir()

	tests := []struct {
		name          string
		setupToken    bool
		tokenContent  string
		tokenFile     string
		outputFormat  string
		expectedError string
	}{
		{
			name:          "invalid output format",
			setupToken:    false,
			tokenFile:     filepath.Join(tmpDir, "token"),
			outputFormat:  "invalid",
			expectedError: "unknown output format",
		},
		{
			name:          "invalid token file content",
			setupToken:    true,
			tokenContent:  "invalid json",
			tokenFile:     filepath.Join(tmpDir, "invalid_token"),
			expectedError: "failed to parse token file",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup invalid token file
			if tt.setupToken && tt.tokenContent != "" {
				err := os.MkdirAll(filepath.Dir(tt.tokenFile), 0o700)
				require.NoError(t, err)
				err = os.WriteFile(tt.tokenFile, []byte(tt.tokenContent), 0o600)
				require.NoError(t, err)
				defer os.Remove(tt.tokenFile)
			}

			// Build command args
			args := []string{"--token-file", tt.tokenFile}
			if tt.outputFormat != "" {
				args = append(args, "--output", tt.outputFormat)
			}

			// Create and execute command
			cmd := newAuthStatusCommand()
			cmd.SetArgs(args)

			var stdout, stderr bytes.Buffer
			cmd.SetOut(&stdout)
			cmd.SetErr(&stderr)

			err := cmd.Execute()
			require.Error(t, err)
			if tt.expectedError != "" {
				assert.Contains(t, err.Error(), tt.expectedError)
			}
		})
	}
}

// TestAuthCommands_DefaultTokenPath tests commands with default token path.
func TestAuthCommands_DefaultTokenPath(t *testing.T) {
	// Save original factory
	origFactory := authClientFactory
	defer func() { authClientFactory = origFactory }()

	// Create mock client
	mockClient := &mockSDKAuthClient{
		LoginFunc: func(ctx context.Context, req *service.ExtendedLoginRequest) (*service.ExtendedLoginResponse, error) {
			return &service.ExtendedLoginResponse{
				AccessToken:  "default-path-token",
				RefreshToken: "default-path-refresh",
				ExpiresAt:    time.Now().Add(1 * time.Hour),
				TokenType:    "Bearer",
			}, nil
		},
		LogoutFunc: func(ctx context.Context, accessToken string) error {
			return nil
		},
	}

	authClientFactory = func() (auth.Client, error) {
		return mockClient, nil
	}

	t.Run("status with default token path", func(t *testing.T) {
		// Create token at default path
		defaultPath, err := cli.GetDefaultTokenPath()
		require.NoError(t, err)

		token := &cli.TokenData{
			AccessToken:  "default-token",
			RefreshToken: "default-refresh",
			ExpiresAt:    time.Now().Add(1 * time.Hour),
			TokenType:    "Bearer",
		}
		err = cli.SaveToken(defaultPath, token)
		require.NoError(t, err)
		defer os.Remove(defaultPath)

		cmd := newAuthStatusCommand()
		cmd.SetArgs([]string{})

		var stdout bytes.Buffer
		cmd.SetOut(&stdout)

		err = cmd.Execute()
		require.NoError(t, err)
		assert.Contains(t, stdout.String(), "Authenticated: true")
	})

	t.Run("logout with default token path", func(t *testing.T) {
		// Create token at default path
		defaultPath, err := cli.GetDefaultTokenPath()
		require.NoError(t, err)

		token := &cli.TokenData{
			AccessToken:  "logout-token",
			RefreshToken: "logout-refresh",
			ExpiresAt:    time.Now().Add(1 * time.Hour),
			TokenType:    "Bearer",
		}
		err = cli.SaveToken(defaultPath, token)
		require.NoError(t, err)

		cmd := newAuthLogoutCommand()
		cmd.SetArgs([]string{})

		err = cmd.Execute()
		require.NoError(t, err)

		// Verify token was removed
		_, err = os.Stat(defaultPath)
		assert.True(t, os.IsNotExist(err), "Token should be removed")
	})
}

// TestNewAuthCommand tests the auth command group creation.
func TestNewAuthCommand(t *testing.T) {
	cmd := newAuthCommand()
	require.NotNil(t, cmd)
	assert.Equal(t, "auth", cmd.Use)
	assert.Contains(t, cmd.Short, "Authentication")

	// Check subcommands
	subcommands := cmd.Commands()
	assert.Len(t, subcommands, 3)

	var foundLogin, foundLogout, foundStatus bool
	for _, subcmd := range subcommands {
		switch subcmd.Use {
		case "login":
			foundLogin = true
		case "logout":
			foundLogout = true
		case "status":
			foundStatus = true
		}
	}

	assert.True(t, foundLogin, "Should have login subcommand")
	assert.True(t, foundLogout, "Should have logout subcommand")
	assert.True(t, foundStatus, "Should have status subcommand")
}

// TestAuthLoginCommand_PrintsInstructions verifies that `quicbaas auth login`
// returns successfully and prints WebAuthn/PIV authentication instructions.
// The CLI no longer performs password-based login; it only directs users
// to the supported authentication methods.
func TestAuthLoginCommand_PrintsInstructions(t *testing.T) {
	cmd := newAuthLoginCommand()
	cmd.SetArgs([]string{})

	var stdout bytes.Buffer
	cmd.SetOut(&stdout)

	err := cmd.Execute()
	require.NoError(t, err)

	output := stdout.String()
	assert.Contains(t, output, "WebAuthn", "output should mention WebAuthn")
	assert.Contains(t, output, "PIV", "output should mention PIV")
}

// TestTokenStorage tests comprehensive token storage scenarios.
func TestTokenStorage(t *testing.T) {
	tmpDir := t.TempDir()
	tokenFile := filepath.Join(tmpDir, "token")

	t.Run("save and load token", func(t *testing.T) {
		token := &cli.TokenData{
			AccessToken:  "test-access-token-123",
			RefreshToken: "test-refresh-token-456",
			ExpiresAt:    time.Now().Add(1 * time.Hour),
			TokenType:    "Bearer",
		}

		err := cli.SaveToken(tokenFile, token)
		require.NoError(t, err)

		// Check file permissions
		info, err := os.Stat(tokenFile)
		require.NoError(t, err)
		assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())

		loadedToken, err := cli.LoadToken(tokenFile)
		require.NoError(t, err)

		assert.Equal(t, token.AccessToken, loadedToken.AccessToken)
		assert.Equal(t, token.RefreshToken, loadedToken.RefreshToken)
		assert.Equal(t, token.TokenType, loadedToken.TokenType)
		assert.WithinDuration(t, token.ExpiresAt, loadedToken.ExpiresAt, time.Second)
	})

	t.Run("load non-existent token", func(t *testing.T) {
		_, err := cli.LoadToken(filepath.Join(tmpDir, "nonexistent"))
		assert.Error(t, err)
		assert.True(t, os.IsNotExist(err))
	})

	t.Run("delete token", func(t *testing.T) {
		token := &cli.TokenData{
			AccessToken: "test",
			ExpiresAt:   time.Now().Add(1 * time.Hour),
			TokenType:   "Bearer",
		}
		err := cli.SaveToken(tokenFile, token)
		require.NoError(t, err)

		err = cli.DeleteToken(tokenFile)
		assert.NoError(t, err)

		_, err = os.Stat(tokenFile)
		assert.True(t, os.IsNotExist(err))
	})

	t.Run("token validation", func(t *testing.T) {
		validToken := &cli.TokenData{
			ExpiresAt: time.Now().Add(1 * time.Hour),
		}
		assert.True(t, cli.IsTokenValid(validToken))

		expiredToken := &cli.TokenData{
			ExpiresAt: time.Now().Add(-1 * time.Hour),
		}
		assert.False(t, cli.IsTokenValid(expiredToken))
	})
}

// TestGetDefaultTokenPath tests default token path retrieval.
func TestGetDefaultTokenPath(t *testing.T) {
	path, err := cli.GetDefaultTokenPath()
	require.NoError(t, err)
	assert.NotEmpty(t, path)
	assert.Contains(t, path, ".quicbaas")
	assert.Contains(t, path, "token")
}

// TestCreateAuthClient tests the createAuthClient function.
// This test uses a mock factory to avoid connecting to a real server.
func TestCreateAuthClient(t *testing.T) {
	// Save original factory
	origFactory := authClientFactory
	defer func() { authClientFactory = origFactory }()

	// Use a mock factory that returns a mock client
	mockClient := &mockSDKAuthClient{}
	authClientFactory = func() (auth.Client, error) {
		return mockClient, nil
	}

	client, err := createAuthClient()
	assert.NotNil(t, client)
	assert.NoError(t, err)
}
