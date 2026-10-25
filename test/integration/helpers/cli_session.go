// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
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

//go:build integration

package helpers

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// CLISessionError reports a step of saving a CLI session that failed.
type CLISessionError struct {
	Step  string
	Path  string
	Cause error
}

func (e *CLISessionError) Error() string {
	return "save CLI session at " + e.Path + ": " + e.Step + ": " + e.Cause.Error()
}

func (e *CLISessionError) Unwrap() error { return e.Cause }

// CLISessionPath is where the quicbaas CLI run with HOME=home reads its
// session.
func CLISessionPath(home string) string {
	return filepath.Join(home, ".quicbaas", "token")
}

// WriteCLISession writes bearer at path in the session envelope the CLI's
// token store reads. expiresAt must be the bearer's real expiry: the CLI
// refuses a session past it, and one later than the server's lifetime makes
// the CLI present an expired bearer.
func WriteCLISession(path, bearer string, expiresAt time.Time) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return &CLISessionError{Step: "create directory", Path: path, Cause: err}
	}
	payload := struct {
		AccessToken  string    `json:"access_token"`
		RefreshToken string    `json:"refresh_token"`
		ExpiresAt    time.Time `json:"expires_at"`
		TokenType    string    `json:"token_type"`
	}{
		AccessToken: bearer,
		ExpiresAt:   expiresAt,
		TokenType:   "Bearer",
	}
	data, err := json.Marshal(&payload)
	if err != nil {
		return &CLISessionError{Step: "encode", Path: path, Cause: err}
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return &CLISessionError{Step: "write", Path: path, Cause: err}
	}
	return nil
}
