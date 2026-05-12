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

package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

func writeAAGUIDFile(t *testing.T, entries []aaguidEntry) string {
	t.Helper()
	data, err := json.Marshal(entries)
	require.NoError(t, err)
	f, err := os.CreateTemp(t.TempDir(), "aaguid-*.json")
	require.NoError(t, err)
	_, err = f.Write(data)
	require.NoError(t, err)
	require.NoError(t, f.Close())
	return f.Name()
}

// TestBuildAAGUIDAllowlist_NoConfig verifies that empty config returns nil (no-op).
func TestBuildAAGUIDAllowlist_NoConfig(t *testing.T) {
	cfg := config.IAMAAGUIDConfig{}
	allowlist, err := buildAAGUIDAllowlist(cfg)
	require.NoError(t, err)
	assert.Nil(t, allowlist, "empty config should return nil allowlist")
}

// TestBuildAAGUIDAllowlist_LogOnlyMode verifies that mode=log_only returns a permissive allowlist.
func TestBuildAAGUIDAllowlist_LogOnlyMode(t *testing.T) {
	file := writeAAGUIDFile(t, []aaguidEntry{
		{AAGUID: "2fc0579f-8113-47ea-b116-bb5a8db9202a", Name: "YubiKey 5 NFC"},
	})
	cfg := config.IAMAAGUIDConfig{
		Mode:          "log_only",
		AllowlistFile: file,
	}
	allowlist, err := buildAAGUIDAllowlist(cfg)
	require.NoError(t, err)
	require.NotNil(t, allowlist)
	assert.Equal(t, iam.AAGUIDPolicyPermissive, allowlist.Policy())
	assert.Equal(t, 1, allowlist.Len())
}

// TestBuildAAGUIDAllowlist_EnforceMode verifies that mode=enforce returns a strict allowlist.
func TestBuildAAGUIDAllowlist_EnforceMode(t *testing.T) {
	file := writeAAGUIDFile(t, []aaguidEntry{
		{AAGUID: "2fc0579f-8113-47ea-b116-bb5a8db9202a", Name: "YubiKey 5 NFC"},
	})
	cfg := config.IAMAAGUIDConfig{
		Mode:          "enforce",
		AllowlistFile: file,
	}
	allowlist, err := buildAAGUIDAllowlist(cfg)
	require.NoError(t, err)
	require.NotNil(t, allowlist)
	assert.Equal(t, iam.AAGUIDPolicyStrict, allowlist.Policy())
}

// TestBuildAAGUIDAllowlist_MissingFile verifies that a non-existent file returns an error.
func TestBuildAAGUIDAllowlist_MissingFile(t *testing.T) {
	cfg := config.IAMAAGUIDConfig{
		Mode:          "enforce",
		AllowlistFile: filepath.Join(t.TempDir(), "does-not-exist.json"),
	}
	allowlist, err := buildAAGUIDAllowlist(cfg)
	assert.Nil(t, allowlist)
	require.Error(t, err)
	var readErr *AAGUIDAllowlistReadError
	assert.ErrorAs(t, err, &readErr)
}

// TestBuildAAGUIDAllowlist_MalformedJSON verifies that a malformed allowlist file returns an error.
func TestBuildAAGUIDAllowlist_MalformedJSON(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "bad-*.json")
	require.NoError(t, err)
	_, _ = f.WriteString("{not valid json}")
	require.NoError(t, f.Close())

	cfg := config.IAMAAGUIDConfig{
		Mode:          "enforce",
		AllowlistFile: f.Name(),
	}
	allowlist, err := buildAAGUIDAllowlist(cfg)
	assert.Nil(t, allowlist)
	require.Error(t, err)
	var parseErr *AAGUIDAllowlistParseError
	assert.ErrorAs(t, err, &parseErr)
}

// TestBuildAAGUIDAllowlist_ModeOnlyNoFile verifies that mode without a file creates
// an allowlist with zero entries and the correct policy.
func TestBuildAAGUIDAllowlist_ModeOnlyNoFile(t *testing.T) {
	cfg := config.IAMAAGUIDConfig{
		Mode: "enforce",
	}
	allowlist, err := buildAAGUIDAllowlist(cfg)
	require.NoError(t, err)
	require.NotNil(t, allowlist)
	assert.Equal(t, iam.AAGUIDPolicyStrict, allowlist.Policy())
	assert.Equal(t, 0, allowlist.Len())
}
