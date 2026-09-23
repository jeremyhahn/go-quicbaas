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

// Package main: targeted coverage for ssh.go helper functions and the
// SSHReadPEMError typed error. The SSH command handlers themselves
// require live xkms transport and are exercised in integration tests.
package main

import (
	"bytes"
	stderrors "errors"
	"os"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	sshSDK "github.com/jeremyhahn/go-quicbaas/sdk/go/client/ssh"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/test/sshfixture"
)

// =============================================================================
// SSHReadPEMError
// =============================================================================

func TestSSHReadPEMError_MessageAndUnwrap(t *testing.T) {
	inner := stderrors.New("permission denied")
	e := &SSHReadPEMError{Path: "/etc/key.pem", Err: inner}
	assert.Contains(t, e.Error(), "/etc/key.pem")
	assert.Contains(t, e.Error(), "permission denied")
	assert.ErrorIs(t, e, inner)
}

// =============================================================================
// isValidSSHAlgorithm
// =============================================================================

func TestIsValidSSHAlgorithm(t *testing.T) {
	for _, a := range []string{"ed25519", "rsa", "ecdsa-p256", "ecdsa-p384", "ecdsa-p521"} {
		assert.True(t, isValidSSHAlgorithm(a), "expected %q to be valid", a)
	}
	for _, a := range []string{"", "dsa", "rsa-4096", "Ed25519", "ECDSA"} {
		assert.False(t, isValidSSHAlgorithm(a), "expected %q to be invalid", a)
	}
}

// =============================================================================
// readKeySelector / readKeySelectorAllowMissingName / defaultScopeAndOwner
// =============================================================================

// makeCmdWithSelectorFlags returns a *cobra.Command pre-wired with the
// scope/owner/name flags so the readKey* helpers can be exercised in
// isolation.
func makeCmdWithSelectorFlags() *cobra.Command {
	cmd := &cobra.Command{Use: "test"}
	addKeysSelectorFlags(cmd)
	return cmd
}

func TestReadKeySelector_RequiresName(t *testing.T) {
	cmd := makeCmdWithSelectorFlags()
	require.NoError(t, cmd.Flags().Set("scope", "user"))
	require.NoError(t, cmd.Flags().Set("owner", "alice"))
	// name omitted
	_, _, _, err := readKeySelector(cmd)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrSSHMissingKeyName)
}

func TestReadKeySelector_AppliesDefaults(t *testing.T) {
	cmd := makeCmdWithSelectorFlags()
	require.NoError(t, cmd.Flags().Set("name", "k1"))
	// scope and owner omitted → defaults
	scope, owner, name, err := readKeySelector(cmd)
	require.NoError(t, err)
	assert.Equal(t, "user", scope)
	assert.NotEmpty(t, owner)
	assert.Equal(t, "k1", name)
}

func TestReadKeySelectorAllowMissingName_AllowsEmptyName(t *testing.T) {
	cmd := makeCmdWithSelectorFlags()
	// name omitted is OK
	scope, owner, name, err := readKeySelectorAllowMissingName(cmd)
	require.NoError(t, err)
	assert.Equal(t, "user", scope)
	assert.NotEmpty(t, owner)
	assert.Equal(t, "", name)
}

func TestReadKeySelectorAllowMissingName_PassesThroughExplicit(t *testing.T) {
	cmd := makeCmdWithSelectorFlags()
	require.NoError(t, cmd.Flags().Set("scope", "tenant"))
	require.NoError(t, cmd.Flags().Set("owner", "bob"))
	require.NoError(t, cmd.Flags().Set("name", "k2"))
	scope, owner, name, err := readKeySelectorAllowMissingName(cmd)
	require.NoError(t, err)
	assert.Equal(t, "tenant", scope)
	assert.Equal(t, "bob", owner)
	assert.Equal(t, "k2", name)
}

func TestDefaultScopeAndOwner_AppliesDefaults(t *testing.T) {
	cmd := &cobra.Command{Use: "t"}
	addKnownHostsSelectorFlags(cmd)
	scope, owner := defaultScopeAndOwner(cmd)
	assert.Equal(t, "user", scope)
	assert.NotEmpty(t, owner)
}

func TestDefaultScopeAndOwner_OverridesFromFlags(t *testing.T) {
	cmd := &cobra.Command{Use: "t"}
	addKnownHostsSelectorFlags(cmd)
	require.NoError(t, cmd.Flags().Set("scope", "service"))
	require.NoError(t, cmd.Flags().Set("owner", "svc-account"))
	scope, owner := defaultScopeAndOwner(cmd)
	assert.Equal(t, "service", scope)
	assert.Equal(t, "svc-account", owner)
}

// =============================================================================
// printSSHKey
// =============================================================================

func TestPrintSSHKey_NilKey(t *testing.T) {
	cmd := &cobra.Command{Use: "t"}
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	printSSHKey(cmd, nil)
	assert.Empty(t, buf.String(), "nil key must print nothing")
}

func TestPrintSSHKey_FullKey(t *testing.T) {
	cmd := &cobra.Command{Use: "t"}
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	user := sshfixture.User(t)
	k := &sshSDK.Key{
		Scope:       "user",
		Owner:       "alice",
		Name:        "k1",
		Algorithm:   "ed25519",
		Fingerprint: user.Fingerprint,
		PublicKey:   user.AuthorizedLine,
		Comment:     "hello",
	}
	printSSHKey(cmd, k)
	got := buf.String()
	assert.Contains(t, got, "user/alice/k1")
	assert.Contains(t, got, "ed25519")
	assert.Contains(t, got, user.Fingerprint,
		"the printed fingerprint must be the key's own, not a truncation of it")
	assert.Contains(t, got, "hello")
	assert.Contains(t, got, "ssh-ed25519")
}

func TestPrintSSHKey_NoCommentNoPublic(t *testing.T) {
	cmd := &cobra.Command{Use: "t"}
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	k := &sshSDK.Key{
		Scope: "user", Owner: "alice", Name: "k1",
		Algorithm: "rsa", Fingerprint: sshfixture.HostAltFingerprint,
	}
	printSSHKey(cmd, k)
	got := buf.String()
	assert.Contains(t, got, "rsa")
	assert.Contains(t, got, sshfixture.HostAltFingerprint)
	assert.NotContains(t, got, "Comment:")
	assert.NotContains(t, got, "Public:")
}

// =============================================================================
// isTerminal — asks the terminal driver, not the file mode.
// =============================================================================

// TestIsTerminal_DevNullIsNotATerminal is the regression: /dev/null is a
// character device, which the old mode check took for a terminal.
func TestIsTerminal_DevNullIsNotATerminal(t *testing.T) {
	devNull, err := os.Open(os.DevNull)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, devNull.Close()) })

	assert.False(t, isTerminal(devNull))
}

// TestIsTerminal_PipeAndClosedFileAreNotTerminals covers the ordinary
// non-interactive attachment and a descriptor that is no longer valid.
func TestIsTerminal_PipeAndClosedFileAreNotTerminals(t *testing.T) {
	r, w, err := os.Pipe()
	require.NoError(t, err)
	assert.False(t, isTerminal(r))
	require.NoError(t, w.Close())
	require.NoError(t, r.Close())
	assert.False(t, isTerminal(r))
}

// =============================================================================
// sshPassphraseProvider also depends on stdin state. We exercise the path
// to cover the branch without asserting which concrete provider is
// returned.
// =============================================================================

func TestSSHPassphraseProvider_NoCrash(t *testing.T) {
	p := sshPassphraseProvider()
	assert.NotNil(t, p)
}
