//go:build iam

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

package main

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/cli"
)

// =============================================================================
// IAM Session Token Parsing Tests - getCurrentSessionID and getCurrentUserID
// =============================================================================

func TestGetCurrentSessionID_TokenPathError_NinetyBoost(t *testing.T) {
	// Set HOME to non-existent path to trigger error
	origHome := os.Getenv("HOME")
	defer os.Setenv("HOME", origHome)

	// Create a temp dir that will be removed
	tmpDir, err := os.MkdirTemp("", "token-test-nb")
	require.NoError(t, err)
	os.RemoveAll(tmpDir)

	os.Setenv("HOME", tmpDir)

	_, err = getCurrentSessionID()
	assert.Error(t, err)
}

func TestGetCurrentSessionID_InvalidTokenFormat_NinetyBoost(t *testing.T) {
	// Create temp directory for token
	tmpDir, err := os.MkdirTemp("", "token-test-nb")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	// Create .quicbaas directory
	quicbaasDir := filepath.Join(tmpDir, ".quicbaas")
	err = os.MkdirAll(quicbaasDir, 0700)
	require.NoError(t, err)

	// Write invalid token file
	tokenPath := filepath.Join(quicbaasDir, "token")
	tokenData := cli.TokenData{
		AccessToken:  "invalid-format",
		RefreshToken: "refresh",
		TokenType:    "Bearer",
		ExpiresAt:    time.Now().Add(time.Hour),
	}
	data, err := json.Marshal(tokenData)
	require.NoError(t, err)
	err = os.WriteFile(tokenPath, data, 0600)
	require.NoError(t, err)

	origHome := os.Getenv("HOME")
	defer os.Setenv("HOME", origHome)
	os.Setenv("HOME", tmpDir)

	_, err = getCurrentSessionID()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid token format")
}

func TestGetCurrentSessionID_InvalidBase64Payload_NinetyBoost(t *testing.T) {
	// Create temp directory for token
	tmpDir, err := os.MkdirTemp("", "token-test-nb")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	// Create .quicbaas directory
	quicbaasDir := filepath.Join(tmpDir, ".quicbaas")
	err = os.MkdirAll(quicbaasDir, 0700)
	require.NoError(t, err)

	// Write token with invalid base64 in payload
	tokenPath := filepath.Join(quicbaasDir, "token")
	tokenData := cli.TokenData{
		AccessToken:  "header.!!!bad-base64!!!.signature",
		RefreshToken: "refresh",
		TokenType:    "Bearer",
		ExpiresAt:    time.Now().Add(time.Hour),
	}
	data, err := json.Marshal(tokenData)
	require.NoError(t, err)
	err = os.WriteFile(tokenPath, data, 0600)
	require.NoError(t, err)

	origHome := os.Getenv("HOME")
	defer os.Setenv("HOME", origHome)
	os.Setenv("HOME", tmpDir)

	_, err = getCurrentSessionID()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to decode token payload")
}

func TestGetCurrentSessionID_InvalidJSONPayload_NinetyBoost(t *testing.T) {
	// Create temp directory for token
	tmpDir, err := os.MkdirTemp("", "token-test-nb")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	// Create .quicbaas directory
	quicbaasDir := filepath.Join(tmpDir, ".quicbaas")
	err = os.MkdirAll(quicbaasDir, 0700)
	require.NoError(t, err)

	// Create token with invalid JSON payload
	invalidJSON := base64.RawURLEncoding.EncodeToString([]byte("not-valid-json-data"))
	tokenPath := filepath.Join(quicbaasDir, "token")
	tokenData := cli.TokenData{
		AccessToken:  "header." + invalidJSON + ".signature",
		RefreshToken: "refresh",
		TokenType:    "Bearer",
		ExpiresAt:    time.Now().Add(time.Hour),
	}
	data, err := json.Marshal(tokenData)
	require.NoError(t, err)
	err = os.WriteFile(tokenPath, data, 0600)
	require.NoError(t, err)

	origHome := os.Getenv("HOME")
	defer os.Setenv("HOME", origHome)
	os.Setenv("HOME", tmpDir)

	_, err = getCurrentSessionID()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse token claims")
}

func TestGetCurrentSessionID_NoSessionIDClaim_NinetyBoost(t *testing.T) {
	// Create temp directory for token
	tmpDir, err := os.MkdirTemp("", "token-test-nb")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	// Create .quicbaas directory
	quicbaasDir := filepath.Join(tmpDir, ".quicbaas")
	err = os.MkdirAll(quicbaasDir, 0700)
	require.NoError(t, err)

	// Create token without session ID claim
	claims := map[string]interface{}{
		"sub": "user-nb-123",
		"exp": time.Now().Add(time.Hour).Unix(),
	}
	claimsJSON, err := json.Marshal(claims)
	require.NoError(t, err)
	payload := base64.RawURLEncoding.EncodeToString(claimsJSON)

	tokenPath := filepath.Join(quicbaasDir, "token")
	tokenData := cli.TokenData{
		AccessToken:  "header." + payload + ".signature",
		RefreshToken: "refresh",
		TokenType:    "Bearer",
		ExpiresAt:    time.Now().Add(time.Hour),
	}
	data, err := json.Marshal(tokenData)
	require.NoError(t, err)
	err = os.WriteFile(tokenPath, data, 0600)
	require.NoError(t, err)

	origHome := os.Getenv("HOME")
	defer os.Setenv("HOME", origHome)
	os.Setenv("HOME", tmpDir)

	_, err = getCurrentSessionID()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no session ID found")
}

func TestGetCurrentSessionID_WithSidClaim_NinetyBoost(t *testing.T) {
	// Create temp directory for token
	tmpDir, err := os.MkdirTemp("", "token-test-nb")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	// Create .quicbaas directory
	quicbaasDir := filepath.Join(tmpDir, ".quicbaas")
	err = os.MkdirAll(quicbaasDir, 0700)
	require.NoError(t, err)

	// Create token with sid claim
	claims := map[string]interface{}{
		"sub": "user-nb-123",
		"sid": "session-nb-456",
		"exp": time.Now().Add(time.Hour).Unix(),
	}
	claimsJSON, err := json.Marshal(claims)
	require.NoError(t, err)
	payload := base64.RawURLEncoding.EncodeToString(claimsJSON)

	tokenPath := filepath.Join(quicbaasDir, "token")
	tokenData := cli.TokenData{
		AccessToken:  "header." + payload + ".signature",
		RefreshToken: "refresh",
		TokenType:    "Bearer",
		ExpiresAt:    time.Now().Add(time.Hour),
	}
	data, err := json.Marshal(tokenData)
	require.NoError(t, err)
	err = os.WriteFile(tokenPath, data, 0600)
	require.NoError(t, err)

	origHome := os.Getenv("HOME")
	defer os.Setenv("HOME", origHome)
	os.Setenv("HOME", tmpDir)

	sessionID, err := getCurrentSessionID()
	require.NoError(t, err)
	assert.Equal(t, "session-nb-456", sessionID)
}

func TestGetCurrentSessionID_WithSessionIDClaim_NinetyBoost(t *testing.T) {
	// Create temp directory for token
	tmpDir, err := os.MkdirTemp("", "token-test-nb")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	// Create .quicbaas directory
	quicbaasDir := filepath.Join(tmpDir, ".quicbaas")
	err = os.MkdirAll(quicbaasDir, 0700)
	require.NoError(t, err)

	// Create token with session_id claim
	claims := map[string]interface{}{
		"sub":        "user-nb-123",
		"session_id": "session-nb-789",
		"exp":        time.Now().Add(time.Hour).Unix(),
	}
	claimsJSON, err := json.Marshal(claims)
	require.NoError(t, err)
	payload := base64.RawURLEncoding.EncodeToString(claimsJSON)

	tokenPath := filepath.Join(quicbaasDir, "token")
	tokenData := cli.TokenData{
		AccessToken:  "header." + payload + ".signature",
		RefreshToken: "refresh",
		TokenType:    "Bearer",
		ExpiresAt:    time.Now().Add(time.Hour),
	}
	data, err := json.Marshal(tokenData)
	require.NoError(t, err)
	err = os.WriteFile(tokenPath, data, 0600)
	require.NoError(t, err)

	origHome := os.Getenv("HOME")
	defer os.Setenv("HOME", origHome)
	os.Setenv("HOME", tmpDir)

	sessionID, err := getCurrentSessionID()
	require.NoError(t, err)
	assert.Equal(t, "session-nb-789", sessionID)
}

func TestGetCurrentUserID_WithSubClaim_NinetyBoost(t *testing.T) {
	// Create temp directory for token
	tmpDir, err := os.MkdirTemp("", "token-test-nb")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	// Create .quicbaas directory
	quicbaasDir := filepath.Join(tmpDir, ".quicbaas")
	err = os.MkdirAll(quicbaasDir, 0700)
	require.NoError(t, err)

	// Create token with sub claim
	claims := map[string]interface{}{
		"sub": "user-nb-sub-123",
		"exp": time.Now().Add(time.Hour).Unix(),
	}
	claimsJSON, err := json.Marshal(claims)
	require.NoError(t, err)
	payload := base64.RawURLEncoding.EncodeToString(claimsJSON)

	tokenPath := filepath.Join(quicbaasDir, "token")
	tokenData := cli.TokenData{
		AccessToken:  "header." + payload + ".signature",
		RefreshToken: "refresh",
		TokenType:    "Bearer",
		ExpiresAt:    time.Now().Add(time.Hour),
	}
	data, err := json.Marshal(tokenData)
	require.NoError(t, err)
	err = os.WriteFile(tokenPath, data, 0600)
	require.NoError(t, err)

	origHome := os.Getenv("HOME")
	defer os.Setenv("HOME", origHome)
	os.Setenv("HOME", tmpDir)

	userID, err := getCurrentUserID()
	require.NoError(t, err)
	assert.Equal(t, "user-nb-sub-123", userID)
}

func TestGetCurrentUserID_WithUserIDClaim_NinetyBoost(t *testing.T) {
	// Create temp directory for token
	tmpDir, err := os.MkdirTemp("", "token-test-nb")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	// Create .quicbaas directory
	quicbaasDir := filepath.Join(tmpDir, ".quicbaas")
	err = os.MkdirAll(quicbaasDir, 0700)
	require.NoError(t, err)

	// Create token with user_id claim
	claims := map[string]interface{}{
		"user_id": "user-nb-uid-456",
		"exp":     time.Now().Add(time.Hour).Unix(),
	}
	claimsJSON, err := json.Marshal(claims)
	require.NoError(t, err)
	payload := base64.RawURLEncoding.EncodeToString(claimsJSON)

	tokenPath := filepath.Join(quicbaasDir, "token")
	tokenData := cli.TokenData{
		AccessToken:  "header." + payload + ".signature",
		RefreshToken: "refresh",
		TokenType:    "Bearer",
		ExpiresAt:    time.Now().Add(time.Hour),
	}
	data, err := json.Marshal(tokenData)
	require.NoError(t, err)
	err = os.WriteFile(tokenPath, data, 0600)
	require.NoError(t, err)

	origHome := os.Getenv("HOME")
	defer os.Setenv("HOME", origHome)
	os.Setenv("HOME", tmpDir)

	userID, err := getCurrentUserID()
	require.NoError(t, err)
	assert.Equal(t, "user-nb-uid-456", userID)
}

func TestGetCurrentUserID_NoUserIDClaim_NinetyBoost(t *testing.T) {
	// Create temp directory for token
	tmpDir, err := os.MkdirTemp("", "token-test-nb")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	// Create .quicbaas directory
	quicbaasDir := filepath.Join(tmpDir, ".quicbaas")
	err = os.MkdirAll(quicbaasDir, 0700)
	require.NoError(t, err)

	// Create token without user ID claim
	claims := map[string]interface{}{
		"sid": "session-nb-only",
		"exp": time.Now().Add(time.Hour).Unix(),
	}
	claimsJSON, err := json.Marshal(claims)
	require.NoError(t, err)
	payload := base64.RawURLEncoding.EncodeToString(claimsJSON)

	tokenPath := filepath.Join(quicbaasDir, "token")
	tokenData := cli.TokenData{
		AccessToken:  "header." + payload + ".signature",
		RefreshToken: "refresh",
		TokenType:    "Bearer",
		ExpiresAt:    time.Now().Add(time.Hour),
	}
	data, err := json.Marshal(tokenData)
	require.NoError(t, err)
	err = os.WriteFile(tokenPath, data, 0600)
	require.NoError(t, err)

	origHome := os.Getenv("HOME")
	defer os.Setenv("HOME", origHome)
	os.Setenv("HOME", tmpDir)

	_, err = getCurrentUserID()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no user ID found")
}

// =============================================================================
// IAM Session Command Tests - Testing tenant flag validation
// Note: Tests for missing args (ExactArgs) are handled by cobra validation
// =============================================================================

func TestIAMSessionListCommand_MissingTenantFlag_NinetyBoost(t *testing.T) {
	origTenant := globalTenantID
	defer func() { globalTenantID = origTenant }()

	globalTenantID = ""
	cmd := newIAMSessionListCommand()
	err := runIAMSessionListCommand(cmd, []string{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "--tenant flag is required")
}

func TestIAMSessionGetCommand_MissingTenantFlag_NinetyBoost(t *testing.T) {
	origTenant := globalTenantID
	defer func() { globalTenantID = origTenant }()

	globalTenantID = ""
	cmd := newIAMSessionGetCommand()
	err := runIAMSessionGetCommand(cmd, []string{"session-nb"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "--tenant flag is required")
}

func TestIAMSessionRevokeCommand_MissingTenantFlag_NinetyBoost(t *testing.T) {
	origTenant := globalTenantID
	defer func() { globalTenantID = origTenant }()

	globalTenantID = ""
	cmd := newIAMSessionRevokeCommand()
	err := runIAMSessionRevokeCommand(cmd, []string{"session-nb"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "--tenant flag is required")
}

func TestIAMSessionRevokeAllCommand_MissingTenantFlag_NinetyBoost(t *testing.T) {
	origTenant := globalTenantID
	defer func() { globalTenantID = origTenant }()

	globalTenantID = ""
	cmd := newIAMSessionRevokeAllCommand()
	err := runIAMSessionRevokeAllCommand(cmd, []string{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "--tenant flag is required")
}
