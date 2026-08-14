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

// Package cli provides secure token storage for QuicBaaS CLI.
package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// TokenData represents stored authentication token information.
type TokenData struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	ExpiresAt    time.Time `json:"expires_at"`
	TokenType    string    `json:"token_type"`
}

// GetDefaultTokenPath returns the default token storage path.
func GetDefaultTokenPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", errors.Wrapf(err, errors.ErrInternal, "cli.GetDefaultTokenPath", "failed to get user home directory")
	}

	tokenDir := filepath.Join(home, ".quicbaas")
	return filepath.Join(tokenDir, "token"), nil
}

// SaveToken securely stores authentication token to disk.
func SaveToken(path string, token *TokenData) error {
	// Ensure directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return errors.Wrapf(err, errors.ErrInternal, "cli.SaveToken", "failed to create token directory")
	}

	// Serialize token to JSON
	data, err := json.MarshalIndent(token, "", "  ")
	if err != nil {
		return errors.Wrapf(err, errors.ErrInternal, "cli.SaveToken", "failed to serialize token")
	}

	// Stage in the same directory and rename over the target. os.WriteFile
	// truncates first, so a reader that loads the token while a renewal is
	// writing sees an empty or partial file ("unexpected end of JSON input") --
	// TokenManager refreshes under a single-flight lock but readers do not take
	// it, so that window is reachable whenever one process refreshes while
	// another reads. Rename is atomic, so a reader gets either the old token or
	// the new one. It also means a crash mid-write cannot destroy the session.
	tmp, err := os.CreateTemp(dir, ".token-*")
	if err != nil {
		return errors.Wrapf(err, errors.ErrInternal, "cli.SaveToken", "failed to create temporary token file")
	}
	tmpPath := tmp.Name()

	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return errors.Wrapf(err, errors.ErrInternal, "cli.SaveToken", "failed to set token file permissions")
	}

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return errors.Wrapf(err, errors.ErrInternal, "cli.SaveToken", "failed to write token file")
	}

	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return errors.Wrapf(err, errors.ErrInternal, "cli.SaveToken", "failed to sync token file")
	}

	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return errors.Wrapf(err, errors.ErrInternal, "cli.SaveToken", "failed to close token file")
	}

	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return errors.Wrapf(err, errors.ErrInternal, "cli.SaveToken", "failed to write token file")
	}

	return nil
}

// LoadToken loads authentication token from disk.
func LoadToken(path string) (*TokenData, error) {
	// Read token file
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, err // Return raw error for not exist check
		}
		return nil, errors.Wrapf(err, errors.ErrInternal, "cli.LoadToken", "failed to read token file")
	}

	// Deserialize token
	var token TokenData
	if err := json.Unmarshal(data, &token); err != nil {
		return nil, errors.Wrapf(err, errors.ErrInternal, "cli.LoadToken", "failed to parse token file")
	}

	return &token, nil
}

// DeleteToken removes the authentication token file.
func DeleteToken(path string) error {
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return errors.Wrapf(err, errors.ErrInternal, "cli.DeleteToken", "failed to delete token file")
	}
	return nil
}

// IsTokenValid checks if a token is still valid (not expired).
func IsTokenValid(token *TokenData) bool {
	return time.Now().Before(token.ExpiresAt)
}

// FormatTokenInfo formats token information for display.
func FormatTokenInfo(token *TokenData) string {
	status := "valid"
	if !IsTokenValid(token) {
		status = "expired"
	}

	return fmt.Sprintf("Token: %s\nType: %s\nExpires: %s\nStatus: %s",
		token.AccessToken[:20]+"...",
		token.TokenType,
		token.ExpiresAt.Format(time.RFC3339),
		status)
}
