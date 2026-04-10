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

// Package cli provides tests for token storage functionality.
package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGetDefaultTokenPath tests default token path retrieval.
func TestGetDefaultTokenPath(t *testing.T) {
	path, err := GetDefaultTokenPath()
	require.NoError(t, err)
	assert.NotEmpty(t, path)
	assert.Contains(t, path, ".quicbaas")
	assert.Contains(t, path, "token")
}

// TestSaveAndLoadToken tests token save and load operations.
func TestSaveAndLoadToken(t *testing.T) {
	tmpDir := t.TempDir()
	tokenPath := filepath.Join(tmpDir, "token")

	t.Run("save and load valid token", func(t *testing.T) {
		token := &TokenData{
			AccessToken:  "test-access-token",
			RefreshToken: "test-refresh-token",
			ExpiresAt:    time.Now().Add(1 * time.Hour),
			TokenType:    "Bearer",
		}

		// Save token
		err := SaveToken(tokenPath, token)
		require.NoError(t, err)

		// Verify file exists with correct permissions
		info, err := os.Stat(tokenPath)
		require.NoError(t, err)
		assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())

		// Load token
		loadedToken, err := LoadToken(tokenPath)
		require.NoError(t, err)
		assert.Equal(t, token.AccessToken, loadedToken.AccessToken)
		assert.Equal(t, token.RefreshToken, loadedToken.RefreshToken)
		assert.Equal(t, token.TokenType, loadedToken.TokenType)
		assert.WithinDuration(t, token.ExpiresAt, loadedToken.ExpiresAt, time.Second)
	})

	t.Run("save token creates directory", func(t *testing.T) {
		nestedPath := filepath.Join(tmpDir, "nested", "dir", "token")
		token := &TokenData{
			AccessToken: "test",
			ExpiresAt:   time.Now().Add(1 * time.Hour),
		}

		err := SaveToken(nestedPath, token)
		require.NoError(t, err)

		// Verify directory was created
		dirInfo, err := os.Stat(filepath.Dir(nestedPath))
		require.NoError(t, err)
		assert.True(t, dirInfo.IsDir())
	})

	t.Run("load nonexistent token", func(t *testing.T) {
		nonexistentPath := filepath.Join(tmpDir, "nonexistent")
		_, err := LoadToken(nonexistentPath)
		assert.Error(t, err)
		assert.True(t, os.IsNotExist(err))
	})

	t.Run("load corrupted token", func(t *testing.T) {
		corruptedPath := filepath.Join(tmpDir, "corrupted")
		err := os.WriteFile(corruptedPath, []byte("invalid json"), 0o600)
		require.NoError(t, err)

		_, err = LoadToken(corruptedPath)
		assert.Error(t, err)
	})
}

// TestDeleteToken tests token deletion.
func TestDeleteToken(t *testing.T) {
	tmpDir := t.TempDir()

	t.Run("delete existing token", func(t *testing.T) {
		tokenPath := filepath.Join(tmpDir, "token1")
		token := &TokenData{
			AccessToken: "test",
			ExpiresAt:   time.Now().Add(1 * time.Hour),
		}

		// Create token
		err := SaveToken(tokenPath, token)
		require.NoError(t, err)

		// Delete token
		err = DeleteToken(tokenPath)
		require.NoError(t, err)

		// Verify token no longer exists
		_, err = os.Stat(tokenPath)
		assert.True(t, os.IsNotExist(err))
	})

	t.Run("delete nonexistent token", func(t *testing.T) {
		nonexistentPath := filepath.Join(tmpDir, "nonexistent")
		err := DeleteToken(nonexistentPath)
		assert.NoError(t, err) // Should not error when file doesn't exist
	})
}

// TestIsTokenValid tests token validation.
func TestIsTokenValid(t *testing.T) {
	t.Run("valid token", func(t *testing.T) {
		token := &TokenData{
			ExpiresAt: time.Now().Add(1 * time.Hour),
		}
		assert.True(t, IsTokenValid(token))
	})

	t.Run("expired token", func(t *testing.T) {
		token := &TokenData{
			ExpiresAt: time.Now().Add(-1 * time.Hour),
		}
		assert.False(t, IsTokenValid(token))
	})

	t.Run("token expiring soon", func(t *testing.T) {
		token := &TokenData{
			ExpiresAt: time.Now().Add(1 * time.Second),
		}
		assert.True(t, IsTokenValid(token))
	})

	t.Run("token just expired", func(t *testing.T) {
		token := &TokenData{
			ExpiresAt: time.Now().Add(-1 * time.Millisecond),
		}
		assert.False(t, IsTokenValid(token))
	})
}

// TestFormatTokenInfo tests token information formatting.
func TestFormatTokenInfo(t *testing.T) {
	t.Run("valid token", func(t *testing.T) {
		token := &TokenData{
			AccessToken:  "very-long-access-token-that-should-be-truncated",
			RefreshToken: "refresh-token",
			ExpiresAt:    time.Now().Add(1 * time.Hour),
			TokenType:    "Bearer",
		}

		output := FormatTokenInfo(token)
		assert.Contains(t, output, "Token:")
		assert.Contains(t, output, "Type: Bearer")
		assert.Contains(t, output, "Expires:")
		assert.Contains(t, output, "Status: valid")
		assert.Contains(t, output, "...")
	})

	t.Run("expired token", func(t *testing.T) {
		token := &TokenData{
			AccessToken: "expired-access-token-abc",
			TokenType:   "Bearer",
			ExpiresAt:   time.Now().Add(-1 * time.Hour),
		}

		output := FormatTokenInfo(token)
		assert.Contains(t, output, "Status: expired")
	})
}

// TestTokenData tests TokenData structure.
func TestTokenData(t *testing.T) {
	now := time.Now()
	token := &TokenData{
		AccessToken:  "access-123",
		RefreshToken: "refresh-456",
		ExpiresAt:    now.Add(1 * time.Hour),
		TokenType:    "Bearer",
	}

	// Test JSON serialization
	data, err := json.Marshal(token)
	require.NoError(t, err)

	var decoded TokenData
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)
	assert.Equal(t, token.AccessToken, decoded.AccessToken)
	assert.Equal(t, token.RefreshToken, decoded.RefreshToken)
	assert.Equal(t, token.TokenType, decoded.TokenType)
}

// BenchmarkSaveToken benchmarks token save operation.
func BenchmarkSaveToken(b *testing.B) {
	tmpDir := b.TempDir()
	token := &TokenData{
		AccessToken:  "benchmark-token",
		RefreshToken: "refresh-token",
		ExpiresAt:    time.Now().Add(1 * time.Hour),
		TokenType:    "Bearer",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tokenPath := filepath.Join(tmpDir, "token")
		SaveToken(tokenPath, token)
	}
}

// BenchmarkLoadToken benchmarks token load operation.
func BenchmarkLoadToken(b *testing.B) {
	tmpDir := b.TempDir()
	tokenPath := filepath.Join(tmpDir, "token")
	token := &TokenData{
		AccessToken:  "benchmark-token",
		RefreshToken: "refresh-token",
		ExpiresAt:    time.Now().Add(1 * time.Hour),
		TokenType:    "Bearer",
	}
	SaveToken(tokenPath, token)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		LoadToken(tokenPath)
	}
}

// BenchmarkIsTokenValid benchmarks token validation.
func BenchmarkIsTokenValid(b *testing.B) {
	token := &TokenData{
		ExpiresAt: time.Now().Add(1 * time.Hour),
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		IsTokenValid(token)
	}
}

// =============================================================================
// Consolidated from token_store_coverage_test.go
// =============================================================================

// TestSaveTokenCreateDirectory tests that SaveToken creates directories correctly.
func TestSaveTokenCreateDirectory(t *testing.T) {
	t.Run("creates deeply nested directory", func(t *testing.T) {
		tmpDir := t.TempDir()
		tokenPath := filepath.Join(tmpDir, "level1", "level2", "level3", "token")

		token := &TokenData{
			AccessToken:  "test-token",
			RefreshToken: "refresh",
			ExpiresAt:    time.Now().Add(time.Hour),
			TokenType:    "Bearer",
		}

		err := SaveToken(tokenPath, token)
		require.NoError(t, err)

		// Verify directory was created
		dirInfo, err := os.Stat(filepath.Dir(tokenPath))
		require.NoError(t, err)
		assert.True(t, dirInfo.IsDir())

		// Verify token was saved
		loadedToken, err := LoadToken(tokenPath)
		require.NoError(t, err)
		assert.Equal(t, "test-token", loadedToken.AccessToken)
	})

	t.Run("overwrites existing token", func(t *testing.T) {
		tmpDir := t.TempDir()
		tokenPath := filepath.Join(tmpDir, "token")

		// Save first token
		token1 := &TokenData{
			AccessToken: "first-token",
			ExpiresAt:   time.Now().Add(time.Hour),
		}
		err := SaveToken(tokenPath, token1)
		require.NoError(t, err)

		// Save second token
		token2 := &TokenData{
			AccessToken: "second-token",
			ExpiresAt:   time.Now().Add(2 * time.Hour),
		}
		err = SaveToken(tokenPath, token2)
		require.NoError(t, err)

		// Verify second token is loaded
		loadedToken, err := LoadToken(tokenPath)
		require.NoError(t, err)
		assert.Equal(t, "second-token", loadedToken.AccessToken)
	})
}

// TestDeleteTokenEdgeCases tests edge cases for DeleteToken.
func TestDeleteTokenEdgeCases(t *testing.T) {
	t.Run("delete token leaves directory intact", func(t *testing.T) {
		tmpDir := t.TempDir()
		subDir := filepath.Join(tmpDir, "subdir")
		tokenPath := filepath.Join(subDir, "token")

		// Create token first
		token := &TokenData{
			AccessToken: "test",
			ExpiresAt:   time.Now().Add(time.Hour),
		}
		err := SaveToken(tokenPath, token)
		require.NoError(t, err)

		// Delete token
		err = DeleteToken(tokenPath)
		require.NoError(t, err)

		// Directory should still exist
		dirInfo, err := os.Stat(subDir)
		require.NoError(t, err)
		assert.True(t, dirInfo.IsDir())
	})
}

// TestGetDefaultTokenPathContainsExpectedPaths tests the path structure.
func TestGetDefaultTokenPathContainsExpectedPaths(t *testing.T) {
	path, err := GetDefaultTokenPath()
	require.NoError(t, err)

	// Should contain .quicbaas directory
	assert.Contains(t, path, ".quicbaas")

	// Should end with token
	assert.True(t, filepath.Base(path) == "token")

	// Path should be absolute
	assert.True(t, filepath.IsAbs(path))
}

// TestFormatTokenInfoEdgeCases tests edge cases for FormatTokenInfo.
func TestFormatTokenInfoEdgeCases(t *testing.T) {
	t.Run("token with exact 20 characters", func(t *testing.T) {
		token := &TokenData{
			AccessToken: "12345678901234567890",
			TokenType:   "Bearer",
			ExpiresAt:   time.Now().Add(time.Hour),
		}
		output := FormatTokenInfo(token)
		assert.Contains(t, output, "12345678901234567890...")
	})

	t.Run("token with more than 20 characters", func(t *testing.T) {
		token := &TokenData{
			AccessToken: "this-is-a-very-long-access-token-that-should-be-truncated",
			TokenType:   "Bearer",
			ExpiresAt:   time.Now().Add(time.Hour),
		}
		output := FormatTokenInfo(token)
		assert.Contains(t, output, "this-is-a-very-long-...")
	})

	t.Run("token about to expire", func(t *testing.T) {
		token := &TokenData{
			AccessToken: "token-about-to-expire",
			TokenType:   "Bearer",
			ExpiresAt:   time.Now().Add(1 * time.Minute),
		}
		output := FormatTokenInfo(token)
		assert.Contains(t, output, "valid")
	})

	t.Run("token expired just now", func(t *testing.T) {
		token := &TokenData{
			AccessToken: "just-expired-token--",
			TokenType:   "Bearer",
			ExpiresAt:   time.Now().Add(-1 * time.Second),
		}
		output := FormatTokenInfo(token)
		assert.Contains(t, output, "expired")
	})
}

// TestIsTokenValidEdgeCases tests edge cases for token validation.
func TestIsTokenValidEdgeCases(t *testing.T) {
	t.Run("token expires exactly now", func(t *testing.T) {
		token := &TokenData{
			ExpiresAt: time.Now(),
		}
		// This should be false since time.Now() is not before itself
		assert.False(t, IsTokenValid(token))
	})

	t.Run("token expires in far future", func(t *testing.T) {
		token := &TokenData{
			ExpiresAt: time.Now().Add(365 * 24 * time.Hour),
		}
		assert.True(t, IsTokenValid(token))
	})

	t.Run("token expired in far past", func(t *testing.T) {
		token := &TokenData{
			ExpiresAt: time.Now().Add(-365 * 24 * time.Hour),
		}
		assert.False(t, IsTokenValid(token))
	})
}

// TestTokenDataJSONRoundTrip tests JSON marshaling and unmarshaling.
func TestTokenDataJSONRoundTrip(t *testing.T) {
	original := &TokenData{
		AccessToken:  "access-token-123",
		RefreshToken: "refresh-token-456",
		ExpiresAt:    time.Now().Add(time.Hour).Truncate(time.Second),
		TokenType:    "Bearer",
	}

	// Marshal to JSON using standard json.Marshal
	data, err := json.Marshal(original)
	require.NoError(t, err)
	assert.NotEmpty(t, data)

	// Unmarshal back
	var decoded TokenData
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	assert.Equal(t, original.AccessToken, decoded.AccessToken)
	assert.Equal(t, original.RefreshToken, decoded.RefreshToken)
	assert.Equal(t, original.TokenType, decoded.TokenType)
}
