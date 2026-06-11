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

package server

import (
	"encoding/json"
	"os"
	"strings"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// aaguidEntry is one entry from the operator-supplied allowlist JSON file.
// Supports both the FIDO Alliance MDS3 format and a simple array format:
//
//	[{"aaguid": "...", "name": "...", "attestation_level": "..."}]
type aaguidEntry struct {
	AAGUID           string `json:"aaguid"`
	Name             string `json:"name"`
	AttestationLevel string `json:"attestation_level"`
}

// buildAAGUIDAllowlist constructs an *iam.AAGUIDAllowlist from the operator
// config. Returns nil, nil when the config is default (empty mode, no file),
// which leaves the WebAuthnManager without an allowlist (permissive default).
//
// Startup behaviour:
//   - Empty mode + empty allowlist_file → returns nil, nil (no-op).
//   - mode = "log_only" (or empty) → AAGUIDPolicyPermissive.
//   - mode = "enforce"             → AAGUIDPolicyStrict.
//   - Non-empty allowlist_file that cannot be read → returns a typed error;
//     the caller logs a warning but does not abort startup.
func buildAAGUIDAllowlist(cfg config.IAMAAGUIDConfig) (*iam.AAGUIDAllowlist, error) {
	if cfg.Mode == "" && cfg.AllowlistFile == "" {
		return nil, nil
	}

	policy := iam.AAGUIDPolicyPermissive
	if strings.ToLower(cfg.Mode) == "enforce" {
		policy = iam.AAGUIDPolicyStrict
	}

	var hexAAGUIDs []string
	if cfg.AllowlistFile != "" {
		data, err := os.ReadFile(cfg.AllowlistFile)
		if err != nil {
			return nil, &AAGUIDAllowlistReadError{Path: cfg.AllowlistFile, Cause: err}
		}
		entries, err := parseAAGUIDAllowlistFile(data)
		if err != nil {
			return nil, &AAGUIDAllowlistParseError{Path: cfg.AllowlistFile, Cause: err}
		}
		hexAAGUIDs = make([]string, 0, len(entries))
		for _, e := range entries {
			if e.AAGUID != "" {
				hexAAGUIDs = append(hexAAGUIDs, e.AAGUID)
			}
		}
	}

	return iam.NewAAGUIDAllowlist(hexAAGUIDs, policy), nil
}

// parseAAGUIDAllowlistFile decodes a JSON file into a slice of aaguidEntry.
// Accepts a simple JSON array of objects:
//
//	[{"aaguid":"...", "name":"..."}]
func parseAAGUIDAllowlistFile(data []byte) ([]aaguidEntry, error) {
	var entries []aaguidEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, err
	}
	return entries, nil
}

// AAGUIDAllowlistReadError is returned when the AAGUID allowlist file cannot be read.
type AAGUIDAllowlistReadError struct {
	Path  string
	Cause error
}

// Error implements the error interface.
func (e *AAGUIDAllowlistReadError) Error() string {
	return "iam.aaguid: failed to read allowlist file " + e.Path + ": " + e.Cause.Error()
}

// Unwrap returns the underlying cause.
func (e *AAGUIDAllowlistReadError) Unwrap() error { return e.Cause }

// AAGUIDAllowlistParseError is returned when the AAGUID allowlist file cannot be parsed.
type AAGUIDAllowlistParseError struct {
	Path  string
	Cause error
}

// Error implements the error interface.
func (e *AAGUIDAllowlistParseError) Error() string {
	return "iam.aaguid: failed to parse allowlist file " + e.Path + ": " + e.Cause.Error()
}

// Unwrap returns the underlying cause.
func (e *AAGUIDAllowlistParseError) Unwrap() error { return e.Cause }
