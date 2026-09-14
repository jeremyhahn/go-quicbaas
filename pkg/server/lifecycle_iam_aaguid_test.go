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

// TestBuildAAGUIDAllowlist_NoConfig verifies that an UNCONFIGURED deployment
// gets a strict, empty allowlist rather than no allowlist at all.
//
// It used to assert the opposite — nil, which the caller then read as "skip
// enforcement". That made a deployment nobody configured accept every
// authenticator, including a virtual one, as an administrator credential. An
// empty strict allowlist is the honest representation of "this deployment has
// not said which authenticator models it trusts", and it refuses with a message
// naming the setting to fill in.
func TestBuildAAGUIDAllowlist_NoConfig(t *testing.T) {
	cfg := config.IAMAAGUIDConfig{}
	allowlist, err := buildAAGUIDAllowlist(cfg)
	require.NoError(t, err)
	require.NotNil(t, allowlist, "an unconfigured deployment must still get a gate")
	assert.Equal(t, iam.AAGUIDPolicyStrict, allowlist.Policy(),
		"the default policy must be strict, not permissive")
	assert.Zero(t, allowlist.Len())
	assert.Error(t, allowlist.Check([]byte{0xaa, 0xbb}),
		"an unconfigured allowlist must refuse, not admit")
}

// TestBuildAAGUIDAllowlist_EnforceIsTheDefault pins the mode mapping: only the
// explicit "log_only" opt-out yields a permissive gate. Any other value —
// including empty and a typo — is strict, so a misspelled mode cannot silently
// disable enforcement.
func TestBuildAAGUIDAllowlist_EnforceIsTheDefault(t *testing.T) {
	for _, mode := range []string{"", "enforce", "ENFORCE"} {
		allowlist, err := buildAAGUIDAllowlist(config.IAMAAGUIDConfig{Mode: mode})
		require.NoError(t, err)
		require.NotNil(t, allowlist)
		assert.Equal(t, iam.AAGUIDPolicyStrict, allowlist.Policy(),
			"mode %q must be strict", mode)
	}

	// A typo is REFUSED, not silently treated as strict. Failing closed on a
	// typo is safe but unkind: an operator who wrote "log-only" and got a node
	// refusing every authenticator cannot tell that from a broken allowlist.
	for _, mode := range []string{"log-only", "typo", "permissive"} {
		_, err := buildAAGUIDAllowlist(config.IAMAAGUIDConfig{Mode: mode})
		require.Error(t, err, "mode %q must be refused", mode)
		var modeErr *AAGUIDModeError
		require.ErrorAs(t, err, &modeErr)
		assert.Contains(t, err.Error(), "log_only", "the refusal must name the accepted values")
	}

	permissive, err := buildAAGUIDAllowlist(config.IAMAAGUIDConfig{Mode: "log_only"})
	require.NoError(t, err)
	require.NotNil(t, permissive)
	assert.Equal(t, iam.AAGUIDPolicyPermissive, permissive.Policy())
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
